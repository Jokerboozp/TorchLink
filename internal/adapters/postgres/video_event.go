package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"strconv"
	"strings"
)

func (r *Repository) GetGovernanceVideoEvent(ctx context.Context, tenant, id string) (out ports.VideoEventSource, err error) {
	var raw []byte
	err = r.pool.QueryRow(ctx, `SELECT e.body,COALESCE(e.body->'raw'->>'governanceDeviceId',''),COALESCE(m.device_id,'') FROM video_alarm_event e LEFT JOIN video_camera_mapping m ON m.tenant_id=e.tenant_id AND m.camera_id=e.camera_id WHERE e.tenant_id=$1 AND e.event_id=$2`, tenant, id).Scan(&raw, &out.DeviceID, &out.CurrentDeviceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, model.ErrNotFound
	}
	if err == nil {
		err = json.Unmarshal(raw, &out.Event)
	}
	return
}
func (r *Repository) ListGovernanceVideoEvents(ctx context.Context, tenant string, f ports.VideoEventFilter) ([]ports.VideoEventSource, error) {
	if f.Start < 0 || f.End <= f.Start || f.Limit < 1 || f.Limit > 1000 {
		return nil, model.ErrGovernanceInvalid
	}
	if f.DeviceIDs == nil {
		f.DeviceIDs = []string{}
	}
	var at int64 = -1
	var id string
	if f.Cursor != "" {
		p := strings.SplitN(f.Cursor, ":", 2)
		if len(p) != 2 {
			return nil, model.ErrGovernanceInvalid
		}
		var err error
		at, err = strconv.ParseInt(p[0], 10, 64)
		if err != nil {
			return nil, model.ErrGovernanceInvalid
		}
		id = p[1]
	}
	rows, err := r.pool.Query(ctx, `SELECT e.body,COALESCE(e.body->'raw'->>'governanceDeviceId',''),COALESCE(m.device_id,'') FROM video_alarm_event e LEFT JOIN video_camera_mapping m ON m.tenant_id=e.tenant_id AND m.camera_id=e.camera_id WHERE e.tenant_id=$1 AND e.event_time >= $3 AND e.event_time < $4 AND (e.event_time,e.event_id)>($5,$6) AND (($8 AND (cardinality($2::text[])=0 OR e.body->'raw'->>'governanceDeviceId'=ANY($2::text[]) OR m.device_id=ANY($2::text[]))) OR (NOT $8 AND e.body->'raw'->>'governanceDeviceId'=ANY($2::text[]) AND (COALESCE(m.device_id,'')='' OR m.device_id=ANY($2::text[])))) ORDER BY e.event_time,e.event_id LIMIT $7`, tenant, f.DeviceIDs, f.Start, f.End, at, id, f.Limit, f.AllDevices)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ports.VideoEventSource{}
	for rows.Next() {
		var v ports.VideoEventSource
		var raw []byte
		if err = rows.Scan(&raw, &v.DeviceID, &v.CurrentDeviceID); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &v.Event); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

var _ ports.VideoEventReader = (*Repository)(nil)
