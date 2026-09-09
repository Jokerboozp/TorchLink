package edgeagent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"iot-platform/internal/protocolworker"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func workerRelease(r model.ProtocolRelease) bool {
	return r.ParserType == parser.GoProtocolParserName && r.Artifact["runtime"] == protocolworker.Runtime && protocolworker.HasCapability(r, "ingress")
}

func (a *Agent) prepareWorker(ctx context.Context, task *model.EdgeTask) error {
	if !a.options.AllowGoWorkers {
		return errors.New("Go worker execution requires the node's allow-go-workers setting")
	}
	if task.Profile.AutoRegister && !a.options.AllowAutoRegister {
		return errors.New("edge automatic registration requires the local allow-auto-register setting")
	}
	allowed := false
	for _, address := range a.options.AllowedListenAddresses {
		if address != "" && net.ParseIP(address) != nil && address == task.Profile.Host {
			allowed = true
		}
	}
	if !allowed {
		return errors.New("listener bind address is not in the node's explicit allowlist")
	}
	if task.Release.Artifact["platform"] != runtime.GOOS+"/"+runtime.GOARCH {
		return errors.New("published worker platform does not match this node")
	}
	expected, _ := task.Release.Artifact["sha256"].(string)
	if b, err := hex.DecodeString(expected); err != nil || len(b) != sha256.Size || strings.ToLower(expected) != expected {
		return errors.New("invalid worker SHA-256")
	}
	name := expected
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	dir := filepath.Join(a.options.DataDir, "workers")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	filename := filepath.Join(dir, name)
	valid := func(data []byte) bool {
		digest := sha256.Sum256(data)
		return len(data) > 0 && len(data) <= 64<<20 && hex.EncodeToString(digest[:]) == expected
	}
	data, err := os.ReadFile(filename)
	if err != nil || !valid(data) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		var used int64
		for _, entry := range entries {
			info, err := entry.Info()
			if err != nil || !info.Mode().IsRegular() {
				return errors.New("invalid worker cache entry")
			}
			used += info.Size()
		}
		if used > (1<<30)-(64<<20) {
			return errors.New("worker cache limit reached; archive unused workers before updating")
		}
		request, err := http.NewRequestWithContext(ctx, "GET", a.options.URL+"/api/v1/edge/"+url.PathEscape(a.options.TenantID)+"/"+url.PathEscape(a.options.NodeID)+"/protocols/"+url.PathEscape(task.Release.ProtocolID)+"/"+url.PathEscape(task.Release.Version)+"/artifact", nil)
		if err != nil {
			return err
		}
		request.Header.Set("X-Edge-Secret", a.options.Secret)
		response, err := a.client.Do(request)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			return &HTTPError{Status: response.StatusCode}
		}
		data, err = io.ReadAll(io.LimitReader(response.Body, (64<<20)+1))
		if err != nil || !valid(data) {
			return errors.New("downloaded worker checksum or size is invalid")
		}
		if err := atomicFile(filename, data); err != nil {
			return err
		}
	}
	if err := os.Chmod(filename, 0700); err != nil {
		return err
	}
	artifact := map[string]any{}
	for key, value := range task.Release.Artifact {
		artifact[key] = value
	}
	artifact["path"] = filepath.ToSlash(filepath.Join("workers", name))
	config := map[string]any{}
	for key, value := range task.Release.Config {
		config[key] = value
	}
	config["artifact"] = artifact
	task.Release.Config = config
	// Preserve server artifact metadata in the immutable release; only execution
	// config uses a node-relative path. The archive retains protocol ID/version.
	return nil
}
