package model /* 声明 model 包。 */

// ExecutionLease uses a monotonic fencing token when ownership changes.
type ExecutionLease struct { /* 定义 ExecutionLease 类型。 */
	TenantID  string `json:"tenantId"`  /* 执行当前语句并推进处理流程。 */
	Resource  string `json:"resource"`  /* 执行当前语句并推进处理流程。 */
	Owner     string `json:"owner"`     /* 执行当前语句并推进处理流程。 */
	Endpoint  string `json:"endpoint"`  /* 执行当前语句并推进处理流程。 */
	Token     int64  `json:"token"`     /* 执行当前语句并推进处理流程。 */
	ExpiresAt int64  `json:"expiresAt"` /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
