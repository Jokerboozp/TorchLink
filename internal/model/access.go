package model /* 声明 model 包。 */

// AccessState is tenant-scoped and persisted with optimistic concurrency.
type AccessState struct { /* 定义 AccessState 类型。 */
	Revision int64          `json:"revision"` /* 执行当前语句并推进处理流程。 */
	Users    []PlatformUser `json:"users"`    /* 执行当前语句并推进处理流程。 */
	Roles    []PlatformRole `json:"roles"`    /* 执行当前语句并推进处理流程。 */
}                          /* 结束当前表达式或代码块。 */
type PlatformUser struct { /* 定义 PlatformUser 类型。 */
	Username       string   `json:"username"`               /* 执行当前语句并推进处理流程。 */
	DisplayName    string   `json:"displayName"`            /* 执行当前语句并推进处理流程。 */
	PasswordHash   string   `json:"passwordHash,omitempty"` /* 执行当前语句并推进处理流程。 */
	Enabled        bool     `json:"enabled"`                /* 执行当前语句并推进处理流程。 */
	RoleIDs        []string `json:"roleIds"`                /* 执行当前语句并推进处理流程。 */
	Permissions    []string `json:"permissions"`            /* 执行当前语句并推进处理流程。 */
	DeviceScope    string   `json:"deviceScope"`            /* 执行当前语句并推进处理流程。 */
	DeviceIDs      []string `json:"deviceIds"`              /* 执行当前语句并推进处理流程。 */
	SessionVersion int64    `json:"sessionVersion"`         /* 执行当前语句并推进处理流程。 */
}                          /* 结束当前表达式或代码块。 */
type PlatformRole struct { /* 定义 PlatformRole 类型。 */
	ID          string   `json:"id"`          /* 执行当前语句并推进处理流程。 */
	Name        string   `json:"name"`        /* 执行当前语句并推进处理流程。 */
	Description string   `json:"description"` /* 执行当前语句并推进处理流程。 */
	Permissions []string `json:"permissions"` /* 执行当前语句并推进处理流程。 */
	DeviceScope string   `json:"deviceScope"`
	DeviceIDs   []string `json:"deviceIds"`
} /* 结束当前表达式或代码块。 */
