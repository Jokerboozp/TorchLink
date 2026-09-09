package protocolmarket

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"iot-platform/internal/protocolcatalog"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestPrivateMarketSigningAndReaderPolicy(t *testing.T) {
	dir := t.TempDir()
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	keyPath := filepath.Join(dir, "key")
	os.WriteFile(keyPath, []byte(base64.StdEncoding.EncodeToString(key)), 0600)
	secret := "private-reader-test-credential-000001"
	sum := sha256.Sum256([]byte(secret))
	org := Organization{Publisher: "组织", KeyID: "trusted", PrivateKeyFile: keyPath, ReaderTokenHashes: []string{hex.EncodeToString(sum[:])}}
	p := Policy{PublicOrigin: "https://market.example.test", Organizations: map[string]Organization{"tenant": org}}
	path := filepath.Join(dir, "policy.json")
	data, _ := json.Marshal(p)
	os.WriteFile(path, data, 0600)
	loaded, err := ReadPolicy(path)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Organizations["tenant"].Authenticate(secret) || org.Authenticate(secret+"wrong") || org.Authenticate("") {
		t.Fatal("reader authentication")
	}
	payload := protocolcatalog.Payload{IssuedAt: time.Now().UnixMilli(), ExpiresAt: time.Now().Add(time.Hour).UnixMilli(), Entries: []protocolcatalog.Entry{}}
	envelope, err := org.Sign(payload)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := base64.StdEncoding.DecodeString(envelope.Payload)
	signature, _ := base64.StdEncoding.DecodeString(envelope.Signature)
	if !ed25519.Verify(pub, encoded, signature) {
		t.Fatal("invalid actual signature")
	}
	if runtime.GOOS != "windows" {
		os.Chmod(keyPath, 0644)
		if _, err := org.Sign(payload); err == nil {
			t.Fatal("publicly readable signing key accepted")
		}
		os.Chmod(keyPath, 0600)
	}
	key[len(key)-1] ^= 1
	os.WriteFile(keyPath, []byte(base64.StdEncoding.EncodeToString(key)), 0600)
	if _, err := org.Sign(payload); err == nil {
		t.Fatal("inconsistent private key accepted")
	}
	p.PublicOrigin = "http://market.example.test"
	data, _ = json.Marshal(p)
	os.WriteFile(path, data, 0600)
	if _, err := ReadPolicy(path); err == nil {
		t.Fatal("plaintext distribution accepted")
	}
}
