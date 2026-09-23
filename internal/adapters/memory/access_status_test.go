package memory /* 声明 memory 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"iot-platform/internal/repositorytest" /* 执行当前语句并推进处理流程。 */
	"testing"                              /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestAccessStatusDoesNotOverwriteConfiguration(t *testing.T) { /* 定义 TestAccessStatusDoesNotOverwriteConfiguration 函数。 */
	repositorytest.AccessStatus(t, NewRepository()) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func TestExecutionLeaseOwnership(t *testing.T) { repositorytest.ExecutionLease(t, NewRepository()) } /* 定义 TestExecutionLeaseOwnership 函数。 */

func TestRawReservationBeforeArchive(t *testing.T) { repositorytest.RawReservation(t, NewRepository()) } /* 定义 TestRawReservationBeforeArchive 函数。 */

func TestProtocolRegistration(t *testing.T) { repositorytest.ProtocolRegistration(t, NewRepository()) } /* 定义 TestProtocolRegistration 函数。 */
