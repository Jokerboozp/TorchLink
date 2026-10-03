package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"iot-platform/internal/notify"
)

// NotificationStore implements notify.Store.
func (r *Repository) NotificationStore() notify.Store { return notificationStore{r} }

type notificationStore struct{ r *Repository }

func (s notificationStore) ListChannels(ctx context.Context, tenant string) ([]notify.Channel, error) {
	rows, err := s.r.pool.Query(ctx, `SELECT body,version,updated_at,secret<>'' FROM notification_channel WHERE tenant_id=$1 ORDER BY body->>'name',id`, tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []notify.Channel{}
	for rows.Next() {
		var c notify.Channel
		var body []byte
		if err = rows.Scan(&body, &c.Version, &c.UpdatedAt, &c.SecretSet); err != nil {
			return nil, err
		}
		version, updated, set := c.Version, c.UpdatedAt, c.SecretSet
		if err = json.Unmarshal(body, &c); err != nil {
			return nil, err
		}
		c.Version, c.UpdatedAt, c.SecretSet = version, updated, set
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s notificationStore) GetChannel(ctx context.Context, tenant, id string) (notify.Channel, string, error) {
	var c notify.Channel
	var body []byte
	var secret string
	err := s.r.pool.QueryRow(ctx, `SELECT body,version,updated_at,secret FROM notification_channel WHERE tenant_id=$1 AND id=$2`, tenant, id).Scan(&body, &c.Version, &c.UpdatedAt, &secret)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, "", notify.ErrNotFound
	}
	if err != nil {
		return c, "", err
	}
	version, updated := c.Version, c.UpdatedAt
	if err = json.Unmarshal(body, &c); err != nil {
		return c, "", err
	}
	c.Version, c.UpdatedAt, c.SecretSet = version, updated, secret != ""
	return c, secret, nil
}

func (s notificationStore) SaveChannel(ctx context.Context, c notify.Channel, sealed *string) (notify.Channel, error) {
	now := time.Now().UnixMilli()
	c.SecretSet = false
	body, err := json.Marshal(c)
	if err != nil {
		return c, err
	}
	var version int64
	var set bool
	if c.Version == 0 {
		secret := ""
		if sealed != nil {
			secret = *sealed
		}
		err = s.r.pool.QueryRow(ctx, `INSERT INTO notification_channel(tenant_id,id,body,secret,version,updated_at) VALUES($1,$2,$3,$4,1,$5) ON CONFLICT DO NOTHING RETURNING version, secret<>''`, c.TenantID, c.ID, body, secret, now).Scan(&version, &set)
	} else {
		err = s.r.pool.QueryRow(ctx, `UPDATE notification_channel SET body=$3, secret=COALESCE($4,secret), version=version+1, updated_at=$5 WHERE tenant_id=$1 AND id=$2 AND version=$6 RETURNING version, secret<>''`, c.TenantID, c.ID, body, sealed, now, c.Version).Scan(&version, &set)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return c, notify.ErrConflict
	}
	if err != nil {
		return c, err
	}
	c.Version, c.UpdatedAt, c.SecretSet = version, now, set
	return c, nil
}

func (s notificationStore) DeleteChannel(ctx context.Context, tenant, id string) error {
	tag, err := s.r.pool.Exec(ctx, `DELETE FROM notification_channel WHERE tenant_id=$1 AND id=$2`, tenant, id)
	if err == nil && tag.RowsAffected() == 0 {
		return notify.ErrNotFound
	}
	return err
}

func scanPolicy(row pgx.Row) (notify.Policy, error) {
	var p notify.Policy
	var body []byte
	var version, updated int64
	if err := row.Scan(&body, &version, &updated); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return p, notify.ErrNotFound
		}
		return p, err
	}
	err := json.Unmarshal(body, &p)
	p.Version, p.UpdatedAt = version, updated
	return p, err
}

