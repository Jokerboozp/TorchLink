package httpapi

import (
	"fmt"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"net/url"
	"strconv"
	"strings"
)

func parseRawFilter(q url.Values) (ports.RawFilter, error) {
	f := ports.RawFilter{}
	for name, target := range map[string]*string{"messageId": &f.MessageID, "deviceId": &f.DeviceID, "productId": &f.ProductID, "protocol": &f.Protocol, "payloadFormat": &f.PayloadFormat, "parseStatus": &f.ParseStatus, "messageType": &f.MessageType, "parser": &f.Parser} {
		*target = strings.TrimSpace(q.Get(name))
		if len([]rune(*target)) > 256 {
			return f, fmt.Errorf("%s 查询条件不能超过 256 个字符", name)
		}
	}
	f.ParseStatus, f.MessageType = strings.ToUpper(f.ParseStatus), strings.ToUpper(f.MessageType)
	switch f.ParseStatus {
	case "", "PARSED", "FAILED", "UNPARSED":
	default:
		return f, fmt.Errorf("解析状态不支持：%s", f.ParseStatus)
	}
	switch model.MessageType(f.MessageType) {
	case "", model.PropertyReport, model.EventReport, model.AlarmReport, model.StateChange, model.CommandReply, model.LogReport:
	default:
		return f, fmt.Errorf("消息类型不支持：%s", f.MessageType)
	}
	for name, target := range map[string]*int64{"start": &f.Start, "end": &f.End} {
		if value := strings.TrimSpace(q.Get(name)); value != "" {
			n, err := strconv.ParseInt(value, 10, 64)
			if err != nil || n < 0 {
				return f, fmt.Errorf("%s 必须为非负毫秒时间戳", name)
			}
			*target = n
		}
	}
	if f.Start > 0 && f.End > 0 && f.Start > f.End {
		return f, fmt.Errorf("开始时间不能晚于结束时间")
	}
	return f, nil
}
