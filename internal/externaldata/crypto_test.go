package externaldata

import "testing"

func TestCipherAuthenticatedRoundTrip(t *testing.T) {
	c, err := NewCipher("deployment-stable-secret")
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := c.Seal("connector-key")
	if err != nil || sealed == "connector-key" {
		t.Fatalf("seal: %v", err)
	}
	value, err := c.Open(sealed)
	if err != nil || value != "connector-key" {
		t.Fatalf("open: %v", err)
	}
	second, _ := c.Seal(value)
	if second == sealed {
		t.Fatal("nonce reused")
	}
	other, _ := NewCipher("other")
	if _, err = other.Open(sealed); err == nil {
		t.Fatal("wrong key accepted")
	}
	if _, err = c.Open(sealed[:len(sealed)-3] + "abc"); err == nil {
		t.Fatal("tampered ciphertext accepted")
	}
	if _, err = c.Open("plaintext"); err == nil {
		t.Fatal("plaintext accepted")
	}
	if _, err = NewCipher(""); err == nil {
		t.Fatal("empty secret accepted")
	}
}
