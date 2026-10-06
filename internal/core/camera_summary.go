package core

import (
	"context"
	"strings"

	"iot-platform/internal/model"
)

// ListCameraSummaries resolves the cameras associated with one device without
// exposing stream URLs or vendor credentials. The relation is intentionally
// one camera -> one device; a device may return many camera summaries.
func (e *Engine) ListCameraSummaries(ctx context.Context, tenant, deviceID string) ([]model.CameraSummary, error) {
	if strings.TrimSpace(tenant) == "" || strings.TrimSpace(deviceID) == "" {
		return nil, nil
	}
	relations, err := e.Repo.ListVideoCameraRelationsByTarget(ctx, tenant, "device", deviceID)
	if err != nil {
		return nil, err
	}
	out := make([]model.CameraSummary, 0, len(relations))
	seen := make(map[string]struct{}, len(relations))
	for _, relation := range relations {
		if relation.CameraID == "" {
			continue
		}
		if _, ok := seen[relation.CameraID]; ok {
			continue
		}
		mapping, getErr := e.Repo.GetVideoCameraMapping(ctx, tenant, relation.CameraID)
		if getErr != nil {
			return nil, getErr
		}
		out = append(out, cameraSummary(mapping))
		seen[relation.CameraID] = struct{}{}
	}
	return out, nil
}

// ListCameraSummariesForDevices resolves a list page with one batched relation
// and mapping lookup, preserving the same safe fields as ListCameraSummaries.
func (e *Engine) ListCameraSummariesForDevices(ctx context.Context, tenant string, deviceIDs []string) (map[string][]model.CameraSummary, error) {
	out := make(map[string][]model.CameraSummary, len(deviceIDs))
	if len(deviceIDs) == 0 {
		return out, nil
	}
	mappings, err := e.Repo.ListVideoCameraMappingsByDeviceIDs(ctx, tenant, deviceIDs)
	if err != nil {
		return nil, err
	}
	for deviceID, cameras := range mappings {
		seen := make(map[string]bool, len(cameras))
		for _, camera := range cameras {
			if camera.CameraID == "" || seen[camera.CameraID] {
				continue
			}
			out[deviceID] = append(out[deviceID], cameraSummary(camera))
			seen[camera.CameraID] = true
		}
	}
	return out, nil
}

func cameraSummary(v model.VideoCameraMapping) model.CameraSummary {
	return model.CameraSummary{CameraID: v.CameraID, Brand: v.Brand, CameraName: v.CameraName, CameraPoint: v.CameraPoint, DeviceID: v.DeviceID, Building: v.Building, Floor: v.Floor, Room: v.Room, Enabled: v.Enabled}
}
