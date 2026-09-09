package edgeagent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"iot-platform/internal/model"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

// The server never redelivers a claimed command. This single-item journal is
// written before dispatch; a restart reports UNKNOWN instead of executing again.
func (a *Agent) commandFile() string { return filepath.Join(a.options.DataDir, "command-result.json") }
func (a *Agent) saveCommandResult(c model.DeviceCommand) error {
	data, err := json.Marshal(c)
	if err != nil || len(data) > 32768 {
		return errors.New("command journal exceeds limit")
	}
	return atomicFile(a.commandFile(), data)
}
func (a *Agent) finishCommandResult(ctx context.Context) (bool, error) {
	f, err := os.Open(a.commandFile())
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	data, err := io.ReadAll(io.LimitReader(f, 32769))
	f.Close()
	if err != nil || len(data) > 32768 {
		return true, errors.New("invalid command journal")
	}
	var c model.DeviceCommand
	if json.Unmarshal(data, &c) != nil || c.Execution == nil || c.TenantID != a.options.TenantID || c.Execution.NodeID != a.options.NodeID || c.ID == "" || c.Execution.Token == "" || !c.EdgeResultAllowed() {
		return true, errors.New("foreign or invalid command journal")
	}
	var receipt struct {
		ID string `json:"id"`
	}
	err = a.request(ctx, "POST", "/commands/"+url.PathEscape(c.ID), c, &receipt)
	if err != nil {
		var status *HTTPError
		if errors.As(err, &status) && status.Status == 409 { // server no longer retains an owned result; preserve local evidence
			if renameErr := os.Rename(a.commandFile(), a.commandFile()+".rejected"); renameErr != nil {
				return true, renameErr
			}
		}
		return true, err
	}
	if receipt.ID != c.ID {
		return true, errors.New("platform did not confirm command result ID")
	}
	return true, os.Remove(a.commandFile())
}
func (a *Agent) commandJob(ctx context.Context) error {
	if pending, err := a.finishCommandResult(ctx); pending || err != nil {
		return err
	}
	if !a.options.AllowCommands || !a.options.AllowGoWorkers {
		return nil
	}
	var c model.DeviceCommand
	if err := a.request(ctx, "GET", "/commands", nil, &c); err != nil {
		return err
	}
	if c.ID == "" {
		return nil
	}
	if c.TenantID != a.options.TenantID || c.Execution == nil || c.Execution.NodeID != a.options.NodeID || c.Execution.Token == "" || c.Status != "DISPATCHING" {
		return errors.New("foreign or invalid claimed command")
	}
	c.Status = "UNKNOWN"
	c.LastError = "节点在命令领取后中断，执行结果未知，不自动重发"
	if err := a.saveCommandResult(c); err != nil {
		return err
	}
	executeCtx, cancel := context.WithDeadline(ctx, time.UnixMilli(c.Execution.ExpiresAt))
	defer cancel()
	if err := a.sync(ctx); err != nil {
		c.Status = "REJECTED"
		c.LastError = "执行前无法重新验证平台配置"
	} else {
		a.mu.Lock()
		valid := false
		for _, task := range a.config.Tasks {
			if task.Profile.ID == c.Execution.ProfileID && task.Profile.Enabled && model.CommandProfileHash(task.Profile) == c.Execution.ConfigurationHash && task.Release.ProtocolID == c.Execution.ProtocolID && task.Release.Version == c.Execution.ProtocolVersion {
				valid = true
				break
			}
		}
		if !valid || a.config.ExpiresAt <= time.Now().UnixMilli() || a.listeners == nil || executeCtx.Err() != nil {
			c.Status = "REJECTED"
			c.LastError = "命令已过期或接入配置、协议版本已变化"
		} else {
			// apply() uses the same mutex; keep the checked configuration through the
			// existing runtime's frame boundary and actual encode/write/ack operation.
			result, err := a.listeners.Command(executeCtx, c.TenantID, c.Execution.ProfileID, c.DeviceID, c.Data)
			if err != nil {
				c.Status = "UNKNOWN"
				c.LastError = err.Error()
				if len(c.LastError) > 512 {
					c.LastError = c.LastError[:512]
				}
			} else {
				c.Reply = result
				c.LastError = ""
				c.Status = "SENT"
				if result["status"] == "acknowledged" {
					c.Status = "ACKNOWLEDGED"
				}
			}
		}
		a.mu.Unlock()
	}
	c.Data = nil // results need identity, receipt and the claim token, not command parameters
	if err := a.saveCommandResult(c); err != nil {
		return err
	}
	_, err := a.finishCommandResult(ctx)
	return err
}
func (a *Agent) commandJobs(ctx context.Context) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			a.record("command", a.commandJob(ctx))
		}
	}
}
