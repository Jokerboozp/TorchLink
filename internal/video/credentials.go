package video

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"time"

	"iot-platform/internal/model"
)

// Credentials are the camera account used for ONVIF and RTSP.
type Credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

var errCredentialKey = errors.New("camera credential key is not configured")

// sealer encrypts camera credentials with AES-256-GCM (standard AEAD). The
// tenant and camera IDs are bound as additional data so a sealed blob cannot
// be moved to another camera.
type sealer struct {
	key   []byte
	keyID string
}

func (s sealer) seal(tenant, camera string, c Credentials) (model.CameraCredential, error) {
	if len(s.key) != 32 {
		return model.CameraCredential{}, errCredentialKey
	}
	aead, err := s.aead()
	if err != nil {
		return model.CameraCredential{}, err
	}
	plain, err := json.Marshal(c)
	if err != nil {
		return model.CameraCredential{}, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return model.CameraCredential{}, err
	}
	return model.CameraCredential{TenantID: tenant, CameraID: camera, KeyID: s.keyID, Nonce: nonce, Ciphertext: aead.Seal(nil, nonce, plain, additionalData(tenant, camera)), UpdatedAt: time.Now().UnixMilli()}, nil
}

func (s sealer) open(v model.CameraCredential) (Credentials, error) {
	var c Credentials
	if len(s.key) != 32 {
		return c, errCredentialKey
	}
	if v.KeyID != s.keyID {
		return c, errors.New("camera credential was sealed with a different key; re-enter the password")
	}
	aead, err := s.aead()
	if err != nil {
		return c, err
	}
	plain, err := aead.Open(nil, v.Nonce, v.Ciphertext, additionalData(v.TenantID, v.CameraID))
	if err != nil {
		return c, errors.New("camera credential cannot be decrypted; re-enter the password")
	}
	err = json.Unmarshal(plain, &c)
	return c, err
}

func (s sealer) aead() (cipher.AEAD, error) {
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func additionalData(tenant, camera string) []byte {
	return []byte("torchlink-camera-credential\x00" + tenant + "\x00" + camera)
}
