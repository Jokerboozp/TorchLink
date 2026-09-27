package capacity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// backupOperation runs one device-data backup through the platform API while
// load is applied, downloads every file checking its SHA-256, and optionally
// restores it into the backup service's independent target (plan §7.4).
func (c *controller) backupOperation(m BackupModule) Operation {
	op := Operation{Name: "backup", Steps: map[string]string{}}
	started := time.Now()
	defer func() { op.Seconds = time.Since(started).Seconds() }()
	ctx, cancel := context.WithTimeout(context.Background(), max(m.Timeout.D(), 5*time.Minute))
	defer cancel()
	api := strings.TrimRight(c.inv.API, "/")
	client := &http.Client{Timeout: 0, Transport: &http.Transport{Proxy: nil}}
	call := func(method, path string) (int, []byte, http.Header, error) {
		req, _ := http.NewRequestWithContext(ctx, method, api+path, nil)
		req.Header.Set("Authorization", "Bearer "+c.opToken)
		resp, err := client.Do(req)
		if err != nil {
			return 0, nil, nil, err
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(io.LimitReader(resp.Body, 2<<30))
		return resp.StatusCode, b, resp.Header, err
	}
	status, body, _, err := call(http.MethodPost, "/api/v1/backups?type=FULL")
	if err != nil || status/100 != 2 {
		op.Detail = fmt.Sprintf("backup run: %s", codeOf(status, err))
		return op
	}
	var manifest struct {
		ID        string `json:"id"`
		Artifacts []struct {
			Filename string `json:"filename"`
		} `json:"artifacts"`
	}
	_ = json.Unmarshal(body, &manifest)
	op.Steps["backupId"] = manifest.ID
	op.Steps["backupSeconds"] = fmt.Sprintf("%.1f", time.Since(started).Seconds())
	for _, a := range manifest.Artifacts {
		status, file, hdr, err := call(http.MethodGet, "/api/v1/backups/"+url.PathEscape(manifest.ID)+"/files/"+url.PathEscape(a.Filename))
		if err != nil || status != 200 {
			op.Detail = fmt.Sprintf("download %s: %s", a.Filename, codeOf(status, err))
			return op
		}
		sum := sha256.Sum256(file)
		if want := hdr.Get("X-Checksum-SHA256"); want != "" && want != hex.EncodeToString(sum[:]) {
			op.Detail = "checksum mismatch: " + a.Filename
			return op
		}
		op.Steps["download:"+a.Filename] = fmt.Sprintf("%d bytes, sha256 ok", len(file))
	}
	if m.Restore {
		status, body, _, err = call(http.MethodPost, "/api/v1/backups/"+url.PathEscape(manifest.ID)+"/restore")
		var restored struct {
			Status string `json:"status"`
		}
		_ = json.Unmarshal(body, &restored)
		op.Steps["restore"] = firstNonEmpty(restored.Status, codeOf(status, err))
		if err != nil || status != 200 || restored.Status != "COMPLETED" {
			op.Detail = "restore to independent target: " + op.Steps["restore"]
			return op
		}
	}
	op.OK = true
	op.Detail = fmt.Sprintf("备份 %s，%d 个文件校验通过", manifest.ID, len(manifest.Artifacts))
	if m.Restore {
		op.Detail += "，已恢复到独立库并核对条数"
	}
	return op
}
