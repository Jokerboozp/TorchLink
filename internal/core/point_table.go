package core

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"iot-platform/internal/model"
)

// ParseModbusPointTable normalizes CSV/XLSX rows into an auditable, zero-based
// Modbus point table. It never guesses an address notation when the value is
// ambiguous: ordinary numeric addresses require an explicit function code.
func ParseModbusPointTable(filename string, data []byte, defaultPoll int) (model.PointTableRelease, []string, error) {
	if len(data) == 0 {
		return model.PointTableRelease{}, nil, errors.New("point table file is empty")
	}
	if defaultPoll <= 0 {
		defaultPoll = 10
	}
	var rows [][]string
	var err error
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".xlsx":
		rows, err = spreadsheetRows(data)
	case ".csv":
		reader := csv.NewReader(bytes.NewReader(data))
		reader.FieldsPerRecord = -1
		reader.TrimLeadingSpace = true
		rows, err = reader.ReadAll()
	default:
		return model.PointTableRelease{}, nil, errors.New("point table must be .xlsx or .csv")
	}
	if err != nil {
		return model.PointTableRelease{}, nil, fmt.Errorf("read point table: %w", err)
	}
	points, warnings, err := normalizeModbusRows(rows, defaultPoll)
	if err != nil {
		return model.PointTableRelease{}, warnings, err
	}
	digest := sha256.Sum256(data)
	return model.PointTableRelease{SourceName: filepath.Base(filename), SourceSHA256: hex.EncodeToString(digest[:]), Points: points}, warnings, nil
}

