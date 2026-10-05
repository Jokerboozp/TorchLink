package redisadapter

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// Repository decorates the durable repository with Redis-backed hot device state.
type Repository struct {
	ports.Repository
	client redis.UniversalClient
}

// Options selects a single Redis address or, with MasterName and Sentinels,
// the current master found through Redis Sentinel.
type Options struct {
	Addr       string
	Password   string
	MasterName string
	Sentinels  []string
}

// NewClient builds a client that follows Sentinel failover when configured.
func NewClient(o Options) redis.UniversalClient {
	if o.MasterName != "" && len(o.Sentinels) > 0 {
		return redis.NewFailoverClient(&redis.FailoverOptions{MasterName: o.MasterName, SentinelAddrs: o.Sentinels, Password: o.Password, SentinelPassword: o.Password})
	}
	return redis.NewClient(&redis.Options{Addr: o.Addr, Password: o.Password})
}

func New(base ports.Repository, client redis.UniversalClient) *Repository {
	return &Repository{Repository: base, client: client}
}

// cachedState stores the state with its row version so a slower writer can
// never replace a newer cached state (see setStateScript).
type cachedState struct {
	Version int64             `json:"v"`
	State   model.DeviceState `json:"s"`
}

// setStateScript writes the cache only when the stored version is older.
var setStateScript = redis.NewScript(`
local cur = redis.call('GET', KEYS[1])
if cur then
  local ok, obj = pcall(cjson.decode, cur)
  if ok and type(obj) == 'table' and obj.v and tonumber(obj.v) >= tonumber(ARGV[1]) then return 0 end
end
redis.call('SET', KEYS[1], ARGV[2])
return 1`)

func decodeCachedState(b []byte) (model.DeviceState, bool) {
	var c cachedState
	if json.Unmarshal(b, &c) == nil && c.Version > 0 && c.State.DeviceID != "" {
		c.State.Version = c.Version
		return c.State, true
	}
	var legacy model.DeviceState
	if json.Unmarshal(b, &legacy) == nil && legacy.DeviceID != "" {
		return legacy, true
	}
	return model.DeviceState{}, false
}
func cacheSegment(value string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}
func stateKey(tenant, device string) string {
	return fmt.Sprintf("device:state:%s:%s", cacheSegment(tenant), cacheSegment(device))
}
func latestKey(tenant, device string) string {
	return fmt.Sprintf("device:latest:%s:%s", cacheSegment(tenant), cacheSegment(device))
}

// UpsertDeviceState does not know the resulting version, so it drops the
// cached copy; the next read falls through to the repository.
func (r *Repository) UpsertDeviceState(ctx context.Context, v model.DeviceState) error {
	if err := r.Repository.UpsertDeviceState(ctx, v); err != nil {
		return err
	}
	pipe := r.client.TxPipeline()
	pipe.Del(ctx, stateKey(v.TenantID, v.DeviceID))
	r.online(ctx, pipe, v)
	_, err := pipe.Exec(ctx)
	return err
}

func (r *Repository) UpsertDeviceStateIf(ctx context.Context, v model.DeviceState) (bool, error) {
	ok, err := r.Repository.UpsertDeviceStateIf(ctx, v)
	if err != nil || !ok {
		return ok, err
	}
	version := v.Version + 1
	b, _ := json.Marshal(cachedState{Version: version, State: v})
	if err = setStateScript.Run(ctx, r.client, []string{stateKey(v.TenantID, v.DeviceID)}, version, b).Err(); err != nil {
		// The row is written; a stale cache entry must not survive.
		_ = r.client.Del(ctx, stateKey(v.TenantID, v.DeviceID)).Err()
		return true, nil
	}
	pipe := r.client.TxPipeline()
	r.online(ctx, pipe, v)
	_, _ = pipe.Exec(ctx)
	return true, nil
}

// CompleteStandardMessage refreshes the cached state after the combined
// write, like UpsertDeviceStateIf.
func (r *Repository) CompleteStandardMessage(ctx context.Context, state *model.DeviceState, tenant, messageID string, token int64) (bool, error) {
	ok, err := r.Repository.CompleteStandardMessage(ctx, state, tenant, messageID, token)
	if !ok || state == nil {
		return ok, err
	}
	v := *state
	version := v.Version + 1
	b, _ := json.Marshal(cachedState{Version: version, State: v})
	if setErr := setStateScript.Run(ctx, r.client, []string{stateKey(v.TenantID, v.DeviceID)}, version, b).Err(); setErr != nil {
		_ = r.client.Del(ctx, stateKey(v.TenantID, v.DeviceID)).Err()
		return ok, err
	}
	pipe := r.client.TxPipeline()
	r.online(ctx, pipe, v)
	_, _ = pipe.Exec(ctx)
	return ok, err
}

