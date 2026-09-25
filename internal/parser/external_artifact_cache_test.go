package parser

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestArtifactVerificationCacheDetectsReplacedBinary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worker")
	original := []byte("worker-binary-v1")
	if err := os.WriteFile(path, original, 0o755); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(original)
	artifact := map[string]any{"sha256": hex.EncodeToString(sum[:])}
	for i := 0; i < 2; i++ {
		if err := verifyExternalArtifact(path, artifact); err != nil {
			t.Fatalf("verification %d: %v", i, err)
		}
	}
	// Same size, different content and modification time: the cached result
	// must not be trusted.
	if err := os.WriteFile(path, []byte("worker-binary-v2"), 0o755); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	if err := verifyExternalArtifact(path, artifact); err == nil {
		t.Fatal("replaced artifact passed checksum verification")
	}
	// A different expected hash for the same path is always re-checked.
	if err := verifyExternalArtifact(path, map[string]any{"sha256": "00"}); err == nil {
		t.Fatal("mismatching expected hash was accepted")
	}
}
