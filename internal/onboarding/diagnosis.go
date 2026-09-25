package onboarding

import "iot-platform/internal/model"

// IngestSummary is the evidence gathered for one device since a check started.
// Only field ingress for the current product, device, connection and protocol
// version is counted; simulated or replayed messages are excluded upstream.
type IngestSummary struct {
	ConfigurationSaved   bool
	RawReceived          bool
	ReceivedAt           int64
	Parsed               bool
	ParseError           string
	Stale                bool
	ContinuouslyUpdating bool
	PreviousParsedAt     int64
}

// DiagnosisInput describes the device configuration relevant to its first data.
type DiagnosisInput struct {
	ProductEnabled bool
	ProtocolValid  bool
	DeviceEnabled  bool
	// Credential devices report through the platform ingress (standard or
	// managed HTTP). AddressReady tells whether the public address is configured.
	UsesCredentials bool
	AddressReady    bool
	IsChild         bool
	ParentVisible   bool
	Profile         *model.DeviceAccessProfile
	Ingest          IngestSummary
}

type DiagnosisCheck struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	State  string `json:"state"` // passed, waiting or failed
	Detail string `json:"detail"`
	At     int64  `json:"at,omitempty"`
}

type Diagnosis struct {
	Stage            string           `json:"stage"`
	Tone             string           `json:"tone"` // success, info, warning or error
	Title            string           `json:"title"`
	NextAction       string           `json:"nextAction"`
	PreviousParsedAt int64            `json:"previousParsedAt,omitempty"`
	Checks           []DiagnosisCheck `json:"checks"`
}

func profileServiceFailed(p *model.DeviceAccessProfile) bool {
	return p != nil && (!p.Enabled || p.RuntimeStatus == "DISABLED" || p.RuntimeStatus == "ERROR" || p.RuntimeStatus == "UNSUPPORTED")
}

func profileNeedsPublicHost(p *model.DeviceAccessProfile) bool {
	return p != nil && p.Mode == "listener" && p.ConnectionMode != "dial" && p.PublicHost == ""
}

// Diagnose explains the current onboarding result in the order an operator
// would fix it: configuration first, then transport, then parsing and freshness.
func Diagnose(in DiagnosisInput) Diagnosis {
	d := Diagnosis{Checks: diagnosisChecks(in)}
	set := func(stage, tone, title, next string) Diagnosis {
		d.Stage, d.Tone, d.Title, d.NextAction = stage, tone, title, next
		return d
	}
	ingest, p := in.Ingest, in.Profile
	switch {
	case !in.ProductEnabled:
		return set("PRODUCT_DISABLED", "warning", "设备模板已停用或不存在", "在设备模板中核对模板状态和协议绑定。")
	case !in.ProtocolValid:
		return set("PROTOCOL_INVALID", "warning", "设备通信协议不可用", "在设备模板的“协议版本”中绑定已发布的协议版本。")
	case !in.DeviceEnabled:
		return set("DEVICE_DISABLED", "warning", "设备已停用", "在设备列表中编辑设备并重新启用。")
	case in.IsChild && !in.ParentVisible:
		return set("PARENT_UNAVAILABLE", "warning", "所属主设备当前不可查看", "确认主设备仍然存在，且当前账号有主设备的访问权限。")
	case !in.UsesCredentials && p == nil:
		return set("PROFILE_MISSING", "warning", "尚未关联平台连接", "为设备选择接入点，或填写平台主动连接所需的设备地址。")
	case p != nil && (!p.Enabled || p.RuntimeStatus == "DISABLED"):
		return set("PROFILE_DISABLED", "warning", "接入点未启用", "在设备模板的接入点中启用对应连接。")
	case p != nil && (p.RuntimeStatus == "ERROR" || p.RuntimeStatus == "UNSUPPORTED"):
		return set("PROFILE_ERROR", "error", "平台连接服务异常", "查看接入点的运行状态与最近错误，修复后等待设备重新上报。")
	case profileNeedsPublicHost(p) && !ingest.RawReceived:
		return set("PUBLIC_HOST_MISSING", "warning", "平台对外地址未配置", "在接入点中填写现场设备可访问的平台域名或 IP。")
	case ingest.ParseError != "":
		return set("PARSE_FAILED", "error", "收到数据，解析失败", "查看原始报文，核对已发布的协议版本与设备实际报文。")
	case ingest.RawReceived && !ingest.Parsed:
		return set("RAW_RECEIVED", "info", "收到数据，等待解析", "稍后刷新；长时间未解析时查看原始报文的处理状态。")
	case ingest.Parsed && ingest.Stale:
		return set("STALE", "warning", "曾解析成功，最近没有新数据", "核对设备供电和网络，查看最近接收时间。")
	case ingest.Parsed && ingest.ContinuouslyUpdating:
		return set("CONTINUOUS", "success", "接入成功，数据持续更新", "可以进入日常管理，继续观察设备数据。")
	case ingest.Parsed:
		return set("PARSED", "success", "本次解析成功，等待下一次上报", "持续收到两次以上有效数据后确认接入稳定。")
	case in.UsesCredentials && !in.AddressReady:
		return set("ADDRESS_MISSING", "warning", "平台对外地址未配置", "请管理员配置设备接入的对外地址后，再按设备端信息连接。")
	case ingest.PreviousParsedAt > 0:
		d.PreviousParsedAt = ingest.PreviousParsedAt
		return set("PREVIOUSLY_PARSED", "info", "曾接入成功，等待本次验证", "让现场设备重新上报，页面会自动刷新检查结果。")
	default:
		return set("WAITING", "info", "等待设备本次上报", "按设备端配置完成连接后，页面会自动刷新检查结果。")
	}
}

