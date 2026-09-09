package edgeagent

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"iot-platform/internal/model"
	"iot-platform/internal/onboarding"
	"iot-platform/internal/protocolworker"
)

// Old single-platform releases retain their original validation contract. Every
// new source/prebuilt release supplies a checksum-bound suite, run on this OS.
func (a *Agent) validateWorkerSamples(ctx context.Context, release model.ProtocolRelease, artifact map[string]any) error {
	expected, _ := artifact["samplesSha256"].(string)
	if expected == "" {
		if variants, _ := release.Artifact["variants"].(map[string]any); len(variants) > 0 {
			return errors.New("multi-platform worker requires executable samples")
		}
		return nil
	}
	if decoded, err := hex.DecodeString(expected); err != nil || len(decoded) != 32 || strings.ToLower(expected) != expected {
		return errors.New("invalid worker sample checksum")
	}
	worker, _ := artifact["sha256"].(string)
	manifest := protocolworker.SampleManifest{ID: release.ProtocolID, Runtime: protocolworker.Runtime, Transport: release.Transport, PayloadFormat: release.PayloadFormat, Capabilities: release.Capabilities}
	identity, _ := json.Marshal(manifest)
	key := worker + ":" + expected + ":" + onboarding.Hash(string(identity))
	a.mu.Lock()
	passed := a.validatedWorkers[key]
	a.mu.Unlock()
	if passed {
		return nil
	}
	filename := filepath.Join(a.options.DataDir, "workers", "samples-"+expected+".json")
	data, err := os.ReadFile(filename)
	valid := func(data []byte) bool {
		return len(data) > 0 && len(data) <= (2<<20)+1024 && onboarding.Hash(string(data)) == expected
	}
	if err != nil || !valid(data) {
		request, err := http.NewRequestWithContext(ctx, "GET", a.options.URL+"/api/v1/edge/"+url.PathEscape(a.options.TenantID)+"/"+url.PathEscape(a.options.NodeID)+"/protocols/"+url.PathEscape(release.ProtocolID)+"/"+url.PathEscape(release.Version)+"/samples", nil)
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
		data, err = io.ReadAll(io.LimitReader(response.Body, (2<<20)+1025))
		if err != nil || !valid(data) {
			return errors.New("downloaded worker sample checksum or size is invalid")
		}
		if err = a.checkWorkerCache(int64(len(data))); err != nil {
			return err
		}
		if err = atomicFile(filename, data); err != nil {
			return err
		}
	}
	var bundle protocolworker.SampleBundle
	if err = json.Unmarshal(data, &bundle); err != nil {
		return errors.New("invalid worker samples")
	}
	_, err = protocolworker.ValidateSamples(ctx, a.options.DataDir, artifact, map[string][]byte{"samples/cases.json": bundle.Cases, "samples/operations.json": bundle.Operations}, manifest, true)
	if err != nil {
		return errors.New("worker samples failed on this node: " + err.Error())
	}
	a.mu.Lock()
	if a.validatedWorkers == nil || len(a.validatedWorkers) >= 512 {
		a.validatedWorkers = map[string]bool{}
	}
	a.validatedWorkers[key] = true
	a.mu.Unlock()
	return nil
}
