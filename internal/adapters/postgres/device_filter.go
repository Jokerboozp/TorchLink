package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// deviceFilterSQL builds the shared WHERE clause; ports.DeviceFilter.Matches is
// the reference behaviour used by the memory adapter and tests.
func deviceFilterSQL(f ports.DeviceFilter) (string, []any) {
	args := []any{f.TenantID}
	where := []string{"d.tenant_id=$1"}
	arg := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
	if f.Role != "" {
		where = append(where, "COALESCE(NULLIF(d.body->>'deviceRole',''),'DIRECT') = "+arg(f.Role))
	}
	if f.RestrictProducts {
		products := f.ProductIDs
		if products == nil {
			products = []string{}
		}
		where = append(where, fmt.Sprintf("d.product_id = ANY(%s::text[])", arg(products)))
	}
	if f.RestrictDevices {
		devices := f.DeviceIDs
		if devices == nil {
			devices = []string{}
		}
		where = append(where, fmt.Sprintf("d.id = ANY(%s::text[])", arg(devices)))
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		pattern := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q) + "%"
		p := arg(pattern)
		where = append(where, fmt.Sprintf(`(d.id ILIKE %s ESCAPE '\' OR COALESCE(d.body->>'name','') ILIKE %s ESCAPE '\')`, p, p))
	}
	if f.Status != "" {
		where = append(where, "d.status = "+arg(f.Status))
	}
	if f.Runtime != "" {
		where = append(where, "COALESCE(NULLIF(s.business_status,''),'NEVER_SEEN') = "+arg(f.Runtime))
	}
	return strings.Join(where, " AND "), args
}

func (r *Repository) ListManagedDevicesFiltered(ctx context.Context, f ports.DeviceFilter, limit, offset int) ([]model.ManagedDevice, int, error) {
	limit, offset = normalizePage(limit, offset)
	where, args := deviceFilterSQL(f)
	from := " FROM device_registry d LEFT JOIN device_state s ON s.tenant_id=d.tenant_id AND s.device_id=d.id WHERE " + where
	var total int
	if err := r.pool.QueryRow(ctx, "SELECT count(*)"+from, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	query := fmt.Sprintf("SELECT d.body,d.secret_hash%s ORDER BY d.updated_at DESC,d.id DESC LIMIT $%d OFFSET $%d", from, len(args)+1, len(args)+2)
	rows, err := r.pool.Query(ctx, query, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]model.ManagedDevice, 0, limit)
	for rows.Next() {
		var item model.ManagedDevice
		var body []byte
		if err = rows.Scan(&body, &item.SecretHash); err != nil {
			return nil, 0, err
		}
		if err = json.Unmarshal(body, &item); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}
