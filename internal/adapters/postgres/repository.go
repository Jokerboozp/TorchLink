package postgres

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

//go:embed schema.sql
var schema string

var ErrNotFound = errors.New("not found")

type Repository struct{ pool *pgxpool.Pool }

const countManagedDeviceChildrenSQL = `SELECT body->>'gatewayId' AS gateway_id,count(*) FROM device_registry WHERE tenant_id=$1 AND body->>'gatewayId' = ANY($2::text[]) GROUP BY body->>'gatewayId'`

func normalizePage(limit, offset int) (int, int) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func New(ctx context.Context, dsn string) (*Repository, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err = pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	r := &Repository{pool: pool}
	if err = r.Migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return r, nil
}
func (r *Repository) Migrate(ctx context.Context) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(728194602)`); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, schema); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (r *Repository) SaveProduct(ctx context.Context, v model.Product) error {
	b, _ := json.Marshal(v)
	_, err := r.pool.Exec(ctx, `INSERT INTO iot_product(tenant_id,id,status,protocol_package_id,body) VALUES($1,$2,$3,$4,$5) ON CONFLICT(tenant_id,id) DO UPDATE SET status=excluded.status,protocol_package_id=excluded.protocol_package_id,body=excluded.body,updated_at=now()`, v.TenantID, v.ID, v.Status, v.ProtocolPackageID, b)
	return err
}
func (r *Repository) GetProduct(ctx context.Context, tenant, id string) (model.Product, error) {
	var v model.Product
	var b []byte
	err := r.pool.QueryRow(ctx, `SELECT body FROM iot_product WHERE tenant_id=$1 AND id=$2`, tenant, id).Scan(&b)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, ErrNotFound
	}
	if err == nil {
		err = json.Unmarshal(b, &v)
	}
	return v, err
}
func (r *Repository) ListProducts(ctx context.Context, tenant string) ([]model.Product, error) {
	rows, err := r.pool.Query(ctx, `SELECT body FROM iot_product WHERE tenant_id=$1 ORDER BY updated_at DESC`, tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Product{}
	for rows.Next() {
		var b []byte
		var v model.Product
		if err = rows.Scan(&b); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(b, &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *Repository) ListProductsPage(ctx context.Context, tenant string, limit, offset int) ([]model.Product, int, error) {
	limit, offset = normalizePage(limit, offset)
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM iot_product WHERE tenant_id=$1`, tenant).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx, `SELECT body FROM iot_product WHERE tenant_id=$1 ORDER BY updated_at DESC,id DESC LIMIT $2 OFFSET $3`, tenant, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]model.Product, 0, limit)
	for rows.Next() {
		var body []byte
		var item model.Product
		if err = rows.Scan(&body); err != nil {
			return nil, 0, err
		}
		if err = json.Unmarshal(body, &item); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}
