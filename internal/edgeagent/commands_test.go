package edgeagent

import (
	"context"
	"encoding/json"
	"io"
	"iot-platform/internal/model"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
)

func TestCommandJournalRestartReportsWithoutReexecution(t *testing.T) {
	var calls, claims atomic.Int64
	var status atomic.Int64
	status.Store(503)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Edge-Secret") != "node-test-secret" {
			w.WriteHeader(401)
			return
		}
		if r.Method == "GET" {
			claims.Add(1)
			w.WriteHeader(500)
			return
		}
		calls.Add(1)
		if int(status.Load()) != 200 {
			w.WriteHeader(int(status.Load()))
			return
		}
		var c model.DeviceCommand
		if json.NewDecoder(r.Body).Decode(&c) != nil || c.Status != "UNKNOWN" || c.Execution.Token != "owned" {
			t.Error("journal did not retain unknown outcome")
		}
		json.NewEncoder(w).Encode(map[string]string{"id": c.ID})
	}))
	defer server.Close()
	options := Options{URL: server.URL, TenantID: "t", NodeID: "n", Secret: "node-test-secret", DataDir: t.TempDir(), AllowInsecureHTTP: true, AllowedCIDRs: []string{"127.0.0.0/8"}}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	a, err := New(options, log)
	if err != nil {
		t.Fatal(err)
	}
	c := model.DeviceCommand{ID: "journal-command", TenantID: "t", Status: "UNKNOWN", Execution: &model.EdgeCommandExecution{NodeID: "n", Token: "owned"}}
	if err := a.saveCommandResult(c); err != nil {
		t.Fatal(err)
	}
	if err := a.commandJob(context.Background()); err == nil {
		t.Fatal("503 reported as success")
	}
	if _, err := os.Stat(a.commandFile()); err != nil {
		t.Fatal("unconfirmed result lost")
	}
	a.Close()
	restarted, err := New(options, log)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	status.Store(200)
	if err := restarted.commandJob(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(restarted.commandFile()); !os.IsNotExist(err) {
		t.Fatal("confirmed journal not removed")
	}
	if calls.Load() != 2 || claims.Load() != 0 {
		t.Fatal("restart attempted new execution", calls.Load(), claims.Load())
	}
}
