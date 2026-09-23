package redisadapter /* 声明 redisadapter 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"         /* 执行当前语句并推进处理流程。 */
	"encoding/base64" /* 执行当前语句并推进处理流程。 */
	"encoding/json"   /* 执行当前语句并推进处理流程。 */
	"errors"          /* 执行当前语句并推进处理流程。 */
	"fmt"             /* 执行当前语句并推进处理流程。 */
	"time"            /* 执行当前语句并推进处理流程。 */

	"github.com/redis/go-redis/v9" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"  /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/ports"  /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

// Repository decorates the durable repository with Redis-backed hot device state.
type Repository struct { /* 定义 Repository 类型。 */
	ports.Repository               /* 执行当前语句并推进处理流程。 */
	client           *redis.Client /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func New(base ports.Repository, addr, password string) *Repository { /* 定义 New 函数。 */
	return &Repository{Repository: base, client: redis.NewClient(&redis.Options{Addr: addr, Password: password})} /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func cacheSegment(value string) string { /* 定义 cacheSegment 函数。 */
	return base64.RawURLEncoding.EncodeToString([]byte(value)) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func stateKey(tenant, device string) string { /* 定义 stateKey 函数。 */
	return fmt.Sprintf("device:state:%s:%s", cacheSegment(tenant), cacheSegment(device)) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func latestKey(tenant, device string) string { /* 定义 latestKey 函数。 */
	return fmt.Sprintf("device:latest:%s:%s", cacheSegment(tenant), cacheSegment(device)) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) UpsertDeviceState(ctx context.Context, v model.DeviceState) error { /* 定义 UpsertDeviceState 函数。 */
	if err := r.Repository.UpsertDeviceState(ctx, v); err != nil { /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	b, _ := json.Marshal(v)                                          /* 更新 _ 的值。 */
	pipe := r.client.TxPipeline()                                    /* 更新 pipe 的值。 */
	pipe.Set(ctx, stateKey(v.TenantID, v.DeviceID), b, 0)            /* 执行当前语句并推进处理流程。 */
	if v.BusinessStatus == "ONLINE" || v.BusinessStatus == "ALARM" { /* 判断条件并选择处理分支。 */
		pipe.SAdd(ctx, "device:online:"+cacheSegment(v.TenantID), v.DeviceID) /* 执行当前语句并推进处理流程。 */
	} else { /* 结束当前表达式或代码块。 */
		pipe.SRem(ctx, "device:online:"+cacheSegment(v.TenantID), v.DeviceID) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	_, err := pipe.Exec(ctx) /* 更新 err 的值。 */
	return err               /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) SaveStandardMessage(ctx context.Context, v model.StandardMessage) error { /* 定义 SaveStandardMessage 函数。 */
	_, err := r.SaveStandardMessageIfAbsent(ctx, v) /* 更新 err 的值。 */
	return err                                      /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) SaveStandardMessageIfAbsent(ctx context.Context, v model.StandardMessage) (bool, error) { /* 定义 SaveStandardMessageIfAbsent 函数。 */
	created, err := r.Repository.SaveStandardMessageIfAbsent(ctx, v) /* 更新 err 的值。 */
	if err != nil || !created {                                      /* 判断条件并选择处理分支。 */
		return created, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	b, _ := json.Marshal(v)                                                                    /* 更新 _ 的值。 */
	return true, r.client.Set(ctx, latestKey(v.TenantID, v.DeviceID), b, 7*24*time.Hour).Err() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ClaimStandardMessage(ctx context.Context, v model.StandardMessage) (bool, bool, error) { /* 定义 ClaimStandardMessage 函数。 */
	shouldProcess, created, err := r.Repository.ClaimStandardMessage(ctx, v) /* 更新 err 的值。 */
	if err != nil || !shouldProcess {                                        /* 判断条件并选择处理分支。 */
		return shouldProcess, created, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	b, _ := json.Marshal(v)                                                                             /* 更新 _ 的值。 */
	return true, created, r.client.Set(ctx, latestKey(v.TenantID, v.DeviceID), b, 7*24*time.Hour).Err() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) GetLatestMessage(ctx context.Context, tenant, device string) (model.StandardMessage, error) { /* 定义 GetLatestMessage 函数。 */
	var v model.StandardMessage                                    /* 声明 v。 */
	b, err := r.client.Get(ctx, latestKey(tenant, device)).Bytes() /* 更新 err 的值。 */
	if err == nil && json.Unmarshal(b, &v) == nil {                /* 判断条件并选择处理分支。 */
		return v, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return r.Repository.GetLatestMessage(ctx, tenant, device) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func alarmKey(v model.Alarm) string { /* 定义 alarmKey 函数。 */
	return fmt.Sprintf("alarm:active:%s:%s:%s", cacheSegment(v.TenantID), cacheSegment(v.DeviceID), cacheSegment(v.RuleID)) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) UpsertAlarm(ctx context.Context, v model.Alarm) (model.Alarm, bool, error) { /* 定义 UpsertAlarm 函数。 */
	a, created, err := r.Repository.UpsertAlarm(ctx, v) /* 更新 err 的值。 */
	if err != nil {                                     /* 判断条件并选择处理分支。 */
		return a, created, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	b, _ := json.Marshal(a)                                                      /* 更新 _ 的值。 */
	if cacheErr := r.client.Set(ctx, alarmKey(a), b, 0).Err(); cacheErr != nil { /* 判断条件并选择处理分支。 */
		return a, created, cacheErr /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return a, created, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) UpdateAlarm(ctx context.Context, v model.Alarm) error { /* 定义 UpdateAlarm 函数。 */
	if err := r.Repository.UpdateAlarm(ctx, v); err != nil { /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if v.Status == "ACTIVE" || v.Status == "ACKED" { /* 判断条件并选择处理分支。 */
		b, _ := json.Marshal(v)                           /* 更新 _ 的值。 */
		return r.client.Set(ctx, alarmKey(v), b, 0).Err() /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return r.client.Del(ctx, alarmKey(v)).Err() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) GetDeviceState(ctx context.Context, tenant, device string) (model.DeviceState, error) { /* 定义 GetDeviceState 函数。 */
	var v model.DeviceState                                       /* 声明 v。 */
	b, err := r.client.Get(ctx, stateKey(tenant, device)).Bytes() /* 更新 err 的值。 */
	if err == nil && json.Unmarshal(b, &v) == nil {               /* 判断条件并选择处理分支。 */
		return v, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return r.Repository.GetDeviceState(ctx, tenant, device) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) Health(ctx context.Context) error { /* 定义 Health 函数。 */
	if err := r.Repository.Health(ctx); err != nil { /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return r.client.Ping(ctx).Err() /* 返回当前处理结果。 */
}                                  /* 结束当前表达式或代码块。 */
func (r *Repository) Close() error { return errors.Join(r.Repository.Close(), r.client.Close()) } /* 定义 Close 函数。 */
