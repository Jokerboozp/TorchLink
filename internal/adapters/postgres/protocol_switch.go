package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"iot-platform/internal/model"
)

const (
	saveProductSQL         = `INSERT INTO iot_product(tenant_id,id,status,protocol_package_id,body) VALUES($1,$2,$3,$4,$5) ON CONFLICT(tenant_id,id) DO UPDATE SET status=excluded.status,protocol_package_id=excluded.protocol_package_id,body=excluded.body,updated_at=now()`
	saveProtocolPackageSQL = `INSERT INTO protocol_package(tenant_id,id,status,parser_type,body) VALUES($1,$2,$3,$4,$5) ON CONFLICT(tenant_id,id) DO UPDATE SET status=excluded.status,parser_type=excluded.parser_type,body=excluded.body,updated_at=now()`
	saveBindingSQL         = `INSERT INTO product_protocol_binding(tenant_id,product_id,protocol_id,version,body) VALUES($1,$2,$3,$4,$5) ON CONFLICT(tenant_id,product_id) DO UPDATE SET protocol_id=excluded.protocol_id,version=excluded.version,body=excluded.body,updated_at=now()`
)

func (r *Repository) SwitchProductProtocol(ctx context.Context, v model.ProtocolSwitch) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Lock the template so concurrent switches compare against the same binding.
	var exists int
	if err = tx.QueryRow(ctx, `SELECT 1 FROM iot_product WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, v.Product.TenantID, v.Product.ID).Scan(&exists); errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	var protocol, version string
	err = tx.QueryRow(ctx, `SELECT protocol_id,version FROM product_protocol_binding WHERE tenant_id=$1 AND product_id=$2`, v.Product.TenantID, v.Product.ID).Scan(&protocol, &version)
	found := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if found != (v.Expected != nil) || found && (protocol != v.Expected.ProtocolID || version != v.Expected.Version) {
		return model.ErrBindingChanged
	}
	body := func(value any) []byte { data, _ := json.Marshal(value); return data }
	if _, err = tx.Exec(ctx, saveProtocolPackageSQL, v.Package.TenantID, v.Package.ID, v.Package.Status, v.Package.ParserType, body(v.Package)); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, saveProductSQL, v.Product.TenantID, v.Product.ID, v.Product.Status, v.Product.ProtocolPackageID, body(v.Product)); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, saveBindingSQL, v.Binding.TenantID, v.Binding.ProductID, v.Binding.ProtocolID, v.Binding.Version, body(v.Binding)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
