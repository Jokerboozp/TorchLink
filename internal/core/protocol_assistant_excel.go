package core /* 声明 core 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"errors"        /* 执行当前语句并推进处理流程。 */
	"fmt"           /* 执行当前语句并推进处理流程。 */
	"path/filepath" /* 执行当前语句并推进处理流程。 */
	"regexp"        /* 执行当前语句并推进处理流程。 */
	"strconv"       /* 执行当前语句并推进处理流程。 */
	"strings"       /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model"  /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

type protocolAssistantPoint struct { /* 定义 protocolAssistantPoint 类型。 */
	Name          string /* 执行当前语句并推进处理流程。 */
	CoilAddress   int    /* 执行当前语句并推进处理流程。 */
	ModbusAddress int    /* 执行当前语句并推进处理流程。 */
	DataType      string /* 执行当前语句并推进处理流程。 */
	NormalValue   string /* 执行当前语句并推进处理流程。 */
	ReportValue   string /* 执行当前语句并推进处理流程。 */
	Description   string /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

// BuildProtocolAssistantSpreadsheetDraft converts a point-table workbook into
// a Go-backed mapping draft. It is deliberately deterministic and does not
// require an AI provider, so a small Excel upload cannot sit behind a model
// gateway long enough to become a 504.
func BuildProtocolAssistantSpreadsheetDraft(in ProtocolAssistantInput) (model.ProtocolAssistantDraft, error) { /* 定义 BuildProtocolAssistantSpreadsheetDraft 函数。 */
	if len(in.DocumentData) == 0 || !strings.EqualFold(filepath.Ext(in.DocumentFilename), ".xlsx") { /* 判断条件并选择处理分支。 */
		return model.ProtocolAssistantDraft{}, errors.New("an xlsx protocol document is required") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	points, notes, err := parseProtocolAssistantWorkbook(in.DocumentData) /* 更新 err 的值。 */
	if err != nil {                                                       /* 判断条件并选择处理分支。 */
		return model.ProtocolAssistantDraft{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(points) == 0 { /* 判断条件并选择处理分支。 */
		return model.ProtocolAssistantDraft{}, errors.New("Excel 中未找到包含变量名称和线圈地址的点表") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */

	transport := strings.ToUpper(strings.TrimSpace(in.Transport)) /* 更新 transport 的值。 */
	if transport == "" {                                          /* 判断条件并选择处理分支。 */
		transport = "MODBUS_RTU" /* 更新 transport 的值。 */
	} /* 结束当前表达式或代码块。 */
	protocol := strings.TrimSpace(in.Protocol) /* 更新 protocol 的值。 */
	if protocol == "" {                        /* 判断条件并选择处理分支。 */
		protocol = "modbus" /* 更新 protocol 的值。 */
	} /* 结束当前表达式或代码块。 */
	payloadFormat := strings.ToLower(strings.TrimSpace(in.PayloadFormat)) /* 更新 payloadFormat 的值。 */
	if payloadFormat == "" {                                              /* 判断条件并选择处理分支。 */
		payloadFormat = "hex" /* 更新 payloadFormat 的值。 */
	} /* 结束当前表达式或代码块。 */
	name := strings.TrimSpace(in.Name) /* 更新 name 的值。 */
	if name == "" {                    /* 判断条件并选择处理分支。 */
		name = "PLC Modbus 线圈点表" /* 更新 name 的值。 */
	} /* 结束当前表达式或代码块。 */

	configFields := make([]map[string]any, 0, len(points))              /* 更新 configFields 的值。 */
	draftFields := make([]model.ProtocolAssistantField, 0, len(points)) /* 更新 draftFields 的值。 */
	for _, point := range points {                                      /* 循环处理当前数据。 */
		description := point.Description /* 更新 description 的值。 */
		if description == "" {           /* 判断条件并选择处理分支。 */
			description = fmt.Sprintf("Modbus 线圈 %d", point.CoilAddress) /* 更新 description 的值。 */
		} /* 结束当前表达式或代码块。 */
		configFields = append(configFields, map[string]any{ /* 更新 configFields 的值。 */
			"name":          point.Name,          /* 执行当前语句并推进处理流程。 */
			"label":         point.Name,          /* 执行当前语句并推进处理流程。 */
			"type":          "boolean",           /* 执行当前语句并推进处理流程。 */
			"dataType":      point.DataType,      /* 执行当前语句并推进处理流程。 */
			"coilAddress":   point.CoilAddress,   /* 执行当前语句并推进处理流程。 */
			"modbusAddress": point.ModbusAddress, /* 执行当前语句并推进处理流程。 */
			"normalValue":   point.NormalValue,   /* 执行当前语句并推进处理流程。 */
			"reportValue":   point.ReportValue,   /* 执行当前语句并推进处理流程。 */
			"description":   description,         /* 执行当前语句并推进处理流程。 */
		}) /* 结束当前表达式或代码块。 */
		draftFields = append(draftFields, model.ProtocolAssistantField{ /* 更新 draftFields 的值。 */
			Name: point.Name, Label: point.Name, Type: "boolean", Address: fmt.Sprintf("M%d", point.CoilAddress), /* 执行当前语句并推进处理流程。 */
			CoilAddress: point.CoilAddress, ModbusAddress: point.ModbusAddress, DataType: point.DataType, /* 执行当前语句并推进处理流程。 */
			NormalValue: point.NormalValue, ReportValue: point.ReportValue, Description: description, /* 执行当前语句并推进处理流程。 */
		}) /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	config := map[string]any{ /* 更新 config 的值。 */
		"frame":        modbusFrameForTransport(transport), /* 执行当前语句并推进处理流程。 */
		"startAddress": 0,                                  /* 执行当前语句并推进处理流程。 */
		"functionCode": 1,                                  /* 执行当前语句并推进处理流程。 */
		"messageType":  string(model.PropertyReport),       /* 执行当前语句并推进处理流程。 */
		"fields":       configFields,                       /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	draft := model.ProtocolAssistantDraft{ /* 更新 draft 的值。 */
		Name: name, Description: "由 Excel 线圈点表生成的 Go Modbus 映射，不执行 JavaScript。", /* 执行当前语句并推进处理流程。 */
		Protocol: protocol, Transport: transport, PayloadFormat: payloadFormat, /* 执行当前语句并推进处理流程。 */
		ParserType: parser.ModbusCoilParserName, MessageType: model.PropertyReport, /* 执行当前语句并推进处理流程。 */
		Config: config, Fields: draftFields, SamplePayload: assistantSampleValue(payloadFormat, in.SamplePayload), /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	draft.Warnings = append(draft.Warnings, notes...)                /* 更新 draft.Warnings 的值。 */
	if transport == "MODBUS_TCP" && hasSerialConnectionNote(notes) { /* 判断条件并选择处理分支。 */
		draft.Warnings = append(draft.Warnings, "表格记录的是串口 19200/8E1/站号信息，与当前 MODBUS_TCP 选择不一致；请确认实际链路，或改为 MODBUS_RTU。") /* 更新 draft.Warnings 的值。 */
	} /* 结束当前表达式或代码块。 */
	draft.Warnings = append(draft.Warnings, "Excel 未声明响应起始地址，当前按 0 处理；请用真实 Modbus 响应样本预览确认。") /* 更新 draft.Warnings 的值。 */
	if strings.TrimSpace(in.SamplePayload) == "" {                                            /* 判断条件并选择处理分支。 */
		draft.Warnings = append(draft.Warnings, "未提供样本报文，发布前请在协议调试中用真实 Modbus RTU/TCP 响应验证。") /* 更新 draft.Warnings 的值。 */
	} else if payload, payloadErr := ProtocolAssistantPayload(payloadFormat, in.SamplePayload); payloadErr != nil { /* 结束当前表达式或代码块。 */
		draft.Warnings = append(draft.Warnings, "样本报文无效："+payloadErr.Error()) /* 更新 draft.Warnings 的值。 */
	} else { /* 结束当前表达式或代码块。 */
		raw := model.RawMessage{MessageID: "raw_protocol_assistant", TenantID: "preview", ProductID: "protocol_assistant", DeviceID: "device_assistant", Protocol: protocol, Transport: transport, PayloadFormat: payloadFormat, Payload: payload} /* 更新 raw 的值。 */
		if message, previewErr := (parser.ModbusCoilParser{}).ParseWithConfig(raw, config); previewErr != nil {                                                                                                                                    /* 判断条件并选择处理分支。 */
			draft.Warnings = append(draft.Warnings, "样本解析失败："+previewErr.Error()) /* 更新 draft.Warnings 的值。 */
		} else { /* 结束当前表达式或代码块。 */
			draft.Preview = message /* 更新 draft.Preview 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return draft, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func parseProtocolAssistantWorkbook(data []byte) ([]protocolAssistantPoint, []string, error) { /* 定义 parseProtocolAssistantWorkbook 函数。 */
	rows, err := spreadsheetRows(data) /* 更新 err 的值。 */
	if err != nil {                    /* 判断条件并选择处理分支。 */
		return nil, nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	header := map[string]int{}        /* 更新 header 的值。 */
	headerRow := -1                   /* 更新 headerRow 的值。 */
	for rowIndex, row := range rows { /* 循环处理当前数据。 */
		candidate := map[string]int{}    /* 更新 candidate 的值。 */
		for column, value := range row { /* 循环处理当前数据。 */
			key := normalizeSpreadsheetHeader(value) /* 更新 key 的值。 */
			lowerKey := strings.ToLower(key)         /* 更新 lowerKey 的值。 */
			switch {                                 /* 根据条件选择处理路径。 */
			case strings.Contains(key, "变量名称"): /* 处理当前分支。 */
				candidate["name"] = column /* 执行当前语句并推进处理流程。 */
			case strings.Contains(key, "PLC线圈地址"): /* 处理当前分支。 */
				candidate["coil"] = column /* 执行当前语句并推进处理流程。 */
			case strings.Contains(lowerKey, "modbus地址"): /* 处理当前分支。 */
				candidate["modbus"] = column /* 执行当前语句并推进处理流程。 */
			case strings.Contains(key, "数据类型"): /* 处理当前分支。 */
				candidate["type"] = column /* 执行当前语句并推进处理流程。 */
			case strings.Contains(key, "无报出状态"): /* 处理当前分支。 */
				candidate["normal"] = column /* 执行当前语句并推进处理流程。 */
			case key == "报出状态": /* 处理当前分支。 */
				candidate["report"] = column /* 执行当前语句并推进处理流程。 */
			case strings.Contains(key, "备注"): /* 处理当前分支。 */
				candidate["note"] = column /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		_, hasName := candidate["name"]        /* 更新 hasName 的值。 */
		_, hasCoil := candidate["coil"]        /* 更新 hasCoil 的值。 */
		_, hasModbus := candidate["modbus"]    /* 更新 hasModbus 的值。 */
		if hasName && (hasCoil || hasModbus) { /* 判断条件并选择处理分支。 */
			header, headerRow = candidate, rowIndex /* 更新 headerRow 的值。 */
			break                                   /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if headerRow < 0 { /* 判断条件并选择处理分支。 */
		return nil, nil, errors.New("Excel 中未找到变量名称、PLC 线圈地址等点表列") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */

	var points []protocolAssistantPoint      /* 声明 points。 */
	for _, row := range rows[headerRow+1:] { /* 循环处理当前数据。 */
		name := spreadsheetValue(row, header["name"]) /* 更新 name 的值。 */
		if name == "" {                               /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		coilColumn, hasCoil := header["coil"]       /* 更新 hasCoil 的值。 */
		modbusColumn, hasModbus := header["modbus"] /* 更新 hasModbus 的值。 */
		coilText := ""                              /* 更新 coilText 的值。 */
		if hasCoil {                                /* 判断条件并选择处理分支。 */
			coilText = spreadsheetValue(row, coilColumn) /* 更新 coilText 的值。 */
		} /* 结束当前表达式或代码块。 */
		modbusText := "" /* 更新 modbusText 的值。 */
		if hasModbus {   /* 判断条件并选择处理分支。 */
			modbusText = spreadsheetValue(row, modbusColumn) /* 更新 modbusText 的值。 */
		} /* 结束当前表达式或代码块。 */
		coilAddress, ok := parseSpreadsheetAddress(coilText) /* 更新 ok 的值。 */
		if !ok {                                             /* 判断条件并选择处理分支。 */
			coilAddress, ok = parseSpreadsheetAddress(modbusText) /* 更新 ok 的值。 */
		} /* 结束当前表达式或代码块。 */
		if !ok { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		modbusAddress, modbusOK := parseSpreadsheetAddress(modbusText) /* 更新 modbusOK 的值。 */
		if !modbusOK {                                                 /* 判断条件并选择处理分支。 */
			modbusAddress = coilAddress /* 更新 modbusAddress 的值。 */
		} /* 结束当前表达式或代码块。 */
		dataType := strings.TrimSpace(spreadsheetValue(row, header["type"])) /* 更新 dataType 的值。 */
		if dataType == "" {                                                  /* 判断条件并选择处理分支。 */
			dataType = "BOOL" /* 更新 dataType 的值。 */
		} /* 结束当前表达式或代码块。 */
		normal := spreadsheetValue(row, header["normal"])                                                                                                                                                           /* 更新 normal 的值。 */
		report := spreadsheetValue(row, header["report"])                                                                                                                                                           /* 更新 report 的值。 */
		note := spreadsheetValue(row, header["note"])                                                                                                                                                               /* 更新 note 的值。 */
		description := strings.TrimSpace(strings.Join(nonEmptySpreadsheetValues(normal, report, note), "；"))                                                                                                        /* 更新 description 的值。 */
		points = append(points, protocolAssistantPoint{Name: name, CoilAddress: coilAddress, ModbusAddress: modbusAddress, DataType: dataType, NormalValue: normal, ReportValue: report, Description: description}) /* 更新 points 的值。 */
	} /* 结束当前表达式或代码块。 */
	if len(points) == 0 { /* 判断条件并选择处理分支。 */
		return nil, nil, errors.New("Excel 点表中没有可识别的线圈地址行") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */

	var notes []string                     /* 声明 notes。 */
	for _, row := range rows[:headerRow] { /* 循环处理当前数据。 */
		text := strings.TrimSpace(strings.Join(nonEmptySpreadsheetValues(row...), " ")) /* 更新 text 的值。 */
		if text == "" {                                                                 /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if strings.Contains(text, "波特率") || strings.Contains(text, "站号") || strings.Contains(text, "串口") { /* 判断条件并选择处理分支。 */
			notes = append(notes, "Excel 连接信息："+text) /* 更新 notes 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return points, notes, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func normalizeSpreadsheetHeader(value string) string { /* 定义 normalizeSpreadsheetHeader 函数。 */
	return strings.Map(func(r rune) rune { /* 返回当前处理结果。 */
		switch r { /* 根据条件选择处理路径。 */
		case ' ', '\t', '\r', '\n', '　': /* 处理当前分支。 */
			return -1 /* 返回当前处理结果。 */
		default: /* 处理当前分支。 */
			return r /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	}, strings.TrimSpace(value)) /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func spreadsheetValue(row []string, column int) string { /* 定义 spreadsheetValue 函数。 */
	if column < 0 || column >= len(row) { /* 判断条件并选择处理分支。 */
		return "" /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return strings.TrimSpace(row[column]) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func nonEmptySpreadsheetValues(values ...string) []string { /* 定义 nonEmptySpreadsheetValues 函数。 */
	out := make([]string, 0, len(values)) /* 更新 out 的值。 */
	for _, value := range values {        /* 循环处理当前数据。 */
		if strings.TrimSpace(value) != "" { /* 判断条件并选择处理分支。 */
			out = append(out, strings.TrimSpace(value)) /* 更新 out 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return out /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

var spreadsheetAddressPattern = regexp.MustCompile(`(?i)^[A-Z]*\s*(\d+)$`) /* 声明 spreadsheetAddressPattern。 */

func parseSpreadsheetAddress(value string) (int, bool) { /* 定义 parseSpreadsheetAddress 函数。 */
	value = strings.TrimSpace(value)                             /* 更新 value 的值。 */
	match := spreadsheetAddressPattern.FindStringSubmatch(value) /* 更新 match 的值。 */
	if len(match) == 2 {                                         /* 判断条件并选择处理分支。 */
		number, err := strconv.Atoi(match[1])    /* 更新 err 的值。 */
		return number, err == nil && number >= 0 /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return 0, false /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func modbusFrameForTransport(transport string) string { /* 定义 modbusFrameForTransport 函数。 */
	switch strings.ToUpper(strings.TrimSpace(transport)) { /* 根据条件选择处理路径。 */
	case "MODBUS_TCP": /* 处理当前分支。 */
		return "tcp" /* 返回当前处理结果。 */
	case "MODBUS_RTU", "MODBUS_SERIAL", "SERIAL": /* 处理当前分支。 */
		return "rtu" /* 返回当前处理结果。 */
	default: /* 处理当前分支。 */
		return "auto" /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func hasSerialConnectionNote(notes []string) bool { /* 定义 hasSerialConnectionNote 函数。 */
	for _, note := range notes { /* 循环处理当前数据。 */
		if strings.Contains(note, "串口") || strings.Contains(note, "波特率") || strings.Contains(note, "站号") { /* 判断条件并选择处理分支。 */
			return true /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return false /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
