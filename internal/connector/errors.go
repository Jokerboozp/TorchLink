package connector /* 声明 connector 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context" /* 执行当前语句并推进处理流程。 */
	"errors"  /* 执行当前语句并推进处理流程。 */
	"net"     /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

var ErrAuthentication = errors.New("connector authentication failed") /* 声明 ErrAuthentication。 */

// ErrorCode uses typed errors; arbitrary broker/driver text is never interpreted
// as proof of an authentication failure.
func ErrorCode(err error, fallback string) string { /* 定义 ErrorCode 函数。 */
	if errors.Is(err, ErrAuthentication) { /* 判断条件并选择处理分支。 */
		return "AUTH_FAILED" /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var network net.Error                                                                            /* 声明 network。 */
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &network) && network.Timeout()) { /* 判断条件并选择处理分支。 */
		return "TIMEOUT" /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if errors.Is(err, context.Canceled) { /* 判断条件并选择处理分支。 */
		return "CANCELED" /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return fallback /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