func (r *Repository) SaveProtocolPackage(ctx context.Context, v model.ProtocolPackage) error {
	b, _ := json.Marshal(v)
	_, err := r.pool.Exec(ctx, `INSERT INTO protocol_package(tenant_id,id,status,parser_type,body) VALUES($1,$2,$3,$4,$5) ON CONFLICT(tenant_id,id) DO UPDATE SET status=excluded.status,parser_type=excluded.parser_type,body=excluded.body,updated_at=now()`, v.TenantID, v.ID, v.Status, v.ParserType, b)
	return err
}
func (r *Repository) GetProtocolPackage(ctx context.Context, tenant, id string) (model.ProtocolPackage, error) {
	var v model.ProtocolPackage
	var b []byte
	err := r.pool.QueryRow(ctx, `SELECT body FROM protocol_package WHERE tenant_id=$1 AND id=$2`, tenant, id).Scan(&b)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, ErrNotFound
	}
	if err == nil {
		err = json.Unmarshal(b, &v)
	}
	return v, err
}
func (r *Repository) ListProtocolPackages(ctx context.Context, tenant string) ([]model.ProtocolPackage, error) {
	rows, err := r.pool.Query(ctx, `SELECT body FROM protocol_package WHERE tenant_id=$1 ORDER BY updated_at DESC`, tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.ProtocolPackage{}
	for rows.Next() {
		var b []byte
		var v model.ProtocolPackage
		if err = rows.Scan(&b); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(b, &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *Repository) ListProtocolPackagesPage(ctx context.Context, tenant string, limit, offset int) ([]model.ProtocolPackage, int, error) {
	limit, offset = normalizePage(limit, offset)
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM protocol_package WHERE tenant_id=$1`, tenant).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx, `SELECT body FROM protocol_package WHERE tenant_id=$1 ORDER BY updated_at DESC,id DESC LIMIT $2 OFFSET $3`, tenant, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]model.ProtocolPackage, 0, limit)
	for rows.Next() {
		var body []byte
		var item model.ProtocolPackage
		if err = rows.Scan(&body); err != nil {
			return nil, 0, err
		}
		if err = json.Unmarshal(body, &item); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (r *Repository) SaveProtocolDefinition(ctx context.Context, v model.ProtocolDefinition) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `INSERT INTO protocol_definition(tenant_id,id,body) VALUES($1,$2,$3) ON CONFLICT(tenant_id,id) DO UPDATE SET body=excluded.body,updated_at=now()`, v.TenantID, v.ID, b)
	return err
}
func (r *Repository) GetProtocolDefinition(ctx context.Context, tenant, id string) (model.ProtocolDefinition, error) {
	var v model.ProtocolDefinition
	var b []byte
	err := r.pool.QueryRow(ctx, `SELECT body FROM protocol_definition WHERE tenant_id=$1 AND id=$2`, tenant, id).Scan(&b)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, ErrNotFound
	}
	if err == nil {
		err = json.Unmarshal(b, &v)
	}
	return v, err
}
func (r *Repository) ListProtocolDefinitions(ctx context.Context, tenant string) ([]model.ProtocolDefinition, error) {
	rows, err := r.pool.Query(ctx, `SELECT body FROM protocol_definition WHERE ($1='' OR tenant_id=$1) ORDER BY updated_at DESC,id`, tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.ProtocolDefinition{}
	for rows.Next() {
		var b []byte
		var v model.ProtocolDefinition
		if err = rows.Scan(&b); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(b, &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *Repository) CreateProtocolRelease(ctx context.Context, v model.ProtocolRelease) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `INSERT INTO protocol_release(tenant_id,protocol_id,version,status,parser_type,body) VALUES($1,$2,$3,$4,$5,$6)`, v.TenantID, v.ProtocolID, v.Version, v.Status, v.ParserType, b)
	return err
}
func (r *Repository) GetProtocolRelease(ctx context.Context, tenant, protocolID, version string) (model.ProtocolRelease, error) {
	var v model.ProtocolRelease
	var b []byte
	err := r.pool.QueryRow(ctx, `SELECT body FROM protocol_release WHERE tenant_id=$1 AND protocol_id=$2 AND version=$3`, tenant, protocolID, version).Scan(&b)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, ErrNotFound
	}
	if err == nil {
		err = json.Unmarshal(b, &v)
	}
	return v, err
}
func (r *Repository) ListProtocolReleases(ctx context.Context, tenant, protocolID string) ([]model.ProtocolRelease, error) {
	rows, err := r.pool.Query(ctx, `SELECT body FROM protocol_release WHERE ($1='' OR tenant_id=$1) AND ($2='' OR protocol_id=$2) ORDER BY created_at DESC,protocol_id,version`, tenant, protocolID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.ProtocolRelease{}
	for rows.Next() {
		var b []byte
		var v model.ProtocolRelease
		if err = rows.Scan(&b); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(b, &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *Repository) UpdateProtocolReleaseStatus(ctx context.Context, tenant, protocolID, version, status string, publishedAt int64) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var b []byte
	if err = tx.QueryRow(ctx, `SELECT body FROM protocol_release WHERE tenant_id=$1 AND protocol_id=$2 AND version=$3 FOR UPDATE`, tenant, protocolID, version).Scan(&b); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	var v model.ProtocolRelease
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	v.Status = status
	v.PublishedAt = publishedAt
	b, _ = json.Marshal(v)
	if _, err = tx.Exec(ctx, `UPDATE protocol_release SET status=$4,body=$5 WHERE tenant_id=$1 AND protocol_id=$2 AND version=$3`, tenant, protocolID, version, status, b); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (r *Repository) CreatePointTableRelease(ctx context.Context, v model.PointTableRelease) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `INSERT INTO point_table_release(tenant_id,protocol_id,version,source_sha256,body) VALUES($1,$2,$3,$4,$5)`, v.TenantID, v.ProtocolID, v.Version, v.SourceSHA256, b)
	return err
}
func (r *Repository) GetPointTableRelease(ctx context.Context, tenant, protocolID, version string) (model.PointTableRelease, error) {
	var v model.PointTableRelease
	var b []byte
	err := r.pool.QueryRow(ctx, `SELECT body FROM point_table_release WHERE tenant_id=$1 AND protocol_id=$2 AND version=$3`, tenant, protocolID, version).Scan(&b)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, ErrNotFound
	}
	if err == nil {
		err = json.Unmarshal(b, &v)
	}
	return v, err
}
func (r *Repository) SaveProductProtocolBinding(ctx context.Context, v model.ProductProtocolBinding) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `INSERT INTO product_protocol_binding(tenant_id,product_id,protocol_id,version,body) VALUES($1,$2,$3,$4,$5) ON CONFLICT(tenant_id,product_id) DO UPDATE SET protocol_id=excluded.protocol_id,version=excluded.version,body=excluded.body,updated_at=now()`, v.TenantID, v.ProductID, v.ProtocolID, v.Version, b)
	return err
}
func (r *Repository) GetProductProtocolBinding(ctx context.Context, tenant, productID string) (model.ProductProtocolBinding, error) {
	var v model.ProductProtocolBinding
	var b []byte
	err := r.pool.QueryRow(ctx, `SELECT body FROM product_protocol_binding WHERE tenant_id=$1 AND product_id=$2`, tenant, productID).Scan(&b)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, ErrNotFound
	}
	if err == nil {
		err = json.Unmarshal(b, &v)
	}
	return v, err
}
func (r *Repository) SaveDeviceAccessProfile(ctx context.Context, v model.DeviceAccessProfile) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `INSERT INTO device_access_profile(tenant_id,id,device_id,product_id,enabled,body) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(tenant_id,id) DO UPDATE SET device_id=excluded.device_id,product_id=excluded.product_id,enabled=excluded.enabled,body=excluded.body,updated_at=now()`, v.TenantID, v.ID, v.DeviceID, v.ProductID, v.Enabled, b)
	return err
}
func (r *Repository) GetDeviceAccessProfile(ctx context.Context, tenant, id string) (model.DeviceAccessProfile, error) {
	var v model.DeviceAccessProfile
	var b []byte
	err := r.pool.QueryRow(ctx, `SELECT body FROM device_access_profile WHERE tenant_id=$1 AND id=$2`, tenant, id).Scan(&b)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, ErrNotFound
	}
	if err == nil {
		err = json.Unmarshal(b, &v)
	}
	return v, err
}
func (r *Repository) ListDeviceAccessProfiles(ctx context.Context, tenant string) ([]model.DeviceAccessProfile, error) {
	rows, err := r.pool.Query(ctx, `SELECT body FROM device_access_profile WHERE ($1='' OR tenant_id=$1) ORDER BY updated_at DESC,id`, tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.DeviceAccessProfile{}
	for rows.Next() {
		var b []byte
		var v model.DeviceAccessProfile
		if err = rows.Scan(&b); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(b, &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *Repository) SaveManagedDevice(ctx context.Context, v model.ManagedDevice) error {
	b, _ := json.Marshal(v)
	_, err := r.pool.Exec(ctx, `INSERT INTO device_registry(tenant_id,id,product_id,status,access_key,secret_hash,body) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(tenant_id,id) DO UPDATE SET product_id=excluded.product_id,status=excluded.status,access_key=excluded.access_key,secret_hash=excluded.secret_hash,body=excluded.body,updated_at=now()`, v.TenantID, v.ID, v.ProductID, v.Status, v.AccessKey, v.SecretHash, b)
	return err
}
func (r *Repository) scanManagedDevice(row pgx.Row) (model.ManagedDevice, error) {
	var v model.ManagedDevice
	var b []byte
	var hash string
	err := row.Scan(&b, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, err
	}
	err = json.Unmarshal(b, &v)
	v.SecretHash = hash
	return v, err
}
func (r *Repository) GetManagedDevice(ctx context.Context, tenant, id string) (model.ManagedDevice, error) {
	return r.scanManagedDevice(r.pool.QueryRow(ctx, `SELECT body,secret_hash FROM device_registry WHERE tenant_id=$1 AND id=$2`, tenant, id))
}
func (r *Repository) GetManagedDeviceByAccessKey(ctx context.Context, accessKey string) (model.ManagedDevice, error) {
	return r.scanManagedDevice(r.pool.QueryRow(ctx, `SELECT body,secret_hash FROM device_registry WHERE access_key=$1`, accessKey))
}
func (r *Repository) ListManagedDevices(ctx context.Context, tenant string) ([]model.ManagedDevice, error) {
	rows, err := r.pool.Query(ctx, `SELECT body,secret_hash FROM device_registry WHERE tenant_id=$1 ORDER BY updated_at DESC`, tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.ManagedDevice{}
	for rows.Next() {
		var v model.ManagedDevice
		var b []byte
		if err = rows.Scan(&b, &v.SecretHash); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(b, &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *Repository) ListManagedDevicesPage(ctx context.Context, tenant string, limit, offset int) ([]model.ManagedDevice, int, error) {
	limit, offset = normalizePage(limit, offset)
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM device_registry WHERE tenant_id=$1`, tenant).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx, `SELECT body,secret_hash FROM device_registry WHERE tenant_id=$1 ORDER BY updated_at DESC,id DESC LIMIT $2 OFFSET $3`, tenant, limit, offset)
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
func (r *Repository) CountManagedDeviceChildren(ctx context.Context, tenant string, ids []string) (map[string]int, error) {
	counts := make(map[string]int, len(ids))
	if len(ids) == 0 {
		return counts, nil
	}
	rows, err := r.pool.Query(ctx, countManagedDeviceChildrenSQL, tenant, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var gatewayID string
		var count int
		if err = rows.Scan(&gatewayID, &count); err != nil {
			return nil, err
		}
		counts[gatewayID] = count
	}
	return counts, rows.Err()
}
func (r *Repository) SaveRawIndex(ctx context.Context, v model.RawArchiveIndex) (bool, error) {
	tag, err := r.pool.Exec(ctx, `INSERT INTO raw_archive_index(tenant_id,product_id,device_id,message_id,protocol,payload_format,object_bucket,object_key,object_offset,payload_hash,payload_size,received_at,archived_at,published_at,publish_attempts,last_publish_error) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16) ON CONFLICT DO NOTHING`, v.TenantID, v.ProductID, v.DeviceID, v.MessageID, v.Protocol, v.PayloadFormat, v.ObjectBucket, v.ObjectKey, v.ObjectOffset, v.PayloadHash, v.PayloadSize, v.ReceivedAt, v.ArchivedAt, v.PublishedAt, v.PublishAttempts, v.LastPublishError)
	return tag.RowsAffected() == 1, err
}

func (r *Repository) SaveRawMessage(ctx context.Context, v model.RawMessage) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	tag, err := r.pool.Exec(ctx, `INSERT INTO raw_message_log(tenant_id,message_id,product_id,device_id,protocol,payload_format,payload_hash,payload_size,received_at,stored_at,body) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT DO NOTHING`, v.TenantID, v.MessageID, v.ProductID, v.DeviceID, v.Protocol, v.PayloadFormat, v.PayloadHash(), len(v.Payload), v.ReceivedAt, time.Now().UnixMilli(), body)
	if err == nil && tag.RowsAffected() == 0 {
		existing, getErr := r.GetRawMessage(ctx, v.TenantID, v.MessageID)
		if getErr != nil {
			return getErr
		}
		if existing.PayloadHash() != v.PayloadHash() || existing.DeviceID != v.DeviceID || existing.ProductID != v.ProductID {
			return model.ErrRawConflict
		}
	}

	return err
}

func (r *Repository) GetRawMessage(ctx context.Context, tenant, messageID string) (model.RawMessage, error) {
	var value model.RawMessage
	var body []byte
	err := r.pool.QueryRow(ctx, `SELECT body FROM raw_message_log WHERE tenant_id=$1 AND message_id=$2`, tenant, messageID).Scan(&body)
	if errors.Is(err, pgx.ErrNoRows) {
		return value, ErrNotFound
	}
	if err == nil {
		err = json.Unmarshal(body, &value)
	}
	return value, err
}

func (r *Repository) MarkRawPublished(ctx context.Context, tenant, messageID string, publishedAt int64, lastError string) error {
	_, err := r.pool.Exec(ctx, `UPDATE raw_archive_index SET publish_attempts=publish_attempts+1,last_publish_error=$4,published_at=CASE WHEN $4='' THEN $3 ELSE published_at END WHERE tenant_id=$1 AND message_id=$2`, tenant, messageID, publishedAt, lastError)
	return err
}
func (r *Repository) ListPendingRawIndexes(ctx context.Context, limit int) ([]model.RawArchiveIndex, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, `SELECT message_id,tenant_id,product_id,device_id,protocol,payload_format,object_bucket,object_key,object_offset,payload_hash,payload_size,received_at,archived_at,published_at,publish_attempts,last_publish_error,parse_attempted_at,parse_error FROM raw_archive_index WHERE published_at=0 ORDER BY archived_at LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.RawArchiveIndex{}
	for rows.Next() {
		var v model.RawArchiveIndex
		if err = scanRawIndex(rows, &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

type rowScanner interface{ Scan(...any) error }

func scanRawIndex(row rowScanner, v *model.RawArchiveIndex) error {
	return row.Scan(&v.MessageID, &v.TenantID, &v.ProductID, &v.DeviceID, &v.Protocol, &v.PayloadFormat, &v.ObjectBucket, &v.ObjectKey, &v.ObjectOffset, &v.PayloadHash, &v.PayloadSize, &v.ReceivedAt, &v.ArchivedAt, &v.PublishedAt, &v.PublishAttempts, &v.LastPublishError, &v.ParseAttemptedAt, &v.ParseError)
}
func (r *Repository) GetRawIndex(ctx context.Context, tenant, messageID string) (model.RawArchiveIndex, error) {
	var v model.RawArchiveIndex
	err := scanRawIndex(r.pool.QueryRow(ctx, `SELECT message_id,tenant_id,product_id,device_id,protocol,payload_format,object_bucket,object_key,object_offset,payload_hash,payload_size,received_at,archived_at,published_at,publish_attempts,last_publish_error,parse_attempted_at,parse_error FROM raw_archive_index WHERE tenant_id=$1 AND message_id=$2`, tenant, messageID), &v)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, ErrNotFound
	}
	return v, err
}
func (r *Repository) ListRawIndexes(ctx context.Context, f ports.RawFilter) ([]model.RawArchiveIndex, error) {
	q := `SELECT message_id,tenant_id,product_id,device_id,protocol,payload_format,object_bucket,object_key,object_offset,payload_hash,payload_size,received_at,archived_at,published_at,publish_attempts,last_publish_error,parse_attempted_at,parse_error FROM raw_archive_index WHERE ($1='' OR tenant_id=$1) AND ($2='' OR product_id=$2) AND ($3='' OR device_id=$3) AND ($4::bigint=0 OR received_at >= $4) AND ($5::bigint=0 OR received_at <= $5) ORDER BY received_at DESC LIMIT $6 OFFSET $7`
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, q, f.TenantID, f.ProductID, f.DeviceID, f.Start, f.End, limit, f.Offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.RawArchiveIndex{}
	for rows.Next() {
		var v model.RawArchiveIndex
		if err := scanRawIndex(rows, &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *Repository) CountRawIndexes(ctx context.Context, f ports.RawFilter) (int, error) {
	var total int
	err := r.pool.QueryRow(ctx, `SELECT count(*) FROM raw_archive_index WHERE ($1='' OR tenant_id=$1) AND ($2='' OR product_id=$2) AND ($3='' OR device_id=$3) AND ($4::bigint=0 OR received_at >= $4) AND ($5::bigint=0 OR received_at <= $5)`, f.TenantID, f.ProductID, f.DeviceID, f.Start, f.End).Scan(&total)
	return total, err
}
func (r *Repository) SaveStandardMessage(ctx context.Context, v model.StandardMessage) error {
	_, err := r.SaveStandardMessageIfAbsent(ctx, v)
	return err
}
func (r *Repository) SaveStandardMessageIfAbsent(ctx context.Context, v model.StandardMessage) (bool, error) {
	body, _ := json.Marshal(v)
	props, _ := json.Marshal(v.Properties)
	event, _ := json.Marshal(v.Event)
	tags, _ := json.Marshal(v.Tags)
	tag, err := r.pool.Exec(ctx, `INSERT INTO standard_message(tenant_id,message_id,raw_message_id,product_id,device_id,message_type,ts,properties,event,tags,body) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT DO NOTHING`, v.TenantID, v.MessageID, v.RawMessageID, v.ProductID, v.DeviceID, v.MessageType, v.Timestamp, props, event, tags, body)
	return tag.RowsAffected() == 1, err
}
func (r *Repository) ClaimStandardMessage(ctx context.Context, v model.StandardMessage) (bool, bool, error) {
	created, err := r.SaveStandardMessageIfAbsent(ctx, v)
	if err != nil || created {
		return created, created, err
	}
	var processed int64
	err = r.pool.QueryRow(ctx, `SELECT processed_at FROM standard_message WHERE tenant_id=$1 AND message_id=$2`, v.TenantID, v.MessageID).Scan(&processed)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, false, ErrNotFound
	}
	return processed == 0, false, err
}
func (r *Repository) MarkStandardMessageProcessed(ctx context.Context, tenant, messageID string) error {
	tag, err := r.pool.Exec(ctx, `UPDATE standard_message SET processed_at=1 WHERE tenant_id=$1 AND message_id=$2`, tenant, messageID)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}
func (r *Repository) getStandard(ctx context.Context, query string, args ...any) (model.StandardMessage, error) {
	var v model.StandardMessage
	var body []byte
	err := r.pool.QueryRow(ctx, query, args...).Scan(&body)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, ErrNotFound
	}
	if err == nil {
		err = json.Unmarshal(body, &v)
	}
	return v, err
}
func (r *Repository) GetStandardMessageByRaw(ctx context.Context, tenant, rawID string) (model.StandardMessage, error) {
	return r.getStandard(ctx, `SELECT body FROM standard_message WHERE tenant_id=$1 AND raw_message_id=$2 ORDER BY ts DESC LIMIT 1`, tenant, rawID)
}
func (r *Repository) GetLatestMessage(ctx context.Context, tenant, device string) (model.StandardMessage, error) {
	return r.getStandard(ctx, `SELECT body FROM standard_message WHERE tenant_id=$1 AND device_id=$2 ORDER BY ts DESC LIMIT 1`, tenant, device)
}
func (r *Repository) PropertyHistory(ctx context.Context, tenant, device, property string, start, end int64, limit int) ([]map[string]any, error) {
	if limit <= 0 {
		limit = 1000
	}
	rows, err := r.pool.Query(ctx, `SELECT ts, properties -> $3, message_id FROM standard_message WHERE tenant_id=$1 AND device_id=$2 AND properties ? $3 AND ts >= $4::bigint AND ($5::bigint=0 OR ts <= $5) ORDER BY ts DESC LIMIT $6`, tenant, device, property, start, end, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var ts int64
		var raw []byte
		var id string
		if err := rows.Scan(&ts, &raw, &id); err != nil {
			return nil, err
		}
		var value any
		_ = json.Unmarshal(raw, &value)
		out = append(out, map[string]any{"timestamp": ts, "value": value, "messageId": id})
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, rows.Err()
}
func (r *Repository) PropertyHistoryPage(ctx context.Context, tenant, device, property string, start, end int64, limit, offset int) ([]map[string]any, int, error) {
	limit, offset = normalizePage(limit, offset)
	where := `WHERE tenant_id=$1 AND device_id=$2 AND properties ? $3 AND ts >= $4::bigint AND ($5::bigint=0 OR ts <= $5)`
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM standard_message `+where, tenant, device, property, start, end).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx, `SELECT ts, properties -> $3, message_id FROM standard_message `+where+` ORDER BY ts DESC, message_id DESC LIMIT $6 OFFSET $7`, tenant, device, property, start, end, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]map[string]any, 0, limit)
	for rows.Next() {
		var ts int64
		var raw []byte
		var id string
		if err := rows.Scan(&ts, &raw, &id); err != nil {
			return nil, 0, err
		}
		var value any
		_ = json.Unmarshal(raw, &value)
		items = append(items, map[string]any{"timestamp": ts, "value": value, "messageId": id})
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
	return items, total, nil
}
func (r *Repository) UpsertDeviceState(ctx context.Context, v model.DeviceState) error {
	b, _ := json.Marshal(v)
	_, err := r.pool.Exec(ctx, `INSERT INTO device_state(tenant_id,device_id,product_id,business_status,last_seen_at,body) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(tenant_id,device_id) DO UPDATE SET product_id=excluded.product_id,business_status=excluded.business_status,last_seen_at=excluded.last_seen_at,body=excluded.body,updated_at=now()`, v.TenantID, v.DeviceID, v.ProductID, v.BusinessStatus, v.LastSeenAt, b)
	return err
}
func (r *Repository) GetDeviceState(ctx context.Context, tenant, device string) (model.DeviceState, error) {
	var v model.DeviceState
	var b []byte
	err := r.pool.QueryRow(ctx, `SELECT body FROM device_state WHERE tenant_id=$1 AND device_id=$2`, tenant, device).Scan(&b)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, ErrNotFound
	}
	if err == nil {
		err = json.Unmarshal(b, &v)
	}
	return v, err
}
func (r *Repository) ListDeviceStates(ctx context.Context, tenant string) ([]model.DeviceState, error) {
	rows, err := r.pool.Query(ctx, `SELECT body FROM device_state WHERE ($1='' OR tenant_id=$1)`, tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.DeviceState{}
	for rows.Next() {
		var b []byte
		var v model.DeviceState
		if err := rows.Scan(&b); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(b, &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *Repository) ListDeviceStatesPage(ctx context.Context, tenant string, limit, offset int) ([]model.DeviceState, int, error) {
	limit, offset = normalizePage(limit, offset)
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM device_state WHERE ($1='' OR tenant_id=$1)`, tenant).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx, `SELECT body FROM device_state WHERE ($1='' OR tenant_id=$1) ORDER BY last_seen_at DESC,device_id LIMIT $2 OFFSET $3`, tenant, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]model.DeviceState, 0, limit)
	for rows.Next() {
		var body []byte
		var item model.DeviceState
		if err = rows.Scan(&body); err != nil {
			return nil, 0, err
		}
		if err = json.Unmarshal(body, &item); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}
func (r *Repository) ListUnregisteredDeviceStatesPage(ctx context.Context, tenant string, limit, offset int) ([]model.DeviceState, int, error) {
	limit, offset = normalizePage(limit, offset)
	where := `WHERE ds.tenant_id=$1 AND NOT EXISTS (SELECT 1 FROM device_registry dr WHERE dr.tenant_id=ds.tenant_id AND dr.id=ds.device_id)`
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM device_state ds `+where, tenant).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx, `SELECT ds.body FROM device_state ds `+where+` ORDER BY ds.last_seen_at DESC,ds.device_id LIMIT $2 OFFSET $3`, tenant, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]model.DeviceState, 0, limit)
	for rows.Next() {
		var body []byte
		var item model.DeviceState
		if err = rows.Scan(&body); err != nil {
			return nil, 0, err
		}
		if err = json.Unmarshal(body, &item); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}
func (r *Repository) CountDeviceStates(ctx context.Context, tenant string, unregisteredOnly bool) (int, int, error) {
	where := `WHERE ds.tenant_id=$1`
	if unregisteredOnly {
		where += ` AND NOT EXISTS (SELECT 1 FROM device_registry dr WHERE dr.tenant_id=ds.tenant_id AND dr.id=ds.device_id)`
	}
	var total, online int
	if err := r.pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER (WHERE ds.business_status IN ('ONLINE','ALARM')) FROM device_state ds `+where, tenant).Scan(&total, &online); err != nil {
		return 0, 0, err
	}
	return total, online, nil
}
func (r *Repository) SaveDeviceStateEvent(ctx context.Context, v model.DeviceState) error {
	b, _ := json.Marshal(v)
	_, err := r.pool.Exec(ctx, `INSERT INTO device_state_event(tenant_id,device_id,business_status,body) VALUES($1,$2,$3,$4)`, v.TenantID, v.DeviceID, v.BusinessStatus, b)
	return err
}
func (r *Repository) SaveRule(ctx context.Context, v model.AlarmRule) error {
	b, _ := json.Marshal(v)
	_, err := r.pool.Exec(ctx, `INSERT INTO alarm_rule(tenant_id,id,product_id,enabled,body) VALUES($1,$2,$3,$4,$5) ON CONFLICT(tenant_id,id) DO UPDATE SET product_id=excluded.product_id,enabled=excluded.enabled,body=excluded.body,updated_at=now()`, v.TenantID, v.ID, v.ProductID, v.Enabled, b)
	return err
}
func (r *Repository) ListRules(ctx context.Context, tenant string) ([]model.AlarmRule, error) {
	rows, err := r.pool.Query(ctx, `SELECT body FROM alarm_rule WHERE ($1='' OR tenant_id=$1) ORDER BY updated_at DESC`, tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.AlarmRule{}
	for rows.Next() {
		var b []byte
		var v model.AlarmRule
		if err := rows.Scan(&b); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(b, &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *Repository) ListRulesPage(ctx context.Context, tenant string, limit, offset int) ([]model.AlarmRule, int, error) {
	limit, offset = normalizePage(limit, offset)
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM alarm_rule WHERE ($1='' OR tenant_id=$1)`, tenant).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx, `SELECT body FROM alarm_rule WHERE ($1='' OR tenant_id=$1) ORDER BY updated_at DESC,id DESC LIMIT $2 OFFSET $3`, tenant, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]model.AlarmRule, 0, limit)
	for rows.Next() {
		var body []byte
		var item model.AlarmRule
		if err = rows.Scan(&body); err != nil {
			return nil, 0, err
		}
		if err = json.Unmarshal(body, &item); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}
