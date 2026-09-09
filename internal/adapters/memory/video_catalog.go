package memory

import (
	"context"
	"errors"
	"iot-platform/internal/model"
)

func (r *Repository) CreateCatalogCamera(ctx context.Context, v model.VideoCameraMapping) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if v.DeviceID != "" || len(v.RelatedDeviceIDs) > 0 || v.StreamURL != "" {
		return false, errors.New("catalog import only creates unassociated metadata")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	k := key(v.TenantID, v.CameraID)
	if _, ok := r.videoMappings[k]; ok {
		return false, nil
	}
	r.videoMappings[k] = clone(v)
	return true, nil
}