func (r *Repository) online(ctx context.Context, pipe redis.Pipeliner, v model.DeviceState) {
	if v.BusinessStatus == "ONLINE" || v.BusinessStatus == "ALARM" {
		pipe.SAdd(ctx, "device:online:"+cacheSegment(v.TenantID), v.DeviceID)
	} else {
		pipe.SRem(ctx, "device:online:"+cacheSegment(v.TenantID), v.DeviceID)
	}
}

// GetDeviceStateFresh bypasses the cache for read-modify-write paths.
func (r *Repository) GetDeviceStateFresh(ctx context.Context, tenant, device string) (model.DeviceState, error) {
	return r.Repository.GetDeviceStateFresh(ctx, tenant, device)
}
func (r *Repository) SaveStandardMessage(ctx context.Context, v model.StandardMessage) error {
	_, err := r.SaveStandardMessageIfAbsent(ctx, v)
	return err
}
func (r *Repository) SaveStandardMessageIfAbsent(ctx context.Context, v model.StandardMessage) (bool, error) {
	created, err := r.Repository.SaveStandardMessageIfAbsent(ctx, v)
	if err != nil || !created {
		return created, err
	}
	b, _ := json.Marshal(v)
	return true, r.client.Set(ctx, latestKey(v.TenantID, v.DeviceID), b, 7*24*time.Hour).Err()
}
func (r *Repository) ClaimStandardMessage(ctx context.Context, v model.StandardMessage, owner string, lease time.Duration) (model.StandardClaim, error) {
	claim, err := r.Repository.ClaimStandardMessage(ctx, v, owner, lease)
	if err != nil || !claim.ShouldProcess {
		return claim, err
	}
	b, _ := json.Marshal(v)
	return claim, r.client.Set(ctx, latestKey(v.TenantID, v.DeviceID), b, 7*24*time.Hour).Err()
}
func (r *Repository) GetLatestMessage(ctx context.Context, tenant, device string) (model.StandardMessage, error) {
	var v model.StandardMessage
	b, err := r.client.Get(ctx, latestKey(tenant, device)).Bytes()
	if err == nil && json.Unmarshal(b, &v) == nil {
		return v, nil
	}
	return r.Repository.GetLatestMessage(ctx, tenant, device)
}
func alarmKey(v model.Alarm) string {
	return fmt.Sprintf("alarm:active:%s:%s:%s", cacheSegment(v.TenantID), cacheSegment(v.DeviceID), cacheSegment(v.RuleID))
}
func (r *Repository) UpsertAlarm(ctx context.Context, v model.Alarm) (model.Alarm, bool, error) {
	a, created, err := r.Repository.UpsertAlarm(ctx, v)
	if err != nil {
		return a, created, err
	}
	b, _ := json.Marshal(a)
	if cacheErr := r.client.Set(ctx, alarmKey(a), b, 0).Err(); cacheErr != nil {
		return a, created, cacheErr
	}
	return a, created, nil
}
func (r *Repository) UpdateAlarm(ctx context.Context, v model.Alarm) error {
	if err := r.Repository.UpdateAlarm(ctx, v); err != nil {
		return err
	}
	return r.cacheAlarm(ctx, v)
}
func (r *Repository) UpdateAlarmIf(ctx context.Context, v model.Alarm) (bool, error) {
	ok, err := r.Repository.UpdateAlarmIf(ctx, v)
	if err != nil || !ok {
		return ok, err
	}
	return true, r.cacheAlarm(ctx, v)
}
func (r *Repository) cacheAlarm(ctx context.Context, v model.Alarm) error {
	if v.Status == "ACTIVE" || v.Status == "ACKED" {
		b, _ := json.Marshal(v)
		return r.client.Set(ctx, alarmKey(v), b, 0).Err()
	}
	return r.client.Del(ctx, alarmKey(v)).Err()
}
func (r *Repository) GetDeviceState(ctx context.Context, tenant, device string) (model.DeviceState, error) {
	b, err := r.client.Get(ctx, stateKey(tenant, device)).Bytes()
	if err == nil {
		if cached, ok := decodeCachedState(b); ok {
			return cached, nil
		}
	}
	return r.Repository.GetDeviceState(ctx, tenant, device)
}

// Health reports the wrapped store only. Redis holds a hot-state cache and
// shared rate budgets; reads fall back to the store and budgets to per-process
// shares, so a Redis outage degrades the platform without making it unready.
// CacheHealth reports Redis itself.
func (r *Repository) Health(ctx context.Context) error { return r.Repository.Health(ctx) }

// CacheHealth pings Redis.
func (r *Repository) CacheHealth(ctx context.Context) error { return r.client.Ping(ctx).Err() }
func (r *Repository) Close() error                          { return errors.Join(r.Repository.Close(), r.client.Close()) }
