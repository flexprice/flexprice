package bootstrap

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestK8sStore_WriteUsesStringData(t *testing.T) {
	cs := fake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "probe-bootstrap", Namespace: "flexprice"},
		Data:       map[string][]byte{"api-key": []byte("")},
	})
	store := &k8sStore{client: cs}

	creds := &Credentials{apiKey: "sk_minted", secretID: "secret_1"}
	if err := store.Write(context.Background(), "flexprice", "probe-bootstrap", "api-key", creds); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// The fake clientset does NOT fold stringData into data the way the real
	// apiserver does, so assert on the patch payload the action recorded.
	var patched bool
	for _, a := range cs.Actions() {
		if a.GetVerb() != "patch" {
			continue
		}
		patched = true
		raw := string(a.(interface{ GetPatch() []byte }).GetPatch())
		if !strings.Contains(raw, "stringData") {
			t.Errorf("patch must use stringData so the apiserver base64-encodes it; got: %s", raw)
		}
		if !strings.Contains(raw, "sk_minted") {
			t.Errorf("patch missing the api key; got: %s", raw)
		}
		if !strings.Contains(raw, "secret_1") {
			t.Errorf("patch missing the secret id; got: %s", raw)
		}
	}
	if !patched {
		t.Fatal("expected a patch action")
	}
}

func TestK8sStore_ReadReturnsExistingKey(t *testing.T) {
	cs := fake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "probe-bootstrap", Namespace: "flexprice"},
		Data: map[string][]byte{
			"api-key":   []byte("sk_existing"),
			SecretIDKey: []byte("secret_old"),
		},
	})
	store := &k8sStore{client: cs}

	apiKey, secretID, err := store.Read(context.Background(), "flexprice", "probe-bootstrap", "api-key")
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if apiKey != "sk_existing" {
		t.Errorf("api key = %q, want sk_existing", apiKey)
	}
	if secretID != "secret_old" {
		t.Errorf("secret id = %q, want secret_old", secretID)
	}
}

func TestK8sStore_ReadEmptyKeyIsNotAnError(t *testing.T) {
	cs := fake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "probe-bootstrap", Namespace: "flexprice"},
		Data:       map[string][]byte{"api-key": []byte("")},
	})
	store := &k8sStore{client: cs}

	apiKey, _, err := store.Read(context.Background(), "flexprice", "probe-bootstrap", "api-key")
	if err != nil {
		t.Fatalf("an empty key is the boot-1 state, not an error: %v", err)
	}
	if apiKey != "" {
		t.Errorf("api key = %q, want empty", apiKey)
	}
}

func TestK8sStore_ReadMissingSecretErrors(t *testing.T) {
	store := &k8sStore{client: fake.NewSimpleClientset()}
	if _, _, err := store.Read(context.Background(), "flexprice", "absent", "api-key"); err == nil {
		t.Fatal("expected an error when the Secret does not exist")
	}
}