func (s notificationStore) ListPolicies(ctx context.Context, tenant string) ([]notify.Policy, error) {
	rows, err := s.r.pool.Query(ctx, `SELECT body,version,updated_at FROM notification_policy WHERE tenant_id=$1 ORDER BY body->>'name',id`, tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []notify.Policy{}
	for rows.Next() {
		p, err := scanPolicy(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s notificationStore) GetPolicy(ctx context.Context, tenant, id string) (notify.Policy, error) {
	return scanPolicy(s.r.pool.QueryRow(ctx, `SELECT body,version,updated_at FROM notification_policy WHERE tenant_id=$1 AND id=$2`, tenant, id))
}

func (s notificationStore) SavePolicy(ctx context.Context, p notify.Policy) (notify.Policy, error) {
	now := time.Now().UnixMilli()
	body, err := json.Marshal(p)
	if err != nil {
		return p, err
	}
	var version int64
	if p.Version == 0 {
		err = s.r.pool.QueryRow(ctx, `INSERT INTO notification_policy(tenant_id,id,body,version,updated_at) VALUES($1,$2,$3,1,$4) ON CONFLICT DO NOTHING RETURNING version`, p.TenantID, p.ID, body, now).Scan(&version)
	} else {
		err = s.r.pool.QueryRow(ctx, `UPDATE notification_policy SET body=$3, version=version+1, updated_at=$4 WHERE tenant_id=$1 AND id=$2 AND version=$5 RETURNING version`, p.TenantID, p.ID, body, now, p.Version).Scan(&version)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return p, notify.ErrConflict
	}
	if err != nil {
		return p, err
	}
	p.Version, p.UpdatedAt = version, now
	return p, nil
}

func (s notificationStore) DeletePolicy(ctx context.Context, tenant, id string) error {
	tag, err := s.r.pool.Exec(ctx, `DELETE FROM notification_policy WHERE tenant_id=$1 AND id=$2`, tenant, id)
	if err == nil && tag.RowsAffected() == 0 {
		return notify.ErrNotFound
	}
	return err
}

func (s notificationStore) EnqueueTasks(ctx context.Context, tasks []notify.Task) (int, error) {
	batch := &pgx.Batch{}
	for _, t := range tasks {
		batch.Queue(`INSERT INTO notification_task(tenant_id,alarm_id,policy_id,policy_name,stage,kind,channel_id,status,next_at,created_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT DO NOTHING`, t.TenantID, t.AlarmID, t.PolicyID, t.PolicyName, t.Stage, t.Kind, t.ChannelID, t.Status, t.NextAt, t.CreatedAt)
	}
	results := s.r.pool.SendBatch(ctx, batch)
	defer results.Close()
	inserted := 0
	for range tasks {
		tag, err := results.Exec()
		if err != nil {
			return inserted, err
		}
		inserted += int(tag.RowsAffected())
	}
	return inserted, nil
}

const taskColumns = `id,tenant_id,alarm_id,policy_id,policy_name,stage,kind,channel_id,status,attempts,next_at,last_error,recipients,created_at,sent_at`

func scanTask(row pgx.Row) (notify.Task, error) {
	var t notify.Task
	var recipients []byte
	err := row.Scan(&t.ID, &t.TenantID, &t.AlarmID, &t.PolicyID, &t.PolicyName, &t.Stage, &t.Kind, &t.ChannelID, &t.Status, &t.Attempts, &t.NextAt, &t.LastError, &recipients, &t.CreatedAt, &t.SentAt)
	if err == nil {
		err = json.Unmarshal(recipients, &t.Recipients)
	}
	return t, err
}

func (s notificationStore) ClaimDueTasks(ctx context.Context, now, leaseMillis int64, limit int) ([]notify.Task, error) {
	rows, err := s.r.pool.Query(ctx, `UPDATE notification_task SET status='SENDING', lease_until=$1+$2
WHERE id IN (
  SELECT id FROM notification_task
  WHERE (status='PENDING' AND next_at<=$1) OR (status='SENDING' AND lease_until<=$1)
  ORDER BY next_at LIMIT $3 FOR UPDATE SKIP LOCKED
) RETURNING `+taskColumns, now, leaseMillis, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []notify.Task{}
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s notificationStore) FinishTask(ctx context.Context, t notify.Task) error {
	if t.Recipients == nil {
		t.Recipients = []string{}
	}
	recipients, _ := json.Marshal(t.Recipients)
	tag, err := s.r.pool.Exec(ctx, `UPDATE notification_task SET status=$2, attempts=$3, next_at=$4, last_error=$5, recipients=$6, sent_at=$7, lease_until=0 WHERE id=$1`, t.ID, t.Status, t.Attempts, t.NextAt, t.LastError, recipients, t.SentAt)
	if err == nil && tag.RowsAffected() == 0 {
		return notify.ErrNotFound
	}
	return err
}

func (s notificationStore) ListAlarmTasks(ctx context.Context, tenant, alarmID string) ([]notify.Task, error) {
	rows, err := s.r.pool.Query(ctx, `SELECT `+taskColumns+` FROM notification_task WHERE tenant_id=$1 AND alarm_id=$2 ORDER BY id`, tenant, alarmID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []notify.Task{}
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
