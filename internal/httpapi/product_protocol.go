package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context" /* 执行当前语句并推进处理流程。 */
	"errors"  /* 执行当前语句并推进处理流程。 */
	"fmt"     /* 执行当前语句并推进处理流程。 */
	"strings" /* 执行当前语句并推进处理流程。 */
	"time"    /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model"  /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

// Products can be created before any devices or access instances exist.
// Resolve only immutable, published releases; uploaded drafts remain unusable.
func (s *Server) productProtocol(ctx context.Context, tenant, id string) (model.ProtocolPackage, error) { /* 定义 productProtocol 函数。 */
	if id == parser.StandardProtocolID+"@1.0.0" { /* 判断条件并选择处理分支。 */
		release, err := s.engine.Repo.GetProtocolRelease(ctx, tenant, parser.StandardProtocolID, "1.0.0") /* 更新 err 的值。 */
		if errors.Is(err, model.ErrNotFound) {                                                            /* 判断条件并选择处理分支。 */
			now := time.Now().UnixMilli()                                                                                                                                                                                                                           /* 更新 now 的值。 */
			release = model.ProtocolRelease{TenantID: tenant, ProtocolID: parser.StandardProtocolID, Version: "1.0.0", Transport: "MQTT_HTTP", PayloadFormat: "json", ParserType: parser.StandardParserName, Status: "PUBLISHED", CreatedAt: now, PublishedAt: now} /* 更新 release 的值。 */
			if err = s.engine.Repo.CreateProtocolRelease(ctx, release); err != nil {                                                                                                                                                                                /* 判断条件并选择处理分支。 */
				// Another product may have initialized the same tenant's release.
				release, err = s.engine.Repo.GetProtocolRelease(ctx, tenant, parser.StandardProtocolID, "1.0.0") /* 更新 err 的值。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		if err != nil { /* 判断条件并选择处理分支。 */
			return model.ProtocolPackage{}, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if release.Status != "PUBLISHED" || release.ParserType != parser.StandardParserName { /* 判断条件并选择处理分支。 */
			return model.ProtocolPackage{}, fmt.Errorf("标准协议当前不可用") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		pkg := legacyProtocolShim(release)                      /* 更新 pkg 的值。 */
		pkg.Name = "标准设备上报"                                     /* 更新 pkg.Name 的值。 */
		return pkg, s.engine.Repo.SaveProtocolPackage(ctx, pkg) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	pkg, err := s.engine.Repo.GetProtocolPackage(ctx, tenant, id) /* 更新 err 的值。 */
	if err == nil {                                               /* 判断条件并选择处理分支。 */
		if pkg.ParserType == parser.GoProtocolParserName { /* 判断条件并选择处理分支。 */
			release, err := s.engine.Repo.GetProtocolRelease(ctx, tenant, pkg.Protocol, pkg.Version) /* 更新 err 的值。 */
			if err != nil {                                                                          /* 判断条件并选择处理分支。 */
				return pkg, err /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			if release.Status != "PUBLISHED" { /* 判断条件并选择处理分支。 */
				return pkg, fmt.Errorf("请选择已发布的协议版本") /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		return pkg, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if !errors.Is(err, model.ErrNotFound) { /* 判断条件并选择处理分支。 */
		return pkg, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	index := strings.LastIndex(id, "@") /* 更新 index 的值。 */
	if index <= 0 {                     /* 判断条件并选择处理分支。 */
		return pkg, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	release, err := s.engine.Repo.GetProtocolRelease(ctx, tenant, id[:index], id[index+1:]) /* 更新 err 的值。 */
	if err != nil {                                                                         /* 判断条件并选择处理分支。 */
		return pkg, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if release.Status != "PUBLISHED" { /* 判断条件并选择处理分支。 */
		return pkg, fmt.Errorf("请选择已发布的协议版本") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	pkg = legacyProtocolShim(release)                       /* 更新 pkg 的值。 */
	return pkg, s.engine.Repo.SaveProtocolPackage(ctx, pkg) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
