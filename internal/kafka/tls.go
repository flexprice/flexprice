package kafka

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"github.com/flexprice/flexprice/internal/config"
)

// BuildTLSConfig returns the *tls.Config for a Kafka client.
//
// With no CA file configured it returns a verifying config backed by the OS
// trust store, which is what publicly-trusted brokers (MSK, Confluent Cloud)
// need. When TLSCACertFile is set, the PEM bundle at that path is appended to a
// copy of the system pool and installed as RootCAs. Scoping the pool to this
// config — rather than setting SSL_CERT_FILE process-wide — keeps every other
// outbound TLS caller (Stripe, GCP, webhooks) on the system roots.
//
// The file must be PEM. JKS/PKCS12 truststores are not readable by Go; convert
// with `keytool -exportcert -alias <alias> -keystore truststore.jks -rfc -file ca.pem`.
func BuildTLSConfig(kafkaCfg *config.KafkaConfig) (*tls.Config, error) {
	tlsConfig := &tls.Config{
		InsecureSkipVerify: false,
	}

	if kafkaCfg.TLSCACertFile == "" {
		return tlsConfig, nil
	}

	pemBytes, err := os.ReadFile(kafkaCfg.TLSCACertFile)
	if err != nil {
		return nil, fmt.Errorf("kafka tls: read CA file %q: %w", kafkaCfg.TLSCACertFile, err)
	}

	// Start from the system pool so a private CA augments the public roots
	// rather than replacing them. A broker chaining to a public root keeps
	// working even when a CA file is supplied.
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}

	if !pool.AppendCertsFromPEM(pemBytes) {
		return nil, fmt.Errorf(
			"kafka tls: no certificates parsed from %q — file must be PEM (a JKS/PKCS12 truststore will not work; export it with `keytool -exportcert -rfc`)",
			kafkaCfg.TLSCACertFile,
		)
	}

	tlsConfig.RootCAs = pool
	return tlsConfig, nil
}
