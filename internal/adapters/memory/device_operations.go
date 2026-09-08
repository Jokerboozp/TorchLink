package memory

import (
	"context"
	"errors"
	"iot-platform/internal/model"
	"sort"
)

func (r *Repository) SaveEdgeNode(_ context.Context, v model.EdgeNode) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.edgeNodes == nil {
		r.edgeNodes = map[string]model.EdgeNode{}
	}
	r.edgeNodes[key(v.TenantID, v.ID)] = clone(v)
	return nil
}
func (r *Repository) GetEdgeNode(_ context.Context, t, id string) (model.EdgeNode, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.edgeNodes[key(t, id)]
	if !ok {
		return v, ErrNotFound
	}
	return clone(v), nil
}
func (r *Repository) ListEdgeNodes(_ context.Context, t string) ([]model.EdgeNode, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []model.EdgeNode{}
	for _, v := range r.edgeNodes {
		if v.TenantID == t {
			out = append(out, clone(v))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
func (r *Repository) ListDeviceStateEvents(_ context.Context, t, d string, limit, offset int) ([]model.DeviceStateEvent, int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []model.DeviceStateEvent{}
	for i := len(r.stateEvents) - 1; i >= 0; i-- {
		v := r.stateEvents[i]
		if v.State.TenantID == t && v.State.DeviceID == d {
			out = append(out, clone(v))
		}
	}
	return page(out, offset, limit), len(out), nil
}
func (r *Repository) ListDeviceMessages(_ context.Context, t, d string, kind model.MessageType, limit, offset int) ([]model.StandardMessage, int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []model.StandardMessage{}
	for _, v := range r.standard {
		if v.TenantID == t && v.DeviceID == d && (kind == "" || v.MessageType == kind) {
			out = append(out, clone(v))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Timestamp == out[j].Timestamp {
			return out[i].MessageID > out[j].MessageID
		}
		return out[i].Timestamp > out[j].Timestamp
	})
	return page(out, offset, limit), len(out), nil
}

// The credential change and its broker revocation intent share the same lock.
func (r *Repository) ChangeDeviceCredential(_ context.Context, t, id, access, hash string, now int64) (model.ManagedDevice, model.CredentialRevocation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.devices[key(t, id)]
	if !ok {
		return d, model.CredentialRevocation{}, ErrNotFound
	}
	v := model.CredentialRevocation{ID: "revoke_" + d.ID + "_" + d.AccessKey, TenantID: t, DeviceID: id, Username: d.AccessKey, Status: "PENDING", CreatedAt: now, UpdatedAt: now}
	if access != "" {
		for _, other := range r.devices {
			if other.AccessKey == access {
				return d, v, errors.New("duplicate access key")
			}
		}
		d.AccessKey = access
	}
	d.SecretHash = hash
	d.SecretHint = ""
	d.UpdatedAt = now
	if r.revocations == nil {
		r.revocations = map[string]model.CredentialRevocation{}
	}
	if old, exists := r.revocations[key(t, v.ID)]; exists {
		v = old
	} else {
		r.revocations[key(t, v.ID)] = v
	}
	r.devices[key(t, id)] = cloneManaged(d)
	return cloneManaged(d), v, nil
}
func (r *Repository) ListCredentialRevocations(_ context.Context, t, d string, pending bool) ([]model.CredentialRevocation, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []model.CredentialRevocation{}
	for _, v := range r.revocations {
		if (t == "" || v.TenantID == t) && (d == "" || v.DeviceID == d) && (!pending || v.Status != "REVOKED") {
			out = append(out, clone(v))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt < out[j].CreatedAt })
	return out, nil
}
func (r *Repository) UpdateCredentialRevocation(_ context.Context, v model.CredentialRevocation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := key(v.TenantID, v.ID)
	old, ok := r.revocations[k]
	if !ok {
		return ErrNotFound
	}
	if old.Status == "REVOKED" {
		return nil
	}
	r.revocations[k] = clone(v)
	return nil
}
func (r *Repository) CreateDeviceCommand(_ context.Context, v model.DeviceCommand) (model.DeviceCommand, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.commands == nil {
		r.commands = map[string]model.DeviceCommand{}
	}
	k := key(v.TenantID, v.ID)
	if old, ok := r.commands[k]; ok {
		return clone(old), false, nil
	}
	r.commands[k] = clone(v)
	return clone(v), true, nil
}
func (r *Repository) UpdateDeviceCommandDispatch(_ context.Context, t, id, status, message string, now int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := key(t, id)
	v, ok := r.commands[k]
	if !ok {
		return ErrNotFound
	}
	if v.Status == "DISPATCHING" {
		v.Status = status
		v.LastError = message
		v.UpdatedAt = now
		r.commands[k] = v
	}
	return nil
}
func (r *Repository) CompleteDeviceCommand(_ context.Context, t, d, id string, reply map[string]any, now int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := key(t, id)
	v, ok := r.commands[k]
	if !ok || v.DeviceID != d {
		return nil
	}
	if v.Status == "SUCCEEDED" || v.Status == "FAILED" {
		return nil
	}
	v.Status = "FAILED"
	if reply["success"] == true {
		v.Status = "SUCCEEDED"
	}
	v.Reply = clone(reply)
	v.UpdatedAt = now
	r.commands[k] = v
	return nil
}
func (r *Repository) ListDeviceCommands(_ context.Context, t, d string, limit, offset int) ([]model.DeviceCommand, int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []model.DeviceCommand{}
	for _, v := range r.commands {
		if v.TenantID == t && v.DeviceID == d {
			out = append(out, clone(v))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt == out[j].CreatedAt {
			return out[i].ID > out[j].ID
		}
		return out[i].CreatedAt > out[j].CreatedAt
	})
	return page(out, offset, limit), len(out), nil
}
