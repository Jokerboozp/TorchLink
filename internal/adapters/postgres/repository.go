package postgres /* 声明 postgres 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"       /* 执行当前语句并推进处理流程。 */
	_ "embed"       /* 执行当前语句并推进处理流程。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"errors"        /* 执行当前语句并推进处理流程。 */
	"fmt"           /* 执行当前语句并推进处理流程。 */
	"strings"       /* 执行当前语句并推进处理流程。 */
	"time"          /* 执行当前语句并推进处理流程。 */

	"github.com/jackc/pgx/v5"         /* 执行当前语句并推进处理流程。 */
	"github.com/jackc/pgx/v5/pgxpool" /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/ports" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

//go:embed schema.sql
var schema string /* 声明 schema。 */

var ErrNotFound = model.ErrNotFound /* 声明 ErrNotFound。 */

type Repository struct{ pool *pgxpool.Pool } /* 定义 Repository 类型。 */

const countManagedDeviceChildrenSQL = `SELECT body->>'gatewayId' AS gateway_id,count(*) FROM device_registry WHERE tenant_id=$1 AND body->>'gatewayId' = ANY($2::text[]) GROUP BY body->>'gatewayId'` /* 声明 countManagedDeviceChildrenSQL。 */

func normalizePage(limit, offset int) (int, int) { /* 定义 normalizePage 函数。 */
	if limit <= 0 { /* 判断条件并选择处理分支。 */
		limit = 20 /* 更新 limit 的值。 */
	} /* 结束当前表达式或代码块。 */
	if limit > 100 { /* 判断条件并选择处理分支。 */
		limit = 100 /* 更新 limit 的值。 */
	} /* 结束当前表达式或代码块。 */
	if offset < 0 { /* 判断条件并选择处理分支。 */
		offset = 0 /* 更新 offset 的值。 */
	} /* 结束当前表达式或代码块。 */
	return limit, offset /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func New(ctx context.Context, dsn string) (*Repository, error) { /* 定义 New 函数。 */
	pool, err := pgxpool.New(ctx, dsn) /* 更新 err 的值。 */
	if err != nil {                    /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err = pool.Ping(ctx); err != nil { /* 判断条件并选择处理分支。 */
		pool.Close()    /* 执行当前语句并推进处理流程。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	r := &Repository{pool: pool}          /* 更新 r 的值。 */
	if err = r.Migrate(ctx); err != nil { /* 判断条件并选择处理分支。 */
		pool.Close()    /* 执行当前语句并推进处理流程。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return r, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) Migrate(ctx context.Context) error { /* 定义 Migrate 函数。 */
	tx, err := r.pool.Begin(ctx) /* 更新 err 的值。 */
	if err != nil {              /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer tx.Rollback(ctx)                                                            /* 安排函数结束时执行清理。 */
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(728194602)`); err != nil { /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if _, err = tx.Exec(ctx, schema); err != nil { /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return tx.Commit(ctx) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) SaveProduct(ctx context.Context, v model.Product) error { /* 定义 SaveProduct 函数。 */
	b, _ := json.Marshal(v)                                                                        /* 更新 _ 的值。 */
	_, err := r.pool.Exec(ctx, saveProductSQL, v.TenantID, v.ID, v.Status, v.ProtocolPackageID, b) /* 更新 err 的值。 */
	return err                                                                                     /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) GetProduct(ctx context.Context, tenant, id string) (model.Product, error) { /* 定义 GetProduct 函数。 */
	var v model.Product                                                                                           /* 声明 v。 */
	var b []byte                                                                                                  /* 声明 b。 */
	err := r.pool.QueryRow(ctx, `SELECT body FROM iot_product WHERE tenant_id=$1 AND id=$2`, tenant, id).Scan(&b) /* 更新 err 的值。 */
	if errors.Is(err, pgx.ErrNoRows) {                                                                            /* 判断条件并选择处理分支。 */
		return v, ErrNotFound /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err == nil { /* 判断条件并选择处理分支。 */
		err = json.Unmarshal(b, &v) /* 更新 err 的值。 */
	} /* 结束当前表达式或代码块。 */
	return v, err /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListProducts(ctx context.Context, tenant string) ([]model.Product, error) { /* 定义 ListProducts 函数。 */
	rows, err := r.pool.Query(ctx, `SELECT body FROM iot_product WHERE tenant_id=$1 ORDER BY updated_at DESC`, tenant) /* 更新 err 的值。 */
	if err != nil {                                                                                                    /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer rows.Close()       /* 安排函数结束时执行清理。 */
	out := []model.Product{} /* 更新 out 的值。 */
	for rows.Next() {        /* 循环处理当前数据。 */
		var b []byte                         /* 声明 b。 */
		var v model.Product                  /* 声明 v。 */
		if err = rows.Scan(&b); err != nil { /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if err = json.Unmarshal(b, &v); err != nil { /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		out = append(out, v) /* 更新 out 的值。 */
	} /* 结束当前表达式或代码块。 */
	return out, rows.Err() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListProductsPage(ctx context.Context, tenant string, limit, offset int) ([]model.Product, int, error) { /* 定义 ListProductsPage 函数。 */
	limit, offset = normalizePage(limit, offset)                                                                             /* 更新 offset 的值。 */
	var total int                                                                                                            /* 声明 total。 */
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM iot_product WHERE tenant_id=$1`, tenant).Scan(&total); err != nil { /* 判断条件并选择处理分支。 */
		return nil, 0, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	rows, err := r.pool.Query(ctx, `SELECT body FROM iot_product WHERE tenant_id=$1 ORDER BY updated_at DESC,id DESC LIMIT $2 OFFSET $3`, tenant, limit, offset) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                              /* 判断条件并选择处理分支。 */
		return nil, 0, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer rows.Close()                       /* 安排函数结束时执行清理。 */
	items := make([]model.Product, 0, limit) /* 更新 items 的值。 */
	for rows.Next() {                        /* 循环处理当前数据。 */
		var body []byte                         /* 声明 body。 */
		var item model.Product                  /* 声明 item。 */
		if err = rows.Scan(&body); err != nil { /* 判断条件并选择处理分支。 */
			return nil, 0, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if err = json.Unmarshal(body, &item); err != nil { /* 判断条件并选择处理分支。 */
			return nil, 0, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		items = append(items, item) /* 更新 items 的值。 */
	} /* 结束当前表达式或代码块。 */
	return items, total, rows.Err() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) SaveProtocolPackage(ctx context.Context, v model.ProtocolPackage) error { /* 定义 SaveProtocolPackage 函数。 */
	b, _ := json.Marshal(v)                                                                         /* 更新 _ 的值。 */
	_, err := r.pool.Exec(ctx, saveProtocolPackageSQL, v.TenantID, v.ID, v.Status, v.ParserType, b) /* 更新 err 的值。 */
	return err                                                                                      /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) GetProtocolPackage(ctx context.Context, tenant, id string) (model.ProtocolPackage, error) { /* 定义 GetProtocolPackage 函数。 */
	var v model.ProtocolPackage                                                                                        /* 声明 v。 */
	var b []byte                                                                                                       /* 声明 b。 */
	err := r.pool.QueryRow(ctx, `SELECT body FROM protocol_package WHERE tenant_id=$1 AND id=$2`, tenant, id).Scan(&b) /* 更新 err 的值。 */
	if errors.Is(err, pgx.ErrNoRows) {                                                                                 /* 判断条件并选择处理分支。 */
		return v, ErrNotFound /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err == nil { /* 判断条件并选择处理分支。 */
		err = json.Unmarshal(b, &v) /* 更新 err 的值。 */
	} /* 结束当前表达式或代码块。 */
	return v, err /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListProtocolPackages(ctx context.Context, tenant string) ([]model.ProtocolPackage, error) { /* 定义 ListProtocolPackages 函数。 */
	rows, err := r.pool.Query(ctx, `SELECT body FROM protocol_package WHERE tenant_id=$1 ORDER BY updated_at DESC`, tenant) /* 更新 err 的值。 */
	if err != nil {                                                                                                         /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer rows.Close()               /* 安排函数结束时执行清理。 */
	out := []model.ProtocolPackage{} /* 更新 out 的值。 */
	for rows.Next() {                /* 循环处理当前数据。 */
		var b []byte                         /* 声明 b。 */
		var v model.ProtocolPackage          /* 声明 v。 */
		if err = rows.Scan(&b); err != nil { /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if err = json.Unmarshal(b, &v); err != nil { /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		out = append(out, v) /* 更新 out 的值。 */
	} /* 结束当前表达式或代码块。 */
	return out, rows.Err() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListProtocolPackagesPage(ctx context.Context, tenant string, limit, offset int) ([]model.ProtocolPackage, int, error) { /* 定义 ListProtocolPackagesPage 函数。 */
	limit, offset = normalizePage(limit, offset)                                                                                  /* 更新 offset 的值。 */
	var total int                                                                                                                 /* 声明 total。 */
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM protocol_package WHERE tenant_id=$1`, tenant).Scan(&total); err != nil { /* 判断条件并选择处理分支。 */
		return nil, 0, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	rows, err := r.pool.Query(ctx, `SELECT body FROM protocol_package WHERE tenant_id=$1 ORDER BY updated_at DESC,id DESC LIMIT $2 OFFSET $3`, tenant, limit, offset) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                   /* 判断条件并选择处理分支。 */
		return nil, 0, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer rows.Close()                               /* 安排函数结束时执行清理。 */
	items := make([]model.ProtocolPackage, 0, limit) /* 更新 items 的值。 */
	for rows.Next() {                                /* 循环处理当前数据。 */
		var body []byte                         /* 声明 body。 */
		var item model.ProtocolPackage          /* 声明 item。 */
		if err = rows.Scan(&body); err != nil { /* 判断条件并选择处理分支。 */
			return nil, 0, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if err = json.Unmarshal(body, &item); err != nil { /* 判断条件并选择处理分支。 */
			return nil, 0, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		items = append(items, item) /* 更新 items 的值。 */
	} /* 结束当前表达式或代码块。 */
	return items, total, rows.Err() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *Repository) SaveProtocolDefinition(ctx context.Context, v model.ProtocolDefinition) error { /* 定义 SaveProtocolDefinition 函数。 */
	b, err := json.Marshal(v) /* 更新 err 的值。 */
	if err != nil {           /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	_, err = r.pool.Exec(ctx, `INSERT INTO protocol_definition(tenant_id,id,body) VALUES($1,$2,$3) ON CONFLICT(tenant_id,id) DO UPDATE SET body=excluded.body,updated_at=now()`, v.TenantID, v.ID, b) /* 更新 err 的值。 */
	return err                                                                                                                                                                                        /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) GetProtocolDefinition(ctx context.Context, tenant, id string) (model.ProtocolDefinition, error) { /* 定义 GetProtocolDefinition 函数。 */
	var v model.ProtocolDefinition                                                                                        /* 声明 v。 */
	var b []byte                                                                                                          /* 声明 b。 */
	err := r.pool.QueryRow(ctx, `SELECT body FROM protocol_definition WHERE tenant_id=$1 AND id=$2`, tenant, id).Scan(&b) /* 更新 err 的值。 */
	if errors.Is(err, pgx.ErrNoRows) {                                                                                    /* 判断条件并选择处理分支。 */
		return v, ErrNotFound /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err == nil { /* 判断条件并选择处理分支。 */
		err = json.Unmarshal(b, &v) /* 更新 err 的值。 */
	} /* 结束当前表达式或代码块。 */
	return v, err /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListProtocolDefinitions(ctx context.Context, tenant string) ([]model.ProtocolDefinition, error) { /* 定义 ListProtocolDefinitions 函数。 */
	rows, err := r.pool.Query(ctx, `SELECT body FROM protocol_definition WHERE ($1='' OR tenant_id=$1) ORDER BY updated_at DESC,id`, tenant) /* 更新 err 的值。 */
	if err != nil {                                                                                                                          /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer rows.Close()                  /* 安排函数结束时执行清理。 */
	out := []model.ProtocolDefinition{} /* 更新 out 的值。 */
	for rows.Next() {                   /* 循环处理当前数据。 */
		var b []byte                         /* 声明 b。 */
		var v model.ProtocolDefinition       /* 声明 v。 */
		if err = rows.Scan(&b); err != nil { /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if err = json.Unmarshal(b, &v); err != nil { /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		out = append(out, v) /* 更新 out 的值。 */
	} /* 结束当前表达式或代码块。 */
	return out, rows.Err() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) CreateProtocolRelease(ctx context.Context, v model.ProtocolRelease) error { /* 定义 CreateProtocolRelease 函数。 */
	b, err := json.Marshal(v) /* 更新 err 的值。 */
	if err != nil {           /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	_, err = r.pool.Exec(ctx, `INSERT INTO protocol_release(tenant_id,protocol_id,version,status,parser_type,body) VALUES($1,$2,$3,$4,$5,$6)`, v.TenantID, v.ProtocolID, v.Version, v.Status, v.ParserType, b) /* 更新 err 的值。 */
	return err                                                                                                                                                                                                 /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) GetProtocolRelease(ctx context.Context, tenant, protocolID, version string) (model.ProtocolRelease, error) { /* 定义 GetProtocolRelease 函数。 */
	var v model.ProtocolRelease                                                                                                                                 /* 声明 v。 */
	var b []byte                                                                                                                                                /* 声明 b。 */
	err := r.pool.QueryRow(ctx, `SELECT body FROM protocol_release WHERE tenant_id=$1 AND protocol_id=$2 AND version=$3`, tenant, protocolID, version).Scan(&b) /* 更新 err 的值。 */
	if errors.Is(err, pgx.ErrNoRows) {                                                                                                                          /* 判断条件并选择处理分支。 */
		return v, ErrNotFound /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err == nil { /* 判断条件并选择处理分支。 */
		err = json.Unmarshal(b, &v) /* 更新 err 的值。 */
	} /* 结束当前表达式或代码块。 */
	return v, err /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListProtocolReleases(ctx context.Context, tenant, protocolID string) ([]model.ProtocolRelease, error) { /* 定义 ListProtocolReleases 函数。 */
	rows, err := r.pool.Query(ctx, `SELECT body FROM protocol_release WHERE ($1='' OR tenant_id=$1) AND ($2='' OR protocol_id=$2) ORDER BY created_at DESC,protocol_id,version`, tenant, protocolID) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                                  /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer rows.Close()               /* 安排函数结束时执行清理。 */
	out := []model.ProtocolRelease{} /* 更新 out 的值。 */
	for rows.Next() {                /* 循环处理当前数据。 */
		var b []byte                         /* 声明 b。 */
		var v model.ProtocolRelease          /* 声明 v。 */
		if err = rows.Scan(&b); err != nil { /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if err = json.Unmarshal(b, &v); err != nil { /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		out = append(out, v) /* 更新 out 的值。 */
	} /* 结束当前表达式或代码块。 */
	return out, rows.Err() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) UpdateProtocolReleaseStatus(ctx context.Context, tenant, protocolID, version, status string, publishedAt int64) error { /* 定义 UpdateProtocolReleaseStatus 函数。 */
	tx, err := r.pool.Begin(ctx) /* 更新 err 的值。 */
	if err != nil {              /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer tx.Rollback(ctx)                                                                                                                                                             /* 安排函数结束时执行清理。 */
	var b []byte                                                                                                                                                                       /* 声明 b。 */
	if err = tx.QueryRow(ctx, `SELECT body FROM protocol_release WHERE tenant_id=$1 AND protocol_id=$2 AND version=$3 FOR UPDATE`, tenant, protocolID, version).Scan(&b); err != nil { /* 判断条件并选择处理分支。 */
		if errors.Is(err, pgx.ErrNoRows) { /* 判断条件并选择处理分支。 */
			return ErrNotFound /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var v model.ProtocolRelease                   /* 声明 v。 */
	if err := json.Unmarshal(b, &v); err != nil { /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	v.Status = status                                                                                                                                                                    /* 更新 v.Status 的值。 */
	v.PublishedAt = publishedAt                                                                                                                                                          /* 更新 v.PublishedAt 的值。 */
	b, _ = json.Marshal(v)                                                                                                                                                               /* 更新 _ 的值。 */
	if _, err = tx.Exec(ctx, `UPDATE protocol_release SET status=$4,body=$5 WHERE tenant_id=$1 AND protocol_id=$2 AND version=$3`, tenant, protocolID, version, status, b); err != nil { /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return tx.Commit(ctx) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) CreatePointTableRelease(ctx context.Context, v model.PointTableRelease) error { /* 定义 CreatePointTableRelease 函数。 */
	b, err := json.Marshal(v) /* 更新 err 的值。 */
	if err != nil {           /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	_, err = r.pool.Exec(ctx, `INSERT INTO point_table_release(tenant_id,protocol_id,version,source_sha256,body) VALUES($1,$2,$3,$4,$5)`, v.TenantID, v.ProtocolID, v.Version, v.SourceSHA256, b) /* 更新 err 的值。 */
	return err                                                                                                                                                                                    /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) GetPointTableRelease(ctx context.Context, tenant, protocolID, version string) (model.PointTableRelease, error) { /* 定义 GetPointTableRelease 函数。 */
	var v model.PointTableRelease                                                                                                                                  /* 声明 v。 */
	var b []byte                                                                                                                                                   /* 声明 b。 */
	err := r.pool.QueryRow(ctx, `SELECT body FROM point_table_release WHERE tenant_id=$1 AND protocol_id=$2 AND version=$3`, tenant, protocolID, version).Scan(&b) /* 更新 err 的值。 */
	if errors.Is(err, pgx.ErrNoRows) {                                                                                                                             /* 判断条件并选择处理分支。 */
		return v, ErrNotFound /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err == nil { /* 判断条件并选择处理分支。 */
		err = json.Unmarshal(b, &v) /* 更新 err 的值。 */
	} /* 结束当前表达式或代码块。 */
	return v, err /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) SaveProductProtocolBinding(ctx context.Context, v model.ProductProtocolBinding) error { /* 定义 SaveProductProtocolBinding 函数。 */
	b, err := json.Marshal(v) /* 更新 err 的值。 */
	if err != nil {           /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	_, err = r.pool.Exec(ctx, saveBindingSQL, v.TenantID, v.ProductID, v.ProtocolID, v.Version, b) /* 更新 err 的值。 */
	return err                                                                                     /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) GetProductProtocolBinding(ctx context.Context, tenant, productID string) (model.ProductProtocolBinding, error) { /* 定义 GetProductProtocolBinding 函数。 */
	var v model.ProductProtocolBinding                                                                                                        /* 声明 v。 */
	var b []byte                                                                                                                              /* 声明 b。 */
	err := r.pool.QueryRow(ctx, `SELECT body FROM product_protocol_binding WHERE tenant_id=$1 AND product_id=$2`, tenant, productID).Scan(&b) /* 更新 err 的值。 */
	if errors.Is(err, pgx.ErrNoRows) {                                                                                                        /* 判断条件并选择处理分支。 */
		return v, ErrNotFound /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err == nil { /* 判断条件并选择处理分支。 */
		err = json.Unmarshal(b, &v) /* 更新 err 的值。 */
	} /* 结束当前表达式或代码块。 */
	return v, err /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) SaveDeviceAccessProfile(ctx context.Context, v model.DeviceAccessProfile) error { /* 定义 SaveDeviceAccessProfile 函数。 */
	b, err := json.Marshal(v) /* 更新 err 的值。 */
	if err != nil {           /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	_, err = r.pool.Exec(ctx, `INSERT INTO device_access_profile(tenant_id,id,device_id,product_id,enabled,body) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(tenant_id,id) DO UPDATE SET device_id=excluded.device_id,product_id=excluded.product_id,enabled=excluded.enabled,body=excluded.body,updated_at=now()`, v.TenantID, v.ID, v.DeviceID, v.ProductID, v.Enabled, b) /* 更新 err 的值。 */
	return err                                                                                                                                                                                                                                                                                                                                                         /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) GetDeviceAccessProfile(ctx context.Context, tenant, id string) (model.DeviceAccessProfile, error) { /* 定义 GetDeviceAccessProfile 函数。 */
	var v model.DeviceAccessProfile                                                                                         /* 声明 v。 */
	var b []byte                                                                                                            /* 声明 b。 */
	err := r.pool.QueryRow(ctx, `SELECT body FROM device_access_profile WHERE tenant_id=$1 AND id=$2`, tenant, id).Scan(&b) /* 更新 err 的值。 */
	if errors.Is(err, pgx.ErrNoRows) {                                                                                      /* 判断条件并选择处理分支。 */
		return v, ErrNotFound /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err == nil { /* 判断条件并选择处理分支。 */
		err = json.Unmarshal(b, &v) /* 更新 err 的值。 */
	} /* 结束当前表达式或代码块。 */
	return v, err /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListDeviceAccessProfiles(ctx context.Context, tenant string) ([]model.DeviceAccessProfile, error) { /* 定义 ListDeviceAccessProfiles 函数。 */
	rows, err := r.pool.Query(ctx, `SELECT body FROM device_access_profile WHERE ($1='' OR tenant_id=$1) ORDER BY updated_at DESC,id`, tenant) /* 更新 err 的值。 */
	if err != nil {                                                                                                                            /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer rows.Close()                   /* 安排函数结束时执行清理。 */
	out := []model.DeviceAccessProfile{} /* 更新 out 的值。 */
	for rows.Next() {                    /* 循环处理当前数据。 */
		var b []byte                         /* 声明 b。 */
		var v model.DeviceAccessProfile      /* 声明 v。 */
		if err = rows.Scan(&b); err != nil { /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if err = json.Unmarshal(b, &v); err != nil { /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		out = append(out, v) /* 更新 out 的值。 */
	} /* 结束当前表达式或代码块。 */
	return out, rows.Err() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) SaveManagedDevice(ctx context.Context, v model.ManagedDevice) error { /* 定义 SaveManagedDevice 函数。 */
	b, _ := json.Marshal(v)                                                                                                                                                                                                                                                                                                                                                                                                     /* 更新 _ 的值。 */
	_, err := r.pool.Exec(ctx, `INSERT INTO device_registry(tenant_id,id,product_id,status,access_key,secret_hash,body) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(tenant_id,id) DO UPDATE SET product_id=excluded.product_id,status=excluded.status,access_key=excluded.access_key,secret_hash=excluded.secret_hash,body=excluded.body,updated_at=now()`, v.TenantID, v.ID, v.ProductID, v.Status, v.AccessKey, v.SecretHash, b) /* 更新 err 的值。 */
	return err                                                                                                                                                                                                                                                                                                                                                                                                                  /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) scanManagedDevice(row pgx.Row) (model.ManagedDevice, error) { /* 定义 scanManagedDevice 函数。 */
	var v model.ManagedDevice          /* 声明 v。 */
	var b []byte                       /* 声明 b。 */
	var hash string                    /* 声明 hash。 */
	err := row.Scan(&b, &hash)         /* 更新 err 的值。 */
	if errors.Is(err, pgx.ErrNoRows) { /* 判断条件并选择处理分支。 */
		return v, ErrNotFound /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err != nil { /* 判断条件并选择处理分支。 */
		return v, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	err = json.Unmarshal(b, &v) /* 更新 err 的值。 */
	v.SecretHash = hash         /* 更新 v.SecretHash 的值。 */
	return v, err               /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) GetManagedDevice(ctx context.Context, tenant, id string) (model.ManagedDevice, error) { /* 定义 GetManagedDevice 函数。 */
	return r.scanManagedDevice(r.pool.QueryRow(ctx, `SELECT body,secret_hash FROM device_registry WHERE tenant_id=$1 AND id=$2`, tenant, id)) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) GetManagedDeviceByAccessKey(ctx context.Context, accessKey string) (model.ManagedDevice, error) { /* 定义 GetManagedDeviceByAccessKey 函数。 */
	return r.scanManagedDevice(r.pool.QueryRow(ctx, `SELECT body,secret_hash FROM device_registry WHERE access_key=$1`, accessKey)) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListManagedDevices(ctx context.Context, tenant string) ([]model.ManagedDevice, error) { /* 定义 ListManagedDevices 函数。 */
	rows, err := r.pool.Query(ctx, `SELECT body,secret_hash FROM device_registry WHERE tenant_id=$1 ORDER BY updated_at DESC`, tenant) /* 更新 err 的值。 */
	if err != nil {                                                                                                                    /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer rows.Close()             /* 安排函数结束时执行清理。 */
	out := []model.ManagedDevice{} /* 更新 out 的值。 */
	for rows.Next() {              /* 循环处理当前数据。 */
		var v model.ManagedDevice                           /* 声明 v。 */
		var b []byte                                        /* 声明 b。 */
		if err = rows.Scan(&b, &v.SecretHash); err != nil { /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if err = json.Unmarshal(b, &v); err != nil { /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		out = append(out, v) /* 更新 out 的值。 */
	} /* 结束当前表达式或代码块。 */
	return out, rows.Err() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListManagedDevicesPage(ctx context.Context, tenant string, limit, offset int) ([]model.ManagedDevice, int, error) { /* 定义 ListManagedDevicesPage 函数。 */
	limit, offset = normalizePage(limit, offset)                                                                                 /* 更新 offset 的值。 */
	var total int                                                                                                                /* 声明 total。 */
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM device_registry WHERE tenant_id=$1`, tenant).Scan(&total); err != nil { /* 判断条件并选择处理分支。 */
		return nil, 0, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	rows, err := r.pool.Query(ctx, `SELECT body,secret_hash FROM device_registry WHERE tenant_id=$1 ORDER BY updated_at DESC,id DESC LIMIT $2 OFFSET $3`, tenant, limit, offset) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                              /* 判断条件并选择处理分支。 */
		return nil, 0, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer rows.Close()                             /* 安排函数结束时执行清理。 */
	items := make([]model.ManagedDevice, 0, limit) /* 更新 items 的值。 */
	for rows.Next() {                              /* 循环处理当前数据。 */
		var item model.ManagedDevice                              /* 声明 item。 */
		var body []byte                                           /* 声明 body。 */
		if err = rows.Scan(&body, &item.SecretHash); err != nil { /* 判断条件并选择处理分支。 */
			return nil, 0, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if err = json.Unmarshal(body, &item); err != nil { /* 判断条件并选择处理分支。 */
			return nil, 0, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		items = append(items, item) /* 更新 items 的值。 */
	} /* 结束当前表达式或代码块。 */
	return items, total, rows.Err() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) CountManagedDeviceChildren(ctx context.Context, tenant string, ids []string) (map[string]int, error) { /* 定义 CountManagedDeviceChildren 函数。 */
	counts := make(map[string]int, len(ids)) /* 更新 counts 的值。 */
	if len(ids) == 0 {                       /* 判断条件并选择处理分支。 */
		return counts, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	rows, err := r.pool.Query(ctx, countManagedDeviceChildrenSQL, tenant, ids) /* 更新 err 的值。 */
	if err != nil {                                                            /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer rows.Close() /* 安排函数结束时执行清理。 */
	for rows.Next() {  /* 循环处理当前数据。 */
		var gatewayID string                                 /* 声明 gatewayID。 */
		var count int                                        /* 声明 count。 */
		if err = rows.Scan(&gatewayID, &count); err != nil { /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		counts[gatewayID] = count /* 更新 counts[gatewayID] 的值。 */
	} /* 结束当前表达式或代码块。 */
	return counts, rows.Err() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) SaveRawIndex(ctx context.Context, v model.RawArchiveIndex) (bool, error) { /* 定义 SaveRawIndex 函数。 */
	tag, err := r.pool.Exec(ctx, `INSERT INTO raw_archive_index(tenant_id,product_id,device_id,message_id,protocol,payload_format,object_bucket,object_key,object_offset,payload_hash,payload_size,received_at,archived_at,published_at,publish_attempts,last_publish_error) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16) ON CONFLICT DO NOTHING`, v.TenantID, v.ProductID, v.DeviceID, v.MessageID, v.Protocol, v.PayloadFormat, v.ObjectBucket, v.ObjectKey, v.ObjectOffset, v.PayloadHash, v.PayloadSize, v.ReceivedAt, v.ArchivedAt, v.PublishedAt, v.PublishAttempts, v.LastPublishError) /* 更新 err 的值。 */
	return tag.RowsAffected() == 1, err                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *Repository) SaveRawMessage(ctx context.Context, v model.RawMessage) error { /* 定义 SaveRawMessage 函数。 */
	body, err := json.Marshal(v) /* 更新 err 的值。 */
	if err != nil {              /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	tag, err := r.pool.Exec(ctx, `INSERT INTO raw_message_log(tenant_id,message_id,product_id,device_id,protocol,payload_format,payload_hash,payload_size,received_at,stored_at,body) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT DO NOTHING`, v.TenantID, v.MessageID, v.ProductID, v.DeviceID, v.Protocol, v.PayloadFormat, v.PayloadHash(), len(v.Payload), v.ReceivedAt, time.Now().UnixMilli(), body) /* 更新 err 的值。 */
	if err == nil && tag.RowsAffected() == 0 {                                                                                                                                                                                                                                                                                                                                                                        /* 判断条件并选择处理分支。 */
		existing, getErr := r.GetRawMessage(ctx, v.TenantID, v.MessageID) /* 更新 getErr 的值。 */
		if getErr != nil {                                                /* 判断条件并选择处理分支。 */
			return getErr /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if existing.PayloadHash() != v.PayloadHash() || existing.DeviceID != v.DeviceID || existing.ProductID != v.ProductID { /* 判断条件并选择处理分支。 */
			return model.ErrRawConflict /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */

	return err /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *Repository) GetRawMessage(ctx context.Context, tenant, messageID string) (model.RawMessage, error) { /* 定义 GetRawMessage 函数。 */
	var value model.RawMessage                                                                                                          /* 声明 value。 */
	var body []byte                                                                                                                     /* 声明 body。 */
	err := r.pool.QueryRow(ctx, `SELECT body FROM raw_message_log WHERE tenant_id=$1 AND message_id=$2`, tenant, messageID).Scan(&body) /* 更新 err 的值。 */
	if errors.Is(err, pgx.ErrNoRows) {                                                                                                  /* 判断条件并选择处理分支。 */
		return value, ErrNotFound /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err == nil { /* 判断条件并选择处理分支。 */
		err = json.Unmarshal(body, &value) /* 更新 err 的值。 */
	} /* 结束当前表达式或代码块。 */
	return value, err /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *Repository) MarkRawPublished(ctx context.Context, tenant, messageID string, publishedAt int64, lastError string) error { /* 定义 MarkRawPublished 函数。 */
	_, err := r.pool.Exec(ctx, `UPDATE raw_archive_index SET publish_attempts=publish_attempts+1,last_publish_error=$4,published_at=CASE WHEN $4='' THEN $3 ELSE published_at END WHERE tenant_id=$1 AND message_id=$2`, tenant, messageID, publishedAt, lastError) /* 更新 err 的值。 */
	return err                                                                                                                                                                                                                                                      /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListPendingRawIndexes(ctx context.Context, limit int) ([]model.RawArchiveIndex, error) { /* 定义 ListPendingRawIndexes 函数。 */
	if limit <= 0 { /* 判断条件并选择处理分支。 */
		limit = 100 /* 更新 limit 的值。 */
	} /* 结束当前表达式或代码块。 */
	rows, err := r.pool.Query(ctx, `SELECT message_id,tenant_id,product_id,device_id,protocol,payload_format,object_bucket,object_key,object_offset,payload_hash,payload_size,received_at,archived_at,published_at,publish_attempts,last_publish_error,parse_attempted_at,parse_error FROM raw_archive_index WHERE published_at=0 ORDER BY archived_at LIMIT $1`, limit) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                                                                                                                                                                                                      /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer rows.Close()               /* 安排函数结束时执行清理。 */
	out := []model.RawArchiveIndex{} /* 更新 out 的值。 */
	for rows.Next() {                /* 循环处理当前数据。 */
		var v model.RawArchiveIndex                   /* 声明 v。 */
		if err = scanRawIndex(rows, &v); err != nil { /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		out = append(out, v) /* 更新 out 的值。 */
	} /* 结束当前表达式或代码块。 */
	return out, rows.Err() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

type rowScanner interface{ Scan(...any) error } /* 定义 rowScanner 类型。 */

func scanRawIndex(row rowScanner, v *model.RawArchiveIndex) error { /* 定义 scanRawIndex 函数。 */
	return row.Scan(&v.MessageID, &v.TenantID, &v.ProductID, &v.DeviceID, &v.Protocol, &v.PayloadFormat, &v.ObjectBucket, &v.ObjectKey, &v.ObjectOffset, &v.PayloadHash, &v.PayloadSize, &v.ReceivedAt, &v.ArchivedAt, &v.PublishedAt, &v.PublishAttempts, &v.LastPublishError, &v.ParseAttemptedAt, &v.ParseError) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) GetRawIndex(ctx context.Context, tenant, messageID string) (model.RawArchiveIndex, error) { /* 定义 GetRawIndex 函数。 */
	var v model.RawArchiveIndex                                                                                                                                                                                                                                                                                                                                                       /* 声明 v。 */
	err := scanRawIndex(r.pool.QueryRow(ctx, `SELECT message_id,tenant_id,product_id,device_id,protocol,payload_format,object_bucket,object_key,object_offset,payload_hash,payload_size,received_at,archived_at,published_at,publish_attempts,last_publish_error,parse_attempted_at,parse_error FROM raw_archive_index WHERE tenant_id=$1 AND message_id=$2`, tenant, messageID), &v) /* 更新 err 的值。 */
	if errors.Is(err, pgx.ErrNoRows) {                                                                                                                                                                                                                                                                                                                                                /* 判断条件并选择处理分支。 */
		return v, ErrNotFound /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return v, err /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListRawIndexes(ctx context.Context, f ports.RawFilter) ([]model.RawArchiveIndex, error) { /* 定义 ListRawIndexes 函数。 */
	q := `SELECT message_id,tenant_id,product_id,device_id,protocol,payload_format,object_bucket,object_key,object_offset,payload_hash,payload_size,received_at,archived_at,published_at,publish_attempts,last_publish_error,parse_attempted_at,parse_error FROM raw_archive_index WHERE ($1='' OR tenant_id=$1) AND ($2='' OR product_id=$2) AND ($3='' OR device_id=$3) AND ($4::bigint=0 OR received_at >= $4) AND ($5::bigint=0 OR received_at <= $5) ORDER BY received_at DESC LIMIT $6 OFFSET $7` /* 更新 q 的值。 */
	limit := f.Limit                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    /* 更新 limit 的值。 */
	if limit <= 0 {                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     /* 判断条件并选择处理分支。 */
		limit = 100 /* 更新 limit 的值。 */
	} /* 结束当前表达式或代码块。 */
	rows, err := r.pool.Query(ctx, q, f.TenantID, f.ProductID, f.DeviceID, f.Start, f.End, limit, f.Offset) /* 更新 err 的值。 */
	if err != nil {                                                                                         /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer rows.Close()               /* 安排函数结束时执行清理。 */
	out := []model.RawArchiveIndex{} /* 更新 out 的值。 */
	for rows.Next() {                /* 循环处理当前数据。 */
		var v model.RawArchiveIndex                    /* 声明 v。 */
		if err := scanRawIndex(rows, &v); err != nil { /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		out = append(out, v) /* 更新 out 的值。 */
	} /* 结束当前表达式或代码块。 */
	return out, rows.Err() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) CountRawIndexes(ctx context.Context, f ports.RawFilter) (int, error) { /* 定义 CountRawIndexes 函数。 */
	var total int                                                                                                                                                                                                                                                                                                  /* 声明 total。 */
	err := r.pool.QueryRow(ctx, `SELECT count(*) FROM raw_archive_index WHERE ($1='' OR tenant_id=$1) AND ($2='' OR product_id=$2) AND ($3='' OR device_id=$3) AND ($4::bigint=0 OR received_at >= $4) AND ($5::bigint=0 OR received_at <= $5)`, f.TenantID, f.ProductID, f.DeviceID, f.Start, f.End).Scan(&total) /* 更新 err 的值。 */
	return total, err                                                                                                                                                                                                                                                                                              /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) SaveStandardMessage(ctx context.Context, v model.StandardMessage) error { /* 定义 SaveStandardMessage 函数。 */
	_, err := r.SaveStandardMessageIfAbsent(ctx, v) /* 更新 err 的值。 */
	return err                                      /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) SaveStandardMessageIfAbsent(ctx context.Context, v model.StandardMessage) (bool, error) { /* 定义 SaveStandardMessageIfAbsent 函数。 */
	body, _ := json.Marshal(v)                                                                                                                                                                                                                                                                                                                                  /* 更新 _ 的值。 */
	props, _ := json.Marshal(v.Properties)                                                                                                                                                                                                                                                                                                                      /* 更新 _ 的值。 */
	event, _ := json.Marshal(v.Event)                                                                                                                                                                                                                                                                                                                           /* 更新 _ 的值。 */
	tags, _ := json.Marshal(v.Tags)                                                                                                                                                                                                                                                                                                                             /* 更新 _ 的值。 */
	tag, err := r.pool.Exec(ctx, `INSERT INTO standard_message(tenant_id,message_id,raw_message_id,product_id,device_id,message_type,ts,properties,event,tags,body) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT DO NOTHING`, v.TenantID, v.MessageID, v.RawMessageID, v.ProductID, v.DeviceID, v.MessageType, v.Timestamp, props, event, tags, body) /* 更新 err 的值。 */
	return tag.RowsAffected() == 1, err                                                                                                                                                                                                                                                                                                                         /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ClaimStandardMessage(ctx context.Context, v model.StandardMessage) (bool, bool, error) { /* 定义 ClaimStandardMessage 函数。 */
	created, err := r.SaveStandardMessageIfAbsent(ctx, v) /* 更新 err 的值。 */
	if err != nil || created {                            /* 判断条件并选择处理分支。 */
		return created, created, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var processed int64                                                                                                                                    /* 声明 processed。 */
	err = r.pool.QueryRow(ctx, `SELECT processed_at FROM standard_message WHERE tenant_id=$1 AND message_id=$2`, v.TenantID, v.MessageID).Scan(&processed) /* 更新 err 的值。 */
	if errors.Is(err, pgx.ErrNoRows) {                                                                                                                     /* 判断条件并选择处理分支。 */
		return false, false, ErrNotFound /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return processed == 0, false, err /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) MarkStandardMessageProcessed(ctx context.Context, tenant, messageID string) error { /* 定义 MarkStandardMessageProcessed 函数。 */
	tag, err := r.pool.Exec(ctx, `UPDATE standard_message SET processed_at=1 WHERE tenant_id=$1 AND message_id=$2`, tenant, messageID) /* 更新 err 的值。 */
	if err == nil && tag.RowsAffected() == 0 {                                                                                         /* 判断条件并选择处理分支。 */
		return ErrNotFound /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return err /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) getStandard(ctx context.Context, query string, args ...any) (model.StandardMessage, error) { /* 定义 getStandard 函数。 */
	var v model.StandardMessage                             /* 声明 v。 */
	var body []byte                                         /* 声明 body。 */
	err := r.pool.QueryRow(ctx, query, args...).Scan(&body) /* 更新 err 的值。 */
	if errors.Is(err, pgx.ErrNoRows) {                      /* 判断条件并选择处理分支。 */
		return v, ErrNotFound /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err == nil { /* 判断条件并选择处理分支。 */
		err = json.Unmarshal(body, &v) /* 更新 err 的值。 */
	} /* 结束当前表达式或代码块。 */
	return v, err /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) GetStandardMessageByRaw(ctx context.Context, tenant, rawID string) (model.StandardMessage, error) { /* 定义 GetStandardMessageByRaw 函数。 */
	return r.getStandard(ctx, `SELECT body FROM standard_message WHERE tenant_id=$1 AND raw_message_id=$2 ORDER BY ts DESC LIMIT 1`, tenant, rawID) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) GetLatestMessage(ctx context.Context, tenant, device string) (model.StandardMessage, error) { /* 定义 GetLatestMessage 函数。 */
	return r.getStandard(ctx, `SELECT body FROM standard_message WHERE tenant_id=$1 AND device_id=$2 ORDER BY ts DESC LIMIT 1`, tenant, device) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) PropertyHistory(ctx context.Context, tenant, device, property string, start, end int64, limit int) ([]map[string]any, error) { /* 定义 PropertyHistory 函数。 */
	if limit <= 0 { /* 判断条件并选择处理分支。 */
		limit = 1000 /* 更新 limit 的值。 */
	} /* 结束当前表达式或代码块。 */
	rows, err := r.pool.Query(ctx, `SELECT ts, properties -> $3, message_id FROM standard_message WHERE tenant_id=$1 AND device_id=$2 AND properties ? $3 AND ts >= $4::bigint AND ($5::bigint=0 OR ts <= $5) ORDER BY ts DESC LIMIT $6`, tenant, device, property, start, end, limit) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                                                                                                                    /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer rows.Close()        /* 安排函数结束时执行清理。 */
	out := []map[string]any{} /* 更新 out 的值。 */
	for rows.Next() {         /* 循环处理当前数据。 */
		var ts int64                                      /* 声明 ts。 */
		var raw []byte                                    /* 声明 raw。 */
		var id string                                     /* 声明 id。 */
		if err := rows.Scan(&ts, &raw, &id); err != nil { /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		var value any                                                                       /* 声明 value。 */
		_ = json.Unmarshal(raw, &value)                                                     /* 更新 _ 的值。 */
		out = append(out, map[string]any{"timestamp": ts, "value": value, "messageId": id}) /* 更新 out 的值。 */
	} /* 结束当前表达式或代码块。 */
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 { /* 循环处理当前数据。 */
		out[i], out[j] = out[j], out[i] /* 更新 out[j] 的值。 */
	} /* 结束当前表达式或代码块。 */
	return out, rows.Err() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) PropertyHistoryPage(ctx context.Context, tenant, device, property string, start, end int64, limit, offset int) ([]map[string]any, int, error) { /* 定义 PropertyHistoryPage 函数。 */
	limit, offset = normalizePage(limit, offset)                                                                                                    /* 更新 offset 的值。 */
	where := `WHERE tenant_id=$1 AND device_id=$2 AND properties ? $3 AND ts >= $4::bigint AND ($5::bigint=0 OR ts <= $5)`                          /* 更新 where 的值。 */
	var total int                                                                                                                                   /* 声明 total。 */
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM standard_message `+where, tenant, device, property, start, end).Scan(&total); err != nil { /* 判断条件并选择处理分支。 */
		return nil, 0, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	rows, err := r.pool.Query(ctx, `SELECT ts, properties -> $3, message_id FROM standard_message `+where+` ORDER BY ts DESC, message_id DESC LIMIT $6 OFFSET $7`, tenant, device, property, start, end, limit, offset) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                                                     /* 判断条件并选择处理分支。 */
		return nil, 0, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer rows.Close()                        /* 安排函数结束时执行清理。 */
	items := make([]map[string]any, 0, limit) /* 更新 items 的值。 */
	for rows.Next() {                         /* 循环处理当前数据。 */
		var ts int64                                      /* 声明 ts。 */
		var raw []byte                                    /* 声明 raw。 */
		var id string                                     /* 声明 id。 */
		if err := rows.Scan(&ts, &raw, &id); err != nil { /* 判断条件并选择处理分支。 */
			return nil, 0, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		var value any                                                                           /* 声明 value。 */
		_ = json.Unmarshal(raw, &value)                                                         /* 更新 _ 的值。 */
		items = append(items, map[string]any{"timestamp": ts, "value": value, "messageId": id}) /* 更新 items 的值。 */
	} /* 结束当前表达式或代码块。 */
	if err := rows.Err(); err != nil { /* 判断条件并选择处理分支。 */
		return nil, 0, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 { /* 循环处理当前数据。 */
		items[i], items[j] = items[j], items[i] /* 更新 items[j] 的值。 */
	} /* 结束当前表达式或代码块。 */
	return items, total, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) UpsertDeviceState(ctx context.Context, v model.DeviceState) error { /* 定义 UpsertDeviceState 函数。 */
	b, _ := json.Marshal(v)                                                                                                                                                                                                                                                                                                                                                                                                  /* 更新 _ 的值。 */
	_, err := r.pool.Exec(ctx, `INSERT INTO device_state(tenant_id,device_id,product_id,business_status,last_seen_at,body) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(tenant_id,device_id) DO UPDATE SET product_id=excluded.product_id,business_status=excluded.business_status,last_seen_at=excluded.last_seen_at,body=excluded.body,updated_at=now()`, v.TenantID, v.DeviceID, v.ProductID, v.BusinessStatus, v.LastSeenAt, b) /* 更新 err 的值。 */
	return err                                                                                                                                                                                                                                                                                                                                                                                                               /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) GetDeviceState(ctx context.Context, tenant, device string) (model.DeviceState, error) { /* 定义 GetDeviceState 函数。 */
	var v model.DeviceState                                                                                                   /* 声明 v。 */
	var b []byte                                                                                                              /* 声明 b。 */
	err := r.pool.QueryRow(ctx, `SELECT body FROM device_state WHERE tenant_id=$1 AND device_id=$2`, tenant, device).Scan(&b) /* 更新 err 的值。 */
	if errors.Is(err, pgx.ErrNoRows) {                                                                                        /* 判断条件并选择处理分支。 */
		return v, ErrNotFound /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err == nil { /* 判断条件并选择处理分支。 */
		err = json.Unmarshal(b, &v) /* 更新 err 的值。 */
	} /* 结束当前表达式或代码块。 */
	return v, err /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListDeviceStates(ctx context.Context, tenant string) ([]model.DeviceState, error) { /* 定义 ListDeviceStates 函数。 */
	rows, err := r.pool.Query(ctx, `SELECT body FROM device_state WHERE ($1='' OR tenant_id=$1)`, tenant) /* 更新 err 的值。 */
	if err != nil {                                                                                       /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer rows.Close()           /* 安排函数结束时执行清理。 */
	out := []model.DeviceState{} /* 更新 out 的值。 */
	for rows.Next() {            /* 循环处理当前数据。 */
		var b []byte                          /* 声明 b。 */
		var v model.DeviceState               /* 声明 v。 */
		if err := rows.Scan(&b); err != nil { /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if err := json.Unmarshal(b, &v); err != nil { /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		out = append(out, v) /* 更新 out 的值。 */
	} /* 结束当前表达式或代码块。 */
	return out, rows.Err() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListDeviceStatesPage(ctx context.Context, tenant string, limit, offset int) ([]model.DeviceState, int, error) { /* 定义 ListDeviceStatesPage 函数。 */
	limit, offset = normalizePage(limit, offset)                                                                                         /* 更新 offset 的值。 */
	var total int                                                                                                                        /* 声明 total。 */
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM device_state WHERE ($1='' OR tenant_id=$1)`, tenant).Scan(&total); err != nil { /* 判断条件并选择处理分支。 */
		return nil, 0, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	rows, err := r.pool.Query(ctx, `SELECT body FROM device_state WHERE ($1='' OR tenant_id=$1) ORDER BY last_seen_at DESC,device_id LIMIT $2 OFFSET $3`, tenant, limit, offset) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                              /* 判断条件并选择处理分支。 */
		return nil, 0, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer rows.Close()                           /* 安排函数结束时执行清理。 */
	items := make([]model.DeviceState, 0, limit) /* 更新 items 的值。 */
	for rows.Next() {                            /* 循环处理当前数据。 */
		var body []byte                         /* 声明 body。 */
		var item model.DeviceState              /* 声明 item。 */
		if err = rows.Scan(&body); err != nil { /* 判断条件并选择处理分支。 */
			return nil, 0, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if err = json.Unmarshal(body, &item); err != nil { /* 判断条件并选择处理分支。 */
			return nil, 0, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		items = append(items, item) /* 更新 items 的值。 */
	} /* 结束当前表达式或代码块。 */
	return items, total, rows.Err() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListUnregisteredDeviceStatesPage(ctx context.Context, tenant string, limit, offset int) ([]model.DeviceState, int, error) { /* 定义 ListUnregisteredDeviceStatesPage 函数。 */
	limit, offset = normalizePage(limit, offset)                                                                                              /* 更新 offset 的值。 */
	where := `WHERE ds.tenant_id=$1 AND NOT EXISTS (SELECT 1 FROM device_registry dr WHERE dr.tenant_id=ds.tenant_id AND dr.id=ds.device_id)` /* 更新 where 的值。 */
	var total int                                                                                                                             /* 声明 total。 */
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM device_state ds `+where, tenant).Scan(&total); err != nil {                          /* 判断条件并选择处理分支。 */
		return nil, 0, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	rows, err := r.pool.Query(ctx, `SELECT ds.body FROM device_state ds `+where+` ORDER BY ds.last_seen_at DESC,ds.device_id LIMIT $2 OFFSET $3`, tenant, limit, offset) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                      /* 判断条件并选择处理分支。 */
		return nil, 0, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer rows.Close()                           /* 安排函数结束时执行清理。 */
	items := make([]model.DeviceState, 0, limit) /* 更新 items 的值。 */
	for rows.Next() {                            /* 循环处理当前数据。 */
		var body []byte                         /* 声明 body。 */
		var item model.DeviceState              /* 声明 item。 */
		if err = rows.Scan(&body); err != nil { /* 判断条件并选择处理分支。 */
			return nil, 0, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if err = json.Unmarshal(body, &item); err != nil { /* 判断条件并选择处理分支。 */
			return nil, 0, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		items = append(items, item) /* 更新 items 的值。 */
	} /* 结束当前表达式或代码块。 */
	return items, total, rows.Err() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) CountDeviceStates(ctx context.Context, tenant string, unregisteredOnly bool) (int, int, error) { /* 定义 CountDeviceStates 函数。 */
	where := `WHERE ds.tenant_id=$1` /* 更新 where 的值。 */
	if unregisteredOnly {            /* 判断条件并选择处理分支。 */
		where += ` AND NOT EXISTS (SELECT 1 FROM device_registry dr WHERE dr.tenant_id=ds.tenant_id AND dr.id=ds.device_id)` /* 更新 where 的值。 */
	} /* 结束当前表达式或代码块。 */
	var total, online int                                                                                                                                                                      /* 声明 total。 */
	if err := r.pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER (WHERE ds.business_status IN ('ONLINE','ALARM')) FROM device_state ds `+where, tenant).Scan(&total, &online); err != nil { /* 判断条件并选择处理分支。 */
		return 0, 0, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return total, online, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) SaveDeviceStateEvent(ctx context.Context, v model.DeviceState) error { /* 定义 SaveDeviceStateEvent 函数。 */
	b, _ := json.Marshal(v)                                                                                                                                                 /* 更新 _ 的值。 */
	_, err := r.pool.Exec(ctx, `INSERT INTO device_state_event(tenant_id,device_id,business_status,body) VALUES($1,$2,$3,$4)`, v.TenantID, v.DeviceID, v.BusinessStatus, b) /* 更新 err 的值。 */
	return err                                                                                                                                                              /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) SaveRule(ctx context.Context, v model.AlarmRule) error { /* 定义 SaveRule 函数。 */
	b, _ := json.Marshal(v)                                                                                                                                                                                                                                                                            /* 更新 _ 的值。 */
	_, err := r.pool.Exec(ctx, `INSERT INTO alarm_rule(tenant_id,id,product_id,enabled,body) VALUES($1,$2,$3,$4,$5) ON CONFLICT(tenant_id,id) DO UPDATE SET product_id=excluded.product_id,enabled=excluded.enabled,body=excluded.body,updated_at=now()`, v.TenantID, v.ID, v.ProductID, v.Enabled, b) /* 更新 err 的值。 */
	return err                                                                                                                                                                                                                                                                                         /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListRules(ctx context.Context, tenant string) ([]model.AlarmRule, error) { /* 定义 ListRules 函数。 */
	rows, err := r.pool.Query(ctx, `SELECT body FROM alarm_rule WHERE ($1='' OR tenant_id=$1) ORDER BY updated_at DESC`, tenant) /* 更新 err 的值。 */
	if err != nil {                                                                                                              /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer rows.Close()         /* 安排函数结束时执行清理。 */
	out := []model.AlarmRule{} /* 更新 out 的值。 */
	for rows.Next() {          /* 循环处理当前数据。 */
		var b []byte                          /* 声明 b。 */
		var v model.AlarmRule                 /* 声明 v。 */
		if err := rows.Scan(&b); err != nil { /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if err := json.Unmarshal(b, &v); err != nil { /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		out = append(out, v) /* 更新 out 的值。 */
	} /* 结束当前表达式或代码块。 */
	return out, rows.Err() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListRulesPage(ctx context.Context, tenant string, limit, offset int) ([]model.AlarmRule, int, error) { /* 定义 ListRulesPage 函数。 */
	limit, offset = normalizePage(limit, offset)                                                                                       /* 更新 offset 的值。 */
	var total int                                                                                                                      /* 声明 total。 */
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM alarm_rule WHERE ($1='' OR tenant_id=$1)`, tenant).Scan(&total); err != nil { /* 判断条件并选择处理分支。 */
		return nil, 0, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	rows, err := r.pool.Query(ctx, `SELECT body FROM alarm_rule WHERE ($1='' OR tenant_id=$1) ORDER BY updated_at DESC,id DESC LIMIT $2 OFFSET $3`, tenant, limit, offset) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                        /* 判断条件并选择处理分支。 */
		return nil, 0, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer rows.Close()                         /* 安排函数结束时执行清理。 */
	items := make([]model.AlarmRule, 0, limit) /* 更新 items 的值。 */
	for rows.Next() {                          /* 循环处理当前数据。 */
		var body []byte                         /* 声明 body。 */
		var item model.AlarmRule                /* 声明 item。 */
		if err = rows.Scan(&body); err != nil { /* 判断条件并选择处理分支。 */
			return nil, 0, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if err = json.Unmarshal(body, &item); err != nil { /* 判断条件并选择处理分支。 */
			return nil, 0, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		items = append(items, item) /* 更新 items 的值。 */
	} /* 结束当前表达式或代码块。 */
	return items, total, rows.Err() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) DeleteRule(ctx context.Context, tenant, id string) error { /* 定义 DeleteRule 函数。 */
	tag, err := r.pool.Exec(ctx, `DELETE FROM alarm_rule WHERE tenant_id=$1 AND id=$2`, tenant, id) /* 更新 err 的值。 */
	if err == nil && tag.RowsAffected() == 0 {                                                      /* 判断条件并选择处理分支。 */
		return ErrNotFound /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return err /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) SaveRulePending(ctx context.Context, tenant, ruleID, deviceID string, since int64) error { /* 定义 SaveRulePending 函数。 */
	_, err := r.pool.Exec(ctx, `INSERT INTO alarm_rule_pending(tenant_id,rule_id,device_id,since_at) VALUES($1,$2,$3,$4) ON CONFLICT(tenant_id,rule_id,device_id) DO UPDATE SET since_at=LEAST(alarm_rule_pending.since_at, EXCLUDED.since_at), updated_at=now()`, tenant, ruleID, deviceID, since) /* 更新 err 的值。 */
	return err                                                                                                                                                                                                                                                                                      /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) GetRulePending(ctx context.Context, tenant, ruleID, deviceID string) (int64, bool, error) { /* 定义 GetRulePending 函数。 */
	var since int64                                                                                                                                                  /* 声明 since。 */
	err := r.pool.QueryRow(ctx, `SELECT since_at FROM alarm_rule_pending WHERE tenant_id=$1 AND rule_id=$2 AND device_id=$3`, tenant, ruleID, deviceID).Scan(&since) /* 更新 err 的值。 */
	if errors.Is(err, pgx.ErrNoRows) {                                                                                                                               /* 判断条件并选择处理分支。 */
		return 0, false, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return since, err == nil, err /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) DeleteRulePending(ctx context.Context, tenant, ruleID, deviceID string) error { /* 定义 DeleteRulePending 函数。 */
	_, err := r.pool.Exec(ctx, `DELETE FROM alarm_rule_pending WHERE tenant_id=$1 AND rule_id=$2 AND device_id=$3`, tenant, ruleID, deviceID) /* 更新 err 的值。 */
	return err                                                                                                                                /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) DeleteRulePendings(ctx context.Context, tenant, ruleID string) error { /* 定义 DeleteRulePendings 函数。 */
	_, err := r.pool.Exec(ctx, `DELETE FROM alarm_rule_pending WHERE tenant_id=$1 AND rule_id=$2`, tenant, ruleID) /* 更新 err 的值。 */
	return err                                                                                                     /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) UpsertAlarm(ctx context.Context, v model.Alarm) (model.Alarm, bool, error) { /* 定义 UpsertAlarm 函数。 */
	tx, err := r.pool.Begin(ctx) /* 更新 err 的值。 */
	if err != nil {              /* 判断条件并选择处理分支。 */
		return v, false, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer tx.Rollback(ctx)                                                                                                                                                                               /* 安排函数结束时执行清理。 */
	var body []byte                                                                                                                                                                                      /* 声明 body。 */
	err = tx.QueryRow(ctx, `SELECT body FROM alarm_record WHERE tenant_id=$1 AND device_id=$2 AND rule_id=$3 AND status IN ('ACTIVE','ACKED') FOR UPDATE`, v.TenantID, v.DeviceID, v.RuleID).Scan(&body) /* 更新 err 的值。 */
	if err == nil {                                                                                                                                                                                      /* 判断条件并选择处理分支。 */
		var old model.Alarm                               /* 声明 old。 */
		if err = json.Unmarshal(body, &old); err != nil { /* 判断条件并选择处理分支。 */
			return v, false, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if v.TriggerID != "" && old.TriggerID == v.TriggerID { /* 判断条件并选择处理分支。 */
			if err = tx.Commit(ctx); err != nil { /* 判断条件并选择处理分支。 */
				return v, false, err /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			return old, false, nil /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		old.LastTriggeredAt = v.LastTriggeredAt /* 更新 old.LastTriggeredAt 的值。 */
		old.TriggerCount++                      /* 执行当前语句并推进处理流程。 */
		if v.Confidence > old.Confidence {      /* 判断条件并选择处理分支。 */
			old.Confidence = v.Confidence /* 更新 old.Confidence 的值。 */
		} /* 结束当前表达式或代码块。 */
		body, _ = json.Marshal(old)                                                                                                                                 /* 更新 _ 的值。 */
		_, err = tx.Exec(ctx, `UPDATE alarm_record SET last_triggered_at=$3,body=$4 WHERE tenant_id=$1 AND id=$2`, old.TenantID, old.ID, old.LastTriggeredAt, body) /* 更新 err 的值。 */
		if err != nil {                                                                                                                                             /* 判断条件并选择处理分支。 */
			return v, false, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if err = tx.Commit(ctx); err != nil { /* 判断条件并选择处理分支。 */
			return v, false, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return old, false, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if !errors.Is(err, pgx.ErrNoRows) { /* 判断条件并选择处理分支。 */
		return v, false, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	body, _ = json.Marshal(v)                                                                                                                                                                                                                                          /* 更新 _ 的值。 */
	_, err = tx.Exec(ctx, `INSERT INTO alarm_record(tenant_id,id,rule_id,device_id,status,level,source,last_triggered_at,body) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, v.TenantID, v.ID, v.RuleID, v.DeviceID, v.Status, v.AlarmLevel, v.Source, v.LastTriggeredAt, body) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                                                                                                    /* 判断条件并选择处理分支。 */
		return v, false, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err = tx.Commit(ctx); err != nil { /* 判断条件并选择处理分支。 */
		return v, false, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return v, true, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) GetAlarm(ctx context.Context, tenant, id string) (model.Alarm, error) { /* 定义 GetAlarm 函数。 */
	var v model.Alarm                                                                                              /* 声明 v。 */
	var b []byte                                                                                                   /* 声明 b。 */
	err := r.pool.QueryRow(ctx, `SELECT body FROM alarm_record WHERE tenant_id=$1 AND id=$2`, tenant, id).Scan(&b) /* 更新 err 的值。 */
	if errors.Is(err, pgx.ErrNoRows) {                                                                             /* 判断条件并选择处理分支。 */
		return v, ErrNotFound /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err == nil { /* 判断条件并选择处理分支。 */
		err = json.Unmarshal(b, &v) /* 更新 err 的值。 */
	} /* 结束当前表达式或代码块。 */
	return v, err /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListAlarms(ctx context.Context, f ports.AlarmFilter) ([]model.Alarm, error) { /* 定义 ListAlarms 函数。 */
	limit := f.Limit /* 更新 limit 的值。 */
	if limit <= 0 {  /* 判断条件并选择处理分支。 */
		limit = 100 /* 更新 limit 的值。 */
	} /* 结束当前表达式或代码块。 */
	rows, err := r.pool.Query(ctx, `SELECT body FROM alarm_record WHERE ($1='' OR tenant_id=$1) AND ($2='' OR device_id=$2) AND ($3='' OR status=$3) AND ($4='' OR level=$4) AND ($5='' OR source=$5) AND ($6::bigint=0 OR last_triggered_at >= $6) AND ($7::bigint=0 OR last_triggered_at <= $7) ORDER BY last_triggered_at DESC LIMIT $8 OFFSET $9`, f.TenantID, f.DeviceID, f.Status, f.Level, f.Source, f.Start, f.End, limit, f.Offset) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                                                                                                                                                                                                                                                                          /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer rows.Close()     /* 安排函数结束时执行清理。 */
	out := []model.Alarm{} /* 更新 out 的值。 */
	for rows.Next() {      /* 循环处理当前数据。 */
		var b []byte                          /* 声明 b。 */
		var v model.Alarm                     /* 声明 v。 */
		if err := rows.Scan(&b); err != nil { /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if err := json.Unmarshal(b, &v); err != nil { /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		out = append(out, v) /* 更新 out 的值。 */
	} /* 结束当前表达式或代码块。 */
	return out, rows.Err() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) CountAlarms(ctx context.Context, f ports.AlarmFilter) (int, error) { /* 定义 CountAlarms 函数。 */
	var total int                                                                                                                                                                                                                                                                                                                                                                      /* 声明 total。 */
	err := r.pool.QueryRow(ctx, `SELECT count(*) FROM alarm_record WHERE ($1='' OR tenant_id=$1) AND ($2='' OR device_id=$2) AND ($3='' OR status=$3) AND ($4='' OR level=$4) AND ($5='' OR source=$5) AND ($6::bigint=0 OR last_triggered_at >= $6) AND ($7::bigint=0 OR last_triggered_at <= $7)`, f.TenantID, f.DeviceID, f.Status, f.Level, f.Source, f.Start, f.End).Scan(&total) /* 更新 err 的值。 */
	return total, err                                                                                                                                                                                                                                                                                                                                                                  /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) UpdateAlarm(ctx context.Context, v model.Alarm) error { /* 定义 UpdateAlarm 函数。 */
	b, _ := json.Marshal(v)                                                                                                                                                                              /* 更新 _ 的值。 */
	tag, err := r.pool.Exec(ctx, `UPDATE alarm_record SET status=$3,level=$4,last_triggered_at=$5,body=$6 WHERE tenant_id=$1 AND id=$2`, v.TenantID, v.ID, v.Status, v.AlarmLevel, v.LastTriggeredAt, b) /* 更新 err 的值。 */
	if err == nil && tag.RowsAffected() == 0 {                                                                                                                                                           /* 判断条件并选择处理分支。 */
		return ErrNotFound /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return err /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) SaveVideoEvent(ctx context.Context, v model.VideoAlarmEvent) (bool, error) { /* 定义 SaveVideoEvent 函数。 */
	b, _ := json.Marshal(v)                                                                                                                                                                                                                 /* 更新 _ 的值。 */
	tag, err := r.pool.Exec(ctx, `INSERT INTO video_alarm_event(tenant_id,event_id,camera_id,alarm_type,event_time,body) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, v.TenantID, v.EventID, v.CameraID, v.AlarmType, v.EventTime, b) /* 更新 err 的值。 */
	return tag.RowsAffected() == 1, err                                                                                                                                                                                                     /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) UpdateVideoEvent(ctx context.Context, v model.VideoAlarmEvent) error { /* 定义 UpdateVideoEvent 函数。 */
	b, _ := json.Marshal(v)                                                                                                                                                               /* 更新 _ 的值。 */
	_, err := r.pool.Exec(ctx, `UPDATE video_alarm_event SET body=$3,alarm_type=$4,event_time=$5 WHERE tenant_id=$1 AND event_id=$2`, v.TenantID, v.EventID, b, v.AlarmType, v.EventTime) /* 更新 err 的值。 */
	return err                                                                                                                                                                            /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListPendingVideoEvents(ctx context.Context, limit int) ([]model.VideoAlarmEvent, error) { /* 定义 ListPendingVideoEvents 函数。 */
	rows, err := r.pool.Query(ctx, `SELECT body FROM video_alarm_event WHERE body->'raw'->>'mediaTransferStatus' IN ('PENDING','FAILED') ORDER BY event_time LIMIT $1`, limit) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                            /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer rows.Close()               /* 安排函数结束时执行清理。 */
	out := []model.VideoAlarmEvent{} /* 更新 out 的值。 */
	for rows.Next() {                /* 循环处理当前数据。 */
		var b []byte                         /* 声明 b。 */
		if err = rows.Scan(&b); err != nil { /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		var v model.VideoAlarmEvent                  /* 声明 v。 */
		if err = json.Unmarshal(b, &v); err != nil { /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		out = append(out, v) /* 更新 out 的值。 */
	} /* 结束当前表达式或代码块。 */
	return out, rows.Err() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

const videoCameraMappingColumns = `tenant_id,camera_id,coalesce(camera_name,''),coalesce(brand,''),coalesce(camera_point,''),coalesce(device_id,''),coalesce(ingest_mode,'direct'),coalesce(project_id,''),coalesce(city_code,''),coalesce(district_code,''),coalesce(building,''),coalesce(floor,''),coalesce(room,''),coalesce(area_id,''),related_device_ids,related_floor_ids,related_room_ids,coalesce(video_platform_id,''),coalesce(stream_url,''),coalesce(stream_type,''),coalesce(sdk_endpoint,''),coalesce(sdk_camera_id,''),coalesce(sdk_credential_ref,''),enabled` /* 声明 videoCameraMappingColumns。 */

func (r *Repository) SaveVideoCameraMapping(ctx context.Context, v model.VideoCameraMapping) error { /* 定义 SaveVideoCameraMapping 函数。 */
	legacyDeviceIDs := cleanUniqueStrings(v.RelatedDeviceIDs) /* 更新 legacyDeviceIDs 的值。 */
	if v.DeviceID == "" && len(legacyDeviceIDs) == 1 {        /* 判断条件并选择处理分支。 */
		v.DeviceID = legacyDeviceIDs[0] /* 更新 v.DeviceID 的值。 */
	} /* 结束当前表达式或代码块。 */
	if len(legacyDeviceIDs) > 1 || v.DeviceID != "" && len(legacyDeviceIDs) == 1 && legacyDeviceIDs[0] != v.DeviceID { /* 判断条件并选择处理分支。 */
		return errors.New("a camera can be associated with at most one device") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	deviceIDs := []string{} /* 更新 deviceIDs 的值。 */
	if v.DeviceID != "" {   /* 判断条件并选择处理分支。 */
		deviceIDs = []string{v.DeviceID} /* 更新 deviceIDs 的值。 */
	} /* 结束当前表达式或代码块。 */
	deviceJSON, _ := json.Marshal(deviceIDs) /* 更新 _ 的值。 */
	floorJSON, _ := json.Marshal([]string{}) /* 更新 _ 的值。 */
	roomJSON, _ := json.Marshal([]string{})  /* 更新 _ 的值。 */
	tx, err := r.pool.Begin(ctx)             /* 更新 err 的值。 */
	if err != nil {                          /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer tx.Rollback(ctx)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             /* 安排函数结束时执行清理。 */
	if _, err = tx.Exec(ctx, `INSERT INTO video_camera_mapping(tenant_id,camera_id,camera_name,brand,camera_point,device_id,ingest_mode,project_id,city_code,district_code,building,floor,room,area_id,related_device_ids,related_floor_ids,related_room_ids,video_platform_id,stream_url,stream_type,sdk_endpoint,sdk_camera_id,sdk_credential_ref,enabled) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24) ON CONFLICT(tenant_id,camera_id) DO UPDATE SET camera_name=excluded.camera_name,brand=excluded.brand,camera_point=excluded.camera_point,device_id=excluded.device_id,ingest_mode=excluded.ingest_mode,project_id=excluded.project_id,city_code=excluded.city_code,district_code=excluded.district_code,building=excluded.building,floor=excluded.floor,room=excluded.room,area_id=excluded.area_id,related_device_ids=excluded.related_device_ids,related_floor_ids=excluded.related_floor_ids,related_room_ids=excluded.related_room_ids,video_platform_id=excluded.video_platform_id,stream_url=excluded.stream_url,stream_type=excluded.stream_type,sdk_endpoint=excluded.sdk_endpoint,sdk_camera_id=excluded.sdk_camera_id,sdk_credential_ref=excluded.sdk_credential_ref,enabled=excluded.enabled`, v.TenantID, v.CameraID, v.CameraName, v.Brand, v.CameraPoint, v.DeviceID, v.IngestMode, v.ProjectID, v.CityCode, v.DistrictCode, v.Building, v.Floor, v.Room, v.AreaID, deviceJSON, floorJSON, roomJSON, v.VideoPlatformID, v.StreamURL, v.StreamType, v.SDKEndpoint, v.SDKCameraID, v.SDKCredentialRef, v.Enabled); err != nil { /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err = replaceVideoCameraRelationsTx(ctx, tx, v.TenantID, v.CameraID, videoRelations(v)); err != nil { /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return tx.Commit(ctx) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) scanVideoMapping(row rowScanner) (model.VideoCameraMapping, error) { /* 定义 scanVideoMapping 函数。 */
	var v model.VideoCameraMapping                                                                                                                                                                                                                                                                                                                     /* 声明 v。 */
	var related, floors, rooms []byte                                                                                                                                                                                                                                                                                                                  /* 声明 related。 */
	err := row.Scan(&v.TenantID, &v.CameraID, &v.CameraName, &v.Brand, &v.CameraPoint, &v.DeviceID, &v.IngestMode, &v.ProjectID, &v.CityCode, &v.DistrictCode, &v.Building, &v.Floor, &v.Room, &v.AreaID, &related, &floors, &rooms, &v.VideoPlatformID, &v.StreamURL, &v.StreamType, &v.SDKEndpoint, &v.SDKCameraID, &v.SDKCredentialRef, &v.Enabled) /* 更新 err 的值。 */
	if err == nil {                                                                                                                                                                                                                                                                                                                                    /* 判断条件并选择处理分支。 */
		_ = json.Unmarshal(related, &v.RelatedDeviceIDs)      /* 更新 _ 的值。 */
		_ = json.Unmarshal(floors, &v.RelatedFloorIDs)        /* 更新 _ 的值。 */
		_ = json.Unmarshal(rooms, &v.RelatedRoomIDs)          /* 更新 _ 的值。 */
		if v.DeviceID == "" && len(v.RelatedDeviceIDs) == 1 { /* 判断条件并选择处理分支。 */
			v.DeviceID = v.RelatedDeviceIDs[0] /* 更新 v.DeviceID 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return v, err /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) GetVideoCameraMapping(ctx context.Context, tenant, camera string) (model.VideoCameraMapping, error) { /* 定义 GetVideoCameraMapping 函数。 */
	v, err := r.scanVideoMapping(r.pool.QueryRow(ctx, `SELECT `+videoCameraMappingColumns+` FROM video_camera_mapping WHERE tenant_id=$1 AND camera_id=$2`, tenant, camera)) /* 更新 err 的值。 */
	if errors.Is(err, pgx.ErrNoRows) {                                                                                                                                       /* 判断条件并选择处理分支。 */
		return v, ErrNotFound /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return v, err /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListVideoCameraMappings(ctx context.Context, tenant string) ([]model.VideoCameraMapping, error) { /* 定义 ListVideoCameraMappings 函数。 */
	rows, err := r.pool.Query(ctx, `SELECT `+videoCameraMappingColumns+` FROM video_camera_mapping WHERE tenant_id=$1 ORDER BY camera_id`, tenant) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer rows.Close()                  /* 安排函数结束时执行清理。 */
	out := []model.VideoCameraMapping{} /* 更新 out 的值。 */
	for rows.Next() {                   /* 循环处理当前数据。 */
		v, e := r.scanVideoMapping(rows) /* 更新 e 的值。 */
		if e != nil {                    /* 判断条件并选择处理分支。 */
			return nil, e /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		out = append(out, v) /* 更新 out 的值。 */
	} /* 结束当前表达式或代码块。 */
	return out, rows.Err() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListVideoCameraMappingsPage(ctx context.Context, tenant string, limit, offset int) ([]model.VideoCameraMapping, int, error) { /* 定义 ListVideoCameraMappingsPage 函数。 */
	limit, offset = normalizePage(limit, offset)                                                                                      /* 更新 offset 的值。 */
	var total int                                                                                                                     /* 声明 total。 */
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM video_camera_mapping WHERE tenant_id=$1`, tenant).Scan(&total); err != nil { /* 判断条件并选择处理分支。 */
		return nil, 0, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	rows, err := r.pool.Query(ctx, `SELECT `+videoCameraMappingColumns+` FROM video_camera_mapping WHERE tenant_id=$1 ORDER BY camera_id LIMIT $2 OFFSET $3`, tenant, limit, offset) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                  /* 判断条件并选择处理分支。 */
		return nil, 0, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer rows.Close()                                  /* 安排函数结束时执行清理。 */
	items := make([]model.VideoCameraMapping, 0, limit) /* 更新 items 的值。 */
	for rows.Next() {                                   /* 循环处理当前数据。 */
		item, scanErr := r.scanVideoMapping(rows) /* 更新 scanErr 的值。 */
		if scanErr != nil {                       /* 判断条件并选择处理分支。 */
			return nil, 0, scanErr /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		items = append(items, item) /* 更新 items 的值。 */
	} /* 结束当前表达式或代码块。 */
	return items, total, rows.Err() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ReplaceVideoCameraRelations(ctx context.Context, tenant, camera string, relations []model.VideoCameraRelation) error { /* 定义 ReplaceVideoCameraRelations 函数。 */
	tx, err := r.pool.Begin(ctx) /* 更新 err 的值。 */
	if err != nil {              /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer tx.Rollback(ctx)                                                                   /* 安排函数结束时执行清理。 */
	if err = replaceVideoCameraRelationsTx(ctx, tx, tenant, camera, relations); err != nil { /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return tx.Commit(ctx) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func replaceVideoCameraRelationsTx(ctx context.Context, tx pgx.Tx, tenant, camera string, relations []model.VideoCameraRelation) error { /* 定义 replaceVideoCameraRelationsTx 函数。 */
	if _, err := tx.Exec(ctx, `DELETE FROM video_camera_relation WHERE tenant_id=$1 AND camera_id=$2`, tenant, camera); err != nil { /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	deviceID := ""                       /* 更新 deviceID 的值。 */
	for _, relation := range relations { /* 循环处理当前数据。 */
		if relation.TargetID == "" || relation.RelationType != "device" { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if deviceID != "" && deviceID != relation.TargetID { /* 判断条件并选择处理分支。 */
			return errors.New("a camera can be associated with at most one device") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		deviceID = relation.TargetID                                                                                                                                                                                                   /* 更新 deviceID 的值。 */
		if _, err := tx.Exec(ctx, `INSERT INTO video_camera_relation(tenant_id,camera_id,relation_type,target_id) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, tenant, camera, relation.RelationType, relation.TargetID); err != nil { /* 判断条件并选择处理分支。 */
			return err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	deviceIDs := []string{} /* 更新 deviceIDs 的值。 */
	if deviceID != "" {     /* 判断条件并选择处理分支。 */
		deviceIDs = []string{deviceID} /* 更新 deviceIDs 的值。 */
	} /* 结束当前表达式或代码块。 */
	floorIDs, roomIDs := []string{}, []string{}                                                                                                                                                                                                           /* 更新 roomIDs 的值。 */
	deviceJSON, _ := json.Marshal(deviceIDs)                                                                                                                                                                                                              /* 更新 _ 的值。 */
	floorJSON, _ := json.Marshal(floorIDs)                                                                                                                                                                                                                /* 更新 _ 的值。 */
	roomJSON, _ := json.Marshal(roomIDs)                                                                                                                                                                                                                  /* 更新 _ 的值。 */
	if _, err := tx.Exec(ctx, `UPDATE video_camera_mapping SET device_id=$3,related_device_ids=$4,related_floor_ids=$5,related_room_ids=$6 WHERE tenant_id=$1 AND camera_id=$2`, tenant, camera, deviceID, deviceJSON, floorJSON, roomJSON); err != nil { /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListVideoCameraRelations(ctx context.Context, tenant, camera string) ([]model.VideoCameraRelation, error) { /* 定义 ListVideoCameraRelations 函数。 */
	rows, err := r.pool.Query(ctx, `SELECT tenant_id,camera_id,relation_type,target_id FROM video_camera_relation WHERE tenant_id=$1 AND camera_id=$2 ORDER BY relation_type,target_id`, tenant, camera) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                                      /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer rows.Close()                   /* 安排函数结束时执行清理。 */
	out := []model.VideoCameraRelation{} /* 更新 out 的值。 */
	for rows.Next() {                    /* 循环处理当前数据。 */
		var relation model.VideoCameraRelation                                                                               /* 声明 relation。 */
		if err = rows.Scan(&relation.TenantID, &relation.CameraID, &relation.RelationType, &relation.TargetID); err != nil { /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		out = append(out, relation) /* 更新 out 的值。 */
	} /* 结束当前表达式或代码块。 */
	return out, rows.Err() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListVideoCameraRelationsByTarget(ctx context.Context, tenant, relationType, targetID string) ([]model.VideoCameraRelation, error) { /* 定义 ListVideoCameraRelationsByTarget 函数。 */
	rows, err := r.pool.Query(ctx, `SELECT tenant_id,camera_id,relation_type,target_id FROM video_camera_relation WHERE tenant_id=$1 AND relation_type=$2 AND target_id=$3 ORDER BY camera_id`, tenant, relationType, targetID) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                                                             /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer rows.Close()                   /* 安排函数结束时执行清理。 */
	out := []model.VideoCameraRelation{} /* 更新 out 的值。 */
	for rows.Next() {                    /* 循环处理当前数据。 */
		var relation model.VideoCameraRelation                                                                               /* 声明 relation。 */
		if err = rows.Scan(&relation.TenantID, &relation.CameraID, &relation.RelationType, &relation.TargetID); err != nil { /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		out = append(out, relation) /* 更新 out 的值。 */
	} /* 结束当前表达式或代码块。 */
	return out, rows.Err() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func videoRelations(v model.VideoCameraMapping) []model.VideoCameraRelation { /* 定义 videoRelations 函数。 */
	deviceID := strings.TrimSpace(v.DeviceID) /* 更新 deviceID 的值。 */
	if deviceID == "" {                       /* 判断条件并选择处理分支。 */
		legacy := cleanUniqueStrings(v.RelatedDeviceIDs) /* 更新 legacy 的值。 */
		if len(legacy) == 1 {                            /* 判断条件并选择处理分支。 */
			deviceID = legacy[0] /* 更新 deviceID 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if deviceID == "" { /* 判断条件并选择处理分支。 */
		return nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	out := []model.VideoCameraRelation{{TenantID: v.TenantID, CameraID: v.CameraID, RelationType: "device", TargetID: deviceID}} /* 更新 out 的值。 */
	return out                                                                                                                   /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func cleanUniqueStrings(values []string) []string { /* 定义 cleanUniqueStrings 函数。 */
	out := make([]string, 0, len(values)) /* 更新 out 的值。 */
	seen := map[string]struct{}{}         /* 更新 seen 的值。 */
	for _, value := range values {        /* 循环处理当前数据。 */
		value = strings.TrimSpace(value) /* 更新 value 的值。 */
		if value == "" {                 /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if _, ok := seen[value]; ok { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		seen[value] = struct{}{} /* 更新 seen[value] 的值。 */
		out = append(out, value) /* 更新 out 的值。 */
	} /* 结束当前表达式或代码块。 */
	return out /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *Repository) SaveAIAnalysis(ctx context.Context, v model.AIAnalysis) error { /* 定义 SaveAIAnalysis 函数。 */
	b, _ := json.Marshal(v)                                                                                                                                                                                                                                                /* 更新 _ 的值。 */
	_, err := r.pool.Exec(ctx, `INSERT INTO alarm_ai_analysis(tenant_id,alarm_id,knowledge_scope,body) VALUES($1,$2,$3,$4) ON CONFLICT(tenant_id,alarm_id,knowledge_scope) DO UPDATE SET body=excluded.body,created_at=now()`, v.TenantID, v.AlarmID, v.KnowledgeScope, b) /* 更新 err 的值。 */
	return err                                                                                                                                                                                                                                                             /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) GetAIAnalysis(ctx context.Context, tenant, id, knowledgeScope string) (model.AIAnalysis, error) { /* 定义 GetAIAnalysis 函数。 */
	var v model.AIAnalysis                                                                                                                                           /* 声明 v。 */
	var b []byte                                                                                                                                                     /* 声明 b。 */
	err := r.pool.QueryRow(ctx, `SELECT body FROM alarm_ai_analysis WHERE tenant_id=$1 AND alarm_id=$2 AND knowledge_scope=$3`, tenant, id, knowledgeScope).Scan(&b) /* 更新 err 的值。 */
	if errors.Is(err, pgx.ErrNoRows) {                                                                                                                               /* 判断条件并选择处理分支。 */
		return v, ErrNotFound /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err == nil { /* 判断条件并选择处理分支。 */
		err = json.Unmarshal(b, &v)       /* 更新 err 的值。 */
		v.KnowledgeScope = knowledgeScope // 迁移前的记录正文中没有该字段，以列值为准。
	} /* 结束当前表达式或代码块。 */
	return v, err /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) SaveKnowledgeDoc(ctx context.Context, v model.KnowledgeDoc) error { /* 定义 SaveKnowledgeDoc 函数。 */
	b, _ := json.Marshal(v.Metadata)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                /* 更新 _ 的值。 */
	_, err := r.pool.Exec(ctx, `INSERT INTO ai_knowledge_doc(id,tenant_id,workflow_id,product_id,category,tags,object_bucket,object_key,filename,status,metadata) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT(id) DO UPDATE SET workflow_id=excluded.workflow_id,product_id=excluded.product_id,category=excluded.category,tags=excluded.tags,status=excluded.status,metadata=excluded.metadata`, v.ID, v.TenantID, v.WorkflowID, v.ProductID, v.Category, v.Tags, v.ObjectBucket, v.ObjectKey, v.Filename, v.Status, b) /* 更新 err 的值。 */
	return err                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListKnowledgeDocs(ctx context.Context, tenant string) ([]model.KnowledgeDoc, error) { /* 定义 ListKnowledgeDocs 函数。 */
	rows, err := r.pool.Query(ctx, `SELECT id,tenant_id,coalesce(workflow_id,''),coalesce(product_id,''),coalesce(category,''),coalesce(tags,'{}'),object_bucket,object_key,filename,status,metadata,(extract(epoch from created_at)*1000)::bigint FROM ai_knowledge_doc WHERE tenant_id=$1 ORDER BY created_at DESC,id DESC`, tenant) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                                                                                                                                                                    /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer rows.Close()            /* 安排函数结束时执行清理。 */
	out := []model.KnowledgeDoc{} /* 更新 out 的值。 */
	for rows.Next() {             /* 循环处理当前数据。 */
		var v model.KnowledgeDoc                                                                                                                                                                /* 声明 v。 */
		var metadata []byte                                                                                                                                                                     /* 声明 metadata。 */
		if err = rows.Scan(&v.ID, &v.TenantID, &v.WorkflowID, &v.ProductID, &v.Category, &v.Tags, &v.ObjectBucket, &v.ObjectKey, &v.Filename, &v.Status, &metadata, &v.CreatedAt); err != nil { /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if len(metadata) > 0 { /* 判断条件并选择处理分支。 */
			if err = json.Unmarshal(metadata, &v.Metadata); err != nil { /* 判断条件并选择处理分支。 */
				return nil, err /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		out = append(out, v) /* 更新 out 的值。 */
	} /* 结束当前表达式或代码块。 */
	return out, rows.Err() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListKnowledgeDocsPage(ctx context.Context, tenant string, limit, offset int) ([]model.KnowledgeDoc, int, error) { /* 定义 ListKnowledgeDocsPage 函数。 */
	limit, offset = normalizePage(limit, offset)                                                                                  /* 更新 offset 的值。 */
	var total int                                                                                                                 /* 声明 total。 */
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM ai_knowledge_doc WHERE tenant_id=$1`, tenant).Scan(&total); err != nil { /* 判断条件并选择处理分支。 */
		return nil, 0, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	rows, err := r.pool.Query(ctx, `SELECT id,tenant_id,coalesce(workflow_id,''),coalesce(product_id,''),coalesce(category,''),coalesce(tags,'{}'),object_bucket,object_key,filename,status,metadata,(extract(epoch from created_at)*1000)::bigint FROM ai_knowledge_doc WHERE tenant_id=$1 ORDER BY created_at DESC,id DESC LIMIT $2 OFFSET $3`, tenant, limit, offset) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                                                                                                                                                                                                      /* 判断条件并选择处理分支。 */
		return nil, 0, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer rows.Close()                            /* 安排函数结束时执行清理。 */
	items := make([]model.KnowledgeDoc, 0, limit) /* 更新 items 的值。 */
	for rows.Next() {                             /* 循环处理当前数据。 */
		var item model.KnowledgeDoc                                                                                                                                                                                              /* 声明 item。 */
		var metadata []byte                                                                                                                                                                                                      /* 声明 metadata。 */
		if err = rows.Scan(&item.ID, &item.TenantID, &item.WorkflowID, &item.ProductID, &item.Category, &item.Tags, &item.ObjectBucket, &item.ObjectKey, &item.Filename, &item.Status, &metadata, &item.CreatedAt); err != nil { /* 判断条件并选择处理分支。 */
			return nil, 0, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if len(metadata) > 0 { /* 判断条件并选择处理分支。 */
			if err = json.Unmarshal(metadata, &item.Metadata); err != nil { /* 判断条件并选择处理分支。 */
				return nil, 0, err /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		items = append(items, item) /* 更新 items 的值。 */
	} /* 结束当前表达式或代码块。 */
	return items, total, rows.Err() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *Repository) SaveWorkflowKnowledgeBinding(ctx context.Context, v model.WorkflowKnowledgeBinding) error { /* 定义 SaveWorkflowKnowledgeBinding 函数。 */
	b, err := json.Marshal(v) /* 更新 err 的值。 */
	if err != nil {           /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	_, err = r.pool.Exec(ctx, `INSERT INTO ai_workflow_knowledge_binding(tenant_id,workflow_id,body) VALUES($1,$2,$3) ON CONFLICT(tenant_id,workflow_id) DO UPDATE SET body=excluded.body,updated_at=now()`, v.TenantID, v.WorkflowID, b) /* 更新 err 的值。 */
	return err                                                                                                                                                                                                                            /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *Repository) GetWorkflowKnowledgeBinding(ctx context.Context, tenant, workflowID string) (model.WorkflowKnowledgeBinding, error) { /* 定义 GetWorkflowKnowledgeBinding 函数。 */
	var v model.WorkflowKnowledgeBinding                                                                                                             /* 声明 v。 */
	var b []byte                                                                                                                                     /* 声明 b。 */
	err := r.pool.QueryRow(ctx, `SELECT body FROM ai_workflow_knowledge_binding WHERE tenant_id=$1 AND workflow_id=$2`, tenant, workflowID).Scan(&b) /* 更新 err 的值。 */
	if errors.Is(err, pgx.ErrNoRows) {                                                                                                               /* 判断条件并选择处理分支。 */
		return v, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err == nil { /* 判断条件并选择处理分支。 */
		err = json.Unmarshal(b, &v) /* 更新 err 的值。 */
	} /* 结束当前表达式或代码块。 */
	return v, err /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) SaveReplay(ctx context.Context, v model.ReplayRequest) error { /* 定义 SaveReplay 函数。 */
	b, _ := json.Marshal(v)                                                                                                            /* 更新 _ 的值。 */
	_, err := r.pool.Exec(ctx, `INSERT INTO replay_task(id,tenant_id,status,body) VALUES($1,$2,$3,$4)`, v.ID, v.TenantID, v.Status, b) /* 更新 err 的值。 */
	return err                                                                                                                         /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) UpdateReplay(ctx context.Context, v model.ReplayRequest) error { /* 定义 UpdateReplay 函数。 */
	b, _ := json.Marshal(v)                                                                                                /* 更新 _ 的值。 */
	_, err := r.pool.Exec(ctx, `UPDATE replay_task SET status=$2,body=$3,updated_at=now() WHERE id=$1`, v.ID, v.Status, b) /* 更新 err 的值。 */
	return err                                                                                                             /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) GetReplay(ctx context.Context, id string) (model.ReplayRequest, error) { /* 定义 GetReplay 函数。 */
	var v model.ReplayRequest                                                            /* 声明 v。 */
	var b []byte                                                                         /* 声明 b。 */
	err := r.pool.QueryRow(ctx, `SELECT body FROM replay_task WHERE id=$1`, id).Scan(&b) /* 更新 err 的值。 */
	if errors.Is(err, pgx.ErrNoRows) {                                                   /* 判断条件并选择处理分支。 */
		return v, ErrNotFound /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err == nil { /* 判断条件并选择处理分支。 */
		err = json.Unmarshal(b, &v) /* 更新 err 的值。 */
	} /* 结束当前表达式或代码块。 */
	return v, err /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) SaveAudit(ctx context.Context, v model.AuditLog) error { /* 定义 SaveAudit 函数。 */
	b, _ := json.Marshal(v.Details)                                                                                                                                                                                                        /* 更新 _ 的值。 */
	_, err := r.pool.Exec(ctx, `INSERT INTO audit_log(id,tenant_id,actor,action,target_type,target_id,details,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, v.ID, v.TenantID, v.Actor, v.Action, v.TargetType, v.TargetID, b, v.CreatedAt) /* 更新 err 的值。 */
	return err                                                                                                                                                                                                                             /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) SaveAIToolCall(ctx context.Context, v model.AIToolCallLog) error { /* 定义 SaveAIToolCall 函数。 */
	in, _ := json.Marshal(v.Input)   /* 更新 _ 的值。 */
	out, _ := json.Marshal(v.Output) /* 更新 _ 的值。 */
	if v.Error != "" {               /* 判断条件并选择处理分支。 */
		out, _ = json.Marshal(map[string]any{"error": v.Error, "output": v.Output}) /* 更新 _ 的值。 */
	} /* 结束当前表达式或代码块。 */
	_, err := r.pool.Exec(ctx, `INSERT INTO ai_tool_call_log(tenant_id,actor,tool,trace_id,input,output,success,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,to_timestamp($8::double precision/1000))`, v.TenantID, v.Actor, v.Tool, v.ID, in, out, v.Success, v.CreatedAt) /* 更新 err 的值。 */
	return err                                                                                                                                                                                                                                                         /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) LoadAIProviderConfig(ctx context.Context) (ports.AIPluginConfig, bool, error) { /* 定义 LoadAIProviderConfig 函数。 */
	var provider, modelName string                                                                                                                            /* 声明 provider。 */
	var raw []byte                                                                                                                                            /* 声明 raw。 */
	err := r.pool.QueryRow(ctx, `SELECT provider,model,config FROM ai_model_config WHERE id='__active__' AND enabled=true`).Scan(&provider, &modelName, &raw) /* 更新 err 的值。 */
	if errors.Is(err, pgx.ErrNoRows) {                                                                                                                        /* 判断条件并选择处理分支。 */
		return ports.AIPluginConfig{}, false, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err != nil { /* 判断条件并选择处理分支。 */
		return ports.AIPluginConfig{}, false, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var values struct { /* 声明 values。 */
		BaseURL   string `json:"baseUrl"` /* 执行当前语句并推进处理流程。 */
		APIKey    string `json:"apiKey"`  /* 执行当前语句并推进处理流程。 */
		MaxTokens int    `json:"maxTokens"`
	} /* 结束当前表达式或代码块。 */
	if len(raw) > 0 { /* 判断条件并选择处理分支。 */
		if err := json.Unmarshal(raw, &values); err != nil { /* 判断条件并选择处理分支。 */
			return ports.AIPluginConfig{}, false, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return ports.AIPluginConfig{Provider: provider, BaseURL: values.BaseURL, Model: modelName, APIKey: values.APIKey, MaxTokens: values.MaxTokens}, true, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) SaveAIProviderConfig(ctx context.Context, v ports.AIPluginConfig) error { /* 定义 SaveAIProviderConfig 函数。 */
	raw, _ := json.Marshal(map[string]any{"baseUrl": v.BaseURL, "apiKey": v.APIKey, "maxTokens": v.MaxTokens}) /* 更新 _ 的值。 */
	_, err := r.pool.Exec(ctx, `
		INSERT INTO ai_model_config(id,tenant_id,provider,model,config,enabled,updated_at)
		VALUES('__active__','__global__',$1,$2,$3,true,now())
		ON CONFLICT(id) DO UPDATE SET tenant_id=EXCLUDED.tenant_id,provider=EXCLUDED.provider,model=EXCLUDED.model,config=EXCLUDED.config,enabled=true,updated_at=now()
	`, v.Provider, v.Model, raw)
	return err /* 返回当前处理结果。 */
}                                                      /* 结束当前表达式或代码块。 */
func (r *Repository) Health(ctx context.Context) error { return r.pool.Ping(ctx) }    /* 定义 Health 函数。 */
func (r *Repository) Close() error                     { r.pool.Close(); return nil } /* 定义 Close 函数。 */

var _ = fmt.Sprintf       /* 声明 _。 */
var _ = strings.Builder{} /* 声明 _。 */

func (r *Repository) MarkRawParseResult(ctx context.Context, tenant, id string, at int64, message string) error { /* 定义 MarkRawParseResult 函数。 */
	_, err := r.pool.Exec(ctx, `UPDATE raw_archive_index SET parse_attempted_at=$3,parse_error=$4 WHERE tenant_id=$1 AND message_id=$2`, tenant, id, at, message) /* 更新 err 的值。 */
	return err                                                                                                                                                    /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *Repository) UpdateDeviceAccessStatus(ctx context.Context, expected model.DeviceAccessProfile, status, message string, at int64) (bool, error) { /* 定义 UpdateDeviceAccessStatus 函数。 */
	snapshot, err := json.Marshal(expected) /* 更新 err 的值。 */
	if err != nil {                         /* 判断条件并选择处理分支。 */
		return false, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	patch := map[string]any{"runtimeStatus": status, "lastError": message} /* 更新 patch 的值。 */
	if status == "ONLINE" || (status == "LISTENING" && at > 0) {           /* 判断条件并选择处理分支。 */
		patch["lastSuccessAt"] = at /* 执行当前语句并推进处理流程。 */
	} else if status == "ERROR" { /* 结束当前表达式或代码块。 */
		patch["lastErrorAt"] = at /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	change, err := json.Marshal(patch) /* 更新 err 的值。 */
	if err != nil {                    /* 判断条件并选择处理分支。 */
		return false, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	result, err := r.pool.Exec(ctx, `UPDATE device_access_profile SET body=body || $4::jsonb WHERE tenant_id=$1 AND id=$2 AND (body - ARRAY['runtimeStatus','lastError','lastSuccessAt','lastErrorAt']) = ($3::jsonb - ARRAY['runtimeStatus','lastError','lastSuccessAt','lastErrorAt'])`, expected.TenantID, expected.ID, snapshot, change) /* 更新 err 的值。 */
	return result.RowsAffected() == 1, err                                                                                                                                                                                                                                                                                                   /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
