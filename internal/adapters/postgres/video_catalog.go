package postgres

import (
	"context"
	"errors"
	"iot-platform/internal/model"
)

func (r *Repository) CreateCatalogCamera(ctx context.Context, v model.VideoCameraMapping) (bool, error) {
	if v.DeviceID != "" || len(v.RelatedDeviceIDs) > 0 || v.StreamURL != "" {
		return false, errors.New("catalog import only creates unassociated metadata")
	}
	result, err := r.pool.Exec(ctx, `INSERT INTO video_camera_mapping(tenant_id,camera_id,camera_name,brand,camera_point,video_platform_id,enabled) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(tenant_id,camera_id) DO NOTHING`, v.TenantID, v.CameraID, v.CameraName, v.Brand, v.CameraPoint, v.VideoPlatformID, v.Enabled)
	return err == nil && result.RowsAffected() == 1, err
}
