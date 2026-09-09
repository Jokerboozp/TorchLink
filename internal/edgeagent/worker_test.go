package edgeagent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"iot-platform/internal/model"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
)

func TestWorkerPolicyAndDownloadIntegrity(t *testing.T) {
	data := []byte("test artifact bytes, never executed")
	digest := sha256.Sum256(data)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Edge-Secret") != "test-secret" {
			w.WriteHeader(401)
			return
		}
		w.Write([]byte("corrupt download"))
	}))
	defer upstream.Close()
	options := Options{URL: upstream.URL, TenantID: "tenant", NodeID: "edge", Secret: "test-secret", DataDir: t.TempDir(), AllowedCIDRs: []string{"127.0.0.0/8"}, AllowInsecureHTTP: true, AllowGoWorkers: true, AllowedListenAddresses: []string{"127.0.0.1"}}
	a, err := New(options, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	makeTask := func() model.EdgeTask {
		return model.EdgeTask{Profile: model.DeviceAccessProfile{Host: "127.0.0.1"}, Release: model.ProtocolRelease{ProtocolID: "worker", Version: "1", Artifact: map[string]any{"sha256": hex.EncodeToString(digest[:]), "platform": runtime.GOOS + "/" + runtime.GOARCH}}}
	}
	for _, test := range []string{"disabled", "bind", "platform", "hash", "corrupt"} {
		t.Run(test, func(t *testing.T) {
			task := makeTask()
			a.options.AllowGoWorkers = true
			switch test {
			case "disabled":
				a.options.AllowGoWorkers = false
			case "bind":
				task.Profile.Host = "0.0.0.0"
			case "platform":
				task.Release.Artifact["platform"] = "foreign/unknown"
			case "hash":
				task.Release.Artifact["sha256"] = "../worker"
			}
			if err := a.prepareWorker(context.Background(), &task); err == nil {
				t.Fatal("invalid worker prepared")
			}
		})
	}
}
