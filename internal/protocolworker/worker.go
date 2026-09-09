// Package protocolworker defines the process protocol used by uploaded Go packages.
package protocolworker

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"iot-platform/internal/model"
	"iot-platform/internal/parser"
)

const Runtime = "go-protocol-v2"
const MaxFrameBytes = 64 << 10
const MaxStateBytes = 64 << 10

type Request struct {
	Version   int               `json:"version"`
	Operation string            `json:"operation"`
	Raw       *model.RawMessage `json:"raw,omitempty"`
	Data      string            `json:"data,omitempty"`
	State     json.RawMessage   `json:"state,omitempty"`
	Command   map[string]any    `json:"command,omitempty"`
	Now       int64             `json:"now,omitempty"`
}

type Response struct {
	Error           string                 `json:"error,omitempty"`
	Consumed        int                    `json:"consumed,omitempty"`
	NeedMore        bool                   `json:"needMore,omitempty"`
	DeviceID        string                 `json:"deviceId,omitempty"`
	DeviceName      string                 `json:"deviceName,omitempty"`
	Reply           string                 `json:"reply,omitempty"`
	State           json.RawMessage        `json:"state,omitempty"`
	CorrelationID   string                 `json:"correlationId,omitempty"`
	StandardMessage *model.StandardMessage `json:"standardMessage,omitempty"`
}

func HasCapability(release model.ProtocolRelease, capability string) bool {
	for _, value := range release.Capabilities {
		if value == capability {
			return true
		}
	}
	return false
}

func Call(ctx context.Context, root string, release model.ProtocolRelease, request Request) (Response, error) {
	var result Response
	if release.ParserType != parser.GoProtocolParserName || release.Artifact["runtime"] != Runtime {
		return result, errors.New("接入操作需要 go-protocol-v2 协议包")
	}
	if !HasCapability(release, request.Operation) {
		return result, fmt.Errorf("协议包未声明 %s 能力", request.Operation)
	}
	if len(request.State) > MaxStateBytes {
		return result, errors.New("协议会话状态超过 64 KiB")
	}
	request.Version = 2
	var data []byte
	var err error
	if request.Operation == "ingress" {
		data, err = hex.DecodeString(request.Data)
		if err != nil || len(data) == 0 || len(data) > MaxFrameBytes {
			return result, errors.New("接入缓冲必须是 1 字节至 64 KiB 的十六进制数据")
		}
	}
	output, err := (parser.ExternalParser{Root: root}).Invoke(ctx, release.Config, request)
	if err != nil {
		return result, err
	}
	if err = json.Unmarshal(output, &result); err != nil {
		return result, fmt.Errorf("协议操作结果无效: %w", err)
	}
	return result, validateResponse(request.Operation, len(data), result)
}

func validateResponse(operation string, dataLength int, result Response) error {
	if result.Error != "" {
		return errors.New(result.Error)
	}
	if len(result.State) > MaxStateBytes || len(result.CorrelationID) > 128 || len(result.DeviceName) > 256 {
		return errors.New("协议操作结果字段超过上限")
	}
	if result.Reply != "" {
		reply, decodeErr := hex.DecodeString(result.Reply)
		if decodeErr != nil || len(reply) > MaxFrameBytes {
			return errors.New("协议应答必须是至多 64 KiB 的十六进制数据")
		}
	}
	switch operation {
	case "ingress":
		if result.NeedMore {
			if result.Consumed != 0 || result.Reply != "" || result.DeviceID != "" {
				return errors.New("等待完整帧时不能消耗数据、标识设备或发送应答")
			}
		} else if result.Consumed <= 0 || result.Consumed > dataLength || !ValidDeviceID(result.DeviceID) {
			return errors.New("协议接入结果需要有效的帧长度和设备标识")
		}
	case "encode":
		if result.Reply == "" {
			return errors.New("协议编码未返回下行数据")
		}
	case "decode":
		if result.StandardMessage == nil {
			return errors.New("协议解析未返回标准消息")
		}
	default:
		return errors.New("不支持的协议操作")
	}
	return nil
}

func ValidDeviceID(id string) bool { return model.ValidProtocolDeviceID(id) }
