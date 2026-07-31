package kafka

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Shopify/sarama"
	"github.com/flexprice/flexprice/internal/config"
)

// writeTestCA generates a throwaway self-signed CA and writes it as PEM,
// returning the path and the certificate's subject. Using a real generated cert
// (rather than a fixture) keeps the test independent of any expiry date.
func writeTestCA(t *testing.T, dir, name string) (string, pkix.Name) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	subject := pkix.Name{CommonName: "flexprice-test-kafka-ca-" + name}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               subject,
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}

	path := filepath.Join(dir, name+".pem")
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(path, pemBytes, 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}

	return path, subject
}

func TestBuildTLSConfig_NoCAFileUsesSystemRoots(t *testing.T) {
	tlsCfg, err := BuildTLSConfig(&config.KafkaConfig{})
	if err != nil {
		t.Fatalf("BuildTLSConfig() error = %v, want nil", err)
	}
	if tlsCfg.RootCAs != nil {
		t.Error("RootCAs should be nil so Go falls back to the OS trust store")
	}
	if tlsCfg.InsecureSkipVerify {
		t.Error("InsecureSkipVerify must stay false — verification is not optional")
	}
}

func TestBuildTLSConfig_LoadsCAIntoRootPool(t *testing.T) {
	dir := t.TempDir()
	path, subject := writeTestCA(t, dir, "ca")

	tlsCfg, err := BuildTLSConfig(&config.KafkaConfig{TLSCACertFile: path})
	if err != nil {
		t.Fatalf("BuildTLSConfig() error = %v, want nil", err)
	}
	if tlsCfg.RootCAs == nil {
		t.Fatal("RootCAs is nil — the configured CA was not loaded")
	}
	if tlsCfg.InsecureSkipVerify {
		t.Error("InsecureSkipVerify must stay false even with a custom CA")
	}

	// The custom CA must actually be in the pool, not merely a non-nil pool.
	var found bool
	for _, rawSubject := range tlsCfg.RootCAs.Subjects() { //nolint:staticcheck // Subjects() is the only way to inspect pool contents
		if strings.Contains(string(rawSubject), subject.CommonName) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("configured CA %q not present in RootCAs", subject.CommonName)
	}
}

// A broker chaining to a public root must keep working when a private CA is
// also configured — the pool has to be system roots PLUS ours, never ours
// alone. Asserted by cloning the system pool the same way BuildTLSConfig does
// and requiring the result to be a strict superset.
//
// Subjects() is empty on platforms where Go defers to the system verifier
// (macOS), so the comparison is made against a freshly cloned system pool
// rather than against a raw count, and is skipped where Go exposes no roots.
func TestBuildTLSConfig_CAAugmentsRatherThanReplacesSystemRoots(t *testing.T) {
	systemPool, err := x509.SystemCertPool()
	if err != nil || systemPool == nil {
		t.Skip("no system cert pool on this platform")
	}
	systemSubjects := systemPool.Subjects() //nolint:staticcheck // see above
	if len(systemSubjects) == 0 {
		t.Skip("Go exposes no system roots on this platform (macOS defers to the system verifier)")
	}

	dir := t.TempDir()
	path, _ := writeTestCA(t, dir, "ca")

	tlsCfg, err := BuildTLSConfig(&config.KafkaConfig{TLSCACertFile: path})
	if err != nil {
		t.Fatalf("BuildTLSConfig() error = %v, want nil", err)
	}

	got := make(map[string]bool)
	for _, s := range tlsCfg.RootCAs.Subjects() { //nolint:staticcheck // see above
		got[string(s)] = true
	}

	for _, want := range systemSubjects {
		if !got[string(want)] {
			t.Fatalf("a system root was dropped from RootCAs — configuring a private CA must not "+
				"replace the OS trust store (%d of %d system roots retained)", len(got)-1, len(systemSubjects))
		}
	}
	if len(got) != len(systemSubjects)+1 {
		t.Errorf("RootCAs has %d subjects, want %d (all system roots + 1 custom CA)",
			len(got), len(systemSubjects)+1)
	}
}

func TestBuildTLSConfig_MissingFileErrors(t *testing.T) {
	_, err := BuildTLSConfig(&config.KafkaConfig{
		TLSCACertFile: filepath.Join(t.TempDir(), "does-not-exist.pem"),
	})
	if err == nil {
		t.Fatal("BuildTLSConfig() error = nil, want an error for a missing CA file")
	}
	if !strings.Contains(err.Error(), "read CA file") {
		t.Errorf("error %q should name the failure as reading the CA file", err)
	}
}

