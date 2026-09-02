package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// SecretIDKey is the Secret field holding the minted key's secret id, so a
// later boot can revoke the key it is replacing.
const SecretIDKey = "secret-id"

// SecretStore persists the minted credential between restarts.
type SecretStore interface {
	Read(ctx context.Context, namespace, name, key string) (apiKey, secretID string, err error)
	Write(ctx context.Context, namespace, name, key string, creds *Credentials) error
}

type k8sStore struct {
	client kubernetes.Interface
}

// NewK8sStore builds a store from the pod's in-cluster config. The bool
// reports whether we are in a cluster at all: outside one (make run-e2eprobe,
// plain Docker) there is no Secret to write, which is not an error.
func NewK8sStore() (SecretStore, bool, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, false, nil
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, true, fmt.Errorf("build kubernetes client: %w", err)
	}
	return &k8sStore{client: cs}, true, nil
}

func (s *k8sStore) Read(ctx context.Context, namespace, name, key string) (string, string, error) {
	secret, err := s.client.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", "", fmt.Errorf("read secret %s/%s: %w", namespace, name, err)
	}
	return string(secret.Data[key]), string(secret.Data[SecretIDKey]), nil
}

// Write patches only the fields it owns. A strategic-merge patch leaves any
// other key an operator added intact and avoids a read-modify-write conflict
// loop; stringData lets the apiserver do the base64 encoding.
func (s *k8sStore) Write(ctx context.Context, namespace, name, key string, creds *Credentials) error {
	patch, err := json.Marshal(map[string]any{
		"stringData": map[string]string{
			key:         creds.APIKey(),
			SecretIDKey: creds.SecretID(),
		},
	})
	if err != nil {
		return fmt.Errorf("encode patch: %w", err)
	}

	if _, err := s.client.CoreV1().Secrets(namespace).Patch(
		ctx, name, types.StrategicMergePatchType, patch, metav1.PatchOptions{},
	); err != nil {
		return fmt.Errorf("patch secret %s/%s: %w", namespace, name, err)
	}
	return nil
}
