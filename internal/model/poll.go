package model /* 声明 model 包。 */

type PollPoint struct { /* 定义 PollPoint 类型。 */
	Identifier string  `json:"identifier"`       /* 执行当前语句并推进处理流程。 */
	Address    string  `json:"address"`          /* 执行当前语句并推进处理流程。 */
	Scale      float64 `json:"scale,omitempty"`  /* 执行当前语句并推进处理流程。 */
	Offset     float64 `json:"offset,omitempty"` /* 执行当前语句并推进处理流程。 */
}                       /* 结束当前表达式或代码块。 */
type PollValue struct { /* 定义 PollValue 类型。 */
	Address   string `json:"address"`             /* 执行当前语句并推进处理流程。 */
	Value     any    `json:"value"`               /* 执行当前语句并推进处理流程。 */
	Quality   string `json:"quality"`             /* 执行当前语句并推进处理流程。 */
	Timestamp int64  `json:"timestamp,omitempty"` /* 执行当前语句并推进处理流程。 */
}                          /* 结束当前表达式或代码块。 */
type PollResponse struct { /* 定义 PollResponse 类型。 */
	Transport string      `json:"transport"` /* 执行当前语句并推进处理流程。 */
	Values    []PollValue `json:"values"`    /* 执行当前语句并推进处理流程。 */
	Response  any         `json:"response"`  /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
