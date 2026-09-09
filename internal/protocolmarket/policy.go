// Package protocolmarket owns local trust and signing policy for an organization's
// private catalog. Browser users cannot change keys or distribution credentials.
package protocolmarket

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"regexp"
	"runtime"
	"strings"
	"time"

	"iot-platform/internal/protocolcatalog"
)

var segment = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type Organization struct {
	Publisher         string   `json:"publisher"`
	KeyID             string   `json:"keyId"`
	PrivateKeyFile    string   `json:"privateKeyFile"`
	ReaderTokenHashes []string `json:"readerTokenHashes"`
}
type Policy struct {
	PublicOrigin  string                  `json:"publicOrigin"`
	Organizations map[string]Organization `json:"organizations"`
}

func ReadPolicy(path string) (Policy, error) {
	var p Policy
	data, err := readPrivate(path, 1<<20, false)
	if err != nil {
		return p, errors.New("read private market policy failed")
	}
	if json.Unmarshal(data, &p) != nil {
		return p, errors.New("invalid private market policy")
	}
	u, err := url.Parse(p.PublicOrigin)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return p, errors.New("market publicOrigin must be an HTTPS origin")
	}
	p.PublicOrigin = strings.TrimRight(p.PublicOrigin, "/")
	if len(p.Organizations) == 0 || len(p.Organizations) > 100 {
		return p, errors.New("market requires 1 to 100 organizations")
	}
	for tenant, org := range p.Organizations {
		if !segment.MatchString(tenant) || org.Publisher == "" || len(org.Publisher) > 256 || !segment.MatchString(org.KeyID) || org.PrivateKeyFile == "" || len(org.ReaderTokenHashes) == 0 || len(org.ReaderTokenHashes) > 64 {
			return p, errors.New("invalid market organization policy")
		}
		for _, hash := range org.ReaderTokenHashes {
			data, err := hex.DecodeString(hash)
			if err != nil || len(data) != 32 || strings.ToLower(hash) != hash {
				return p, errors.New("invalid market reader credential hash")
			}
		}
	}
	return p, nil
}
func (o Organization) Authenticate(secret string) bool {
	if len(secret) < 32 || len(secret) > 4096 {
		return false
	}
	sum := sha256.Sum256([]byte(secret))
	digest := hex.EncodeToString(sum[:])
	allowed := false
	for _, hash := range o.ReaderTokenHashes {
		if hmac.Equal([]byte(hash), []byte(digest)) {
			allowed = true
		}
	}
	return allowed
}
func (o Organization) Sign(payload protocolcatalog.Payload) (protocolcatalog.Envelope, error) {
	var envelope protocolcatalog.Envelope
	if err := protocolcatalog.Validate(payload, time.Now()); err != nil {
		return envelope, err
	}
	data, err := readPrivate(o.PrivateKeyFile, 4096, true)
	if err != nil {
		return envelope, errors.New("market signing key requires a private regular file")
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
	if err != nil || len(key) != ed25519.PrivateKeySize {
		return envelope, errors.New("invalid market signing key")
	}
	// Reject inconsistent seed/public-key pairs, including accidental truncation.
	derived := ed25519.NewKeyFromSeed(key[:ed25519.SeedSize])
	if !hmac.Equal(key, derived) {
		return envelope, errors.New("inconsistent market signing key")
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return envelope, err
	}
	return protocolcatalog.Envelope{KeyID: o.KeyID, Payload: base64.StdEncoding.EncodeToString(encoded), Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(key, encoded))}, nil
}
func readPrivate(path string, limit int64, private bool) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || (private && runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0) {
		return nil, errors.New("invalid private file")
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, errors.New("private file size exceeds limit")
	}
	return data, nil
}
