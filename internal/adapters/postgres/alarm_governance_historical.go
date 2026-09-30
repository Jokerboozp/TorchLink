package postgres

import (
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"strconv"
	"strings"
)

func (t *governanceTx) GetGovernanceHistoricalMessage(id string) (model.StandardMessage, error) {
	var v model.StandardMessage
	var raw []byte
	err := t.tx.QueryRow(t.ctx, `SELECT body FROM standard_message WHERE tenant_id=$1 AND message_id=$2 AND processed_at>0`, t.tenant, id).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, model.ErrNotFound
	}
	if err != nil {
		return v, err
	}
	err = json.Unmarshal(raw, &v)
	return v, err
}

func (t *governanceTx) ListGovernanceHistoricalMessages(f ports.AlarmObservationFilter) ([]model.StandardMessage, error) {
	if f.Limit < 1 || f.Limit > 1000 || f.Start < 0 || f.End <= f.Start || len(f.DeviceIDs) == 0 {
		return nil, model.ErrGovernanceInvalid
	}
	at := int64(-1)
	id := ""
	if f.Cursor != "" {
		parts := strings.SplitN(f.Cursor, ":", 2)
		if len(parts) != 2 {
			return nil, model.ErrGovernanceInvalid
		}
		var err error
		at, err = strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return nil, model.ErrGovernanceInvalid
		}
		id = parts[1]
	}
	rows, err := t.tx.Query(t.ctx, `SELECT body FROM standard_message WHERE tenant_id=$1 AND device_id=ANY($2::text[]) AND ts >= $3 AND ts < $4 AND processed_at>0 AND (ts,message_id)>($5,$6) ORDER BY ts,message_id LIMIT $7`, t.tenant, f.DeviceIDs, f.Start, f.End, at, id, f.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.StandardMessage{}
	for rows.Next() {
		var raw []byte
		var v model.StandardMessage
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
