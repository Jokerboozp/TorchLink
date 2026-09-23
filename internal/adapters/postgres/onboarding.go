package postgres /* 声明 postgres 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                     /* 执行当前语句并推进处理流程。 */
	"encoding/json"               /* 执行当前语句并推进处理流程。 */
	"errors"                      /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func (r *Repository) SaveOnboarding(ctx context.Context, b model.OnboardingBundle) error { /* 定义 SaveOnboarding 函数。 */
	tx, err := r.pool.Begin(ctx) /* 更新 err 的值。 */
	if err != nil {              /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer tx.Rollback(ctx) /* 安排函数结束时执行清理。 */
	// Serialize onboarding across replicas, including listener port reservation.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(728194601)`); err != nil { /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	exec := func(sql string, args ...any) error { _, e := tx.Exec(ctx, sql, args...); return e } /* 更新 exec 的值。 */
	body := func(v any) []byte { data, _ := json.Marshal(v); return data }                       /* 更新 body 的值。 */
	var status string                                                                            /* 声明 status。 */
	if p := b.Product; p != nil {                                                                /* 判断条件并选择处理分支。 */
		if err = exec(`INSERT INTO iot_product(tenant_id,id,status,protocol_package_id,body) VALUES($1,$2,$3,$4,$5)`, p.TenantID, p.ID, p.Status, p.ProtocolPackageID, body(p)); err != nil { /* 判断条件并选择处理分支。 */
			return err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		v := b.Release                                                                                                                                                                                                                                                                                                    /* 更新 v 的值。 */
		pkg := model.ProtocolPackage{TenantID: v.TenantID, ID: p.ProtocolPackageID, Name: v.ProtocolID, Version: v.Version, Protocol: v.ProtocolID, Transport: v.Transport, PayloadFormat: v.PayloadFormat, ParserType: v.ParserType, Status: v.Status, Config: v.Config, CreatedAt: v.CreatedAt, UpdatedAt: v.CreatedAt} /* 更新 pkg 的值。 */
		if err = exec(`INSERT INTO protocol_package(tenant_id,id,status,parser_type,body) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, pkg.TenantID, pkg.ID, pkg.Status, pkg.ParserType, body(pkg)); err != nil {                                                                                                      /* 判断条件并选择处理分支。 */
			return err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if err = tx.QueryRow(ctx, `SELECT status FROM iot_product WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, b.Device.TenantID, b.Device.ProductID).Scan(&status); err != nil { /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if status != "ENABLED" { /* 判断条件并选择处理分支。 */
		return errors.New("product is disabled") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if v := b.Release; v != nil { /* 判断条件并选择处理分支。 */
		definition := model.ProtocolDefinition{TenantID: v.TenantID, ID: v.ProtocolID, Name: v.ProtocolID, CreatedAt: v.CreatedAt, UpdatedAt: v.CreatedAt}                    /* 更新 definition 的值。 */
		if err = exec(`INSERT INTO protocol_definition(tenant_id,id,body) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, v.TenantID, v.ProtocolID, body(definition)); err != nil { /* 判断条件并选择处理分支。 */
			return err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if err = exec(`INSERT INTO protocol_release(tenant_id,protocol_id,version,status,parser_type,body) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, v.TenantID, v.ProtocolID, v.Version, v.Status, v.ParserType, body(v)); err != nil { /* 判断条件并选择处理分支。 */
			return err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		var saved model.ProtocolRelease                                                                                                                                                              /* 声明 saved。 */
		var data []byte                                                                                                                                                                              /* 声明 data。 */
		if err = tx.QueryRow(ctx, `SELECT body FROM protocol_release WHERE tenant_id=$1 AND protocol_id=$2 AND version=$3 FOR SHARE`, v.TenantID, v.ProtocolID, v.Version).Scan(&data); err != nil { /* 判断条件并选择处理分支。 */
			return err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if err = json.Unmarshal(data, &saved); err != nil { /* 判断条件并选择处理分支。 */
			return err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if saved.Status != "PUBLISHED" || saved.ParserType != v.ParserType { /* 判断条件并选择处理分支。 */
			return errors.New("protocol release changed; test again") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if v := b.PointTable; v != nil { /* 判断条件并选择处理分支。 */
		if err = exec(`INSERT INTO point_table_release(tenant_id,protocol_id,version,source_sha256,body) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, v.TenantID, v.ProtocolID, v.Version, v.SourceSHA256, body(v)); err != nil { /* 判断条件并选择处理分支。 */
			return err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if v := b.Binding; v != nil { /* 判断条件并选择处理分支。 */
		if err = exec(`INSERT INTO product_protocol_binding(tenant_id,product_id,protocol_id,version,body) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, v.TenantID, v.ProductID, v.ProtocolID, v.Version, body(v)); err != nil { /* 判断条件并选择处理分支。 */
			return err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if v := b.Profile; v != nil { /* 判断条件并选择处理分支。 */
		var protocol, version string                                                                                                                                                                          /* 声明 protocol。 */
		if err = tx.QueryRow(ctx, `SELECT protocol_id,version FROM product_protocol_binding WHERE tenant_id=$1 AND product_id=$2 FOR SHARE`, v.TenantID, v.ProductID).Scan(&protocol, &version); err != nil { /* 判断条件并选择处理分支。 */
			return err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if protocol != v.ProtocolID || version != v.ProtocolVersion { /* 判断条件并选择处理分支。 */
			return errors.New("product binding changed; test again") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if b.ReuseProfile { /* 判断条件并选择处理分支。 */
			var data []byte                                                                                                                                        /* 声明 data。 */
			if err = tx.QueryRow(ctx, `SELECT body FROM device_access_profile WHERE tenant_id=$1 AND id=$2 FOR SHARE`, v.TenantID, v.ID).Scan(&data); err != nil { /* 判断条件并选择处理分支。 */
				return err /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			var old model.DeviceAccessProfile                 /* 声明 old。 */
			if err = json.Unmarshal(data, &old); err != nil { /* 判断条件并选择处理分支。 */
				return err /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			if !old.Enabled || old.ProductID != v.ProductID || old.Host != v.Host || old.Port != v.Port || old.Network != v.Network { /* 判断条件并选择处理分支。 */
				return errors.New("listener changed; test again") /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		if v.ConnectionMode != "dial" && v.Mode == "listener" && !b.ReuseProfile { /* 判断条件并选择处理分支。 */
			var used bool                                                                                                                                                                                                                                                                /* 声明 used。 */
			if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM device_access_profile WHERE enabled AND COALESCE(body->>'connectionMode','')!='dial' AND body->>'mode'='listener' AND body->>'network'=$1 AND (body->>'port')::int=$2)`, v.Network, v.Port).Scan(&used); err != nil { /* 判断条件并选择处理分支。 */
				return err /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			if used { /* 判断条件并选择处理分支。 */
				return errors.New("listener port is already reserved") /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		if !b.ReuseProfile { /* 判断条件并选择处理分支。 */
			if err = exec(`INSERT INTO device_access_profile(tenant_id,id,device_id,product_id,enabled,body) VALUES($1,$2,$3,$4,$5,$6)`, v.TenantID, v.ID, v.DeviceID, v.ProductID, v.Enabled, body(v)); err != nil { /* 判断条件并选择处理分支。 */
				return err /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	d := b.Device                                                                                                                                                                                                                    /* 更新 d 的值。 */
	if err = exec(`INSERT INTO device_registry(tenant_id,id,product_id,status,access_key,secret_hash,body) VALUES($1,$2,$3,$4,$5,$6,$7)`, d.TenantID, d.ID, d.ProductID, d.Status, d.AccessKey, d.SecretHash, body(d)); err != nil { /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return tx.Commit(ctx) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
