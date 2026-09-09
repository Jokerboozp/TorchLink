package memory

import (
	"context"
	"errors"
	"iot-platform/internal/model"
	"time"
)

func (r *Repository) CreateEdgeCommand(_ context.Context, c model.DeviceCommand) (model.DeviceCommand, bool, error) {
	if err := c.ValidEdgeQueue(); err != nil {
		return c, false, err
	}
	execution := *c.Execution
	c.Execution = &execution
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.commands == nil {
		r.commands = map[string]model.DeviceCommand{}
	}
	if old, ok := r.commands[key(c.TenantID, c.ID)]; ok {
		return clone(old), false, nil
	}
	count := 0
	for _, old := range r.commands {
		if old.TenantID == c.TenantID && old.Execution != nil && old.Execution.NodeID == c.Execution.NodeID && old.Execution.ExpiresAt > time.Now().UnixMilli() && (old.Status == "QUEUED" || old.Status == "DISPATCHING") {
			count++
		}
	}
	if count >= 8 {
		return c, false, errors.New("edge command queue full")
	}
	c.Status = "QUEUED"
	c.Execution.Token = ""
	c.Reply = nil
	c.LastError = ""
	r.commands[key(c.TenantID, c.ID)] = clone(c)
	return clone(c), true, nil
}
func (r *Repository) GetDeviceCommand(_ context.Context, tenant, id string) (model.DeviceCommand, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.commands[key(tenant, id)]
	if !ok {
		return c, ErrNotFound
	}
	return clone(c), nil
}
func (r *Repository) ClaimEdgeCommand(_ context.Context, tenant, node, token string) (model.DeviceCommand, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if token == "" {
		return model.DeviceCommand{}, errors.New("claim token required")
	}
	var selected string
	for k, c := range r.commands {
		if c.TenantID == tenant && c.Execution != nil && c.Execution.NodeID == node && c.Status == "QUEUED" && c.Execution.ExpiresAt > time.Now().UnixMilli() {
			if selected == "" || c.CreatedAt < r.commands[selected].CreatedAt {
				selected = k
			}
		}
	}
	if selected == "" {
		return model.DeviceCommand{}, nil
	}
	c := clone(r.commands[selected])
	c.Status = "DISPATCHING"
	c.Execution.Token = token
	c.UpdatedAt = time.Now().UnixMilli()
	r.commands[selected] = clone(c)
	return c, nil
}
func (r *Repository) FinishEdgeCommand(_ context.Context, c model.DeviceCommand) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	old, ok := r.commands[key(c.TenantID, c.ID)]
	if !ok || old.Execution == nil || c.Execution == nil || old.Execution.NodeID != c.Execution.NodeID || old.Execution.Token == "" || old.Execution.Token != c.Execution.Token || time.Now().UnixMilli()-old.CreatedAt > 86400000 || !c.EdgeResultAllowed() {
		return errors.New("unowned or invalid edge command result")
	}
	if old.Status != "DISPATCHING" {
		return nil
	} // result retransmission never changes outcome
	old.Status = c.Status
	old.Reply = clone(c.Reply)
	old.LastError = c.LastError
	old.UpdatedAt = time.Now().UnixMilli()
	r.commands[key(c.TenantID, c.ID)] = old
	return nil
}
