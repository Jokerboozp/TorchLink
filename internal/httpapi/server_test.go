package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestVideoSignature(t *testing.T) {
	body := []byte(`{"eventId":"1"}`)
	mac := hmac.New(sha256.New, []byte("secret"))
	_, _ = mac.Write([]byte("123"))
	_, _ = mac.Write(body)
	signature := hex.EncodeToString(mac.Sum(nil))
	if !verifySignature("secret", "123", body, signature) {
		t.Fatal("valid signature rejected")
	}
	if verifySignature("secret", "123", body, "bad") {
		t.Fatal("bad signature accepted")
	}
}

func TestAIProviderURLFormat(t *testing.T) {
	tests := []struct {
		name, target string
		want         bool
	}{
		{name: "official API", target: "https://api.deepseek.com/v1", want: true},
		{name: "default HTTPS port", target: "https://api.deepseek.com:443", want: true},
		{name: "exact Ollama port", target: "http://localhost:11434/api", want: true},
		{name: "custom local port", target: "http://localhost:8080", want: true},
		{name: "remote LAN model", target: "http://192.168.10.20:9000/v1", want: true},
		{name: "custom cloud model", target: "https://models.example.com/v1", want: true},
		{name: "IPv6 model", target: "http://[::1]:11434", want: true},
		{name: "userinfo rejected", target: "https://token@api.deepseek.com", want: false},
		{name: "query rejected", target: "https://models.example.com?key=secret", want: false},
		{name: "fragment rejected", target: "https://models.example.com/#v1", want: false},
		{name: "markdown rejected", target: "[http://ollama:11434](http://ollama:11434)", want: false},
		{name: "unsupported scheme", target: "file:///tmp/provider", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validateAIProviderURL(tt.target) == nil; got != tt.want {
				t.Fatalf("valid provider URL(%q)=%v want=%v", tt.target, got, tt.want)
			}
		})
	}
}