func normalizeModbusRows(rows [][]string, defaultPoll int) ([]model.ModbusPoint, []string, error) {
	aliases := map[string]string{
		"identifier": "identifier", "标识": "identifier", "属性标识": "identifier", "变量标识": "identifier",
		"name": "name", "名称": "name", "变量名称": "name", "点位名称": "name",
		"functioncode": "functionCode", "功能码": "functionCode", "fc": "functionCode",
		"address": "address", "地址": "address", "modbus地址": "address", "寄存器地址": "address", "plc线圈地址": "address",
		"addressnotation": "addressNotation", "地址格式": "addressNotation", "地址基准": "addressNotation",
		"datatype": "dataType", "数据类型": "dataType", "类型": "dataType",
		"registercount": "registerCount", "寄存器数量": "registerCount", "长度": "registerCount",
		"byteorder": "byteOrder", "字节序": "byteOrder", "端序": "byteOrder",
		"wordorder": "wordOrder", "字序": "wordOrder",
		"bit": "bit", "位": "bit", "位索引": "bit",
		"scale": "scale", "倍率": "scale", "缩放": "scale",
		"offset": "offset", "偏移": "offset",
		"unit": "unit", "单位": "unit",
		"access": "access", "读写": "access", "访问方式": "access",
		"pollintervalsec": "pollIntervalSec", "轮询周期": "pollIntervalSec", "采集周期": "pollIntervalSec",
		"deadband": "deadband", "死区": "deadband",
		"alarmmapping": "alarmMapping", "告警映射": "alarmMapping", "报警映射": "alarmMapping",
		"description": "description", "说明": "description", "备注": "description",
	}
	headerRow := -1
	columns := map[string]int{}
	for i, row := range rows {
		candidate := map[string]int{}
		for column, cell := range row {
			key := normalizedPointHeader(cell)
			if canonical, ok := aliases[key]; ok {
				candidate[canonical] = column
			}
		}
		if _, address := candidate["address"]; address {
			if _, name := candidate["name"]; name {
				headerRow, columns = i, candidate
				break
			}
		}
	}
	if headerRow < 0 {
		return nil, nil, errors.New("point table header must contain name/名称 and address/地址")
	}
	var points []model.ModbusPoint
	var warnings []string
	seen := map[string]int{}
	for rowIndex, row := range rows[headerRow+1:] {
		line := headerRow + rowIndex + 2
		name := pointCell(row, columns, "name")
		addressText := pointCell(row, columns, "address")
		if name == "" && addressText == "" {
			continue
		}
		if name == "" || addressText == "" {
			return nil, warnings, fmt.Errorf("row %d: name and address are required", line)
		}
		fc, _ := parsePointInt(pointCell(row, columns, "functionCode"))
		address, notation, inferredFC, parseErr := parseModbusAddress(addressText, pointCell(row, columns, "addressNotation"), fc)
		if parseErr != nil {
			return nil, warnings, fmt.Errorf("row %d: %w", line, parseErr)
		}
		if fc == 0 {
			fc = inferredFC
		}
		if fc < 1 || fc > 4 {
			return nil, warnings, fmt.Errorf("row %d: functionCode must be 01, 02, 03 or 04", line)
		}
		dataType := strings.ToLower(strings.TrimSpace(pointCell(row, columns, "dataType")))
		if dataType == "" {
			if fc == 1 || fc == 2 {
				dataType = "bool"
			} else {
				dataType = "uint16"
			}
		}
		registerCount, err := pointRegisterCount(dataType, pointCell(row, columns, "registerCount"))
		if err != nil {
			return nil, warnings, fmt.Errorf("row %d: %w", line, err)
		}
		bit, err := optionalPointInt(pointCell(row, columns, "bit"))
		if err != nil || bit != nil && (*bit < 0 || *bit > 15) {
			return nil, warnings, fmt.Errorf("row %d: bit must be between 0 and 15", line)
		}
		scale := 1.0
		if text := pointCell(row, columns, "scale"); text != "" {
			scale, err = strconv.ParseFloat(text, 64)
			if err != nil {
				return nil, warnings, fmt.Errorf("row %d: invalid scale", line)
			}
		}
		offset := 0.0
		if text := pointCell(row, columns, "offset"); text != "" {
			offset, err = strconv.ParseFloat(text, 64)
			if err != nil {
				return nil, warnings, fmt.Errorf("row %d: invalid offset", line)
			}
		}
		poll := defaultPoll
		if text := pointCell(row, columns, "pollIntervalSec"); text != "" {
			poll, err = strconv.Atoi(text)
			if err != nil || poll <= 0 {
				return nil, warnings, fmt.Errorf("row %d: pollIntervalSec must be positive", line)
			}
		}
		identifier := strings.TrimSpace(pointCell(row, columns, "identifier"))
		if identifier == "" {
			identifier = generatedPointIdentifier(name, fc, address)
		}
		if previous, duplicate := seen[identifier]; duplicate {
			return nil, warnings, fmt.Errorf("row %d: duplicate identifier %q (first at row %d)", line, identifier, previous)
		}
		seen[identifier] = line
		byteOrder := strings.ToLower(strings.TrimSpace(pointCell(row, columns, "byteOrder")))
		if byteOrder == "" {
			byteOrder = "big"
		}
		if byteOrder != "big" && byteOrder != "little" {
			return nil, warnings, fmt.Errorf("row %d: byteOrder must be big or little", line)
		}
		wordOrder := strings.ToUpper(strings.TrimSpace(pointCell(row, columns, "wordOrder")))
		if wordOrder == "" {
			wordOrder = "ABCD"
		}
		if registerCount > 1 && pointCell(row, columns, "wordOrder") == "" {
			warnings = append(warnings, fmt.Sprintf("第 %d 行 %s 未填写字序，暂按 ABCD", line, name))
		}
		if dataType != "string" && !validWordOrder(wordOrder, registerCount*2) {
			return nil, warnings, fmt.Errorf("row %d: wordOrder %q does not match %d bytes", line, wordOrder, registerCount*2)
		}
		access := strings.ToLower(strings.TrimSpace(pointCell(row, columns, "access")))
		if access == "" {
			access = "read"
		}
		if access != "read" && access != "read_write" {
			return nil, warnings, fmt.Errorf("row %d: access must be read or read_write", line)
		}
		deadband := 0.0
		if text := pointCell(row, columns, "deadband"); text != "" {
			deadband, err = strconv.ParseFloat(text, 64)
			if err != nil || deadband < 0 {
				return nil, warnings, fmt.Errorf("row %d: invalid deadband", line)
			}
		}
		alarmMapping, mapErr := parseAlarmMapping(pointCell(row, columns, "alarmMapping"))
		if mapErr != nil {
			return nil, warnings, fmt.Errorf("row %d: %w", line, mapErr)
		}
		points = append(points, model.ModbusPoint{Identifier: identifier, Name: name, FunctionCode: fc, Address: address, AddressNotation: notation, DataType: dataType, RegisterCount: registerCount, ByteOrder: byteOrder, WordOrder: wordOrder, Bit: bit, Scale: scale, Offset: offset, Unit: pointCell(row, columns, "unit"), Access: access, PollIntervalSec: poll, Deadband: deadband, AlarmMapping: alarmMapping, Description: pointCell(row, columns, "description")})
	}
	if len(points) == 0 {
		return nil, warnings, errors.New("point table does not contain any data rows")
	}
	return points, warnings, nil
}

