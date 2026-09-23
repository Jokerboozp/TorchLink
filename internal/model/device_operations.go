package model /* 声明 model 包。 */

type ThingField struct { /* 定义 ThingField 类型。 */
	Writable   bool   `json:"writable,omitempty"` /* 执行当前语句并推进处理流程。 */
	Identifier string `json:"identifier"`         /* 执行当前语句并推进处理流程。 */
	Name       string `json:"name"`               /* 执行当前语句并推进处理流程。 */
	DataType   string `json:"dataType"`           /* 执行当前语句并推进处理流程。 */
	Unit       string `json:"unit,omitempty"`     /* 执行当前语句并推进处理流程。 */
	Required   bool   `json:"required,omitempty"` /* 执行当前语句并推进处理流程。 */
}                            /* 结束当前表达式或代码块。 */
type ThingOperation struct { /* 定义 ThingOperation 类型。 */
	Identifier string       `json:"identifier"`       /* 执行当前语句并推进处理流程。 */
	Name       string       `json:"name"`             /* 执行当前语句并推进处理流程。 */
	Fields     []ThingField `json:"fields,omitempty"` /* 执行当前语句并推进处理流程。 */
}                        /* 结束当前表达式或代码块。 */
type ThingModel struct { /* 定义 ThingModel 类型。 */
	Properties []ThingField     `json:"properties"` /* 执行当前语句并推进处理流程。 */
	Events     []ThingOperation `json:"events"`     /* 执行当前语句并推进处理流程。 */
	Commands   []ThingOperation `json:"commands"`   /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type DeviceStateEvent struct { /* 定义 DeviceStateEvent 类型。 */
	State      DeviceState `json:"state"`      /* 执行当前语句并推进处理流程。 */
	RecordedAt int64       `json:"recordedAt"` /* 执行当前语句并推进处理流程。 */
}                                  /* 结束当前表达式或代码块。 */
type CredentialRevocation struct { /* 定义 CredentialRevocation 类型。 */
	ID        string `json:"id"`                  /* 执行当前语句并推进处理流程。 */
	TenantID  string `json:"tenantId"`            /* 执行当前语句并推进处理流程。 */
	DeviceID  string `json:"deviceId"`            /* 执行当前语句并推进处理流程。 */
	Username  string `json:"username"`            /* 执行当前语句并推进处理流程。 */
	Status    string `json:"status"`              /* 执行当前语句并推进处理流程。 */
	LastError string `json:"lastError,omitempty"` /* 执行当前语句并推进处理流程。 */
	CreatedAt int64  `json:"createdAt"`           /* 执行当前语句并推进处理流程。 */
	UpdatedAt int64  `json:"updatedAt"`           /* 执行当前语句并推进处理流程。 */
}                           /* 结束当前表达式或代码块。 */
type DeviceCommand struct { /* 定义 DeviceCommand 类型。 */
	Confirmed bool           `json:"confirmed,omitempty"` /* 执行当前语句并推进处理流程。 */
	ID        string         `json:"id"`                  /* 执行当前语句并推进处理流程。 */
	TenantID  string         `json:"tenantId"`            /* 执行当前语句并推进处理流程。 */
	ProductID string         `json:"productId"`           /* 执行当前语句并推进处理流程。 */
	DeviceID  string         `json:"deviceId"`            /* 执行当前语句并推进处理流程。 */
	Type      string         `json:"type"`                /* 执行当前语句并推进处理流程。 */
	Data      map[string]any `json:"data"`                /* 执行当前语句并推进处理流程。 */
	Status    string         `json:"status"`              /* 执行当前语句并推进处理流程。 */
	LastError string         `json:"lastError,omitempty"` /* 执行当前语句并推进处理流程。 */
	Reply     map[string]any `json:"reply,omitempty"`     /* 执行当前语句并推进处理流程。 */
	CreatedAt int64          `json:"createdAt"`           /* 执行当前语句并推进处理流程。 */
	UpdatedAt int64          `json:"updatedAt"`           /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

// ObservedOutcome projects a bounded wait without inventing an execution result.
// A later authenticated reply can still supply the actual terminal outcome.
func (c DeviceCommand) ObservedOutcome(now int64) DeviceCommand { /* 定义 ObservedOutcome 函数。 */
	if c.Status == "QUEUED" { /* 判断条件并选择处理分支。 */
		c.Status, c.LastError = "UNKNOWN", "现场命令执行器已移除，历史命令不会自动重发；请核实设备状态" /* 更新 c.LastError 的值。 */
		return c                                                           /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if (c.Status == "SENT" || c.Status == "DISPATCHING") && c.CreatedAt > 0 && now-c.CreatedAt >= 30000 { /* 判断条件并选择处理分支。 */
		c.Status = "UNKNOWN"                       /* 更新 c.Status 的值。 */
		c.LastError = "30 秒内未收到设备执行结果；请核实设备，不自动重试" /* 更新 c.LastError 的值。 */
	} /* 结束当前表达式或代码块。 */
	return c /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (c DeviceCommand) Public() DeviceCommand { return c } /* 定义 Public 函数。 */
