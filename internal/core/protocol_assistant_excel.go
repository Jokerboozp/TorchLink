package core

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"iot-platform/internal/model"
	"iot-platform/internal/parser"
)

type protocolAssistantPoint struct {
	Name          string
	CoilAddress   int
	ModbusAddress int
	DataType      string
	NormalValue   string
	ReportValue   string
	Description   string
}

// BuildProtocolAssistantSpreadsheetDraft converts a point-table workbook into
// a Go-backed mapping draft. It is deliberately deterministic and does not
// require an AI provider, so a small Excel upload cannot sit behind a model
// gateway long enough to become a 504.
func BuildProtocolAssistantSpreadsheetDraft(in ProtocolAssistantInput) (model.ProtocolAssistantDraft, error) {
	if len(in.DocumentData) == 0 || !strings.EqualFold(filepath.Ext(in.DocumentFilename), ".xlsx") {
		return model.ProtocolAssistantDraft{}, errors.New("an xlsx protocol document is required")
	}
	points, notes, err := parseProtocolAssistantWorkbook(in.DocumentData)
	if err != nil {
		return model.ProtocolAssistantDraft{}, err
	}
	if len(points) == 0 {
		return model.ProtocolAssistantDraft{}, errors.New("Excel 中未找到包含变量名称和线圈地址的点表")
	}

	transport := strings.ToUpper(strings.TrimSpace(in.Transport))
	if transport == "" {
		transport = "MODBUS_RTU"
	}
	protocol := strings.TrimSpace(in.Protocol)
	if protocol == "" {
		protocol = "modbus"
	}
	payloadFormat := strings.ToLower(strings.TrimSpace(in.PayloadFormat))
	if payloadFormat == "" {
		payloadFormat = "hex"
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = "PLC Modbus 线圈点表"
	}

	configFields := make([]map[string]any, 0, len(points))
	draftFields := make([]model.ProtocolAssistantField, 0, len(points))
	for _, point := range points {
		description := point.Description
		if description == "" {
			description = fmt.Sprintf("Modbus 线圈 %d", point.CoilAddress)
		}
		configFields = append(configFields, map[string]any{
			"name":          point.Name,
			"label":         point.Name,
			"type":          "boolean",
			"dataType":      point.DataType,
			"coilAddress":   point.CoilAddress,
			"modbusAddress": point.ModbusAddress,
			"normalValue":   point.NormalValue,
			"reportValue":   point.ReportValue,
			"description":   description,
		})
		draftFields = append(draftFields, model.ProtocolAssistantField{
			Name: point.Name, Label: point.Name, Type: "boolean", Address: fmt.Sprintf("M%d", point.CoilAddress),
			CoilAddress: point.CoilAddress, ModbusAddress: point.ModbusAddress, DataType: point.DataType,
			NormalValue: point.NormalValue, ReportValue: point.ReportValue, Description: description,
		})
	}
	config := map[string]any{
		"frame":        modbusFrameForTransport(transport),
		"startAddress": 0,
		"functionCode": 1,
		"messageType":  string(model.PropertyReport),
		"fields":       configFields,
	}
	draft := model.ProtocolAssistantDraft{
		Name: name, Description: "由 Excel 线圈点表生成的 Go Modbus 映射，不执行 JavaScript。",
		Protocol: protocol, Transport: transport, PayloadFormat: payloadFormat,
		ParserType: parser.ModbusCoilParserName, MessageType: model.PropertyReport,
		Config: config, Fields: draftFields, SamplePayload: assistantSampleValue(payloadFormat, in.SamplePayload),
	}
	draft.Warnings = append(draft.Warnings, notes...)
	if transport == "MODBUS_TCP" && hasSerialConnectionNote(notes) {
		draft.Warnings = append(draft.Warnings, "表格记录的是串口 19200/8E1/站号信息，与当前 MODBUS_TCP 选择不一致；请确认实际链路，或改为 MODBUS_RTU。")
	}
	draft.Warnings = append(draft.Warnings, "Excel 未声明响应起始地址，当前按 0 处理；请用真实 Modbus 响应样本预览确认。")
	if strings.TrimSpace(in.SamplePayload) == "" {
		draft.Warnings = append(draft.Warnings, "未提供样本报文，发布前请在协议调试中用真实 Modbus RTU/TCP 响应验证。")
	} else if payload, payloadErr := ProtocolAssistantPayload(payloadFormat, in.SamplePayload); payloadErr != nil {
		draft.Warnings = append(draft.Warnings, "样本报文无效："+payloadErr.Error())
	} else {
		raw := model.RawMessage{MessageID: "raw_protocol_assistant", TenantID: "preview", ProductID: "protocol_assistant", DeviceID: "device_assistant", Protocol: protocol, Transport: transport, PayloadFormat: payloadFormat, Payload: payload}
		if message, previewErr := (parser.ModbusCoilParser{}).ParseWithConfig(raw, config); previewErr != nil {
			draft.Warnings = append(draft.Warnings, "样本解析失败："+previewErr.Error())
		} else {
			draft.Preview = message
		}
	}
	return draft, nil
}