// A JKS truststore is the exact wrong-format case operators hit, since that is
// what Java-based Kafka tooling produces. The error must say so rather than
// failing later at connect time with an opaque TLS handshake error.
func TestBuildTLSConfig_NonPEMFileErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "truststore.jks")
	// First bytes of a real JKS file (magic number 0xFEEDFEED).
	if err := os.WriteFile(path, []byte{0xFE, 0xED, 0xFE, 0xED, 0x00, 0x00, 0x00, 0x02}, 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	_, err := BuildTLSConfig(&config.KafkaConfig{TLSCACertFile: path})
	if err == nil {
		t.Fatal("BuildTLSConfig() error = nil, want an error for a non-PEM file")
	}
	if !strings.Contains(err.Error(), "JKS") {
		t.Errorf("error %q should point the operator at the JKS→PEM conversion", err)
	}
}

// SASL_SSL is the protocol the SCRAM-SHA-512 setup uses: enabling SASL must
// enable TLS and carry the CA through, even when kafka.tls is left false.
func TestGetSaramaConfig_SASLEnablesTLSWithCA(t *testing.T) {
	dir := t.TempDir()
	path, _ := writeTestCA(t, dir, "ca")

	cfg := &config.KafkaConfig{
		TLS:           false, // deliberately false — UseSASL must still force TLS
		UseSASL:       true,
		SASLMechanism: sarama.SASLTypeSCRAMSHA512,
		SASLUser:      "flexprice",
		SASLPassword:  "secret",
		TLSCACertFile: path,
	}

	saramaCfg, err := GetSaramaConfig(cfg)
	if err != nil {
		t.Fatalf("GetSaramaConfig() error = %v, want nil", err)
	}
	if !saramaCfg.Net.TLS.Enable {
		t.Error("Net.TLS.Enable = false, want true — SASL implies SASL_SSL")
	}
	if saramaCfg.Net.TLS.Config == nil || saramaCfg.Net.TLS.Config.RootCAs == nil {
		t.Fatal("custom CA was not applied to the SASL TLS config")
	}
	if !saramaCfg.Net.SASL.Enable {
		t.Error("Net.SASL.Enable = false, want true")
	}
	if saramaCfg.Net.SASL.Mechanism != sarama.SASLTypeSCRAMSHA512 {
		t.Errorf("Mechanism = %q, want %q", saramaCfg.Net.SASL.Mechanism, sarama.SASLTypeSCRAMSHA512)
	}
	if saramaCfg.Net.SASL.SCRAMClientGeneratorFunc == nil {
		t.Fatal("SCRAMClientGeneratorFunc is nil — SCRAM auth would fail at handshake")
	}

	// The generator must produce a SHA-512 client; a SHA-256 client against a
	// SHA-512 broker fails with an opaque authentication error.
	client, ok := saramaCfg.Net.SASL.SCRAMClientGeneratorFunc().(*XDGSCRAMClient)
	if !ok {
		t.Fatal("SCRAMClientGeneratorFunc did not return an *XDGSCRAMClient")
	}
	if got := client.HashGeneratorFcn().Size(); got != 64 {
		t.Errorf("SCRAM hash size = %d bytes, want 64 (SHA-512)", got)
	}
}

func TestGetSaramaConfig_BadCAFilePropagatesError(t *testing.T) {
	_, err := GetSaramaConfig(&config.KafkaConfig{
		UseSASL:       true,
		SASLMechanism: sarama.SASLTypeSCRAMSHA512,
		TLSCACertFile: filepath.Join(t.TempDir(), "missing.pem"),
	})
	if err == nil {
		t.Fatal("GetSaramaConfig() error = nil, want the CA failure surfaced as a startup error")
	}
}

// Without SASL or TLS the CA is irrelevant and must not be read — a plaintext
// local broker should not fail because a stale path is configured.
func TestGetSaramaConfig_PlaintextIgnoresCA(t *testing.T) {
	saramaCfg, err := GetSaramaConfig(&config.KafkaConfig{
		TLS:           false,
		UseSASL:       false,
		TLSCACertFile: filepath.Join(t.TempDir(), "missing.pem"),
	})
	if err != nil {
		t.Fatalf("GetSaramaConfig() error = %v, want nil for a plaintext broker", err)
	}
	if saramaCfg.Net.TLS.Enable {
		t.Error("Net.TLS.Enable = true, want false for a plaintext broker")
	}
}

// TLS without SASL is a valid combination (a broker on 9094 with no auth), and
// must honour the CA too.
func TestGetSaramaConfig_TLSWithoutSASLUsesCA(t *testing.T) {
	dir := t.TempDir()
	path, _ := writeTestCA(t, dir, "ca")

	saramaCfg, err := GetSaramaConfig(&config.KafkaConfig{
		TLS:           true,
		UseSASL:       false,
		TLSCACertFile: path,
	})
	if err != nil {
		t.Fatalf("GetSaramaConfig() error = %v, want nil", err)
	}
	if !saramaCfg.Net.TLS.Enable {
		t.Fatal("Net.TLS.Enable = false, want true")
	}
	if saramaCfg.Net.TLS.Config == nil || saramaCfg.Net.TLS.Config.RootCAs == nil {
		t.Error("custom CA was not applied when TLS is enabled without SASL")
	}
}
