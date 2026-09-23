package model /* 声明 model 包。 */

// DashboardCount contains aggregate data only; no device credentials or alarm payloads.
type DashboardCount struct { /* 定义 DashboardCount 类型。 */
	Kind  string `json:"kind"`           /* 执行当前语句并推进处理流程。 */
	Key   string `json:"key"`            /* 执行当前语句并推进处理流程。 */
	Name  string `json:"name,omitempty"` /* 执行当前语句并推进处理流程。 */
	Count int    `json:"count"`          /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
