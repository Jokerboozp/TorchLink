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
	client *redis.Client
}

func New(base ports.Repository, addr, password string) *Repository {
	return &Repository{Repository: base, client: redis.NewClient(&redis.Options{Addr: addr, Password: password})}
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
func (r *Repository) UpsertDeviceState(ctx context.Context, v model.DeviceState) error {
	if err := r.Repository.UpsertDeviceState(ctx, v); err != nil {
		return err
	}
	b, _ := json.Marshal(v)
	pipe := r.client.TxPipeline()
	pipe.Set(ctx, stateKey(v.TenantID, v.DeviceID), b, 0)
	if v.BusinessStatus == "ONLINE" || v.BusinessStatus == "ALARM" {
		pipe.SAdd(ctx, "device:online:"+cacheSegment(v.TenantID), v.DeviceID)
	} else {
		pipe.SRem(ctx, "device:online:"+cacheSegment(v.TenantID), v.DeviceID)
	}
	_, err := pipe.Exec(ctx)
	return err
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
func (r *Repository) ClaimStandardMessage(ctx context.Context, v model.StandardMessage) (bool, bool, error) {
	shouldProcess, created, err := r.Repository.ClaimStandardMessage(ctx, v)
	if err != nil || !shouldProcess {
		return shouldProcess, created, err
	}
	b, _ := json.Marshal(v)
	return true, created, r.client.Set(ctx, latestKey(v.TenantID, v.DeviceID), b, 7*24*time.Hour).Err()
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
	if v.Status == "ACTIVE" || v.Status == "ACKED" {
		b, _ := json.Marshal(v)
		return r.client.Set(ctx, alarmKey(v), b, 0).Err()
	}
	return r.client.Del(ctx, alarmKey(v)).Err()
}
func (r *Repository) GetDeviceState(ctx context.Context, tenant, device string) (model.DeviceState, error) {
	var v model.DeviceState
	b, err := r.client.Get(ctx, stateKey(tenant, device)).Bytes()
	if err == nil && json.Unmarshal(b, &v) == nil {
		return v, nil
	}
	return r.Repository.GetDeviceState(ctx, tenant, device)
}
func (r *Repository) Health(ctx context.Context) error {
	if err := r.Repository.Health(ctx); err != nil {
		return err
	}
	return r.client.Ping(ctx).Err()
}
func (r *Repository) Close() error { return errors.Join(r.Repository.Close(), r.client.Close()) }