func diagnosisChecks(in DiagnosisInput) []DiagnosisCheck {
	ingest, p := in.Ingest, in.Profile
	saved := DiagnosisCheck{Key: "configuration", Label: "配置保存", State: "waiting", Detail: "等待确认"}
	if ingest.ConfigurationSaved {
		saved.State, saved.Detail = "passed", "已保存"
	}
	service := DiagnosisCheck{Key: "service", Label: "接收服务", State: "waiting", Detail: "等待连接配置"}
	switch {
	case in.UsesCredentials && !in.AddressReady:
		service.State, service.Detail = "failed", "对外地址未配置"
	case !in.UsesCredentials && profileServiceFailed(p):
		service.State, service.Detail = "failed", "连接服务异常或未启用"
	case ingest.RawReceived:
		service.State, service.Detail = "passed", "已收到设备数据"
	case in.UsesCredentials:
		service.Detail = "接口已就绪，等待设备连接"
	case p != nil && p.Enabled && (p.RuntimeStatus == "LISTENING" || p.RuntimeStatus == "CONNECTED" || p.RuntimeStatus == "ONLINE"):
		service.State, service.Detail = "passed", "服务运行中"
	}
	raw := DiagnosisCheck{Key: "raw", Label: "收到原始报文", State: "waiting", Detail: "本次尚未收到"}
	if ingest.RawReceived {
		raw.State, raw.Detail, raw.At = "passed", "已接收", ingest.ReceivedAt
	}
	parsed := DiagnosisCheck{Key: "parsed", Label: "解析结果", State: "waiting", Detail: "等待解析"}
	switch {
	case ingest.ParseError != "":
		parsed.State, parsed.Detail = "failed", "解析失败"
	case ingest.Parsed:
		parsed.State, parsed.Detail = "passed", "本次解析成功"
	}
	continuous := DiagnosisCheck{Key: "continuous", Label: "持续上报", State: "waiting", Detail: "等待后续上报"}
	switch {
	case ingest.Stale:
		continuous.State, continuous.Detail = "failed", "超过 15 分钟没有新数据"
	case ingest.ContinuouslyUpdating:
		continuous.State, continuous.Detail = "passed", "已有连续上报"
	}
	return []DiagnosisCheck{saved, service, raw, parsed, continuous}
}
