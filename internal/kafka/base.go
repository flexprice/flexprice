package kafka

import (
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"hash"

	"crypto/tls"
	"time"

	"github.com/Shopify/sarama"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/xdg-go/scram"
)

func GetSaramaConfig(cfg *config.Configuration) *sarama.Config {
	saramaConfig := sarama.NewConfig()
	saramaConfig.Version = sarama.V2_1_0_0

	// Configure client ID regardless of SASL
	saramaConfig.ClientID = cfg.Kafka.ClientID

	// Set consumer offset reset policy to ensure we don't miss messages
	// "earliest" ensures that when a consumer starts with no initial offset or
	// current offset is out of range, it will start from the earliest message
	saramaConfig.Consumer.Offsets.Initial = sarama.OffsetOldest

	// Enable auto commit to ensure offsets are committed regularly
	saramaConfig.Consumer.Offsets.AutoCommit.Enable = true
	saramaConfig.Consumer.Offsets.AutoCommit.Interval = 5000 * time.Millisecond // 5 seconds

	// When rebalancing happens, use the last committed offset
	saramaConfig.Consumer.Offsets.Retry.Max = 3

	if cfg.Kafka.TLS {
		saramaConfig.Net.TLS.Enable = true
		saramaConfig.Net.TLS.Config = &tls.Config{
			InsecureSkipVerify: false,
		}
	}

	if !cfg.Kafka.UseSASL {
		return saramaConfig
	}

	// SASL specific configs
	saramaConfig.Net.SASL.Enable = true
	saramaConfig.Net.TLS.Enable = true

	// sasl configs
	saramaConfig.Net.SASL.Mechanism = cfg.Kafka.SASLMechanism

	switch cfg.Kafka.SASLMechanism {
	case sarama.SASLTypeOAuth:
		// OAUTHBEARER (e.g. GCP Managed Kafka). Token comes from Application
		// Default Credentials — Workload Identity on GKE, gcloud locally.
		// User/Password are not used.
		provider, err := newGCPTokenProvider(context.Background(), cfg.Kafka.SASLOAuthScopes)
		if err != nil {
			panic(err)
		}
		saramaConfig.Net.SASL.TokenProvider = provider
	case sarama.SASLTypeSCRAMSHA256, sarama.SASLTypeSCRAMSHA512:
		saramaConfig.Net.SASL.User = cfg.Kafka.SASLUser
		saramaConfig.Net.SASL.Password = cfg.Kafka.SASLPassword
		saramaConfig.Net.SASL.SCRAMClientGeneratorFunc = func() sarama.SCRAMClient {
			return &XDGSCRAMClient{HashGeneratorFcn: getHashGenerator(cfg.Kafka.SASLMechanism)}
		}
	default:
		// PLAIN and any other mechanism that uses user+password.
		saramaConfig.Net.SASL.User = cfg.Kafka.SASLUser
		saramaConfig.Net.SASL.Password = cfg.Kafka.SASLPassword
	}

	return saramaConfig
}

// XDGSCRAMClient implements sarama.SCRAMClient for SCRAM authentication
type XDGSCRAMClient struct {
	*scram.ClientConversation
	scram.HashGeneratorFcn
}

func (x *XDGSCRAMClient) Begin(userName, password, authzID string) (err error) {
	client, err := x.HashGeneratorFcn.NewClient(userName, password, authzID)
	if err != nil {
		return err
	}
	x.ClientConversation = client.NewConversation()
	return nil
}

func (x *XDGSCRAMClient) Step(challenge string) (response string, err error) {
	response, err = x.ClientConversation.Step(challenge)
	return
}

func (x *XDGSCRAMClient) Done() bool {
	return x.ClientConversation.Done()
}

// getHashGenerator returns the appropriate hash generator for the SASL mechanism
func getHashGenerator(mechanism sarama.SASLMechanism) scram.HashGeneratorFcn {
	switch mechanism {
	case sarama.SASLTypeSCRAMSHA512:
		return func() hash.Hash { return sha512.New() }
	case sarama.SASLTypeSCRAMSHA256:
		return func() hash.Hash { return sha256.New() }
	default:
		return func() hash.Hash { return sha512.New() }
	}
}
