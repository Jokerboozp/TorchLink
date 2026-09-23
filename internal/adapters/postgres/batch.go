package postgres

import (
	"context"
	"encoding/json"

	"iot-platform/internal/model"
)

func (r *Repository) GetProductsByIDs(ctx context.Context, tenant string, ids []string) (map[string]model.Product, error) {
	out := make(map[string]model.Product, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT body FROM iot_product WHERE tenant_id=$1 AND id=ANY($2::text[])`, tenant, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var body []byte
		var value model.Product
		if err = rows.Scan(&body); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(body, &value); err != nil {
			return nil, err
		}
		out[value.ID] = value
	}
	return out, rows.Err()
}

func (r *Repository) GetDeviceStatesByIDs(ctx context.Context, tenant string, ids []string) (map[string]model.DeviceState, error) {
	out := make(map[string]model.DeviceState, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT body FROM device_state WHERE tenant_id=$1 AND device_id=ANY($2::text[])`, tenant, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var body []byte
		var value model.DeviceState
		if err = rows.Scan(&body); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(body, &value); err != nil {
			return nil, err
		}
		out[value.DeviceID] = value
	}
	return out, rows.Err()
}

func (r *Repository) GetStandardMessagesByRawIDs(ctx context.Context, tenant string, ids []string) (map[string]model.StandardMessage, error) {
	out := make(map[string]model.StandardMessage, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT DISTINCT ON (raw_message_id) raw_message_id,body FROM standard_message WHERE tenant_id=$1 AND raw_message_id=ANY($2::text[]) ORDER BY raw_message_id,ts DESC,message_id DESC`, tenant, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var body []byte
		var value model.StandardMessage
		if err = rows.Scan(&id, &body); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(body, &value); err != nil {
			return nil, err
		}
		out[id] = value
	}
	return out, rows.Err()
}

func (r *Repository) ListVideoCameraMappingsByDeviceIDs(ctx context.Context, tenant string, ids []string) (map[string][]model.VideoCameraMapping, error) {
	out := make(map[string][]model.VideoCameraMapping, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	relations, err := r.pool.Query(ctx, `SELECT target_id,camera_id FROM video_camera_relation WHERE tenant_id=$1 AND relation_type='device' AND target_id=ANY($2::text[]) ORDER BY target_id,camera_id`, tenant, ids)
	if err != nil {
		return nil, err
	}
	cameraToDevice := map[string]string{}
	cameraIDs := []string{}
	for relations.Next() {
		var deviceID, cameraID string
		if err = relations.Scan(&deviceID, &cameraID); err != nil {
			break
		}
		cameraToDevice[cameraID] = deviceID
		cameraIDs = append(cameraIDs, cameraID)
	}
	if err == nil {
		err = relations.Err()
	}
	relations.Close()
	if err != nil {
		return nil, err
	}
	if len(cameraIDs) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT `+videoCameraMappingColumns+` FROM video_camera_mapping WHERE tenant_id=$1 AND camera_id=ANY($2::text[]) ORDER BY camera_id`, tenant, cameraIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		value, scanErr := r.scanVideoMapping(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out[cameraToDevice[value.CameraID]] = append(out[cameraToDevice[value.CameraID]], value)
	}
	return out, rows.Err()
}
