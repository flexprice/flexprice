package kafka

import (
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"fmt"
	"hash"

	"time"

	"github.com/Shopify/sarama"
	"github.com/flexprice/flexprice/internal/config"
	mainkafka "github.com/flexprice/flexprice/internal/kafka"
	"github.com/xdg-go/scram"
)

// GetSaramaConfig builds a Sarama config for the pubsub Kafka clients.
//
// It returns an error rather than panicking on unusable TLS/SASL material (bad
// CA path, non-PEM bundle, missing GCP credentials) so a misconfigured deploy
// surfaces as a normal startup failure instead of a stack trace.
func GetSaramaConfig(cfg *config.Configuration) (*sarama.Config, error) {
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
	// A non-zero retention makes Sarama use OffsetCommit v2; zero defers retention
	// to the broker and retains Sarama's legacy OffsetCommit behavior.
	saramaConfig.Consumer.Offsets.Retention = cfg.Kafka.OffsetRetention

	// When rebalancing happens, use the last committed offset
	saramaConfig.Consumer.Offsets.Retry.Max = 3

	// SASL implies TLS: SASL_SSL is the only SASL protocol this app speaks, so
	// enabling SASL turns TLS on even when cfg.Kafka.TLS is false. TLS material
	// is built by the shared helper in internal/kafka so both config builders
	// honour kafka.tls_ca_cert_file identically.
	if cfg.Kafka.TLS || cfg.Kafka.UseSASL {
		tlsConfig, err := mainkafka.BuildTLSConfig(&cfg.Kafka)
		if err != nil {
			return nil, err
		}
		saramaConfig.Net.TLS.Enable = true
		saramaConfig.Net.TLS.Config = tlsConfig
	}

	if !cfg.Kafka.UseSASL {
		return saramaConfig, nil
	}

	// SASL specific configs
	saramaConfig.Net.SASL.Enable = true

	// sasl configs
	saramaConfig.Net.SASL.Mechanism = cfg.Kafka.SASLMechanism

	switch cfg.Kafka.SASLMechanism {
	case sarama.SASLTypeOAuth:
		// OAUTHBEARER (e.g. GCP Managed Kafka). Reuse the shared token provider
		// from internal/kafka so this pubsub path emits the same GMK-format
		// token. Without this, sarama panics at connect time with "An
		// AccessTokenProvider instance must be provided to Net.SASL.TokenProvider".
		// User/Password are not used.
		provider, err := mainkafka.NewGCPTokenProvider(context.Background(), cfg.Kafka.SASLOAuthScopes)
		if err != nil {
			return nil, fmt.Errorf("kafka oauthbearer: init token provider (scopes=%v) — check GCP Application Default Credentials: %w", cfg.Kafka.SASLOAuthScopes, err)
		}
		saramaConfig.Net.SASL.TokenProvider = provider

	case sarama.SASLTypeSCRAMSHA256, sarama.SASLTypeSCRAMSHA512:
		saramaConfig.Net.SASL.User = cfg.Kafka.SASLUser
		saramaConfig.Net.SASL.Password = cfg.Kafka.SASLPassword
		// Configure SCRAM client generator for SCRAM mechanisms
		saramaConfig.Net.SASL.SCRAMClientGeneratorFunc = func() sarama.SCRAMClient {
			return &XDGSCRAMClient{HashGeneratorFcn: getHashGenerator(cfg.Kafka.SASLMechanism)}
		}

	default:
		// PLAIN and any other mechanism that uses user+password.
		saramaConfig.Net.SASL.User = cfg.Kafka.SASLUser
		saramaConfig.Net.SASL.Password = cfg.Kafka.SASLPassword
	}

	return saramaConfig, nil
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