func (r *Repository) DeleteRule(ctx context.Context, tenant, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM alarm_rule WHERE tenant_id=$1 AND id=$2`, tenant, id)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}
func (r *Repository) SaveRulePending(ctx context.Context, tenant, ruleID, deviceID string, since int64) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO alarm_rule_pending(tenant_id,rule_id,device_id,since_at) VALUES($1,$2,$3,$4) ON CONFLICT(tenant_id,rule_id,device_id) DO UPDATE SET since_at=LEAST(alarm_rule_pending.since_at, EXCLUDED.since_at), updated_at=now()`, tenant, ruleID, deviceID, since)
	return err
}
func (r *Repository) GetRulePending(ctx context.Context, tenant, ruleID, deviceID string) (int64, bool, error) {
	var since int64
	err := r.pool.QueryRow(ctx, `SELECT since_at FROM alarm_rule_pending WHERE tenant_id=$1 AND rule_id=$2 AND device_id=$3`, tenant, ruleID, deviceID).Scan(&since)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	return since, err == nil, err
}
func (r *Repository) DeleteRulePending(ctx context.Context, tenant, ruleID, deviceID string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM alarm_rule_pending WHERE tenant_id=$1 AND rule_id=$2 AND device_id=$3`, tenant, ruleID, deviceID)
	return err
}
func (r *Repository) DeleteRulePendings(ctx context.Context, tenant, ruleID string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM alarm_rule_pending WHERE tenant_id=$1 AND rule_id=$2`, tenant, ruleID)
	return err
}
func (r *Repository) UpsertAlarm(ctx context.Context, v model.Alarm) (model.Alarm, bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return v, false, err
	}
	defer tx.Rollback(ctx)
	var body []byte
	err = tx.QueryRow(ctx, `SELECT body FROM alarm_record WHERE tenant_id=$1 AND device_id=$2 AND rule_id=$3 AND status IN ('ACTIVE','ACKED') FOR UPDATE`, v.TenantID, v.DeviceID, v.RuleID).Scan(&body)
	if err == nil {
		var old model.Alarm
		if err = json.Unmarshal(body, &old); err != nil {
			return v, false, err
		}
		if v.TriggerID != "" && old.TriggerID == v.TriggerID {
			if err = tx.Commit(ctx); err != nil {
				return v, false, err
			}
			return old, false, nil
		}
		old.LastTriggeredAt = v.LastTriggeredAt
		old.TriggerCount++
		if v.Confidence > old.Confidence {
			old.Confidence = v.Confidence
		}
		body, _ = json.Marshal(old)
		_, err = tx.Exec(ctx, `UPDATE alarm_record SET last_triggered_at=$3,body=$4 WHERE tenant_id=$1 AND id=$2`, old.TenantID, old.ID, old.LastTriggeredAt, body)
		if err != nil {
			return v, false, err
		}
		if err = tx.Commit(ctx); err != nil {
			return v, false, err
		}
		return old, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return v, false, err
	}
	body, _ = json.Marshal(v)
	_, err = tx.Exec(ctx, `INSERT INTO alarm_record(tenant_id,id,rule_id,device_id,status,level,source,last_triggered_at,body) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, v.TenantID, v.ID, v.RuleID, v.DeviceID, v.Status, v.AlarmLevel, v.Source, v.LastTriggeredAt, body)
	if err != nil {
		return v, false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return v, false, err
	}
	return v, true, nil
}
func (r *Repository) GetAlarm(ctx context.Context, tenant, id string) (model.Alarm, error) {
	var v model.Alarm
	var b []byte
	err := r.pool.QueryRow(ctx, `SELECT body FROM alarm_record WHERE tenant_id=$1 AND id=$2`, tenant, id).Scan(&b)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, ErrNotFound
	}
	if err == nil {
		err = json.Unmarshal(b, &v)
	}
	return v, err
}
func (r *Repository) ListAlarms(ctx context.Context, f ports.AlarmFilter) ([]model.Alarm, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, `SELECT body FROM alarm_record WHERE ($1='' OR tenant_id=$1) AND ($2='' OR device_id=$2) AND ($3='' OR status=$3) AND ($4='' OR level=$4) AND ($5='' OR source=$5) AND ($6::bigint=0 OR last_triggered_at >= $6) AND ($7::bigint=0 OR last_triggered_at <= $7) ORDER BY last_triggered_at DESC LIMIT $8 OFFSET $9`, f.TenantID, f.DeviceID, f.Status, f.Level, f.Source, f.Start, f.End, limit, f.Offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Alarm{}
	for rows.Next() {
		var b []byte
		var v model.Alarm
		if err := rows.Scan(&b); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(b, &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *Repository) CountAlarms(ctx context.Context, f ports.AlarmFilter) (int, error) {
	var total int
	err := r.pool.QueryRow(ctx, `SELECT count(*) FROM alarm_record WHERE ($1='' OR tenant_id=$1) AND ($2='' OR device_id=$2) AND ($3='' OR status=$3) AND ($4='' OR level=$4) AND ($5='' OR source=$5) AND ($6::bigint=0 OR last_triggered_at >= $6) AND ($7::bigint=0 OR last_triggered_at <= $7)`, f.TenantID, f.DeviceID, f.Status, f.Level, f.Source, f.Start, f.End).Scan(&total)
	return total, err
}
func (r *Repository) UpdateAlarm(ctx context.Context, v model.Alarm) error {
	b, _ := json.Marshal(v)
	tag, err := r.pool.Exec(ctx, `UPDATE alarm_record SET status=$3,level=$4,last_triggered_at=$5,body=$6 WHERE tenant_id=$1 AND id=$2`, v.TenantID, v.ID, v.Status, v.AlarmLevel, v.LastTriggeredAt, b)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}
func (r *Repository) SaveVideoEvent(ctx context.Context, v model.VideoAlarmEvent) (bool, error) {
	b, _ := json.Marshal(v)
	tag, err := r.pool.Exec(ctx, `INSERT INTO video_alarm_event(tenant_id,event_id,camera_id,alarm_type,event_time,body) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, v.TenantID, v.EventID, v.CameraID, v.AlarmType, v.EventTime, b)
	return tag.RowsAffected() == 1, err
}
func (r *Repository) UpdateVideoEvent(ctx context.Context, v model.VideoAlarmEvent) error {
	b, _ := json.Marshal(v)
	_, err := r.pool.Exec(ctx, `UPDATE video_alarm_event SET body=$3,alarm_type=$4,event_time=$5 WHERE tenant_id=$1 AND event_id=$2`, v.TenantID, v.EventID, b, v.AlarmType, v.EventTime)
	return err
}
func (r *Repository) ListPendingVideoEvents(ctx context.Context, limit int) ([]model.VideoAlarmEvent, error) {
	rows, err := r.pool.Query(ctx, `SELECT body FROM video_alarm_event WHERE body->'raw'->>'mediaTransferStatus' IN ('PENDING','FAILED') ORDER BY event_time LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.VideoAlarmEvent{}
	for rows.Next() {
		var b []byte
		if err = rows.Scan(&b); err != nil {
			return nil, err
		}
		var v model.VideoAlarmEvent
		if err = json.Unmarshal(b, &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

const videoCameraMappingColumns = `tenant_id,camera_id,coalesce(camera_name,''),coalesce(brand,''),coalesce(camera_point,''),coalesce(device_id,''),coalesce(ingest_mode,'direct'),coalesce(project_id,''),coalesce(city_code,''),coalesce(district_code,''),coalesce(building,''),coalesce(floor,''),coalesce(room,''),coalesce(area_id,''),related_device_ids,related_floor_ids,related_room_ids,coalesce(video_platform_id,''),coalesce(stream_url,''),coalesce(stream_type,''),coalesce(sdk_endpoint,''),coalesce(sdk_camera_id,''),coalesce(sdk_credential_ref,''),enabled`

func (r *Repository) SaveVideoCameraMapping(ctx context.Context, v model.VideoCameraMapping) error {
	legacyDeviceIDs := cleanUniqueStrings(v.RelatedDeviceIDs)
	if v.DeviceID == "" && len(legacyDeviceIDs) == 1 {
		v.DeviceID = legacyDeviceIDs[0]
	}
	if len(legacyDeviceIDs) > 1 || v.DeviceID != "" && len(legacyDeviceIDs) == 1 && legacyDeviceIDs[0] != v.DeviceID {
		return errors.New("a camera can be associated with at most one device")
	}
	deviceIDs := []string{}
	if v.DeviceID != "" {
		deviceIDs = []string{v.DeviceID}
	}
	deviceJSON, _ := json.Marshal(deviceIDs)
	floorJSON, _ := json.Marshal([]string{})
	roomJSON, _ := json.Marshal([]string{})
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `INSERT INTO video_camera_mapping(tenant_id,camera_id,camera_name,brand,camera_point,device_id,ingest_mode,project_id,city_code,district_code,building,floor,room,area_id,related_device_ids,related_floor_ids,related_room_ids,video_platform_id,stream_url,stream_type,sdk_endpoint,sdk_camera_id,sdk_credential_ref,enabled) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24) ON CONFLICT(tenant_id,camera_id) DO UPDATE SET camera_name=excluded.camera_name,brand=excluded.brand,camera_point=excluded.camera_point,device_id=excluded.device_id,ingest_mode=excluded.ingest_mode,project_id=excluded.project_id,city_code=excluded.city_code,district_code=excluded.district_code,building=excluded.building,floor=excluded.floor,room=excluded.room,area_id=excluded.area_id,related_device_ids=excluded.related_device_ids,related_floor_ids=excluded.related_floor_ids,related_room_ids=excluded.related_room_ids,video_platform_id=excluded.video_platform_id,stream_url=excluded.stream_url,stream_type=excluded.stream_type,sdk_endpoint=excluded.sdk_endpoint,sdk_camera_id=excluded.sdk_camera_id,sdk_credential_ref=excluded.sdk_credential_ref,enabled=excluded.enabled`, v.TenantID, v.CameraID, v.CameraName, v.Brand, v.CameraPoint, v.DeviceID, v.IngestMode, v.ProjectID, v.CityCode, v.DistrictCode, v.Building, v.Floor, v.Room, v.AreaID, deviceJSON, floorJSON, roomJSON, v.VideoPlatformID, v.StreamURL, v.StreamType, v.SDKEndpoint, v.SDKCameraID, v.SDKCredentialRef, v.Enabled); err != nil {
		return err
	}
	if err = replaceVideoCameraRelationsTx(ctx, tx, v.TenantID, v.CameraID, videoRelations(v)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (r *Repository) scanVideoMapping(row rowScanner) (model.VideoCameraMapping, error) {
	var v model.VideoCameraMapping
	var related, floors, rooms []byte
	err := row.Scan(&v.TenantID, &v.CameraID, &v.CameraName, &v.Brand, &v.CameraPoint, &v.DeviceID, &v.IngestMode, &v.ProjectID, &v.CityCode, &v.DistrictCode, &v.Building, &v.Floor, &v.Room, &v.AreaID, &related, &floors, &rooms, &v.VideoPlatformID, &v.StreamURL, &v.StreamType, &v.SDKEndpoint, &v.SDKCameraID, &v.SDKCredentialRef, &v.Enabled)
	if err == nil {
		_ = json.Unmarshal(related, &v.RelatedDeviceIDs)
		_ = json.Unmarshal(floors, &v.RelatedFloorIDs)
		_ = json.Unmarshal(rooms, &v.RelatedRoomIDs)
		if v.DeviceID == "" && len(v.RelatedDeviceIDs) == 1 {
			v.DeviceID = v.RelatedDeviceIDs[0]
		}
	}
	return v, err
}
func (r *Repository) GetVideoCameraMapping(ctx context.Context, tenant, camera string) (model.VideoCameraMapping, error) {
	v, err := r.scanVideoMapping(r.pool.QueryRow(ctx, `SELECT `+videoCameraMappingColumns+` FROM video_camera_mapping WHERE tenant_id=$1 AND camera_id=$2`, tenant, camera))
	if errors.Is(err, pgx.ErrNoRows) {
		return v, ErrNotFound
	}
	return v, err
}
func (r *Repository) ListVideoCameraMappings(ctx context.Context, tenant string) ([]model.VideoCameraMapping, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+videoCameraMappingColumns+` FROM video_camera_mapping WHERE tenant_id=$1 ORDER BY camera_id`, tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.VideoCameraMapping{}
	for rows.Next() {
		v, e := r.scanVideoMapping(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *Repository) ListVideoCameraMappingsPage(ctx context.Context, tenant string, limit, offset int) ([]model.VideoCameraMapping, int, error) {
	limit, offset = normalizePage(limit, offset)
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM video_camera_mapping WHERE tenant_id=$1`, tenant).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx, `SELECT `+videoCameraMappingColumns+` FROM video_camera_mapping WHERE tenant_id=$1 ORDER BY camera_id LIMIT $2 OFFSET $3`, tenant, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]model.VideoCameraMapping, 0, limit)
	for rows.Next() {
		item, scanErr := r.scanVideoMapping(rows)
		if scanErr != nil {
			return nil, 0, scanErr
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}
func (r *Repository) ReplaceVideoCameraRelations(ctx context.Context, tenant, camera string, relations []model.VideoCameraRelation) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = replaceVideoCameraRelationsTx(ctx, tx, tenant, camera, relations); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func replaceVideoCameraRelationsTx(ctx context.Context, tx pgx.Tx, tenant, camera string, relations []model.VideoCameraRelation) error {
	if _, err := tx.Exec(ctx, `DELETE FROM video_camera_relation WHERE tenant_id=$1 AND camera_id=$2`, tenant, camera); err != nil {
		return err
	}
	deviceID := ""
	for _, relation := range relations {
		if relation.TargetID == "" || relation.RelationType != "device" {
			continue
		}
		if deviceID != "" && deviceID != relation.TargetID {
			return errors.New("a camera can be associated with at most one device")
		}
		deviceID = relation.TargetID
		if _, err := tx.Exec(ctx, `INSERT INTO video_camera_relation(tenant_id,camera_id,relation_type,target_id) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, tenant, camera, relation.RelationType, relation.TargetID); err != nil {
			return err
		}
	}
	deviceIDs := []string{}
	if deviceID != "" {
		deviceIDs = []string{deviceID}
	}
	floorIDs, roomIDs := []string{}, []string{}
	deviceJSON, _ := json.Marshal(deviceIDs)
	floorJSON, _ := json.Marshal(floorIDs)
	roomJSON, _ := json.Marshal(roomIDs)
	if _, err := tx.Exec(ctx, `UPDATE video_camera_mapping SET device_id=$3,related_device_ids=$4,related_floor_ids=$5,related_room_ids=$6 WHERE tenant_id=$1 AND camera_id=$2`, tenant, camera, deviceID, deviceJSON, floorJSON, roomJSON); err != nil {
		return err
	}
	return nil
}
func (r *Repository) ListVideoCameraRelations(ctx context.Context, tenant, camera string) ([]model.VideoCameraRelation, error) {
	rows, err := r.pool.Query(ctx, `SELECT tenant_id,camera_id,relation_type,target_id FROM video_camera_relation WHERE tenant_id=$1 AND camera_id=$2 ORDER BY relation_type,target_id`, tenant, camera)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.VideoCameraRelation{}
	for rows.Next() {
		var relation model.VideoCameraRelation
		if err = rows.Scan(&relation.TenantID, &relation.CameraID, &relation.RelationType, &relation.TargetID); err != nil {
			return nil, err
		}
		out = append(out, relation)
	}
	return out, rows.Err()
}
func (r *Repository) ListVideoCameraRelationsByTarget(ctx context.Context, tenant, relationType, targetID string) ([]model.VideoCameraRelation, error) {
	rows, err := r.pool.Query(ctx, `SELECT tenant_id,camera_id,relation_type,target_id FROM video_camera_relation WHERE tenant_id=$1 AND relation_type=$2 AND target_id=$3 ORDER BY camera_id`, tenant, relationType, targetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.VideoCameraRelation{}
	for rows.Next() {
		var relation model.VideoCameraRelation
		if err = rows.Scan(&relation.TenantID, &relation.CameraID, &relation.RelationType, &relation.TargetID); err != nil {
			return nil, err
		}
		out = append(out, relation)
	}
	return out, rows.Err()
}

func videoRelations(v model.VideoCameraMapping) []model.VideoCameraRelation {
	deviceID := strings.TrimSpace(v.DeviceID)
	if deviceID == "" {
		legacy := cleanUniqueStrings(v.RelatedDeviceIDs)
		if len(legacy) == 1 {
			deviceID = legacy[0]
		}
	}
	if deviceID == "" {
		return nil
	}
	out := []model.VideoCameraRelation{{TenantID: v.TenantID, CameraID: v.CameraID, RelationType: "device", TargetID: deviceID}}
	return out
}

func cleanUniqueStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func relationIDs(relations []model.VideoCameraRelation) (deviceIDs, floorIDs, roomIDs []string) {
	for _, relation := range relations {
		if relation.TargetID == "" {
			continue
		}
		switch relation.RelationType {
		case "device":
			deviceIDs = append(deviceIDs, relation.TargetID)
		case "floor":
			floorIDs = append(floorIDs, relation.TargetID)
		case "room":
			roomIDs = append(roomIDs, relation.TargetID)
		}
	}
	return deviceIDs, floorIDs, roomIDs
}
func (r *Repository) SaveAIAnalysis(ctx context.Context, v model.AIAnalysis) error {
	b, _ := json.Marshal(v)
	_, err := r.pool.Exec(ctx, `INSERT INTO alarm_ai_analysis(tenant_id,alarm_id,body) VALUES($1,$2,$3) ON CONFLICT(tenant_id,alarm_id) DO UPDATE SET body=excluded.body,created_at=now()`, v.TenantID, v.AlarmID, b)
	return err
}
func (r *Repository) GetAIAnalysis(ctx context.Context, tenant, id string) (model.AIAnalysis, error) {
	var v model.AIAnalysis
	var b []byte
	err := r.pool.QueryRow(ctx, `SELECT body FROM alarm_ai_analysis WHERE tenant_id=$1 AND alarm_id=$2`, tenant, id).Scan(&b)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, ErrNotFound
	}
	if err == nil {
		err = json.Unmarshal(b, &v)
	}
	return v, err
}
func (r *Repository) SaveKnowledgeDoc(ctx context.Context, v model.KnowledgeDoc) error {
	b, _ := json.Marshal(v.Metadata)
	_, err := r.pool.Exec(ctx, `INSERT INTO ai_knowledge_doc(id,tenant_id,workflow_id,product_id,category,tags,object_bucket,object_key,filename,status,metadata) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT(id) DO UPDATE SET workflow_id=excluded.workflow_id,product_id=excluded.product_id,category=excluded.category,tags=excluded.tags,status=excluded.status,metadata=excluded.metadata`, v.ID, v.TenantID, v.WorkflowID, v.ProductID, v.Category, v.Tags, v.ObjectBucket, v.ObjectKey, v.Filename, v.Status, b)
	return err
}
func (r *Repository) ListKnowledgeDocs(ctx context.Context, tenant string) ([]model.KnowledgeDoc, error) {
	rows, err := r.pool.Query(ctx, `SELECT id,tenant_id,coalesce(workflow_id,''),coalesce(product_id,''),coalesce(category,''),coalesce(tags,'{}'),object_bucket,object_key,filename,status,metadata,(extract(epoch from created_at)*1000)::bigint FROM ai_knowledge_doc WHERE tenant_id=$1 ORDER BY created_at DESC,id DESC`, tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.KnowledgeDoc{}
	for rows.Next() {
		var v model.KnowledgeDoc
		var metadata []byte
		if err = rows.Scan(&v.ID, &v.TenantID, &v.WorkflowID, &v.ProductID, &v.Category, &v.Tags, &v.ObjectBucket, &v.ObjectKey, &v.Filename, &v.Status, &metadata, &v.CreatedAt); err != nil {
			return nil, err
		}
		if len(metadata) > 0 {
			if err = json.Unmarshal(metadata, &v.Metadata); err != nil {
				return nil, err
			}
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *Repository) ListKnowledgeDocsPage(ctx context.Context, tenant string, limit, offset int) ([]model.KnowledgeDoc, int, error) {
	limit, offset = normalizePage(limit, offset)
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM ai_knowledge_doc WHERE tenant_id=$1`, tenant).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx, `SELECT id,tenant_id,coalesce(workflow_id,''),coalesce(product_id,''),coalesce(category,''),coalesce(tags,'{}'),object_bucket,object_key,filename,status,metadata,(extract(epoch from created_at)*1000)::bigint FROM ai_knowledge_doc WHERE tenant_id=$1 ORDER BY created_at DESC,id DESC LIMIT $2 OFFSET $3`, tenant, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]model.KnowledgeDoc, 0, limit)
	for rows.Next() {
		var item model.KnowledgeDoc
		var metadata []byte
		if err = rows.Scan(&item.ID, &item.TenantID, &item.WorkflowID, &item.ProductID, &item.Category, &item.Tags, &item.ObjectBucket, &item.ObjectKey, &item.Filename, &item.Status, &metadata, &item.CreatedAt); err != nil {
			return nil, 0, err
		}
		if len(metadata) > 0 {
			if err = json.Unmarshal(metadata, &item.Metadata); err != nil {
				return nil, 0, err
			}
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (r *Repository) SaveWorkflowKnowledgeBinding(ctx context.Context, v model.WorkflowKnowledgeBinding) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `INSERT INTO ai_workflow_knowledge_binding(tenant_id,workflow_id,body) VALUES($1,$2,$3) ON CONFLICT(tenant_id,workflow_id) DO UPDATE SET body=excluded.body,updated_at=now()`, v.TenantID, v.WorkflowID, b)
	return err
}

func (r *Repository) GetWorkflowKnowledgeBinding(ctx context.Context, tenant, workflowID string) (model.WorkflowKnowledgeBinding, error) {
	var v model.WorkflowKnowledgeBinding
	var b []byte
	err := r.pool.QueryRow(ctx, `SELECT body FROM ai_workflow_knowledge_binding WHERE tenant_id=$1 AND workflow_id=$2`, tenant, workflowID).Scan(&b)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, nil
	}
	if err == nil {
		err = json.Unmarshal(b, &v)
	}
	return v, err
}
func (r *Repository) SaveReplay(ctx context.Context, v model.ReplayRequest) error {
	b, _ := json.Marshal(v)
	_, err := r.pool.Exec(ctx, `INSERT INTO replay_task(id,tenant_id,status,body) VALUES($1,$2,$3,$4)`, v.ID, v.TenantID, v.Status, b)
	return err
}
func (r *Repository) UpdateReplay(ctx context.Context, v model.ReplayRequest) error {
	b, _ := json.Marshal(v)
	_, err := r.pool.Exec(ctx, `UPDATE replay_task SET status=$2,body=$3,updated_at=now() WHERE id=$1`, v.ID, v.Status, b)
	return err
}
func (r *Repository) GetReplay(ctx context.Context, id string) (model.ReplayRequest, error) {
	var v model.ReplayRequest
	var b []byte
	err := r.pool.QueryRow(ctx, `SELECT body FROM replay_task WHERE id=$1`, id).Scan(&b)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, ErrNotFound
	}
	if err == nil {
		err = json.Unmarshal(b, &v)
	}
	return v, err
}
func (r *Repository) SaveAudit(ctx context.Context, v model.AuditLog) error {
	b, _ := json.Marshal(v.Details)
	_, err := r.pool.Exec(ctx, `INSERT INTO audit_log(id,tenant_id,actor,action,target_type,target_id,details,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, v.ID, v.TenantID, v.Actor, v.Action, v.TargetType, v.TargetID, b, v.CreatedAt)
	return err
}
func (r *Repository) SaveAIToolCall(ctx context.Context, v model.AIToolCallLog) error {
	in, _ := json.Marshal(v.Input)
	out, _ := json.Marshal(v.Output)
	if v.Error != "" {
		out, _ = json.Marshal(map[string]any{"error": v.Error, "output": v.Output})
	}
	_, err := r.pool.Exec(ctx, `INSERT INTO ai_tool_call_log(tenant_id,actor,tool,trace_id,input,output,success,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,to_timestamp($8::double precision/1000))`, v.TenantID, v.Actor, v.Tool, v.ID, in, out, v.Success, v.CreatedAt)
	return err
}
func (r *Repository) LoadAIProviderConfig(ctx context.Context) (ports.AIPluginConfig, bool, error) {
	var provider, modelName string
	var raw []byte
	err := r.pool.QueryRow(ctx, `SELECT provider,model,config FROM ai_model_config WHERE id='__active__' AND enabled=true`).Scan(&provider, &modelName, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.AIPluginConfig{}, false, nil
	}
	if err != nil {
		return ports.AIPluginConfig{}, false, err
	}
	var values struct {
		BaseURL string `json:"baseUrl"`
		APIKey  string `json:"apiKey"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &values); err != nil {
			return ports.AIPluginConfig{}, false, err
		}
	}
	return ports.AIPluginConfig{Provider: provider, BaseURL: values.BaseURL, Model: modelName, APIKey: values.APIKey}, true, nil
}
func (r *Repository) SaveAIProviderConfig(ctx context.Context, v ports.AIPluginConfig) error {
	raw, _ := json.Marshal(map[string]string{"baseUrl": v.BaseURL, "apiKey": v.APIKey})
	_, err := r.pool.Exec(ctx, `
		INSERT INTO ai_model_config(id,tenant_id,provider,model,config,enabled,updated_at)
		VALUES('__active__','__global__',$1,$2,$3,true,now())
		ON CONFLICT(id) DO UPDATE SET tenant_id=EXCLUDED.tenant_id,provider=EXCLUDED.provider,model=EXCLUDED.model,config=EXCLUDED.config,enabled=true,updated_at=now()
	`, v.Provider, v.Model, raw)
	return err
}
func (r *Repository) Health(ctx context.Context) error { return r.pool.Ping(ctx) }
func (r *Repository) Close() error                     { r.pool.Close(); return nil }

