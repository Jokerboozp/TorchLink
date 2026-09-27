package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"iot-platform/internal/model"
)

const videoModuleStateID = "platform"

func (r *Repository) GetVideoModuleState(ctx context.Context) (model.VideoModuleState, error) {
	var v model.VideoModuleState
	err := r.pool.QueryRow(ctx, `SELECT enabled,updated_by,updated_at FROM video_module_state WHERE id=$1`, videoModuleStateID).Scan(&v.Enabled, &v.UpdatedBy, &v.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		// Never switched by an administrator: live is on by default. Cameras
		// without live configuration simply have no video.
		return model.VideoModuleState{Enabled: true}, nil
	}
	return v, err
}

func (r *Repository) SaveVideoModuleState(ctx context.Context, v model.VideoModuleState) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO video_module_state(id,enabled,updated_by,updated_at) VALUES($1,$2,$3,$4)
ON CONFLICT(id) DO UPDATE SET enabled=EXCLUDED.enabled,updated_by=EXCLUDED.updated_by,updated_at=EXCLUDED.updated_at`, videoModuleStateID, v.Enabled, v.UpdatedBy, v.UpdatedAt)
	return err
}

func (r *Repository) GetCameraLiveConfig(ctx context.Context, tenant, camera string) (model.CameraLiveConfig, error) {
	var v model.CameraLiveConfig
	var body []byte
	err := r.pool.QueryRow(ctx, `SELECT body FROM video_camera_live_config WHERE tenant_id=$1 AND camera_id=$2`, tenant, camera).Scan(&body)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, model.ErrNotFound
	}
	if err != nil {
		return v, err
	}
	err = json.Unmarshal(body, &v)
	return v, err
}

func (r *Repository) ListCameraLiveConfigs(ctx context.Context, tenant string, cameras []string) (map[string]model.CameraLiveConfig, error) {
	out := make(map[string]model.CameraLiveConfig, len(cameras))
	if len(cameras) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT body FROM video_camera_live_config WHERE tenant_id=$1 AND camera_id=ANY($2::text[])`, tenant, cameras)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var body []byte
		var v model.CameraLiveConfig
		if err = rows.Scan(&body); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(body, &v); err != nil {
			return nil, err
		}
		out[v.CameraID] = v
	}
	return out, rows.Err()
}

func (r *Repository) SaveCameraLiveConfig(ctx context.Context, v model.CameraLiveConfig) error {
	// Username and hasPassword are derived from the credential row.
	v.Username, v.HasPassword = "", false
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `INSERT INTO video_camera_live_config(tenant_id,camera_id,enabled,body,updated_at) VALUES($1,$2,$3,$4,$5)
ON CONFLICT(tenant_id,camera_id) DO UPDATE SET enabled=EXCLUDED.enabled,body=EXCLUDED.body,updated_at=EXCLUDED.updated_at`, v.TenantID, v.CameraID, v.Enabled, body, v.UpdatedAt)
	return err
}

func (r *Repository) GetCameraCredential(ctx context.Context, tenant, camera string) (model.CameraCredential, error) {
	v := model.CameraCredential{TenantID: tenant, CameraID: camera}
	err := r.pool.QueryRow(ctx, `SELECT key_id,nonce,ciphertext,updated_at FROM video_camera_credential WHERE tenant_id=$1 AND camera_id=$2`, tenant, camera).Scan(&v.KeyID, &v.Nonce, &v.Ciphertext, &v.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, model.ErrNotFound
	}
	return v, err
}

func (r *Repository) SaveCameraCredential(ctx context.Context, v model.CameraCredential) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO video_camera_credential(tenant_id,camera_id,key_id,nonce,ciphertext,updated_at) VALUES($1,$2,$3,$4,$5,$6)
ON CONFLICT(tenant_id,camera_id) DO UPDATE SET key_id=EXCLUDED.key_id,nonce=EXCLUDED.nonce,ciphertext=EXCLUDED.ciphertext,updated_at=EXCLUDED.updated_at`, v.TenantID, v.CameraID, v.KeyID, v.Nonce, v.Ciphertext, v.UpdatedAt)
	return err
}

func (r *Repository) DeleteCameraCredential(ctx context.Context, tenant, camera string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM video_camera_credential WHERE tenant_id=$1 AND camera_id=$2`, tenant, camera)
	return err
}

func (r *Repository) SaveVideoPlaySession(ctx context.Context, v model.VideoPlaySession) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `INSERT INTO video_play_session(id,tenant_id,camera_id,expires_at,revoked_at,body) VALUES($1,$2,$3,$4,$5,$6)
ON CONFLICT(id) DO UPDATE SET expires_at=EXCLUDED.expires_at,revoked_at=EXCLUDED.revoked_at,body=EXCLUDED.body`, v.ID, v.TenantID, v.CameraID, v.ExpiresAt, v.RevokedAt, body)
	return err
}

