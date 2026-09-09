package core

import (
	"context"
	"errors"
	"fmt"
	"iot-platform/internal/model"
	"math"
	"time"
)

// SetShadowDesired records operator intent. Devices retrieve and reconcile it
// explicitly; this does not silently issue transport commands.
func (e *Engine) SetShadowDesired(ctx context.Context, tenant, device, actor string, version int64, patch map[string]any) (model.DeviceShadow, error) {
	var empty model.DeviceShadow
	d, err := e.Repo.GetManagedDevice(ctx, tenant, device)
	if err != nil || d.Status != "ENABLED" {
		return empty, errors.New("device is unavailable or disabled")
	}
	p, err := e.Repo.GetProduct(ctx, tenant, d.ProductID)
	if err != nil || p.Status != "ENABLED" || p.ThingModel == nil {
		return empty, errors.New("an enabled product with a thing model is required")
	}
	if patch == nil || len(patch) > 256 {
		return empty, errors.New("desired patch must contain at most 256 properties")
	}
	for key, value := range patch {
		var field *model.ThingField
		for i := range p.ThingModel.Properties {
			if p.ThingModel.Properties[i].Identifier == key {
				field = &p.ThingModel.Properties[i]
				break
			}
		}
		// Null removes intent even after a property becomes read-only.
		if value == nil {
			continue
		}
		if field == nil || !field.Writable {
			return empty, fmt.Errorf("property %s is not writable", key)
		}
		valid := false
		switch field.DataType {
		case "string":
			_, valid = value.(string)
		case "boolean":
			_, valid = value.(bool)
		case "object":
			_, valid = value.(map[string]any)
		case "array":
			_, valid = value.([]any)
		case "number", "integer":
			if number, ok := value.(float64); ok {
				valid = !math.IsNaN(number) && !math.IsInf(number, 0) && (field.DataType != "integer" || math.Trunc(number) == number)
			}
		}
		if !valid {
			return empty, fmt.Errorf("property %s does not match %s", key, field.DataType)
		}
	}
	return e.Repo.UpdateDeviceShadow(ctx, model.ShadowUpdate{TenantID: tenant, DeviceID: device, Actor: actor, ExpectedVersion: version, Desired: patch, Timestamp: time.Now().UnixMilli()})
}
