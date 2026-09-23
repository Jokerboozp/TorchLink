package parser /* 声明 parser 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"encoding/json"               /* 执行当前语句并推进处理流程。 */
	"errors"                      /* 执行当前语句并推进处理流程。 */
	"fmt"                         /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
	"math"                        /* 执行当前语句并推进处理流程。 */
	"strings"                     /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

const PollResponseParserName = "protocol_read_response_v1" /* 声明 PollResponseParserName。 */

type PollResponseParser struct{} /* 定义 PollResponseParser 类型。 */

func (PollResponseParser) Name() string    { return PollResponseParserName } /* 定义 Name 函数。 */
func (PollResponseParser) Version() string { return "1.0.0" }                /* 定义 Version 函数。 */
func (PollResponseParser) Match(Meta) bool { return false }                  /* 定义 Match 函数。 */
func (PollResponseParser) Parse(model.RawMessage) (*model.StandardMessage, error) { /* 定义 Parse 函数。 */
	return nil, errors.New("protocol read response requires versioned point mapping") /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (PollResponseParser) ParseWithConfig(raw model.RawMessage, config map[string]any) (*model.StandardMessage, error) { /* 定义 ParseWithConfig 函数。 */
	var response model.PollResponse                                /* 声明 response。 */
	if err := json.Unmarshal(raw.Payload, &response); err != nil { /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if response.Transport != raw.Transport || (raw.Transport != "OPC_UA" && raw.Transport != "SNMP" && raw.Transport != "BACNET" && raw.Transport != "ONVIF") { /* 判断条件并选择处理分支。 */
		return nil, errors.New("protocol response transport mismatch") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	points, err := PollPoints(config) /* 更新 err 的值。 */
	if err != nil {                   /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	properties := map[string]any{} /* 更新 properties 的值。 */
	for _, p := range points {     /* 循环处理当前数据。 */
		found := false                          /* 更新 found 的值。 */
		for _, value := range response.Values { /* 循环处理当前数据。 */
			if value.Address != p.Address { /* 判断条件并选择处理分支。 */
				continue /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
			if found { /* 判断条件并选择处理分支。 */
				return nil, errors.New("duplicate protocol response address") /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			found = true                 /* 更新 found 的值。 */
			if value.Quality != "GOOD" { /* 判断条件并选择处理分支。 */
				return nil, fmt.Errorf("point %s quality: %s", p.Identifier, value.Quality) /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			v := value.Value                   /* 更新 v 的值。 */
			if number, ok := v.(float64); ok { /* 判断条件并选择处理分支。 */
				scale := p.Scale /* 更新 scale 的值。 */
				if scale == 0 {  /* 判断条件并选择处理分支。 */
					scale = 1 /* 更新 scale 的值。 */
				} /* 结束当前表达式或代码块。 */
				number = number*scale + p.Offset                 /* 更新 number 的值。 */
				if math.IsNaN(number) || math.IsInf(number, 0) { /* 判断条件并选择处理分支。 */
					return nil, errors.New("invalid numeric response") /* 返回当前处理结果。 */
				} /* 结束当前表达式或代码块。 */
				v = number /* 更新 v 的值。 */
			} /* 结束当前表达式或代码块。 */
			properties[p.Identifier] = v /* 更新 properties[p.Identifier] 的值。 */
		} /* 结束当前表达式或代码块。 */
		if !found { /* 判断条件并选择处理分支。 */
			return nil, fmt.Errorf("point %s missing from response", p.Identifier) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return &model.StandardMessage{MessageID: "msg_" + strings.TrimPrefix(raw.MessageID, "raw_"), RawMessageID: raw.MessageID, TenantID: raw.TenantID, ProductID: raw.ProductID, DeviceID: raw.DeviceID, MessageType: model.PropertyReport, Timestamp: raw.ReceivedAt, Properties: properties, Tags: map[string]string{"protocolId": raw.ProtocolID, "protocolVersion": raw.ProtocolVersion}, Raw: map[string]any{"response": response}}, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func PollPoints(config map[string]any) ([]model.PollPoint, error) { /* 定义 PollPoints 函数。 */
	b, err := json.Marshal(config["reads"]) /* 更新 err 的值。 */
	if err != nil {                         /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var points []model.PollPoint                      /* 声明 points。 */
	if err = json.Unmarshal(b, &points); err != nil { /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(points) < 1 || len(points) > 64 { /* 判断条件并选择处理分支。 */
		return nil, errors.New("read mapping requires 1 to 64 points") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	seen := map[string]bool{}      /* 更新 seen 的值。 */
	addresses := map[string]bool{} /* 更新 addresses 的值。 */
	for _, p := range points {     /* 循环处理当前数据。 */
		if strings.TrimSpace(p.Identifier) == "" || len(p.Identifier) > 128 || strings.TrimSpace(p.Address) == "" || len(p.Address) > 512 || seen[p.Identifier] || addresses[p.Address] { /* 判断条件并选择处理分支。 */
			return nil, errors.New("invalid or duplicate read point") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		seen[p.Identifier] = true   /* 更新 seen[p.Identifier] 的值。 */
		addresses[p.Address] = true /* 更新 addresses[p.Address] 的值。 */
	} /* 结束当前表达式或代码块。 */
	return points, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
