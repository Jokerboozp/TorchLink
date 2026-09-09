package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"

	"iot-platform/internal/model"
	"iot-platform/internal/protocolworker"
)

// Store only new files belonging to this attempted immutable release. Native
// validation has already run; foreign binaries are explicitly untested here.
func storeProtocolVariants(root, directory string, artifact map[string]any, entries map[string][]byte, manifest protocolPackageManifestV2) (created []string, err error) {
	defer func() {
		if err != nil {
			for _, path := range created {
				_ = os.Remove(path)
			}
		}
	}()
	if len(manifest.Entrypoints) > 6 {
		return nil, errors.New("at most six protocol platforms are supported")
	}
	native := runtime.GOOS + "-" + runtime.GOARCH
	variants := map[string]any{}
	var total int
	for target, entry := range manifest.Entrypoints {
		platform := model.ProtocolPlatform(target)
		if platform == "" || platform != target {
			return created, errors.New("unsupported protocol artifact platform")
		}
		data := entries[entry]
		total += len(data)
		if len(data) == 0 || len(data) > 64<<20 || total > 128<<20 {
			return created, errors.New("protocol artifact exceeds per-platform or total size limits")
		}
		if platform == native {
			continue
		}
		name := "artifact-" + platform
		if len(platform) > 8 && platform[:8] == "windows-" {
			name += ".exe"
		}
		path := filepath.Join(directory, name)
		if err = writeExclusiveFile(path, data, 0700); err != nil {
			return created, err
		}
		created = append(created, path)
		relative, _ := filepath.Rel(root, path)
		digest := sha256.Sum256(data)
		validation := "UNTESTED"
		if build, ok := artifact["build"].(map[string]any); ok && build["kind"] == "go-source" {
			validation = "COMPILED"
		}
		variants[platform] = map[string]any{"platform": platform, "path": filepath.ToSlash(relative), "sha256": hex.EncodeToString(digest[:]), "size": len(data), "validation": validation, "testCases": 0}
	}
	bundle := protocolworker.SampleBundle{Cases: entries["samples/cases.json"], Operations: entries["samples/operations.json"]}
	data, err := json.Marshal(bundle)
	if err != nil || len(data) > (2<<20)+1024 {
		return created, errors.New("invalid protocol sample bundle")
	}
	path := filepath.Join(directory, "samples.json")
	if err = writeExclusiveFile(path, data, 0600); err != nil {
		return created, err
	}
	created = append(created, path)
	relative, _ := filepath.Rel(root, path)
	digest := sha256.Sum256(data)
	artifact["variants"], artifact["validation"] = variants, "PASSED"
	artifact["samplesPath"], artifact["samplesSha256"] = filepath.ToSlash(relative), hex.EncodeToString(digest[:])
	return created, nil
}
