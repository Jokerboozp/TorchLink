package ports /* 声明 ports 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                     /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

type AccessStore interface { /* 定义 AccessStore 类型。 */
	LoadAccessState(context.Context, string) (model.AccessState, error)       /* 执行当前语句并推进处理流程。 */
	SaveAccessState(context.Context, string, model.AccessState) (bool, error) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
