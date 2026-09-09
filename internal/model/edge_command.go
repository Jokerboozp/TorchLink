package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

type EdgeCommandExecution struct {
	NodeID            string `json:"nodeId"`
	ProfileID         string `json:"profileId"`
	ConfigurationHash string `json:"configurationHash"`
	ProtocolID        string `json:"protocolId"`
	ProtocolVersion   string `json:"protocolVersion"`
	ExpiresAt         int64  `json:"expiresAt"`
	Token             string `json:"token,omitempty"`
}

func CommandProfileHash(p DeviceAccessProfile) string {
	p = p.Configuration()
	p.EdgeNodeID = "" // local execution copy removes the assigned node
	data, _ := json.Marshal(p)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func (c DeviceCommand) Public() DeviceCommand {
	if c.Execution != nil {
		e := *c.Execution
		e.Token = ""
		c.Execution = &e
	}
	return c
}
func (c DeviceCommand) ValidEdgeQueue() error {
	data, err := json.Marshal(c.Data)
	if err != nil || len(data) > 8192 || c.Execution == nil || c.Execution.NodeID == "" || c.Execution.ProfileID == "" || c.ID == "" || c.DeviceID == "" || c.TenantID == "" || c.Type == "" || c.Execution.ExpiresAt <= time.Now().UnixMilli() {
		return errors.New("invalid or expired edge command")
	}
	return nil
}
func (c DeviceCommand) SameEdgeRequest(other DeviceCommand) bool {
	if c.Execution == nil || other.Execution == nil {
		return false
	}
	a, _ := json.Marshal(c.Data)
	b, _ := json.Marshal(other.Data)
	return c.DeviceID == other.DeviceID && c.ProductID == other.ProductID && c.Type == other.Type && string(a) == string(b) && c.Execution.ProfileID == other.Execution.ProfileID && c.Execution.NodeID == other.Execution.NodeID
}
func (c DeviceCommand) EdgeResultAllowed() bool {
	return c.Status == "SENT" || c.Status == "ACKNOWLEDGED" || c.Status == "UNKNOWN" || c.Status == "REJECTED"
}
