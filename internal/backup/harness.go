package backup

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"
)

type harnessSnapshotEntry struct {
	Path   string `json:"path"`
	Base64 string `json:"base64"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}
type harnessSnapshot struct {
	FormatVersion int                    `json:"formatVersion"`
	Entries       []harnessSnapshotEntry `json:"entries"`
	FileCount     int64                  `json:"fileCount"`
	TotalBytes    int64                  `json:"totalBytes"`
}

var harnessSessionPath = regexp.MustCompile(`^sessions/[^/]+/[^/]+/session(?:\.v[1-9][0-9]*)?\.jsonl(?:\.zstd)?$`)

// The only files eligible for Agent recovery are the public manifests and
// sessions. Provider settings, environment files and runtime homes are excluded.
func validHarnessSnapshotPath(p string) bool {
	if p == "" || strings.ContainsAny(p, "\\\x00\r\n") || path.Clean(p) != p || path.IsAbs(p) {
		return false
	}
	if strings.HasPrefix(p, "plugins/") {
		rest := strings.TrimPrefix(p, "plugins/")
		return !strings.Contains(rest, "/") && strings.HasSuffix(rest, ".json") && rest != ".json"
	}
	return harnessSessionPath.MatchString(p)
}

func (s *Service) fetchHarnessSnapshot(ctx context.Context, endpoint string) (harnessSnapshot, error) {
	var result harnessSnapshot
	u, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") || u.Path != "/v1/backup/snapshot" {
		return result, errors.New("Harness snapshot URL must be an HTTP endpoint ending in /v1/backup/snapshot without credentials")
	}
	if strings.TrimSpace(s.cfg.HarnessToken) == "" {
		return result, errors.New("Harness snapshot service token is not configured")
	}
	// Do not follow a redirect with a service token to another endpoint.
	client := http.Client{Timeout: 90 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for attempt := 0; attempt < 3; attempt++ {
		req, e := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if e != nil {
			return result, e
		}
		req.Header.Set("X-IOT-Harness-Token", s.cfg.HarnessToken)
		resp, e := client.Do(req)
		if e != nil {
			return result, errors.New("Harness snapshot endpoint is unreachable")
		}
		if resp.StatusCode == http.StatusConflict {
			resp.Body.Close()
			if attempt == 2 {
				return result, errors.New("Harness snapshot remained busy after retries")
			}
			timer := time.NewTimer(time.Duration(attempt+1) * time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return result, ctx.Err()
			case <-timer.C:
			}
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return result, fmt.Errorf("Harness snapshot HTTP %d", resp.StatusCode)
		}
		dec := json.NewDecoder(io.LimitReader(resp.Body, 96<<20))
		e = dec.Decode(&result)
		if e == nil {
			var extra any
			if e2 := dec.Decode(&extra); !errors.Is(e2, io.EOF) {
				e = errors.New("invalid Harness snapshot payload")
			}
		}
		resp.Body.Close()
		if e != nil {
			return result, errors.New("invalid Harness snapshot payload")
		}
		if result.FormatVersion != 1 || result.FileCount != int64(len(result.Entries)) || result.FileCount > 4096 || result.TotalBytes < 0 || result.TotalBytes > 64<<20 {
			return result, errors.New("unsupported or incomplete Harness snapshot")
		}
		return result, nil
	}
	return result, errors.New("Harness snapshot unavailable")
}

func (s *Service) archivePersistentAgents(ctx context.Context, destination string) (files, bytes, instances int64, err error) {
	if len(s.cfg.HarnessSnapshotURLs) == 0 {
		files, bytes, err = archiveHarness(ctx, s.cfg.HarnessDataDir, destination)
		return files, bytes, 1, err
	}
	err = writeGzip(destination, func(w io.Writer) error {
		tw := tar.NewWriter(w)
		var canonicalPlugins map[string]string
		seenURLs := map[string]bool{}
		for i, endpoint := range s.cfg.HarnessSnapshotURLs {
			endpoint = strings.TrimSpace(endpoint)
			if seenURLs[endpoint] {
				return errors.New("Harness snapshot URLs contain duplicate instances")
			}
			seenURLs[endpoint] = true
			snapshot, e := s.fetchHarnessSnapshot(ctx, endpoint)
			if e != nil {
				return fmt.Errorf("Harness instance %d: %w", i, e)
			}
			plugins := map[string]string{}
			seen := map[string]bool{}
			var total int64
			for _, entry := range snapshot.Entries {
				if !validHarnessSnapshotPath(entry.Path) || seen[entry.Path] || entry.Size < 0 || entry.Size > 64<<20 || total+entry.Size > snapshot.TotalBytes {
					return errors.New("invalid Harness snapshot file")
				}
				seen[entry.Path] = true
				body, e := base64.StdEncoding.Strict().DecodeString(entry.Base64)
				if e != nil {
					return errors.New("invalid Harness snapshot content")
				}
				hash := sha256.Sum256(body)
				if int64(len(body)) != entry.Size || hex.EncodeToString(hash[:]) != entry.SHA256 {
					return errors.New("Harness snapshot checksum mismatch")
				}
				if strings.HasPrefix(entry.Path, "plugins/") {
					plugins[entry.Path] = entry.SHA256
				}
				name := fmt.Sprintf("instances/%03d/%s", i, entry.Path)
				if e = tw.WriteHeader(&tar.Header{Name: name, Mode: 0600, Typeflag: tar.TypeReg, Size: entry.Size}); e != nil {
					return e
				}
				if _, e = tw.Write(body); e != nil {
					return e
				}
				total += entry.Size
				files++
				bytes += entry.Size
			}
			if total != snapshot.TotalBytes {
				return errors.New("Harness snapshot byte count mismatch")
			}
			if canonicalPlugins == nil {
				canonicalPlugins = plugins
			} else {
				if len(plugins) != len(canonicalPlugins) {
					return errors.New("Harness instances have inconsistent Agent manifests")
				}
				for p, hash := range canonicalPlugins {
					if plugins[p] != hash {
						return errors.New("Harness instances have inconsistent Agent manifests")
					}
				}
			}
			instances++
		}
		return tw.Close()
	})
	return
}
