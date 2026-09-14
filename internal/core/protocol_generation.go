package core

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"iot-platform/internal/model"
	"iot-platform/internal/parser"
)

func buildUploadedProtocol(in ProtocolAssistantInput) (model.ProtocolAssistantDraft, bool, error) {
	if in.InputKind == "point-table" {
		filename, data := in.DocumentFilename, in.DocumentData
		if len(data) == 0 {
			filename = "points.csv"
			data = []byte(in.PointTable)
		}
		table, warnings, err := ParseModbusPointTable(filename, data, 10)
		if err != nil && strings.EqualFold(filepath.Ext(filename), ".xlsx") {
			if legacy, legacyErr := BuildProtocolAssistantSpreadsheetDraft(in); legacyErr == nil {
				table.Points = nil
				for _, field := range legacy.Fields {
					table.Points = append(table.Points, model.ModbusPoint{Identifier: generatedPointIdentifier(field.Name, 1, field.CoilAddress), Name: field.Name, FunctionCode: 1, Address: field.CoilAddress, DataType: "bool", RegisterCount: 1, Scale: 1, PollIntervalSec: 10, Description: field.Description})
				}
				warnings = legacy.Warnings
				err = nil
			}
		}
		if err != nil {
			return model.ProtocolAssistantDraft{}, true, err
		}
		blocks, err := CompileModbusReadBlocks(table.Points)
		if err != nil {
			return model.ProtocolAssistantDraft{}, true, err
		}
		transport := strings.ToUpper(in.Transport)
		if transport != "MODBUS_TCP" && transport != "MODBUS_RTU" {
			return model.ProtocolAssistantDraft{}, true, errors.New("点表请选择 Modbus TCP 或 Modbus RTU")
		}
		parserType := parser.ModbusTCPParserName
		if transport == "MODBUS_RTU" {
			parserType = parser.ModbusRTUParserName
		}
		draft := model.ProtocolAssistantDraft{Name: in.Name, Protocol: in.Protocol, Transport: transport, PayloadFormat: "hex", ParserType: parserType, MessageType: model.PropertyReport, Config: map[string]any{"points": table.Points, "blocks": blocks}, Warnings: warnings}
		if err := NormalizeGeneratedModbusConfig(draft.Config); err != nil {
			return draft, true, err
		}
		for _, p := range table.Points {
			draft.Fields = append(draft.Fields, model.ProtocolAssistantField{Name: p.Identifier, Label: p.Name, Address: fmt.Sprint(p.Address), DataType: p.DataType, Description: p.Description})
		}
		if draft.Name == "" {
			draft.Name = strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename))
		}
		return draft, true, nil
	}
	sample := strings.TrimSpace(in.SamplePayload)
	if in.InputKind == "sample" && sample == "" {
		sample = strings.TrimSpace(in.DocumentText)
	}
	if sample == "" || (in.PayloadFormat != "json" && !strings.HasPrefix(sample, "{")) {
		return model.ProtocolAssistantDraft{}, false, nil
	}
	var object map[string]any
	if err := json.Unmarshal([]byte(sample), &object); err != nil || object == nil {
		return model.ProtocolAssistantDraft{}, true, errors.New("报文须为有效的 JSON 对象")
	}
	// Preserve explicit instructions for the AI mapping path.
	if strings.TrimSpace(in.PointTable) != "" {
		return model.ProtocolAssistantDraft{}, false, nil
	}
	root, prefix := object, "$"
	for _, key := range []string{"properties", "data"} {
		if nested, ok := object[key].(map[string]any); ok {
			root = nested
			prefix = "$." + key
			break
		}
	}
	mappings := map[string]any{}
	fields := []model.ProtocolAssistantField{}
	keys := make([]string, 0, len(root))
	for key := range root {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if prefix == "$" {
			switch key {
			case "timestamp", "messageType", "event", "tags", "alarm", "deviceId", "productId", "tenantId", "messageId":
				continue
			}
		}
		if strings.ContainsAny(key, ".[]") {
			return model.ProtocolAssistantDraft{}, true, fmt.Errorf("字段 %q 包含路径保留字符，请提供字段映射说明", key)
		}
		path := prefix + "." + key
		mappings[key] = path
		fields = append(fields, model.ProtocolAssistantField{Name: key, Label: key, Expression: path, Type: fmt.Sprintf("%T", root[key])})
	}
	if len(fields) == 0 {
		return model.ProtocolAssistantDraft{}, true, errors.New("报文中没有可生成映射的属性字段")
	}
	draft := model.ProtocolAssistantDraft{Name: in.Name, Protocol: in.Protocol, Transport: in.Transport, PayloadFormat: "json", ParserType: "configurable_json_parser", MessageType: model.PropertyReport, Fields: fields, Config: map[string]any{"properties": mappings}, SamplePayload: object}
	if draft.Name == "" {
		draft.Name = "JSON 报文协议"
	}
	if draft.Transport == "" {
		draft.Transport = "MQTT"
	}
	preview, err := PreviewProtocolAssistant(draft, "preview", sample)
	if err != nil {
		return draft, true, err
	}
	draft.MessageType = preview.MessageType
	draft.Preview = preview
	return draft, true, nil
}

