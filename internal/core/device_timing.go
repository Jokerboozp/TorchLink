package core

import (
	"context"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// EffectiveDeviceTiming resolves a device's reporting interval and offline
// tolerance: the device's own setting, then its template's, then the default.
func EffectiveDeviceTiming(product *model.Product, device *model.ManagedDevice) (int64, int64) {
	interval, tolerance := int64(model.DefaultReportIntervalSec), int64(model.DefaultOfflineToleranceSec)
	if product != nil {
		if product.ReportIntervalSec > 0 {
			interval = product.ReportIntervalSec
		}
		if product.OfflineToleranceSec > 0 {
			tolerance = product.OfflineToleranceSec
		}
	}
	if device != nil {
		if device.ReportIntervalSec > 0 {
			interval = device.ReportIntervalSec
		}
		if device.OfflineToleranceSec > 0 {
			tolerance = device.OfflineToleranceSec
		}
	}
	return interval, tolerance
}

// deviceTiming reads the timing for a device whose state is being created.
// Later messages keep the state's timing; configuration changes are applied
// by ApplyDeviceTiming, so the message path does not read the registry.
func (e *Engine) deviceTiming(ctx context.Context, tenant, productID, deviceID string) (int64, int64) {
	var product *model.Product
	if p, err := e.cachedProduct(ctx, tenant, productID); err == nil {
		product = &p
	}
	var device *model.ManagedDevice
	if d, err := e.Repo.GetManagedDevice(ctx, tenant, deviceID); err == nil {
		device = &d
	}
	return EffectiveDeviceTiming(product, device)
}

// deviceTimingPage is how many devices ApplyDeviceTiming reads at a time.
const deviceTimingPage = 500

// ApplyDeviceTiming writes the current reporting timing into the existing
// states of one device, or of every device of a template when deviceID is
// empty. The offline check time follows the new timing. It returns how many
// states changed.
func (e *Engine) ApplyDeviceTiming(ctx context.Context, tenant, productID, deviceID string) (int, error) {
	e.ProtocolsChanged(tenant)
	var product *model.Product
	if p, err := e.Repo.GetProduct(ctx, tenant, productID); err == nil {
		product = &p
	}
	changed := 0
	apply := func(devices []model.ManagedDevice) error {
		for _, device := range devices {
			interval, tolerance := EffectiveDeviceTiming(product, &device)
			unlock := e.lockDeviceState(tenant, device.ID)
			before, after, written, err := e.mutateDeviceState(ctx, tenant, device.ID, func(state *model.DeviceState, found bool) (bool, error) {
				if !found || state.ReportIntervalSec == interval && state.OfflineToleranceSec == tolerance {
					return false, nil
				}
				state.ReportIntervalSec, state.OfflineToleranceSec = interval, tolerance
				return true, nil
			})
			unlock()
			if err != nil {
				return err
			}
			if written {
				changed++
				e.publishStateChange(ctx, before, after)
			}
		}
		return nil
	}
	if deviceID != "" {
		device, err := e.Repo.GetManagedDevice(ctx, tenant, deviceID)
		if err != nil {
			return 0, err
		}
		return changed, apply([]model.ManagedDevice{device})
	}
	for offset := 0; ; offset += deviceTimingPage {
		devices, _, err := e.Repo.ListManagedDevicesFiltered(ctx, ports.DeviceFilter{TenantID: tenant, RestrictProducts: true, ProductIDs: []string{productID}}, deviceTimingPage, offset)
		if err != nil {
			return changed, err
		}
		if err = apply(devices); err != nil {
			return changed, err
		}
		if len(devices) < deviceTimingPage {
			return changed, nil
		}
	}
}