func parseProtocolAssistantWorkbook(data []byte) ([]protocolAssistantPoint, []string, error) {
	rows, err := spreadsheetRows(data)
	if err != nil {
		return nil, nil, err
	}
	header := map[string]int{}
	headerRow := -1
	for rowIndex, row := range rows {
		candidate := map[string]int{}
		for column, value := range row {
			key := normalizeSpreadsheetHeader(value)
			lowerKey := strings.ToLower(key)
			switch {
			case strings.Contains(key, "变量名称"):
				candidate["name"] = column
			case strings.Contains(key, "PLC线圈地址"):
				candidate["coil"] = column
			case strings.Contains(lowerKey, "modbus地址"):
				candidate["modbus"] = column
			case strings.Contains(key, "数据类型"):
				candidate["type"] = column
			case strings.Contains(key, "无报出状态"):
				candidate["normal"] = column
			case key == "报出状态":
				candidate["report"] = column
			case strings.Contains(key, "备注"):
				candidate["note"] = column
			}
		}
		_, hasName := candidate["name"]
		_, hasCoil := candidate["coil"]
		_, hasModbus := candidate["modbus"]
		if hasName && (hasCoil || hasModbus) {
			header, headerRow = candidate, rowIndex
			break
		}
	}
	if headerRow < 0 {
		return nil, nil, errors.New("Excel 中未找到变量名称、PLC 线圈地址等点表列")
	}

	var points []protocolAssistantPoint
	for _, row := range rows[headerRow+1:] {
		name := spreadsheetValue(row, header["name"])
		if name == "" {
			continue
		}
		coilColumn, hasCoil := header["coil"]
		modbusColumn, hasModbus := header["modbus"]
		coilText := ""
		if hasCoil {
			coilText = spreadsheetValue(row, coilColumn)
		}
		modbusText := ""
		if hasModbus {
			modbusText = spreadsheetValue(row, modbusColumn)
		}
		coilAddress, ok := parseSpreadsheetAddress(coilText)
		if !ok {
			coilAddress, ok = parseSpreadsheetAddress(modbusText)
		}
		if !ok {
			continue
		}
		modbusAddress, modbusOK := parseSpreadsheetAddress(modbusText)
		if !modbusOK {
			modbusAddress = coilAddress
		}
		dataType := strings.TrimSpace(spreadsheetValue(row, header["type"]))
		if dataType == "" {
			dataType = "BOOL"
		}
		normal := spreadsheetValue(row, header["normal"])
		report := spreadsheetValue(row, header["report"])
		note := spreadsheetValue(row, header["note"])
		description := strings.TrimSpace(strings.Join(nonEmptySpreadsheetValues(normal, report, note), "；"))
		points = append(points, protocolAssistantPoint{Name: name, CoilAddress: coilAddress, ModbusAddress: modbusAddress, DataType: dataType, NormalValue: normal, ReportValue: report, Description: description})
	}
	if len(points) == 0 {
		return nil, nil, errors.New("Excel 点表中没有可识别的线圈地址行")
	}

	var notes []string
	for _, row := range rows[:headerRow] {
		text := strings.TrimSpace(strings.Join(nonEmptySpreadsheetValues(row...), " "))
		if text == "" {
			continue
		}
		if strings.Contains(text, "波特率") || strings.Contains(text, "站号") || strings.Contains(text, "串口") {
			notes = append(notes, "Excel 连接信息："+text)
		}
	}
	return points, notes, nil
}

func normalizeSpreadsheetHeader(value string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\r', '\n', '　':
			return -1
		default:
			return r
		}
	}, strings.TrimSpace(value))
}

func spreadsheetValue(row []string, column int) string {
	if column < 0 || column >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[column])
}

func nonEmptySpreadsheetValues(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, strings.TrimSpace(value))
		}
	}
	return out
}

var spreadsheetAddressPattern = regexp.MustCompile(`(?i)^[A-Z]*\s*(\d+)$`)

func parseSpreadsheetAddress(value string) (int, bool) {
	value = strings.TrimSpace(value)
	match := spreadsheetAddressPattern.FindStringSubmatch(value)
	if len(match) == 2 {
		number, err := strconv.Atoi(match[1])
		return number, err == nil && number >= 0
	}
	return 0, false
}

func modbusFrameForTransport(transport string) string {
	switch strings.ToUpper(strings.TrimSpace(transport)) {
	case "MODBUS_TCP":
		return "tcp"
	case "MODBUS_RTU", "MODBUS_SERIAL", "SERIAL":
		return "rtu"
	default:
		return "auto"
	}
}

func hasSerialConnectionNote(notes []string) bool {
	for _, note := range notes {
		if strings.Contains(note, "串口") || strings.Contains(note, "波特率") || strings.Contains(note, "站号") {
			return true
		}
	}
	return false
}
