package memory /* 声明 memory 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                     /* 执行当前语句并推进处理流程。 */
	"errors"                      /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func (r *Repository) SaveOnboarding(_ context.Context, b model.OnboardingBundle) error { /* 定义 SaveOnboarding 函数。 */
	r.mu.Lock()                    /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()            /* 安排函数结束时执行清理。 */
	d := b.Device                  /* 更新 d 的值。 */
	k := key(d.TenantID, d.ID)     /* 更新 k 的值。 */
	if _, ok := r.devices[k]; ok { /* 判断条件并选择处理分支。 */
		return errors.New("device already exists") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	pk := key(d.TenantID, d.ProductID) /* 更新 pk 的值。 */
	if b.Product != nil {              /* 判断条件并选择处理分支。 */
		if _, ok := r.products[pk]; ok { /* 判断条件并选择处理分支。 */
			return errors.New("product already exists") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} else if p, ok := r.products[pk]; !ok || p.Status != "ENABLED" { /* 结束当前表达式或代码块。 */
		return errors.New("product is disabled") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for _, other := range r.devices { /* 循环处理当前数据。 */
		if other.AccessKey == d.AccessKey { /* 判断条件并选择处理分支。 */
			return errors.New("access key already exists") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if v := b.Release; v != nil { /* 判断条件并选择处理分支。 */
		if old, ok := r.protocolReleases[key(v.TenantID, v.ProtocolID, v.Version)]; ok && (old.Status != "PUBLISHED" || old.ParserType != v.ParserType) { /* 判断条件并选择处理分支。 */
			return errors.New("protocol release changed; test again") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if v := b.Profile; v != nil { /* 判断条件并选择处理分支。 */
		if _, ok := r.accessProfiles[key(v.TenantID, v.ID)]; ok && !b.ReuseProfile { /* 判断条件并选择处理分支。 */
			return errors.New("profile already exists") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if b.ReuseProfile { /* 判断条件并选择处理分支。 */
			old, ok := r.accessProfiles[key(v.TenantID, v.ID)]                                                                               /* 更新 ok 的值。 */
			if !ok || !old.Enabled || old.ProductID != v.ProductID || old.Host != v.Host || old.Port != v.Port || old.Network != v.Network { /* 判断条件并选择处理分支。 */
				return errors.New("listener changed; test again") /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		binding, ok := r.protocolBindings[key(v.TenantID, v.ProductID)] /* 更新 ok 的值。 */
		if !ok && b.Binding != nil {                                    /* 判断条件并选择处理分支。 */
			binding = *b.Binding /* 更新 binding 的值。 */
		} /* 结束当前表达式或代码块。 */
		if binding.ProtocolID != v.ProtocolID || binding.Version != v.ProtocolVersion { /* 判断条件并选择处理分支。 */
			return errors.New("product binding changed; test again") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		for _, p := range r.accessProfiles { /* 循环处理当前数据。 */
			if v.ConnectionMode != "dial" && p.ConnectionMode != "dial" && !b.ReuseProfile && p.Enabled && p.Mode == "listener" && v.Mode == "listener" && p.Network == v.Network && p.Port == v.Port { /* 判断条件并选择处理分支。 */
				return errors.New("listener port is already reserved") /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if v := b.Release; v != nil { /* 判断条件并选择处理分支。 */
		rk := key(v.TenantID, v.ProtocolID, v.Version) /* 更新 rk 的值。 */
		if _, ok := r.protocolReleases[rk]; !ok {      /* 判断条件并选择处理分支。 */
			r.protocolReleases[rk] = clone(*v) /* 更新 r.protocolReleases[rk] 的值。 */
		} /* 结束当前表达式或代码块。 */
		dk := key(v.TenantID, v.ProtocolID)          /* 更新 dk 的值。 */
		if _, ok := r.protocolDefinitions[dk]; !ok { /* 判断条件并选择处理分支。 */
			r.protocolDefinitions[dk] = model.ProtocolDefinition{TenantID: v.TenantID, ID: v.ProtocolID, Name: v.ProtocolID, CreatedAt: v.CreatedAt, UpdatedAt: v.CreatedAt} /* 更新 r.protocolDefinitions[dk] 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if v := b.PointTable; v != nil { /* 判断条件并选择处理分支。 */
		pk := key(v.TenantID, v.ProtocolID, v.Version) /* 更新 pk 的值。 */
		if _, ok := r.pointTables[pk]; !ok {           /* 判断条件并选择处理分支。 */
			r.pointTables[pk] = clone(*v) /* 更新 r.pointTables[pk] 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if b.Product != nil { /* 判断条件并选择处理分支。 */
		r.products[pk] = clone(*b.Product)                                                                                                                                                                                                                                                                                        /* 更新 r.products[pk] 的值。 */
		v := b.Release                                                                                                                                                                                                                                                                                                            /* 更新 v 的值。 */
		pkg := model.ProtocolPackage{TenantID: v.TenantID, ID: b.Product.ProtocolPackageID, Name: v.ProtocolID, Version: v.Version, Protocol: v.ProtocolID, Transport: v.Transport, PayloadFormat: v.PayloadFormat, ParserType: v.ParserType, Status: v.Status, Config: v.Config, CreatedAt: v.CreatedAt, UpdatedAt: v.CreatedAt} /* 更新 pkg 的值。 */
		if _, ok := r.protocols[key(pkg.TenantID, pkg.ID)]; !ok {                                                                                                                                                                                                                                                                 /* 判断条件并选择处理分支。 */
			r.protocols[key(pkg.TenantID, pkg.ID)] = clone(pkg) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if v := b.Binding; v != nil { /* 判断条件并选择处理分支。 */
		bk := key(v.TenantID, v.ProductID)        /* 更新 bk 的值。 */
		if _, ok := r.protocolBindings[bk]; !ok { /* 判断条件并选择处理分支。 */
			r.protocolBindings[bk] = *v /* 更新 r.protocolBindings[bk] 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if v := b.Profile; v != nil && !b.ReuseProfile { /* 判断条件并选择处理分支。 */
		r.accessProfiles[key(v.TenantID, v.ID)] = clone(*v) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	saved := clone(d)               /* 更新 saved 的值。 */
	saved.SecretHash = d.SecretHash /* 更新 saved.SecretHash 的值。 */
	r.devices[k] = saved            /* 更新 r.devices[k] 的值。 */
	return nil                      /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
