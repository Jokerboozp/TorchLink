package externaldata

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
)

// Cipher encrypts connector credentials in a purpose-separated key space.
// The deployment secret must remain stable across service restarts.
type Cipher struct{ aead cipher.AEAD }

func NewCipher(secret string) (*Cipher, error) {
	if strings.TrimSpace(secret) == "" {
		return nil, errors.New("外部接口凭据加密密钥未配置")
	}
	key := sha256.Sum256([]byte("torchlink/external-data/credentials/v1\x00" + secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead}, nil
}

func (c *Cipher) Seal(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if c == nil || c.aead == nil {
		return "", errors.New("外部接口凭据加密不可用")
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", errors.New("外部接口凭据加密失败")
	}
	sealed := c.aead.Seal(nonce, nonce, []byte(value), []byte("external-data:v1"))
	return "v1:" + base64.RawStdEncoding.EncodeToString(sealed), nil
}

func (c *Cipher) Open(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	failed := errors.New("外部接口凭据解密失败")
	if c == nil || c.aead == nil || !strings.HasPrefix(value, "v1:") {
		return "", failed
	}
	sealed, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(value, "v1:"))
	if err != nil || len(sealed) < c.aead.NonceSize() {
		return "", failed
	}
	plain, err := c.aead.Open(nil, sealed[:c.aead.NonceSize()], sealed[c.aead.NonceSize():], []byte("external-data:v1"))
	if err != nil {
		return "", failed
	}
	return string(plain), nil
}
