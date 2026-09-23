package memory /* 声明 memory 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"iot-platform/internal/repositorytest" /* 执行当前语句并推进处理流程。 */
	"testing"                              /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestProtocolChildren(t *testing.T) { repositorytest.ProtocolChildren(t, NewRepository()) } /* 定义 TestProtocolChildren 函数。 */
