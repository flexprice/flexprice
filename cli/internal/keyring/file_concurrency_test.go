package keyring

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// Two processes truncating the same destination can leave a leftover tail from
// the longer write, producing a file that decrypts as neither writer's output.
func TestFileStore_ConcurrentSetDoesNotCorrupt(t *testing.T) {
	dir := t.TempDir()
	s := &FileStore{Dir: dir}

	const writers = 20
	var wg sync.WaitGroup
	wg.Add(writers)
	for i := 0; i < writers; i++ {
		go func(i int) {
			defer wg.Done()
			// Vary payload length so a truncate/write race would surface as
			// leftover bytes from a longer, earlier write.
			val := fmt.Sprintf("sk_%d_%s", i, string(make([]byte, i)))
			if err := s.Set("shared-profile", val); err != nil {
				t.Errorf("Set(%d): %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	got, err := s.Get("shared-profile")
	if err != nil {
		t.Fatalf("Get after concurrent Set: %v (file corrupted by a race)", err)
	}
	if got == "" {
		t.Error("Get after concurrent Set returned empty value")
	}

	// No leftover temp files should remain: writeAtomic must clean up after
	// every writer, win or lose the rename race.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".key" {
			t.Errorf("leftover temp file after concurrent Set: %s", e.Name())
		}
	}

	// Sanity: stored bytes must still be validly-encoded ciphertext, not a
	// truncated splice of two writers' output.
	raw, err := os.ReadFile(filepath.Join(dir, "shared-profile.key"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if _, err := base64.StdEncoding.DecodeString(string(raw)); err != nil {
		t.Errorf("stored file is not valid base64 after concurrent Set: %v", err)
	}
}
