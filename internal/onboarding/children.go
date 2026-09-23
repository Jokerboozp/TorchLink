package onboarding /* 声明 onboarding 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                              /* 执行当前语句并推进处理流程。 */
	"errors"                               /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"          /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/ports"          /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/protocolworker" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func ValidateChildProducts(ctx context.Context, repo ports.Repository, p model.DeviceAccessProfile, parent model.ProtocolRelease) error { /* 定义 ValidateChildProducts 函数。 */
	if len(p.Queries) > 0 && !protocolworker.HasCapability(parent, "encode") { /* 判断条件并选择处理分支。 */
		return errors.New("定时查询需要协议 encode 能力") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for _, mapping := range p.ChildProducts { /* 循环处理当前数据。 */
		product, err := repo.GetProduct(ctx, p.TenantID, mapping.ProductID) /* 更新 err 的值。 */
		if err != nil || product.Status != "ENABLED" {                      /* 判断条件并选择处理分支。 */
			return errors.New("子设备产品不存在或未启用") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		binding, err := repo.GetProductProtocolBinding(ctx, p.TenantID, product.ID) /* 更新 err 的值。 */
		if err != nil {                                                             /* 判断条件并选择处理分支。 */
			return errors.New("请先为每种子设备产品绑定协议") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		release, err := repo.GetProtocolRelease(ctx, p.TenantID, binding.ProtocolID, binding.Version) /* 更新 err 的值。 */
		if err != nil || release.Status != "PUBLISHED" || release.PayloadFormat != "hex" {            /* 判断条件并选择处理分支。 */
			return errors.New("子设备产品需要已发布的 HEX 报文解析协议") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
