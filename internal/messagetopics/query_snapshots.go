package messagetopics

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

const (
	maxQueryScan    = 10000
	maxSnapshotRows = 1000
	queryPageSize   = 200
)

// QuerySamples returns bounded, real records. It never publishes or saves a
// query, and callers must authorize the requested business dataset first.
func (s *Service) QuerySamples(ctx context.Context, tenant string, query model.MessageTopicQuery, limit int) ([][]byte, error) {
	if err := validTenant(tenant); err != nil {
		return nil, err
	}
	if err := ValidateQuery(query); err != nil {
		return nil, err
	}
	if limit < 1 || limit > queryPageSize {
		return nil, errors.New("预览样本数量无效")
	}
	rows := make([][]byte, 0, limit)
	if query.Dataset == "device_reports" {
		filter := ports.DeviceFilter{TenantID: tenant, RestrictDevices: query.DeviceScope == "selected", DeviceIDs: query.DeviceIDs}
		devices, _, err := s.repo.ListManagedDevicesFiltered(ctx, filter, limit, 0)
		if err != nil {
			return nil, err
		}
		for _, device := range devices {
			messages, _, err := s.repo.ListDeviceMessages(ctx, tenant, device.ID, "", 5, 0)
			if err != nil {
				return nil, err
			}
			for _, message := range messages {
				if message.TenantID != tenant || message.DeviceID != device.ID {
					return nil, errors.New("设备上报样本范围不一致")
				}
				body, err := json.Marshal(message)
				if err != nil {
					return nil, err
				}
				body, err = NormalizeQueryRecord(query, body)
				if err != nil {
					return nil, err
				}
				rows = append(rows, body)
				if len(rows) == limit {
					return rows, nil
				}
			}
		}
		return rows, nil
	}
	return s.queryRows(ctx, tenant, query, limit, 0)
}

func (s *Service) queryRows(ctx context.Context, tenant string, query model.MessageTopicQuery, limit, offset int) ([][]byte, error) {
	rows := make([][]byte, 0, limit)
	appendRow := func(value any) error {
		body, err := json.Marshal(value)
		if err != nil {
			return err
		}
		body, err = NormalizeQueryRecord(query, body)
		if err == nil {
			rows = append(rows, body)
		}
		return err
	}
	if query.Dataset == "devices" {
		var ids []string
		if query.DeviceScope == "selected" {
			ids = append([]string{}, query.DeviceIDs...)
		}
		if offset != 0 {
			return nil, errors.New("设备快照不支持分页读取")
		}
		records, err := s.repo.ListMessageTopicDevices(ctx, tenant, ids, limit)
		if err != nil {
			return nil, err
		}
		for _, record := range records {
			device := record.Device
			if device.TenantID != tenant {
				return nil, errors.New("设备查询范围不一致")
			}
			row := map[string]any{
				"id": device.ID, "deviceId": device.ID, "tenantId": tenant, "productId": device.ProductID,
				"name": device.Name, "status": device.Status, "deviceRole": device.DeviceRole, "gatewayId": device.GatewayID,
				"description": device.Description, "tags": device.Tags, "createdAt": device.CreatedAt, "updatedAt": device.UpdatedAt,
				"online": nil, "lastSeen": int64(0), "lastSeenAt": int64(0), "connectionStatus": "UNKNOWN", "dataStatus": "UNKNOWN", "businessStatus": "NEVER_SEEN",
			}
			if state := record.State; state != nil {
				if state.TenantID != tenant || state.DeviceID != device.ID {
					return nil, errors.New("设备状态范围不一致")
				}
				if state.ConnectionStatus == "CONNECTED" || state.ConnectionStatus == "DISCONNECTED" {
					row["online"] = state.ConnectionStatus == "CONNECTED"
				}
				row["connectionStatus"], row["dataStatus"], row["businessStatus"] = state.ConnectionStatus, state.DataStatus, state.BusinessStatus
				row["lastSeen"], row["lastSeenAt"] = state.LastSeenAt, state.LastSeenAt
			}
			if err := appendRow(row); err != nil {
				return nil, err
			}
		}
		return rows, nil
	}
	filter := ports.AlarmFilter{TenantID: tenant, Limit: limit, Offset: offset}
	if query.DeviceScope == "selected" {
		filter.DeviceIDs = append([]string{}, query.DeviceIDs...)
	}
	switch query.Dataset {
	case "alarms", "alarms_current":
	case "alarm_recoveries":
		filter.Status = "RECOVERED"
	case "alarm_confirmations":
		filter.Status = "ACKED"
	default:
		return nil, errors.New("此数据集不支持查询记录")
	}
	alarms, err := s.repo.ListAlarms(ctx, filter)
	if err != nil {
		return nil, err
	}
	for _, alarm := range alarms {
		if alarm.TenantID != tenant {
			return nil, errors.New("告警查询范围不一致")
		}
		if err := appendRow(alarm); err != nil {
			return nil, err
		}
	}
	return rows, nil
}

