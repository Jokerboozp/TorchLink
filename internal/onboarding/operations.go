package onboarding

import (
	"context"

	"encoding/json"
	"errors"
	"fmt"
	"iot-platform/internal/model"
	"reflect"
	"time"
)

// ValidateThingModel keeps the extension small and independent of codecs.
func ValidateThingModel(m *model.ThingModel) error {
	if m == nil {
		return nil
	}
	fields := func(fs []model.ThingField) error {
		if len(fs) > 256 {
			return errors.New("too many fields")
		}
		seen := map[string]bool{}
		for _, f := range fs {
			if !segment.MatchString(f.Identifier) || seen[f.Identifier] {
				return errors.New("invalid or duplicate field identifier")
			}
			seen[f.Identifier] = true
			switch f.DataType {
			case "string", "number", "integer", "boolean", "object", "array":
			default:
				return fmt.Errorf("unsupported dataType for %s", f.Identifier)
			}
		}
		return nil
	}
	if e := fields(m.Properties); e != nil {
		return e
	}
	for _, ops := range [][]model.ThingOperation{m.Events, m.Commands} {
		if len(ops) > 128 {
			return errors.New("too many operations")
		}
		seen := map[string]bool{}
		for _, op := range ops {
			if !segment.MatchString(op.Identifier) || seen[op.Identifier] {
				return errors.New("invalid or duplicate operation identifier")
			}
			seen[op.Identifier] = true
			if e := fields(op.Fields); e != nil {
				return e
			}
		}
	}
	return nil
}
func (s *Service) SaveEdge(ctx context.Context, t string, v model.EdgeNode) (model.EdgeNode, error) {
	if !segment.MatchString(v.ID) || v.Name == "" || len(v.Name) > 256 || len(v.Description) > 4096 {
		return v, errors.New("valid id and name are required")
	}
	if v.Status == "" {
		v.Status = "ENABLED"
	}
	if v.Status != "ENABLED" && v.Status != "DISABLED" {
		return v, errors.New("status must be ENABLED or DISABLED")
	}
	v.TenantID = t
	v.CreatedAt = time.Now().UnixMilli()
	if old, e := s.Repo.GetEdgeNode(ctx, t, v.ID); e == nil {
		v.CreatedAt = old.CreatedAt
	}
	v.UpdatedAt = time.Now().UnixMilli()
	return v, s.Repo.SaveEdgeNode(ctx, v)
}

