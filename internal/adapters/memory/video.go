package memory

import (
	"context"
	"sort"
	"time"

	"iot-platform/internal/model"
)

// videoLiveState keeps the optional live module records for development and
// tests. Maps are created on first write so the zero value is ready to use.
type videoLiveState struct {
	module      model.VideoModuleState
	configs     map[string]model.CameraLiveConfig
	credentials map[string]model.CameraCredential
	sessions    map[string]model.VideoPlaySession
}

func (r *Repository) GetVideoModuleState(context.Context) (model.VideoModuleState, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.live.module, nil
}

func (r *Repository) SaveVideoModuleState(_ context.Context, v model.VideoModuleState) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.live.module = v
	return nil
}

func (r *Repository) GetCameraLiveConfig(_ context.Context, tenant, camera string) (model.CameraLiveConfig, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.live.configs[key(tenant, camera)]
	if !ok {
		return v, model.ErrNotFound
	}
	return clone(v), nil
}

func (r *Repository) ListCameraLiveConfigs(_ context.Context, tenant string, cameras []string) (map[string]model.CameraLiveConfig, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]model.CameraLiveConfig, len(cameras))
	for _, camera := range cameras {
		if v, ok := r.live.configs[key(tenant, camera)]; ok {
			out[camera] = clone(v)
		}
	}
	return out, nil
}

func (r *Repository) SaveCameraLiveConfig(_ context.Context, v model.CameraLiveConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.live.configs == nil {
		r.live.configs = map[string]model.CameraLiveConfig{}
	}
	v.Username, v.HasPassword = "", false
	r.live.configs[key(v.TenantID, v.CameraID)] = clone(v)
	return nil
}

func (r *Repository) GetCameraCredential(_ context.Context, tenant, camera string) (model.CameraCredential, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.live.credentials[key(tenant, camera)]
	if !ok {
		return model.CameraCredential{TenantID: tenant, CameraID: camera}, model.ErrNotFound
	}
	v.Nonce, v.Ciphertext = append([]byte(nil), v.Nonce...), append([]byte(nil), v.Ciphertext...)
	return v, nil
}

func (r *Repository) SaveCameraCredential(_ context.Context, v model.CameraCredential) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.live.credentials == nil {
		r.live.credentials = map[string]model.CameraCredential{}
	}
	v.Nonce, v.Ciphertext = append([]byte(nil), v.Nonce...), append([]byte(nil), v.Ciphertext...)
	r.live.credentials[key(v.TenantID, v.CameraID)] = v
	return nil
}

func (r *Repository) DeleteCameraCredential(_ context.Context, tenant, camera string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.live.credentials, key(tenant, camera))
	return nil
}

func (r *Repository) SaveVideoPlaySession(_ context.Context, v model.VideoPlaySession) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.live.sessions == nil {
		r.live.sessions = map[string]model.VideoPlaySession{}
	}
	r.live.sessions[v.ID] = v
	return nil
}

func (r *Repository) ListActiveVideoPlaySessions(_ context.Context, now int64) ([]model.VideoPlaySession, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []model.VideoPlaySession{}
	for _, v := range r.live.sessions {
		if v.RevokedAt == 0 && v.ExpiresAt > now {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (r *Repository) PruneVideoPlaySessions(_ context.Context, before int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, v := range r.live.sessions {
		if v.ExpiresAt < before || (v.RevokedAt > 0 && v.RevokedAt < before) {
			delete(r.live.sessions, id)
		}
	}
	return nil
}

// deleteCameraLiveLocked mirrors the PostgreSQL camera deletion transaction.
func (r *Repository) deleteCameraLiveLocked(tenant, camera string) {
	delete(r.live.configs, key(tenant, camera))
	delete(r.live.credentials, key(tenant, camera))
	now := time.Now().UnixMilli()
	for id, v := range r.live.sessions {
		if v.TenantID == tenant && v.CameraID == camera && v.RevokedAt == 0 {
			v.RevokedAt, v.RevokeReason = now, "camera deleted"
			r.live.sessions[id] = v
		}
	}
}