// Snapshot returns the complete bounded query result. Exceeding a limit is an
// error, never a silently truncated list presented to consumers as complete.
func (s *Service) Snapshot(ctx context.Context, tenant string, query model.MessageTopicQuery) ([]byte, error) {
	if err := validTenant(tenant); err != nil {
		return nil, err
	}
	if err := ValidateQuery(query); err != nil {
		return nil, err
	}
	if query.Mode != "interval" {
		return nil, errors.New("此查询不是定时查询")
	}
	items := make([]json.RawMessage, 0)
	bytes := 0
	// One bounded repository read avoids duplicate/missing records when newer
	// devices or alarms move between pages while the query is running.
	rows, err := s.queryRows(ctx, tenant, query, maxQueryScan+1, 0)
	if err != nil {
		return nil, err
	}
	if len(rows) > maxQueryScan {
		return nil, errors.New("查询扫描超过 10000 条，请缩小设备范围")
	}
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		result, matched, err := PreviewQuery(query, row)
		if err != nil {
			return nil, err
		}
		if !matched {
			continue
		}
		items = append(items, result)
		bytes += len(result) + 1
		if len(items) > maxSnapshotRows || bytes > MaxPayload {
			return nil, errors.New("查询结果超过 1000 条或 256 KiB，请缩小查询范围或减少返回字段")
		}
	}
	s.mu.Lock()
	now := s.now()
	s.mu.Unlock()
	payload, err := json.Marshal(struct {
		Dataset     string            `json:"dataset"`
		GeneratedAt int64             `json:"generatedAt"`
		Items       []json.RawMessage `json:"items"`
	}{query.Dataset, now.UnixMilli(), items})
	if err != nil {
		return nil, err
	}
	if len(payload) > MaxPayload {
		return nil, errors.New("查询结果超过 256 KiB，请减少返回字段")
	}
	return payload, nil
}

type scheduledQuery struct {
	signature [32]byte
	next      time.Time
}

type QueryScheduler struct {
	service *Service
	mu      sync.Mutex
	next    map[string]scheduledQuery
}

func NewQueryScheduler(service *Service) *QueryScheduler {
	return &QueryScheduler{service: service, next: make(map[string]scheduledQuery)}
}

// RunOnce is driven by the existing cluster singleton runner. Restart or leader
// handover may emit a fresh snapshot; these are full query results, not deltas.
func (q *QueryScheduler) RunOnce(ctx context.Context, publish func(context.Context, string, string, []byte) error) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	s := q.service
	tenants, err := s.repo.ListMessageTopicTenants(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	now := s.now()
	s.mu.Unlock()
	active := make(map[string]bool)
	var failures []error
	for _, tenant := range tenants {
		cfg, err := s.Load(ctx, tenant)
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", tenant, err))
			continue
		}
		for _, route := range cfg.Topics {
			if !route.Enabled || route.Query == nil || route.Query.Mode != "interval" {
				continue
			}
			key := tenant + "\x00" + route.ID
			active[key] = true
			encoded, _ := json.Marshal(route)
			signature := sha256.Sum256(encoded)
			if previous, ok := q.next[key]; ok && previous.signature == signature && now.Before(previous.next) {
				continue
			}
			if !s.topicReady(ctx, tenant, cfg, route) {
				continue
			}
			// A broker timeout has an uncertain outcome; do not retry the same
			// snapshot immediately. The next interval produces a new result.
			q.next[key] = scheduledQuery{signature: signature, next: now.Add(time.Duration(route.Query.IntervalSeconds) * time.Second)}
			runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			payload, err := s.Snapshot(runCtx, tenant, *route.Query)
			if err == nil {
				latest, loadErr := s.Load(runCtx, tenant)
				if loadErr != nil {
					err = loadErr
				} else if latest.Revision != cfg.Revision || !s.topicReady(runCtx, tenant, latest, route) {
					cancel()
					continue
				} else if runCtx.Err() != nil {
					err = runCtx.Err()
				} else {
					err = publish(runCtx, route.Protocol, Destination(tenant, route), payload)
				}
			}
			cancel()
			if err != nil {
				failures = append(failures, fmt.Errorf("tenant %s topic %s: %w", tenant, route.ID, err))
			}
		}
	}
	for key := range q.next {
		if !active[key] {
			delete(q.next, key)
		}
	}
	return errors.Join(failures...)
}
