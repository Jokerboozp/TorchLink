package core /* 声明 core 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"bytes"         /* 执行当前语句并推进处理流程。 */
	"encoding/csv"  /* 执行当前语句并推进处理流程。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"errors"        /* 执行当前语句并推进处理流程。 */
	"fmt"           /* 执行当前语句并推进处理流程。 */
	"path/filepath" /* 执行当前语句并推进处理流程。 */
	"sort"          /* 执行当前语句并推进处理流程。 */
	"strings"       /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model"  /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func buildUploadedProtocol(in ProtocolAssistantInput) (model.ProtocolAssistantDraft, bool, error) { /* 定义 buildUploadedProtocol 函数。 */
	if in.InputKind == "point-table" { /* 判断条件并选择处理分支。 */
		filename, data := in.DocumentFilename, in.DocumentData /* 更新 data 的值。 */
		if len(data) == 0 {                                    /* 判断条件并选择处理分支。 */
			filename = "points.csv"      /* 更新 filename 的值。 */
			data = []byte(in.PointTable) /* 更新 data 的值。 */
		} /* 结束当前表达式或代码块。 */
		table, warnings, err := ParseModbusPointTable(filename, data, 10)     /* 更新 err 的值。 */
		if err != nil && strings.EqualFold(filepath.Ext(filename), ".xlsx") { /* 判断条件并选择处理分支。 */
			if legacy, legacyErr := BuildProtocolAssistantSpreadsheetDraft(in); legacyErr == nil { /* 判断条件并选择处理分支。 */
				table.Points = nil                    /* 更新 table.Points 的值。 */
				for _, field := range legacy.Fields { /* 循环处理当前数据。 */
					table.Points = append(table.Points, model.ModbusPoint{Identifier: generatedPointIdentifier(field.Name, 1, field.CoilAddress), Name: field.Name, FunctionCode: 1, Address: field.CoilAddress, DataType: "bool", RegisterCount: 1, Scale: 1, PollIntervalSec: 10, Description: field.Description}) /* 更新 table.Points 的值。 */
				} /* 结束当前表达式或代码块。 */
				warnings = legacy.Warnings /* 更新 warnings 的值。 */
				err = nil                  /* 检查错误并决定后续处理。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		if err != nil { /* 判断条件并选择处理分支。 */
			return model.ProtocolAssistantDraft{}, true, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		blocks, err := CompileModbusReadBlocks(table.Points) /* 更新 err 的值。 */
		if err != nil {                                      /* 判断条件并选择处理分支。 */
			return model.ProtocolAssistantDraft{}, true, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		transport := strings.ToUpper(in.Transport)                  /* 更新 transport 的值。 */
		if transport != "MODBUS_TCP" && transport != "MODBUS_RTU" { /* 判断条件并选择处理分支。 */
			return model.ProtocolAssistantDraft{}, true, errors.New("点表请选择 Modbus TCP 或 Modbus RTU") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		parserType := parser.ModbusTCPParserName /* 更新 parserType 的值。 */
		if transport == "MODBUS_RTU" {           /* 判断条件并选择处理分支。 */
			parserType = parser.ModbusRTUParserName /* 更新 parserType 的值。 */
		} /* 结束当前表达式或代码块。 */
		draft := model.ProtocolAssistantDraft{Name: in.Name, Protocol: in.Protocol, Transport: transport, PayloadFormat: "hex", ParserType: parserType, MessageType: model.PropertyReport, Config: map[string]any{"points": table.Points, "blocks": blocks}, Warnings: warnings} /* 更新 draft 的值。 */
		if err := NormalizeGeneratedModbusConfig(draft.Config); err != nil {                                                                                                                                                                                                     /* 判断条件并选择处理分支。 */
			return draft, true, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		for _, p := range table.Points { /* 循环处理当前数据。 */
			draft.Fields = append(draft.Fields, model.ProtocolAssistantField{Name: p.Identifier, Label: p.Name, Address: fmt.Sprint(p.Address), DataType: p.DataType, Description: p.Description}) /* 更新 draft.Fields 的值。 */
		} /* 结束当前表达式或代码块。 */
		if draft.Name == "" { /* 判断条件并选择处理分支。 */
			draft.Name = strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename)) /* 更新 draft.Name 的值。 */
		} /* 结束当前表达式或代码块。 */
		return draft, true, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	sample := strings.TrimSpace(in.SamplePayload) /* 更新 sample 的值。 */
	if in.InputKind == "sample" && sample == "" { /* 判断条件并选择处理分支。 */
		sample = strings.TrimSpace(in.DocumentText) /* 更新 sample 的值。 */
	} /* 结束当前表达式或代码块。 */
	if sample == "" || (in.PayloadFormat != "json" && !strings.HasPrefix(sample, "{")) { /* 判断条件并选择处理分支。 */
		return model.ProtocolAssistantDraft{}, false, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var object map[string]any                                                        /* 声明 object。 */
	if err := json.Unmarshal([]byte(sample), &object); err != nil || object == nil { /* 判断条件并选择处理分支。 */
		return model.ProtocolAssistantDraft{}, true, errors.New("报文须为有效的 JSON 对象") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	// Preserve explicit instructions for the AI mapping path.
	if strings.TrimSpace(in.PointTable) != "" { /* 判断条件并选择处理分支。 */
		return model.ProtocolAssistantDraft{}, false, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	root, prefix := object, "$"                          /* 更新 prefix 的值。 */
	for _, key := range []string{"properties", "data"} { /* 循环处理当前数据。 */
		if nested, ok := object[key].(map[string]any); ok { /* 判断条件并选择处理分支。 */
			root = nested       /* 更新 root 的值。 */
			prefix = "$." + key /* 更新 prefix 的值。 */
			break               /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	mappings := map[string]any{}               /* 更新 mappings 的值。 */
	fields := []model.ProtocolAssistantField{} /* 更新 fields 的值。 */
	keys := make([]string, 0, len(root))       /* 更新 keys 的值。 */
	for key := range root {                    /* 循环处理当前数据。 */
		keys = append(keys, key) /* 更新 keys 的值。 */
	} /* 结束当前表达式或代码块。 */
	sort.Strings(keys)         /* 执行当前语句并推进处理流程。 */
	for _, key := range keys { /* 循环处理当前数据。 */
		if prefix == "$" { /* 判断条件并选择处理分支。 */
			switch key { /* 根据条件选择处理路径。 */
			case "timestamp", "messageType", "event", "tags", "alarm", "deviceId", "productId", "tenantId", "messageId": /* 处理当前分支。 */
				continue /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		if strings.ContainsAny(key, ".[]") { /* 判断条件并选择处理分支。 */
			return model.ProtocolAssistantDraft{}, true, fmt.Errorf("字段 %q 包含路径保留字符，请提供字段映射说明", key) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		path := prefix + "." + key                                                                                                         /* 更新 path 的值。 */
		mappings[key] = path                                                                                                               /* 更新 mappings[key] 的值。 */
		fields = append(fields, model.ProtocolAssistantField{Name: key, Label: key, Expression: path, Type: fmt.Sprintf("%T", root[key])}) /* 更新 fields 的值。 */
	} /* 结束当前表达式或代码块。 */
	if len(fields) == 0 { /* 判断条件并选择处理分支。 */
		return model.ProtocolAssistantDraft{}, true, errors.New("报文中没有可生成映射的属性字段") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	draft := model.ProtocolAssistantDraft{Name: in.Name, Protocol: in.Protocol, Transport: in.Transport, PayloadFormat: "json", ParserType: "configurable_json_parser", MessageType: model.PropertyReport, Fields: fields, Config: map[string]any{"properties": mappings}, SamplePayload: object} /* 更新 draft 的值。 */
	if draft.Name == "" {                                                                                                                                                                                                                                                                         /* 判断条件并选择处理分支。 */
		draft.Name = "JSON 报文协议" /* 更新 draft.Name 的值。 */
	} /* 结束当前表达式或代码块。 */
	if draft.Transport == "" { /* 判断条件并选择处理分支。 */
		draft.Transport = "MQTT" /* 更新 draft.Transport 的值。 */
	} /* 结束当前表达式或代码块。 */
	preview, err := PreviewProtocolAssistant(draft, "preview", sample) /* 更新 err 的值。 */
	if err != nil {                                                    /* 判断条件并选择处理分支。 */
		return draft, true, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	draft.MessageType = preview.MessageType /* 更新 draft.MessageType 的值。 */
	draft.Preview = preview                 /* 更新 draft.Preview 的值。 */
	return draft, true, nil                 /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func NormalizeGeneratedModbusConfig(config map[string]any) error { /* 定义 NormalizeGeneratedModbusConfig 函数。 */
	data, err := json.Marshal(config["points"]) /* 更新 err 的值。 */
	if err != nil {                             /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var points []model.ModbusPoint                       /* 声明 points。 */
	if err = json.Unmarshal(data, &points); err != nil { /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var buffer bytes.Buffer                                                                                                                                                                                                                                         /* 声明 buffer。 */
	writer := csv.NewWriter(&buffer)                                                                                                                                                                                                                                /* 更新 writer 的值。 */
	_ = writer.Write([]string{"identifier", "name", "functionCode", "address", "addressNotation", "dataType", "registerCount", "byteOrder", "wordOrder", "bit", "scale", "offset", "unit", "access", "pollIntervalSec", "deadband", "alarmMapping", "description"}) /* 更新 _ 的值。 */
	for _, p := range points {                                                                                                                                                                                                                                      /* 循环处理当前数据。 */
		if p.Address < 0 || p.Address > 65535 { /* 判断条件并选择处理分支。 */
			return errors.New("点位地址须在 0 到 65535 之间") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if p.DataType != "string" { /* 判断条件并选择处理分支。 */
			width, e := pointRegisterCount(p.DataType, "") /* 更新 e 的值。 */
			if e != nil {                                  /* 判断条件并选择处理分支。 */
				return e /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			if p.RegisterCount != width { /* 判断条件并选择处理分支。 */
				return fmt.Errorf("点位 %s 的寄存器数量与数据类型不一致", p.Name) /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		if p.RegisterCount < 1 || p.RegisterCount > 125 || p.Address+p.RegisterCount > 65536 { /* 判断条件并选择处理分支。 */
			return errors.New("点位寄存器范围无效") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if p.FunctionCode <= 2 && p.DataType != "bool" { /* 判断条件并选择处理分支。 */
			return errors.New("线圈与离散输入须使用 bool 类型") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		bit := ""         /* 更新 bit 的值。 */
		if p.Bit != nil { /* 判断条件并选择处理分支。 */
			bit = fmt.Sprint(*p.Bit) /* 更新 bit 的值。 */
		} /* 结束当前表达式或代码块。 */
		alarm := ""                  /* 更新 alarm 的值。 */
		if len(p.AlarmMapping) > 0 { /* 判断条件并选择处理分支。 */
			b, _ := json.Marshal(p.AlarmMapping) /* 更新 _ 的值。 */
			alarm = string(b)                    /* 更新 alarm 的值。 */
		} /* 结束当前表达式或代码块。 */
		_ = writer.Write([]string{p.Identifier, p.Name, fmt.Sprint(p.FunctionCode), fmt.Sprint(p.Address), "zero_based", p.DataType, fmt.Sprint(p.RegisterCount), p.ByteOrder, p.WordOrder, bit, fmt.Sprint(p.Scale), fmt.Sprint(p.Offset), p.Unit, p.Access, fmt.Sprint(p.PollIntervalSec), fmt.Sprint(p.Deadband), alarm, p.Description}) /* 更新 _ 的值。 */
	} /* 结束当前表达式或代码块。 */
	writer.Flush()                        /* 执行当前语句并推进处理流程。 */
	if err = writer.Error(); err != nil { /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	table, _, err := ParseModbusPointTable("points.csv", buffer.Bytes(), 10) /* 更新 err 的值。 */
	if err != nil {                                                          /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	blocks, err := CompileModbusReadBlocks(table.Points) /* 更新 err 的值。 */
	if err != nil {                                      /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	config["points"] = table.Points /* 执行当前语句并推进处理流程。 */
	config["blocks"] = blocks       /* 执行当前语句并推进处理流程。 */
	return nil                      /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
