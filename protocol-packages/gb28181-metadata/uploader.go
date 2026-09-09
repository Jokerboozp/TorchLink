package gbmetadata

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Uploader struct {
	URL, TenantID, NodeID, Secret, DataDir string
	AllowHTTP                              bool
	client                                 *http.Client
}

var ErrNodeUnauthorized = errors.New("node credential is invalid or disabled")

func (u *Uploader) Init(ctx context.Context) error {
	address, err := url.Parse(u.URL)
	if err != nil || address.Host == "" || address.User != nil || address.RawQuery != "" || address.Fragment != "" || (address.Path != "" && address.Path != "/") || (address.Scheme != "https" && !(u.AllowHTTP && address.Scheme == "http")) {
		return errors.New("platform URL requires HTTPS; isolated HTTP must be explicitly enabled")
	}
	for _, id := range []string{u.TenantID, u.NodeID} {
		if id == "" || len(id) > 128 || strings.ContainsAny(id, "/\\?#\x00") {
			return errors.New("invalid platform node identity")
		}
	}
	if u.Secret == "" || u.DataDir == "" {
		return errors.New("node credential and data directory are required")
	}
	u.URL = strings.TrimRight(u.URL, "/")
	u.client = &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if err := os.MkdirAll(u.DataDir, 0700); err != nil {
		return err
	}
	var cfg struct {
		TenantID string `json:"tenantId"`
		NodeID   string `json:"nodeId"`
		Tasks    []any  `json:"tasks"`
	}
	if err := u.request(ctx, "GET", "/config", nil, &cfg); err != nil {
		return err
	}
	if cfg.TenantID != u.TenantID || cfg.NodeID != u.NodeID || len(cfg.Tasks) != 0 {
		return errors.New("GB28181 metadata service requires its own node with no assigned collection profiles")
	}
	return nil
}
func (u *Uploader) request(ctx context.Context, method, suffix string, body any, result any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	if len(data) > 4<<20 {
		return errors.New("video catalog exceeds platform upload limit")
	}
	request, err := http.NewRequestWithContext(ctx, method, u.URL+"/api/v1/edge/"+url.PathEscape(u.TenantID)+"/"+url.PathEscape(u.NodeID)+suffix, bytes.NewReader(data))
	if err != nil {
		return err
	}
	request.Header.Set("X-Edge-Secret", u.Secret)
	request.Header.Set("Content-Type", "application/json")
	response, err := u.client.Do(request)
	if err != nil {
		return errors.New("platform request failed")
	}
	defer response.Body.Close()
	if response.StatusCode == 401 || response.StatusCode == 403 {
		return ErrNodeUnauthorized
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("platform returned HTTP %d", response.StatusCode)
	}
	if result != nil {
		return json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(result)
	}
	_, err = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
	return err
}
func (u *Uploader) Upload(ctx context.Context, devices []DeviceCatalog) error {
	count := 0
	for _, device := range devices {
		count += len(device.Channels)
	}
	if len(devices) > 256 || count > 1000 {
		return errors.New("node catalog exceeds 256 systems or 1000 channels")
	}
	data, err := json.Marshal(devices)
	if err != nil || len(data) > 4<<20 {
		return errors.New("catalog cache limit exceeded")
	}
	file, err := os.CreateTemp(u.DataDir, ".catalog-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, filepath.Join(u.DataDir, "catalog.json")); err != nil {
		return err
	}
	return u.request(ctx, "POST", "/heartbeat", map[string]any{"version": "gb28181-metadata-v1", "capabilities": []string{"GB28181_REGISTER", "GB28181_CATALOG"}, "videoCatalog": devices}, nil)
}
func (u *Uploader) Restore(r *Registrar) error {
	data, err := os.ReadFile(filepath.Join(u.DataDir, "catalog.json"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(data) > 4<<20 {
		return errors.New("cached catalog exceeds limit")
	}
	var devices []DeviceCatalog
	if err := json.Unmarshal(data, &devices); err != nil {
		return err
	}
	if len(devices) > 256 {
		return errors.New("cached catalog exceeds device limit")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	count := 0
	for _, d := range devices {
		count += len(d.Channels)
		if count > 1000 {
			return errors.New("cached catalog exceeds channel limit")
		}
		if _, ok := r.cfg.Devices[d.DeviceID]; !ok {
			continue
		}
		d.Registered = false
		d.LastError = "waiting for authenticated device registration"
		r.sessions[d.DeviceID] = &session{catalog: d}
	}
	return nil
}
