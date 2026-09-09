package postgres

import (
	"context"
	"encoding/json"
	"iot-platform/internal/model"
)

func (r *Repository) ListProtocolMarket(ctx context.Context, tenant string) ([]model.ProtocolMarketEntry, error) {
	rows, err := r.pool.Query(ctx, `SELECT body FROM protocol_market_entry WHERE tenant_id=$1 ORDER BY protocol_id,version LIMIT 1001`, tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []model.ProtocolMarketEntry{}
	for rows.Next() {
		var data []byte
		var v model.ProtocolMarketEntry
		if err = rows.Scan(&data); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(data, &v); err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}
func (r *Repository) GetProtocolMarket(ctx context.Context, tenant, id, version string) (model.ProtocolMarketEntry, error) {
	var v model.ProtocolMarketEntry
	var data []byte
	err := r.pool.QueryRow(ctx, `SELECT body FROM protocol_market_entry WHERE tenant_id=$1 AND protocol_id=$2 AND version=$3`, tenant, id, version).Scan(&data)
	if err == nil {
		err = json.Unmarshal(data, &v)
	}
	return v, err
}
func (r *Repository) SubmitProtocolMarket(ctx context.Context, v model.ProtocolMarketEntry) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "protocol-market:"+v.TenantID); err != nil {
		return err
	}
	var exists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM protocol_market_entry WHERE tenant_id=$1 AND protocol_id=$2 AND version=$3)`, v.TenantID, v.ProtocolID, v.Version).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return model.ErrMarketConflict
	}
	var count int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM protocol_market_entry WHERE tenant_id=$1`, v.TenantID).Scan(&count); err != nil {
		return err
	}
	if count >= 1000 {
		return model.ErrMarketLimit
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO protocol_market_entry(tenant_id,protocol_id,version,body) VALUES($1,$2,$3,$4)`, v.TenantID, v.ProtocolID, v.Version, data); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (r *Repository) ReviewProtocolMarket(ctx context.Context, tenant, id, version, actor, decision, reason string, expected int64) (model.ProtocolMarketEntry, error) {
	var v model.ProtocolMarketEntry
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return v, err
	}
	defer tx.Rollback(ctx)
	var data []byte
	err = tx.QueryRow(ctx, `SELECT body FROM protocol_market_entry WHERE tenant_id=$1 AND protocol_id=$2 AND version=$3 FOR UPDATE`, tenant, id, version).Scan(&data)
	if err != nil {
		return v, err
	}
	if err = json.Unmarshal(data, &v); err != nil {
		return v, err
	}
	v, err = v.Review(actor, decision, reason, expected)
	if err != nil {
		return v, err
	}
	data, err = json.Marshal(v)
	if err != nil {
		return v, err
	}
	if _, err = tx.Exec(ctx, `UPDATE protocol_market_entry SET body=$4 WHERE tenant_id=$1 AND protocol_id=$2 AND version=$3`, tenant, id, version, data); err != nil {
		return v, err
	}
	return v, tx.Commit(ctx)
}
