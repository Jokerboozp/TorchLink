package edgeagent

import (
	"context"
	"errors"
	"iot-platform/internal/model"
)

func (a *Agent) registerProtocolDevice(ctx context.Context, p model.DeviceAccessProfile, id, name string) (model.ManagedDevice, error) {
	if !a.options.AllowGoWorkers || !a.options.AllowAutoRegister {
		return model.ManagedDevice{}, errors.New("automatic registration is disabled by local node policy")
	}
	var result struct {
		Device model.ManagedDevice `json:"device"`
	}
	err := a.request(ctx, "POST", "/devices/register", map[string]any{"profileId": p.ID, "deviceId": id, "name": name, "configurationHash": model.CommandProfileHash(p)}, &result)
	return result.Device, err
}
