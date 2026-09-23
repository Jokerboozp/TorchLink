package httpapi /* 声明 httpapi 包。 */

import "iot-platform/internal/protocolworker" /* 引入当前代码需要的依赖。 */

type protocolOperationCase struct { /* 定义 protocolOperationCase 类型。 */
	Name     string                 `json:"name"`     /* 执行当前语句并推进处理流程。 */
	Request  protocolworker.Request `json:"request"`  /* 执行当前语句并推进处理流程。 */
	Expected map[string]any         `json:"expected"` /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
