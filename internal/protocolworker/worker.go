// Package protocolworker defines the process protocol used by uploaded Go packages.
package protocolworker /* 声明 protocolworker 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"       /* 执行当前语句并推进处理流程。 */
	"encoding/hex"  /* 执行当前语句并推进处理流程。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"errors"        /* 执行当前语句并推进处理流程。 */
	"fmt"           /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model"  /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

const Runtime = "go-protocol-v2" /* 声明 Runtime。 */
const MaxFrameBytes = 64 << 10   /* 声明 MaxFrameBytes。 */
const MaxStateBytes = 64 << 10   /* 声明 MaxStateBytes。 */

type Request struct { /* 定义 Request 类型。 */
	DeviceID  string            `json:"deviceId,omitempty"` /* 执行当前语句并推进处理流程。 */
	Version   int               `json:"version"`            /* 执行当前语句并推进处理流程。 */
	Operation string            `json:"operation"`          /* 执行当前语句并推进处理流程。 */
	Raw       *model.RawMessage `json:"raw,omitempty"`      /* 执行当前语句并推进处理流程。 */
	Data      string            `json:"data,omitempty"`     /* 执行当前语句并推进处理流程。 */
	State     json.RawMessage   `json:"state,omitempty"`    /* 执行当前语句并推进处理流程。 */
	Command   map[string]any    `json:"command,omitempty"`  /* 执行当前语句并推进处理流程。 */
	Now       int64             `json:"now,omitempty"`      /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type ChildFrame struct { /* 定义 ChildFrame 类型。 */
	model.ChildIdentity        /* 执行当前语句并推进处理流程。 */
	Payload             string `json:"payload,omitempty"` /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type Response struct { /* 定义 Response 类型。 */
	Children        []ChildFrame           `json:"children,omitempty"`        /* 执行当前语句并推进处理流程。 */
	Error           string                 `json:"error,omitempty"`           /* 执行当前语句并推进处理流程。 */
	Consumed        int                    `json:"consumed,omitempty"`        /* 执行当前语句并推进处理流程。 */
	NeedMore        bool                   `json:"needMore,omitempty"`        /* 执行当前语句并推进处理流程。 */
	DeviceID        string                 `json:"deviceId,omitempty"`        /* 执行当前语句并推进处理流程。 */
	DeviceName      string                 `json:"deviceName,omitempty"`      /* 执行当前语句并推进处理流程。 */
	Reply           string                 `json:"reply,omitempty"`           /* 执行当前语句并推进处理流程。 */
	State           json.RawMessage        `json:"state,omitempty"`           /* 执行当前语句并推进处理流程。 */
	CorrelationID   string                 `json:"correlationId,omitempty"`   /* 执行当前语句并推进处理流程。 */
	StandardMessage *model.StandardMessage `json:"standardMessage,omitempty"` /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func HasCapability(release model.ProtocolRelease, capability string) bool { /* 定义 HasCapability 函数。 */
	for _, value := range release.Capabilities { /* 循环处理当前数据。 */
		if value == capability { /* 判断条件并选择处理分支。 */
			return true /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return false /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func Call(ctx context.Context, root string, release model.ProtocolRelease, request Request) (Response, error) { /* 定义 Call 函数。 */
	var result Response                                                                              /* 声明 result。 */
	if release.ParserType != parser.GoProtocolParserName || release.Artifact["runtime"] != Runtime { /* 判断条件并选择处理分支。 */
		return result, errors.New("接入操作需要 go-protocol-v2 协议包") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if !HasCapability(release, request.Operation) { /* 判断条件并选择处理分支。 */
		return result, fmt.Errorf("协议包未声明 %s 能力", request.Operation) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(request.State) > MaxStateBytes { /* 判断条件并选择处理分支。 */
		return result, errors.New("协议会话状态超过 64 KiB") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	request.Version = 2                 /* 更新 request.Version 的值。 */
	var data []byte                     /* 声明 data。 */
	var err error                       /* 声明 err。 */
	if request.Operation == "ingress" { /* 判断条件并选择处理分支。 */
		data, err = hex.DecodeString(request.Data)                     /* 更新 err 的值。 */
		if err != nil || len(data) == 0 || len(data) > MaxFrameBytes { /* 判断条件并选择处理分支。 */
			return result, errors.New("接入缓冲必须是 1 字节至 64 KiB 的十六进制数据") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	output, err := (parser.ExternalParser{Root: root}).Invoke(ctx, release.Config, request) /* 更新 err 的值。 */
	if err != nil {                                                                         /* 判断条件并选择处理分支。 */
		return result, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err = json.Unmarshal(output, &result); err != nil { /* 判断条件并选择处理分支。 */
		return result, fmt.Errorf("协议操作结果无效: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return result, validateResponse(request.Operation, len(data), result) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func validateResponse(operation string, dataLength int, result Response) error { /* 定义 validateResponse 函数。 */
	if result.Error != "" { /* 判断条件并选择处理分支。 */
		return errors.New(result.Error) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(result.State) > MaxStateBytes || len(result.CorrelationID) > 128 || len(result.DeviceName) > 256 { /* 判断条件并选择处理分支。 */
		return errors.New("协议操作结果字段超过上限") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if result.Reply != "" { /* 判断条件并选择处理分支。 */
		reply, decodeErr := hex.DecodeString(result.Reply)  /* 更新 decodeErr 的值。 */
		if decodeErr != nil || len(reply) > MaxFrameBytes { /* 判断条件并选择处理分支。 */
			return errors.New("协议应答必须是至多 64 KiB 的十六进制数据") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if len(result.Children) > 256 { /* 判断条件并选择处理分支。 */
		return errors.New("too many child observations") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for _, child := range result.Children { /* 循环处理当前数据。 */
		if operation != "ingress" || result.NeedMore || !model.ValidProtocolDeviceID(child.Address) || !model.ValidProtocolDeviceID(child.Type) || len(child.Name) > 256 { /* 判断条件并选择处理分支。 */
			return errors.New("invalid child observation") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		data, err := hex.DecodeString(child.Payload) /* 更新 err 的值。 */
		if err != nil || len(data) > MaxFrameBytes { /* 判断条件并选择处理分支。 */
			return errors.New("invalid child payload") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	switch operation { /* 根据条件选择处理路径。 */
	case "ingress": /* 处理当前分支。 */
		if result.NeedMore { /* 判断条件并选择处理分支。 */
			if result.Consumed != 0 || result.Reply != "" || result.DeviceID != "" { /* 判断条件并选择处理分支。 */
				return errors.New("等待完整帧时不能消耗数据、标识设备或发送应答") /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} else if result.Consumed <= 0 || result.Consumed > dataLength || !ValidDeviceID(result.DeviceID) { /* 结束当前表达式或代码块。 */
			return errors.New("协议接入结果需要有效的帧长度和设备标识") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	case "encode": /* 处理当前分支。 */
		if result.Reply == "" { /* 判断条件并选择处理分支。 */
			return errors.New("协议编码未返回下行数据") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	case "decode": /* 处理当前分支。 */
		if result.StandardMessage == nil { /* 判断条件并选择处理分支。 */
			return errors.New("协议解析未返回标准消息") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	default: /* 处理当前分支。 */
		return errors.New("不支持的协议操作") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func ValidDeviceID(id string) bool { return model.ValidProtocolDeviceID(id) } /* 定义 ValidDeviceID 函数。 */