func CompileModbusReadBlocks(points []model.ModbusPoint) ([]model.ModbusReadBlock, error) {
	grouped := map[string][]model.ModbusPoint{}
	for _, p := range points {
		if p.FunctionCode < 1 || p.FunctionCode > 4 {
			return nil, fmt.Errorf("point %q has unsupported function code", p.Identifier)
		}
		interval := p.PollIntervalSec
		if interval <= 0 {
			interval = 10
		}
		k := fmt.Sprintf("%d/%d", p.FunctionCode, interval)
		grouped[k] = append(grouped[k], p)
	}
	keys := make([]string, 0, len(grouped))
	for k := range grouped {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []model.ModbusReadBlock
	for _, k := range keys {
		ps := grouped[k]
		sort.Slice(ps, func(i, j int) bool { return ps[i].Address < ps[j].Address })
		fc, interval := ps[0].FunctionCode, ps[0].PollIntervalSec
		if interval <= 0 {
			interval = 10
		}
		limit := 125
		if fc <= 2 {
			limit = 2000
		}
		start, end := ps[0].Address, ps[0].Address+pointWidth(ps[0])
		flush := func() {
			out = append(out, model.ModbusReadBlock{ID: fmt.Sprintf("fc%02d_%d_%d", fc, start, end-start), FunctionCode: fc, StartAddress: start, Quantity: end - start, PollIntervalSec: interval})
		}
		for _, p := range ps[1:] {
			pEnd := p.Address + pointWidth(p)
			if p.Address > end || pEnd-start > limit {
				flush()
				start, end = p.Address, pEnd
				continue
			}
			if pEnd > end {
				end = pEnd
			}
		}
		flush()
	}
	return out, nil
}

func pointCell(row []string, columns map[string]int, name string) string {
	i, ok := columns[name]
	if !ok || i < 0 || i >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[i])
}
func normalizedPointHeader(v string) string {
	return strings.ToLower(strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || r == '_' || r == '-' || r == '/' {
			return -1
		}
		return r
	}, strings.TrimSpace(v)))
}
func parsePointInt(v string) (int, error) {
	v = strings.TrimSpace(strings.TrimPrefix(strings.ToUpper(v), "FC"))
	if v == "" {
		return 0, nil
	}
	return strconv.Atoi(v)
}
func optionalPointInt(v string) (*int, error) {
	if strings.TrimSpace(v) == "" {
		return nil, nil
	}
	n, e := strconv.Atoi(strings.TrimSpace(v))
	return &n, e
}
func parseModbusAddress(text, notation string, fc int) (int, string, int, error) {
	clean := strings.TrimSpace(strings.ToUpper(text))
	for _, prefix := range []string{"HR", "IR", "COIL", "DI"} {
		clean = strings.TrimPrefix(clean, prefix)
	}
	clean = strings.TrimLeft(clean, " ")
	n, err := strconv.Atoi(clean)
	if err != nil || n < 0 {
		return 0, "", 0, fmt.Errorf("invalid Modbus address %q", text)
	}
	if strings.EqualFold(strings.TrimSpace(notation), "zero_based") {
		if fc == 0 {
			return 0, "", 0, errors.New("functionCode is required for zero/one based addresses")
		}
		if n > 65535 {
			return 0, "", 0, errors.New("Modbus address must not exceed 65535")
		}
		return n, "zero_based", fc, nil
	}
	notation = strings.ToLower(strings.TrimSpace(notation))
	if notation == "" {
		notation = "zero_based"
	}
	if n >= 40001 && n <= 49999 {
		if fc != 0 && fc != 3 {
			return 0, "", 0, errors.New("4xxxx address conflicts with functionCode")
		}
		return n - 40001, "4xxxx", 3, nil
	}
	if n >= 30001 && n <= 39999 {
		if fc != 0 && fc != 4 {
			return 0, "", 0, errors.New("3xxxx address conflicts with functionCode")
		}
		return n - 30001, "3xxxx", 4, nil
	}
	if n >= 10001 && n <= 19999 {
		if fc != 0 && fc != 2 {
			return 0, "", 0, errors.New("1xxxx address conflicts with functionCode")
		}
		return n - 10001, "1xxxx", 2, nil
	}
	if notation == "one_based" {
		if n == 0 {
			return 0, "", 0, errors.New("one_based address must start at 1")
		}
		n--
	} else if notation != "zero_based" {
		return 0, "", 0, fmt.Errorf("unsupported addressNotation %q", notation)
	}
	if fc == 0 {
		return 0, "", 0, errors.New("functionCode is required for zero/one based addresses")
	}
	return n, notation, fc, nil
}
func pointRegisterCount(dataType, text string) (int, error) {
	if text != "" {
		n, e := strconv.Atoi(text)
		if e != nil || n <= 0 {
			return 0, errors.New("registerCount must be positive")
		}
		return n, nil
	}
	switch dataType {
	case "bool", "uint16", "int16", "bits":
		return 1, nil
	case "uint32", "int32", "float32":
		return 2, nil
	case "uint64", "int64", "float64":
		return 4, nil
	case "string":
		return 0, errors.New("string requires registerCount")
	default:
		return 0, fmt.Errorf("unsupported dataType %q", dataType)
	}
}
func pointWidth(p model.ModbusPoint) int {
	if p.FunctionCode <= 2 {
		return 1
	}
	if p.RegisterCount > 0 {
		return p.RegisterCount
	}
	return 1
}
func generatedPointIdentifier(name string, fc, address int) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' {
			b.WriteRune(r)
		} else if b.Len() > 0 && b.String()[b.Len()-1] != '_' {
			b.WriteByte('_')
		}
	}
	v := strings.Trim(b.String(), "_")
	if v == "" {
		v = fmt.Sprintf("fc%02d_%d", fc, address)
	}
	return v
}
func validWordOrder(value string, bytes int) bool {
	if bytes <= 2 {
		return value == "ABCD" || value == "AB"
	}
	allowed := map[int]map[string]bool{4: {"ABCD": true, "CDAB": true, "BADC": true, "DCBA": true}, 8: {"ABCDEFGH": true, "GHEFCDAB": true, "BADCFEHG": true, "HGFEDCBA": true}}
	return allowed[bytes][value]
}

func parseAlarmMapping(value string) (map[string]any, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	out := map[string]any{}
	for _, item := range strings.FieldsFunc(value, func(r rune) bool { return r == ';' || r == '；' || r == ',' || r == '，' }) {
		parts := strings.SplitN(strings.TrimSpace(item), "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return nil, fmt.Errorf("alarmMapping %q must use value=alarmType", item)
		}
		out[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	return out, nil
}
