// Package protocolcatalog authenticates remotely distributed Go source catalogs.
// It downloads only from an administrator configured HTTPS origin; publishing
// and executing a release remain separate existing platform operations.
package protocolcatalog

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

const MaxSource = 32 << 20

var segment = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)

type Entry struct {
	ID          string   `json:"id"`
	Version     string   `json:"version"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Publisher   string   `json:"publisher"`
	License     string   `json:"license"`
	SourceURL   string   `json:"sourceUrl"`
	SHA256      string   `json:"sha256"`
	Size        int64    `json:"size"`
	Tags        []string `json:"tags"`
}
type Payload struct {
	IssuedAt  int64   `json:"issuedAt"`
	ExpiresAt int64   `json:"expiresAt"`
	Entries   []Entry `json:"entries"`
}
type Envelope struct {
	KeyID     string `json:"keyId"`
	Payload   string `json:"payload"`
	Signature string `json:"signature"`
}
type Policy struct {
	URL        string            `json:"url"`
	PublicKeys map[string]string `json:"publicKeys"`
	CAFile     string            `json:"caFile,omitempty"`
}
type Catalog struct {
	Payload
	Digest string `json:"digest"`
	KeyID  string `json:"keyId"`
}
type Client struct {
	policy  Policy
	address *url.URL
	http    *http.Client
}

func ReadPolicy(path string) (*Client, error) {
	data, err := readLimitedFile(path, 64<<10)
	if err != nil {
		return nil, errors.New("read protocol catalog policy failed")
	}
	var p Policy
	if json.Unmarshal(data, &p) != nil {
		return nil, errors.New("invalid protocol catalog policy")
	}
	return New(p)
}
func readLimitedFile(path string, size int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, size+1))
	if err != nil || int64(len(b)) > size {
		return nil, errors.New("file exceeds limit")
	}
	return b, nil
}
func sourceURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(raw, "\r\n") {
		return nil, errors.New("catalog and sources require HTTPS URLs without credentials or query")
	}
	return u, nil
}
func New(p Policy) (*Client, error) {
	u, err := sourceURL(p.URL)
	if err != nil {
		return nil, err
	}
	if len(p.PublicKeys) == 0 || len(p.PublicKeys) > 16 {
		return nil, errors.New("catalog trust requires 1 to 16 Ed25519 public keys")
	}
	for id, value := range p.PublicKeys {
		key, err := base64.StdEncoding.DecodeString(value)
		if !segment.MatchString(id) || err != nil || len(key) != ed25519.PublicKeySize {
			return nil, errors.New("invalid catalog public key")
		}
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxConnsPerHost = 4
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	if p.CAFile != "" {
		data, err := readLimitedFile(p.CAFile, 1<<20)
		if err != nil {
			return nil, errors.New("read catalog CA failed")
		}
		roots, err := x509.SystemCertPool()
		if err != nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(data) {
			return nil, errors.New("invalid catalog CA")
		}
		transport.TLSClientConfig.RootCAs = roots
	}
	return &Client{policy: p, address: u, http: &http.Client{Transport: transport, Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (c *Client) Close() { c.http.CloseIdleConnections() }
func (c *Client) download(ctx context.Context, raw string, max int64) ([]byte, error) {
	u, err := sourceURL(raw)
	if err != nil || !strings.EqualFold(u.Host, c.address.Host) {
		return nil, errors.New("source is outside the configured catalog HTTPS origin")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", raw, nil)
	if err != nil {
		return nil, err
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, errors.New("catalog HTTPS request failed")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("catalog HTTP %d", res.StatusCode)
	}
	if res.ContentLength > max {
		return nil, errors.New("catalog download exceeds limit")
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, max+1))
	if err != nil || int64(len(data)) > max {
		return nil, errors.New("catalog download incomplete or exceeds limit")
	}
	return data, nil
}
func Validate(p Payload, now time.Time) error {
	ms := now.UnixMilli()
	if p.IssuedAt <= 0 || p.IssuedAt > ms+300000 || p.ExpiresAt <= ms || p.ExpiresAt <= p.IssuedAt || p.ExpiresAt-p.IssuedAt > 86400000 || len(p.Entries) > 1000 {
		return errors.New("catalog is expired or exceeds validity and size limits")
	}
	seen := map[string]bool{}
	for _, e := range p.Entries {
		key := e.ID + "/" + e.Version
		hash, err := hex.DecodeString(e.SHA256)
		if !segment.MatchString(e.ID) || !segment.MatchString(e.Version) || seen[key] || e.Name == "" || len(e.Name) > 256 || len(e.Description) > 4096 || len(e.Publisher) > 256 || len(e.License) > 128 || len(e.Tags) > 16 || err != nil || len(hash) != 32 || e.SHA256 != strings.ToLower(e.SHA256) || e.Size < 1 || e.Size > MaxSource {
			return errors.New("invalid catalog entry or duplicate version")
		}
		if _, err := sourceURL(e.SourceURL); err != nil {
			return err
		}
		for _, tag := range e.Tags {
			if len(tag) > 64 {
				return errors.New("catalog tag exceeds limit")
			}
		}
		seen[key] = true
	}
	return nil
}
func (c *Client) Fetch(ctx context.Context) (Catalog, error) {
	data, err := c.download(ctx, c.policy.URL, 8<<20)
	if err != nil {
		return Catalog{}, err
	}
	var envelope Envelope
	if json.Unmarshal(data, &envelope) != nil {
		return Catalog{}, errors.New("invalid signed catalog envelope")
	}
	public, err := base64.StdEncoding.DecodeString(c.policy.PublicKeys[envelope.KeyID])
	if err != nil || len(public) != ed25519.PublicKeySize {
		return Catalog{}, errors.New("untrusted catalog signing key")
	}
	payload, err := base64.StdEncoding.DecodeString(envelope.Payload)
	if err != nil {
		return Catalog{}, errors.New("invalid catalog payload")
	}
	signature, err := base64.StdEncoding.DecodeString(envelope.Signature)
	if err != nil || !ed25519.Verify(public, payload, signature) {
		return Catalog{}, errors.New("catalog signature verification failed")
	}
	var p Payload
	if json.Unmarshal(payload, &p) != nil {
		return Catalog{}, errors.New("invalid catalog payload JSON")
	}
	if err := Validate(p, time.Now()); err != nil {
		return Catalog{}, err
	}
	for _, e := range p.Entries {
		u, _ := sourceURL(e.SourceURL)
		if !strings.EqualFold(u.Host, c.address.Host) {
			return Catalog{}, errors.New("catalog source is outside configured HTTPS origin")
		}
	}
	sum := sha256.Sum256(payload)
	return Catalog{Payload: p, Digest: hex.EncodeToString(sum[:]), KeyID: envelope.KeyID}, nil
}
func (c *Client) Source(ctx context.Context, e Entry) ([]byte, error) {
	if e.Size < 1 || e.Size > MaxSource {
		return nil, errors.New("source exceeds catalog size limit")
	}
	data, err := c.download(ctx, e.SourceURL, e.Size)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	if int64(len(data)) != e.Size || hex.EncodeToString(sum[:]) != e.SHA256 {
		return nil, errors.New("protocol source size or SHA-256 mismatch")
	}
	return data, nil
}
