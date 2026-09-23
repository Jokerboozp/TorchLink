package core /* 声明 core 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"       /* 执行当前语句并推进处理流程。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"fmt"           /* 执行当前语句并推进处理流程。 */
	"reflect"       /* 执行当前语句并推进处理流程。 */
	"time"          /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/ports" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func (e *Engine) StartReplay(ctx context.Context, req model.ReplayRequest) (model.ReplayRequest, error) { /* 定义 StartReplay 函数。 */
	if req.TenantID == "" || req.Start <= 0 || req.End <= req.Start { /* 判断条件并选择处理分支。 */
		return req, fmt.Errorf("tenantId and a valid start/end range are required") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	switch req.Mode { /* 根据条件选择处理路径。 */
	case "DRY_RUN", "REINGEST", "DIFF": /* 处理当前分支。 */
	default: /* 处理当前分支。 */
		return req, fmt.Errorf("mode must be DRY_RUN, REINGEST or DIFF") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if req.RatePerSecond <= 0 { /* 判断条件并选择处理分支。 */
		req.RatePerSecond = 100 /* 更新 req.RatePerSecond 的值。 */
	} /* 结束当前表达式或代码块。 */
	req.ID = id("replay")                               /* 更新 req.ID 的值。 */
	req.Status = "PENDING"                              /* 更新 req.Status 的值。 */
	req.CreatedAt = e.Clock.Now().UnixMilli()           /* 更新 req.CreatedAt 的值。 */
	if err := e.Repo.SaveReplay(ctx, req); err != nil { /* 判断条件并选择处理分支。 */
		return req, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	_ = e.Repo.SaveAudit(ctx, model.AuditLog{ID: id("audit"), TenantID: req.TenantID, Actor: req.CreatedBy, Action: "replay.create", TargetType: "replay", TargetID: req.ID, Details: map[string]any{"mode": req.Mode, "start": req.Start, "end": req.End}, CreatedAt: req.CreatedAt}) /* 更新 _ 的值。 */
	go e.runReplay(context.Background(), req)                                                                                                                                                                                                                                          /* 执行当前语句并推进处理流程。 */
	return req, nil                                                                                                                                                                                                                                                                    /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (e *Engine) runReplay(ctx context.Context, req model.ReplayRequest) { /* 定义 runReplay 函数。 */
	req.Status = "RUNNING"                                                                    /* 更新 req.Status 的值。 */
	_ = e.Repo.UpdateReplay(ctx, req)                                                         /* 更新 _ 的值。 */
	ticker := time.NewTicker(time.Second / time.Duration(req.RatePerSecond))                  /* 更新 ticker 的值。 */
	defer ticker.Stop()                                                                       /* 安排函数结束时执行清理。 */
	offset := 0                                                                               /* 更新 offset 的值。 */
	req.DiffSummary = map[string]int{"unchanged": 0, "changed": 0, "missing": 0, "errors": 0} /* 更新 req.DiffSummary 的值。 */
	for {                                                                                     /* 循环处理当前数据。 */
		indexes, err := e.Repo.ListRawIndexes(ctx, ports.RawFilter{TenantID: req.TenantID, ProductID: req.ProductID, DeviceID: req.DeviceID, Start: req.Start, End: req.End, Limit: 500, Offset: offset}) /* 更新 err 的值。 */
		if err != nil {                                                                                                                                                                                   /* 判断条件并选择处理分支。 */
			e.Log.Error("list replay archive indexes", "replayId", req.ID, "error", err) /* 执行当前语句并推进处理流程。 */
			req.Status = "FAILED"                                                        /* 更新 req.Status 的值。 */
			req.Failed++                                                                 /* 执行当前语句并推进处理流程。 */
			break                                                                        /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if len(indexes) == 0 { /* 判断条件并选择处理分支。 */
			req.Status = "COMPLETED" /* 更新 req.Status 的值。 */
			break                    /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		for _, idx := range indexes { /* 循环处理当前数据。 */
			<-ticker.C                     /* 执行当前语句并推进处理流程。 */
			raw, err := e.GetRaw(ctx, idx) /* 更新 err 的值。 */
			if err != nil {                /* 判断条件并选择处理分支。 */
				e.Log.Error("read replay archive", "replayId", req.ID, "messageId", idx.MessageID, "bucket", idx.ObjectBucket, "key", idx.ObjectKey, "error", err) /* 执行当前语句并推进处理流程。 */
				req.Failed++                                                                                                                                       /* 执行当前语句并推进处理流程。 */
				continue                                                                                                                                           /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
			raw.ParserVersion = req.ParserVersion /* 更新 raw.ParserVersion 的值。 */
			switch req.Mode {                     /* 根据条件选择处理路径。 */
			case "REINGEST": /* 处理当前分支。 */
				b, _ := json.Marshal(raw)                                 /* 更新 _ 的值。 */
				err = e.Bus.Publish(ctx, model.TopicRaw, raw.DeviceID, b) /* 更新 err 的值。 */
			case "DRY_RUN": /* 处理当前分支。 */
				_, err = e.parseReplay(ctx, raw, req.ParserVersion) /* 更新 err 的值。 */
			case "DIFF": /* 处理当前分支。 */
				var current *model.StandardMessage                                      /* 声明 current。 */
				current, err = e.parseReplay(ctx, raw, req.ParserVersion)               /* 更新 err 的值。 */
				diff := model.ReplayDiff{RawMessageID: raw.MessageID, Current: current} /* 更新 diff 的值。 */
				if err == nil {                                                         /* 判断条件并选择处理分支。 */
					previous, previousErr := e.Repo.GetStandardMessageByRaw(ctx, req.TenantID, raw.MessageID) /* 更新 previousErr 的值。 */
					if previousErr != nil {                                                                   /* 判断条件并选择处理分支。 */
						diff.Status = "MISSING"      /* 更新 diff.Status 的值。 */
						req.DiffSummary["missing"]++ /* 执行当前语句并推进处理流程。 */
					} else { /* 结束当前表达式或代码块。 */
						diff.Previous = &previous                  /* 更新 diff.Previous 的值。 */
						if equivalentMessage(previous, *current) { /* 判断条件并选择处理分支。 */
							diff.Status = "UNCHANGED"      /* 更新 diff.Status 的值。 */
							req.DiffSummary["unchanged"]++ /* 执行当前语句并推进处理流程。 */
						} else { /* 结束当前表达式或代码块。 */
							diff.Status = "CHANGED"      /* 更新 diff.Status 的值。 */
							req.DiffSummary["changed"]++ /* 执行当前语句并推进处理流程。 */
						} /* 结束当前表达式或代码块。 */
					} /* 结束当前表达式或代码块。 */
				} /* 结束当前表达式或代码块。 */
				if err != nil { /* 判断条件并选择处理分支。 */
					diff.Status = "ERROR"       /* 更新 diff.Status 的值。 */
					diff.Error = err.Error()    /* 更新 diff.Error 的值。 */
					req.DiffSummary["errors"]++ /* 执行当前语句并推进处理流程。 */
				} /* 结束当前表达式或代码块。 */
				if len(req.Diffs) < 1000 && diff.Status != "UNCHANGED" { /* 判断条件并选择处理分支。 */
					req.Diffs = append(req.Diffs, diff) /* 更新 req.Diffs 的值。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
			if err != nil { /* 判断条件并选择处理分支。 */
				e.Log.Error("process replay message", "replayId", req.ID, "messageId", idx.MessageID, "mode", req.Mode, "error", err) /* 执行当前语句并推进处理流程。 */
				req.Failed++                                                                                                          /* 执行当前语句并推进处理流程。 */
			} else { /* 结束当前表达式或代码块。 */
				req.Processed++ /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		offset += len(indexes)            /* 更新 offset 的值。 */
		_ = e.Repo.UpdateReplay(ctx, req) /* 更新 _ 的值。 */
		if len(indexes) < 500 {           /* 判断条件并选择处理分支。 */
			req.Status = "COMPLETED" /* 更新 req.Status 的值。 */
			break                    /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	req.CompletedAt = e.Clock.Now().UnixMilli() /* 更新 req.CompletedAt 的值。 */
	_ = e.Repo.UpdateReplay(ctx, req)           /* 更新 _ 的值。 */
} /* 结束当前表达式或代码块。 */

func (e *Engine) parseReplay(ctx context.Context, raw model.RawMessage, version string) (*model.StandardMessage, error) { /* 定义 parseReplay 函数。 */
	protocolID, releaseVersion := raw.ProtocolID, raw.ProtocolVersion                                                 /* 更新 releaseVersion 的值。 */
	if binding, bindingErr := e.Repo.GetProductProtocolBinding(ctx, raw.TenantID, raw.ProductID); bindingErr == nil { /* 判断条件并选择处理分支。 */
		if protocolID == "" { /* 判断条件并选择处理分支。 */
			protocolID = binding.ProtocolID /* 更新 protocolID 的值。 */
		} /* 结束当前表达式或代码块。 */
		if releaseVersion == "" { /* 判断条件并选择处理分支。 */
			releaseVersion = binding.Version /* 更新 releaseVersion 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if version != "" && protocolID != "" { /* 判断条件并选择处理分支。 */
		releaseVersion = version /* 更新 releaseVersion 的值。 */
	} /* 结束当前表达式或代码块。 */
	if protocolID != "" && releaseVersion != "" { /* 判断条件并选择处理分支。 */
		release, releaseErr := e.Repo.GetProtocolRelease(ctx, raw.TenantID, protocolID, releaseVersion) /* 更新 releaseErr 的值。 */
		if releaseErr != nil {                                                                          /* 判断条件并选择处理分支。 */
			return nil, releaseErr /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if release.Status == "REVOKED" { /* 判断条件并选择处理分支。 */
			return nil, fmt.Errorf("protocol release %s@%s is revoked", protocolID, releaseVersion) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		raw.ProtocolID, raw.ProtocolVersion, raw.PointTableVersion = protocolID, releaseVersion, release.PointTableVersion /* 更新 raw.PointTableVersion 的值。 */
		return e.Parsers.ParseWithConfig(release.ParserType, release.Config, raw)                                          /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	product, err := e.Repo.GetProduct(ctx, raw.TenantID, raw.ProductID) /* 更新 err 的值。 */
	if err == nil && product.ProtocolPackageID != "" {                  /* 判断条件并选择处理分支。 */
		pkg, pkgErr := e.Repo.GetProtocolPackage(ctx, raw.TenantID, product.ProtocolPackageID) /* 更新 pkgErr 的值。 */
		if pkgErr == nil {                                                                     /* 判断条件并选择处理分支。 */
			return e.Parsers.ParseVersionWithConfig(pkg.ParserType, version, pkg.Config, raw) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if version != "" { /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("cannot select parser version %s without a product protocol package", version) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return e.Parsers.Parse(raw) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func equivalentMessage(a, b model.StandardMessage) bool { /* 定义 equivalentMessage 函数。 */
	return a.MessageType == b.MessageType && equivalentMap(a.Properties, b.Properties) && equivalentMap(a.Event, b.Event) && equivalentTags(a.Tags, b.Tags) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func equivalentMap(a, b map[string]any) bool { /* 定义 equivalentMap 函数。 */
	return len(a) == 0 && len(b) == 0 || reflect.DeepEqual(a, b) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func equivalentTags(a, b map[string]string) bool { /* 定义 equivalentTags 函数。 */
	return len(a) == 0 && len(b) == 0 || reflect.DeepEqual(a, b) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
