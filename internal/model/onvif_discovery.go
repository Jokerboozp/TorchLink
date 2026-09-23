package model /* 声明 model 包。 */

type ONVIFCandidate struct { /* 定义 ONVIFCandidate 类型。 */
	EndpointID string   `json:"endpointId"` /* 执行当前语句并推进处理流程。 */
	SourceIP   string   `json:"sourceIp"`   /* 执行当前语句并推进处理流程。 */
	XAddrs     []string `json:"xAddrs"`     /* 执行当前语句并推进处理流程。 */
	Scopes     []string `json:"scopes"`     /* 执行当前语句并推进处理流程。 */
	ObservedAt int64    `json:"observedAt"` /* 执行当前语句并推进处理流程。 */
}                            /* 结束当前表达式或代码块。 */
type ONVIFDiscovery struct { /* 定义 ONVIFDiscovery 类型。 */
	Items         []ONVIFCandidate `json:"items"`         /* 执行当前语句并推进处理流程。 */
	Discarded     int              `json:"discarded"`     /* 执行当前语句并推进处理流程。 */
	Truncated     bool             `json:"truncated"`     /* 执行当前语句并推进处理流程。 */
	Authenticated bool             `json:"authenticated"` /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