func (r *Repository) ListActiveVideoPlaySessions(ctx context.Context, now int64) ([]model.VideoPlaySession, error) {
	rows, err := r.pool.Query(ctx, `SELECT body FROM video_play_session WHERE revoked_at=0 AND expires_at>$1 ORDER BY id`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.VideoPlaySession{}
	for rows.Next() {
		var body []byte
		var v model.VideoPlaySession
		if err = rows.Scan(&body); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(body, &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *Repository) PruneVideoPlaySessions(ctx context.Context, before int64) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM video_play_session WHERE expires_at<$1 OR (revoked_at>0 AND revoked_at<$1)`, before)
	return err
}

// deleteCameraLiveTx removes a deleted camera's live configuration and sealed
// credential in the same transaction as the camera itself.
func deleteCameraLiveTx(ctx context.Context, tx pgx.Tx, tenant, camera string) error {
	for _, table := range []string{"video_camera_live_config", "video_camera_credential"} {
		if _, err := tx.Exec(ctx, "DELETE FROM "+table+" WHERE tenant_id=$1 AND camera_id=$2", tenant, camera); err != nil {
			return err
		}
	}
	_, err := tx.Exec(ctx, `UPDATE video_play_session SET revoked_at=$3 WHERE tenant_id=$1 AND camera_id=$2 AND revoked_at=0`, tenant, camera, time.Now().UnixMilli())
	return err
}

const gbDeviceColumns = `tenant_id,body,state,key_id,nonce,ciphertext`

func scanGBDevice(row pgx.Row) (model.GBDevice, error) {
	var v model.GBDevice
	var body, state, nonce, ciphertext []byte
	var keyID string
	if err := row.Scan(&v.TenantID, &body, &state, &keyID, &nonce, &ciphertext); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return v, model.ErrNotFound
		}
		return v, err
	}
	tenant := v.TenantID
	if err := json.Unmarshal(body, &v); err != nil {
		return v, err
	}
	if err := json.Unmarshal(state, &v.State); err != nil {
		return v, err
	}
	v.TenantID = tenant
	if keyID != "" {
		v.Password = &model.CameraCredential{TenantID: tenant, CameraID: v.DeviceID, KeyID: keyID, Nonce: nonce, Ciphertext: ciphertext}
	}
	return v, nil
}

func (r *Repository) ListGBDevices(ctx context.Context, tenant string) ([]model.GBDevice, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+gbDeviceColumns+` FROM video_gb_device WHERE tenant_id=$1 ORDER BY device_id`, tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.GBDevice{}
	for rows.Next() {
		v, err := scanGBDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *Repository) GetGBDevice(ctx context.Context, deviceID string) (model.GBDevice, error) {
	return scanGBDevice(r.pool.QueryRow(ctx, `SELECT `+gbDeviceColumns+` FROM video_gb_device WHERE device_id=$1`, deviceID))
}

// SaveGBDevice writes the administrator-owned settings and the sealed
// password; the reported state is left untouched. A device ID owned by
// another tenant is refused.
func (r *Repository) SaveGBDevice(ctx context.Context, v model.GBDevice) error {
	settings := v
	settings.State, settings.Online, settings.HasPassword, settings.Password = model.GBDeviceState{}, false, false, nil
	body, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	var keyID string
	var nonce, ciphertext []byte
	if v.Password != nil {
		keyID, nonce, ciphertext = v.Password.KeyID, v.Password.Nonce, v.Password.Ciphertext
	}
	tag, err := r.pool.Exec(ctx, `INSERT INTO video_gb_device(device_id,tenant_id,body,key_id,nonce,ciphertext,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7)
ON CONFLICT(device_id) DO UPDATE SET body=EXCLUDED.body,key_id=EXCLUDED.key_id,nonce=EXCLUDED.nonce,ciphertext=EXCLUDED.ciphertext,updated_at=EXCLUDED.updated_at
WHERE video_gb_device.tenant_id=EXCLUDED.tenant_id`, v.DeviceID, v.TenantID, body, keyID, nonce, ciphertext, v.UpdatedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return model.ErrGBDeviceTaken
	}
	return nil
}

func (r *Repository) SaveGBDeviceState(ctx context.Context, deviceID string, state model.GBDeviceState) error {
	body, err := json.Marshal(state)
	if err != nil {
		return err
	}
	tag, err := r.pool.Exec(ctx, `UPDATE video_gb_device SET state=$2 WHERE device_id=$1`, deviceID, body)
	if err == nil && tag.RowsAffected() == 0 {
		return model.ErrNotFound
	}
	return err
}

func (r *Repository) DeleteGBDevice(ctx context.Context, tenant, deviceID string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM video_gb_device WHERE tenant_id=$1 AND device_id=$2`, tenant, deviceID)
	if err == nil && tag.RowsAffected() == 0 {
		return model.ErrNotFound
	}
	return err
}