func NormalizeGeneratedModbusConfig(config map[string]any) error {
	data, err := json.Marshal(config["points"])
	if err != nil {
		return err
	}
	var points []model.ModbusPoint
	if err = json.Unmarshal(data, &points); err != nil {
		return err
	}
	var buffer bytes.Buffer
	writer := csv.NewWriter(&buffer)
	_ = writer.Write([]string{"identifier", "name", "functionCode", "address", "addressNotation", "dataType", "registerCount", "byteOrder", "wordOrder", "bit", "scale", "offset", "unit", "access", "pollIntervalSec", "deadband", "alarmMapping", "description"})
	for _, p := range points {
		if p.Address < 0 || p.Address > 65535 {
			return errors.New("点位地址须在 0 到 65535 之间")
		}
		if p.DataType != "string" {
			width, e := pointRegisterCount(p.DataType, "")
			if e != nil {
				return e
			}
			if p.RegisterCount != width {
				return fmt.Errorf("点位 %s 的寄存器数量与数据类型不一致", p.Name)
			}
		}
		if p.RegisterCount < 1 || p.RegisterCount > 125 || p.Address+p.RegisterCount > 65536 {
			return errors.New("点位寄存器范围无效")
		}
		if p.FunctionCode <= 2 && p.DataType != "bool" {
			return errors.New("线圈与离散输入须使用 bool 类型")
		}
		bit := ""
		if p.Bit != nil {
			bit = fmt.Sprint(*p.Bit)
		}
		alarm := ""
		if len(p.AlarmMapping) > 0 {
			b, _ := json.Marshal(p.AlarmMapping)
			alarm = string(b)
		}
		_ = writer.Write([]string{p.Identifier, p.Name, fmt.Sprint(p.FunctionCode), fmt.Sprint(p.Address), "zero_based", p.DataType, fmt.Sprint(p.RegisterCount), p.ByteOrder, p.WordOrder, bit, fmt.Sprint(p.Scale), fmt.Sprint(p.Offset), p.Unit, p.Access, fmt.Sprint(p.PollIntervalSec), fmt.Sprint(p.Deadband), alarm, p.Description})
	}
	writer.Flush()
	if err = writer.Error(); err != nil {
		return err
	}
	table, _, err := ParseModbusPointTable("points.csv", buffer.Bytes(), 10)
	if err != nil {
		return err
	}
	blocks, err := CompileModbusReadBlocks(table.Points)
	if err != nil {
		return err
	}
	config["points"] = table.Points
	config["blocks"] = blocks
	return nil
}