var _ = fmt.Sprintf
var _ = strings.Builder{}

func (r *Repository) MarkRawParseResult(ctx context.Context, tenant, id string, at int64, message string) error {
	_, err := r.pool.Exec(ctx, `UPDATE raw_archive_index SET parse_attempted_at=$3,parse_error=$4 WHERE tenant_id=$1 AND message_id=$2`, tenant, id, at, message)
	return err
}

func (r *Repository) UpdateDeviceAccessStatus(ctx context.Context, expected model.DeviceAccessProfile, status, message string, at int64) (bool, error) {
	snapshot, err := json.Marshal(expected)
	if err != nil {
		return false, err
	}
	patch := map[string]any{"runtimeStatus": status, "lastError": message}
	if status == "ONLINE" || (status == "LISTENING" && at > 0) {
		patch["lastSuccessAt"] = at
	} else if status == "ERROR" {
		patch["lastErrorAt"] = at
	}
	change, err := json.Marshal(patch)
	if err != nil {
		return false, err
	}
	result, err := r.pool.Exec(ctx, `UPDATE device_access_profile SET body=body || $4::jsonb WHERE tenant_id=$1 AND id=$2 AND (body - ARRAY['runtimeStatus','lastError','lastSuccessAt','lastErrorAt']) = ($3::jsonb - ARRAY['runtimeStatus','lastError','lastSuccessAt','lastErrorAt'])`, expected.TenantID, expected.ID, snapshot, change)
	return result.RowsAffected() == 1, err
}
