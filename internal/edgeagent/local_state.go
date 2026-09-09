package edgeagent

import (
	"encoding/json"
	"os"
)

// Version may be set by a controlled build with -ldflags -X.
var Version = "edge-agent-v1"

type ReadyState struct {
	Version  string `json:"version"`
	TenantID string `json:"tenantId"`
	NodeID   string `json:"nodeId"`
	Nonce    string `json:"nonce"`
	PID      int    `json:"pid"`
}

// LockState and WriteState share the queue's process lock and durable atomic
// replacement for the independent launcher. Paths are supplied locally.
func LockState(path string) (*os.File, error)   { return lockQueue(path) }
func WriteState(path string, data []byte) error { return atomicFile(path, data) }
func (a *Agent) writeReady() error {
	path, nonce := os.Getenv("IOT_EDGE_READY_FILE"), os.Getenv("IOT_EDGE_READY_NONCE")
	if path == "" || nonce == "" {
		return nil
	}
	data, err := json.Marshal(ReadyState{Version: Version, TenantID: a.options.TenantID, NodeID: a.options.NodeID, Nonce: nonce, PID: os.Getpid()})
	if err != nil {
		return err
	}
	return atomicFile(path, data)
}