func (s *Service) ChangeCredential(ctx context.Context, t, id string, rotate bool) (model.DeviceCredential, model.CredentialRevocation, error) {
	c := model.DeviceCredential{}
	hash := ""
	if rotate {
		var e error
		c, e = Credential()
		if e != nil {
			return c, model.CredentialRevocation{}, e
		}
		hash = Hash(c.Secret)
	}
	_, v, e := s.Repo.ChangeDeviceCredential(ctx, t, id, c.AccessKey, hash, time.Now().UnixMilli())
	if e != nil {
		return model.DeviceCredential{}, v, e
	}
	v = s.revoke(ctx, v)
	return c, v, nil
}
func (s *Service) revoke(ctx context.Context, v model.CredentialRevocation) model.CredentialRevocation {
	if v.Status == "REVOKED" {
		return v
	}
	v.LastError = "broker administration is not configured"
	if s.RevokeUsername != nil {
		limited, cancel := context.WithTimeout(ctx, 10*time.Second)
		e := s.RevokeUsername(limited, v.Username)
		cancel()
		if e == nil {
			v.Status = "REVOKED"
			v.LastError = ""
		} else {
			v.LastError = "broker revocation failed; retry pending"
		}
	}
	v.UpdatedAt = time.Now().UnixMilli()
	if e := s.Repo.UpdateCredentialRevocation(ctx, v); e != nil {
		v.Status = "PENDING"
		v.LastError = "revocation status could not be persisted"
	}
	return v
}
func (s *Service) RetryRevocations(ctx context.Context) {
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		if s.RevokeUsername != nil {
			items, e := s.Repo.ListCredentialRevocations(ctx, "", "", true)
			if e == nil {
				for _, v := range items {
					if ctx.Err() != nil {
						return
					}
					s.revoke(ctx, v)
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (s *Service) SendCommand(ctx context.Context, t, d string, q model.DeviceCommand) (model.DeviceCommand, error) {
	if !q.Confirmed {
		return q, errors.New("manual command confirmation is required")
	}
	if !segment.MatchString(q.ID) || !segment.MatchString(q.Type) || q.Data == nil {
		return q, errors.New("valid command id, type and data object are required")
	}
	device, e := s.Repo.GetManagedDevice(ctx, t, d)
	if e != nil || device.Status != "ENABLED" || device.SecretHash == "" || device.Tags["connector"] != "MQTT" {
		return q, errors.New("enabled standard MQTT device required")
	}
	p, e := s.Repo.GetProduct(ctx, t, device.ProductID)
	if e != nil || p.Status != "ENABLED" {
		return q, errors.New("product disabled")
	}
	if p.ThingModel != nil && len(p.ThingModel.Commands) > 0 {
		var op *model.ThingOperation
		for i := range p.ThingModel.Commands {
			if p.ThingModel.Commands[i].Identifier == q.Type {
				op = &p.ThingModel.Commands[i]
			}
		}
		if op == nil {
			return q, errors.New("command is not defined in product thing model")
		}
		for _, f := range op.Fields {
			v, ok := q.Data[f.Identifier]
			if !ok {
				if f.Required {
					return q, fmt.Errorf("%s is required", f.Identifier)
				}
				continue
			}
			valid := false
			switch f.DataType {
			case "string":
				_, valid = v.(string)
			case "boolean":
				_, valid = v.(bool)
			case "number":
				_, valid = v.(float64)
			case "integer":
				n, ok := v.(float64)
				valid = ok && n == float64(int64(n))
			case "object":
				_, valid = v.(map[string]any)
			case "array":
				_, valid = v.([]any)
			}
			if !valid {
				return q, fmt.Errorf("invalid %s", f.Identifier)
			}
		}
	}
	if s.PublishCommand == nil {
		return q, errors.New("MQTT publisher is unavailable")
	}
	now := time.Now().UnixMilli()
	q = model.DeviceCommand{ID: q.ID, TenantID: t, DeviceID: d, ProductID: device.ProductID, Type: q.Type, Data: q.Data, Status: "DISPATCHING", CreatedAt: now, UpdatedAt: now}
	payload, e := json.Marshal(map[string]any{"id": q.ID, "version": "1.0", "timestamp": now, "command": q.Type, "params": q.Data, "type": q.Type, "data": q.Data})
	if e != nil || len(payload) > 64<<10 {
		return q, errors.New("command exceeds 64 KiB or contains invalid JSON")
	}
	saved, created, e := s.Repo.CreateDeviceCommand(ctx, q)
	if e != nil {
		return q, e
	}
	if !created {
		if saved.Execution != nil || saved.DeviceID != d || saved.Type != q.Type || !reflect.DeepEqual(saved.Data, q.Data) {
			return model.DeviceCommand{}, errors.New("command id is already used by a different request")
		}
		return saved.ObservedOutcome(time.Now().UnixMilli()), nil
	}
	limited, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	e = s.PublishCommand(limited, fmt.Sprintf("/iot/down/%s/%s/%s/command", t, device.ProductID, d), payload, 1, false)
	status, message := "SENT", ""
	if e != nil {
		status, message = "UNKNOWN", "publish outcome unknown; inspect device before sending a new command"
	}
	if err := s.Repo.UpdateDeviceCommandDispatch(ctx, t, q.ID, status, message, time.Now().UnixMilli()); err != nil {
		return q, err
	}
	q.Status = status
	q.LastError = message
	return q, nil
}
