package core /* 声明 core 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"bytes"         /* 执行当前语句并推进处理流程。 */
	"crypto/sha256" /* 执行当前语句并推进处理流程。 */
	"encoding/csv"  /* 执行当前语句并推进处理流程。 */
	"encoding/hex"  /* 执行当前语句并推进处理流程。 */
	"errors"        /* 执行当前语句并推进处理流程。 */
	"fmt"           /* 执行当前语句并推进处理流程。 */
	"path/filepath" /* 执行当前语句并推进处理流程。 */
	"sort"          /* 执行当前语句并推进处理流程。 */
	"strconv"       /* 执行当前语句并推进处理流程。 */
	"strings"       /* 执行当前语句并推进处理流程。 */
	"unicode"       /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

// ParseModbusPointTable normalizes CSV/XLSX rows into an auditable, zero-based
// Modbus point table. It never guesses an address notation when the value is
// ambiguous: ordinary numeric addresses require an explicit function code.
func ParseModbusPointTable(filename string, data []byte, defaultPoll int) (model.PointTableRelease, []string, error) { /* 定义 ParseModbusPointTable 函数。 */
	if len(data) == 0 { /* 判断条件并选择处理分支。 */
		return model.PointTableRelease{}, nil, errors.New("point table file is empty") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if defaultPoll <= 0 { /* 判断条件并选择处理分支。 */
		defaultPoll = 10 /* 更新 defaultPoll 的值。 */
	} /* 结束当前表达式或代码块。 */
	var rows [][]string                              /* 声明 rows。 */
	var err error                                    /* 声明 err。 */
	switch strings.ToLower(filepath.Ext(filename)) { /* 根据条件选择处理路径。 */
	case ".xlsx": /* 处理当前分支。 */
		rows, err = spreadsheetRows(data) /* 更新 err 的值。 */
	case ".csv": /* 处理当前分支。 */
		reader := csv.NewReader(bytes.NewReader(data)) /* 更新 reader 的值。 */
		reader.FieldsPerRecord = -1                    /* 更新 reader.FieldsPerRecord 的值。 */
		reader.TrimLeadingSpace = true                 /* 更新 reader.TrimLeadingSpace 的值。 */
		rows, err = reader.ReadAll()                   /* 更新 err 的值。 */
	default: /* 处理当前分支。 */
		return model.PointTableRelease{}, nil, errors.New("point table must be .xlsx or .csv") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err != nil { /* 判断条件并选择处理分支。 */
		return model.PointTableRelease{}, nil, fmt.Errorf("read point table: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	points, warnings, err := normalizeModbusRows(rows, defaultPoll) /* 更新 err 的值。 */
	if err != nil {                                                 /* 判断条件并选择处理分支。 */
		return model.PointTableRelease{}, warnings, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	digest := sha256.Sum256(data)                                                                                                                   /* 更新 digest 的值。 */
	return model.PointTableRelease{SourceName: filepath.Base(filename), SourceSHA256: hex.EncodeToString(digest[:]), Points: points}, warnings, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func normalizeModbusRows(rows [][]string, defaultPoll int) ([]model.ModbusPoint, []string, error) { /* 定义 normalizeModbusRows 函数。 */
	aliases := map[string]string{ /* 更新 aliases 的值。 */
		"identifier": "identifier", "标识": "identifier", "属性标识": "identifier", "变量标识": "identifier", /* 执行当前语句并推进处理流程。 */
		"name": "name", "名称": "name", "变量名称": "name", "点位名称": "name", /* 执行当前语句并推进处理流程。 */
		"functioncode": "functionCode", "功能码": "functionCode", "fc": "functionCode", /* 执行当前语句并推进处理流程。 */
		"address": "address", "地址": "address", "modbus地址": "address", "寄存器地址": "address", "plc线圈地址": "address", /* 执行当前语句并推进处理流程。 */
		"addressnotation": "addressNotation", "地址格式": "addressNotation", "地址基准": "addressNotation", /* 执行当前语句并推进处理流程。 */
		"datatype": "dataType", "数据类型": "dataType", "类型": "dataType", /* 执行当前语句并推进处理流程。 */
		"registercount": "registerCount", "寄存器数量": "registerCount", "长度": "registerCount", /* 执行当前语句并推进处理流程。 */
		"byteorder": "byteOrder", "字节序": "byteOrder", "端序": "byteOrder", /* 执行当前语句并推进处理流程。 */
		"wordorder": "wordOrder", "字序": "wordOrder", /* 执行当前语句并推进处理流程。 */
		"bit": "bit", "位": "bit", "位索引": "bit", /* 执行当前语句并推进处理流程。 */
		"scale": "scale", "倍率": "scale", "缩放": "scale", /* 执行当前语句并推进处理流程。 */
		"offset": "offset", "偏移": "offset", /* 执行当前语句并推进处理流程。 */
		"unit": "unit", "单位": "unit", /* 执行当前语句并推进处理流程。 */
		"access": "access", "读写": "access", "访问方式": "access", /* 执行当前语句并推进处理流程。 */
		"pollintervalsec": "pollIntervalSec", "轮询周期": "pollIntervalSec", "采集周期": "pollIntervalSec", /* 执行当前语句并推进处理流程。 */
		"deadband": "deadband", "死区": "deadband", /* 执行当前语句并推进处理流程。 */
		"alarmmapping": "alarmMapping", "告警映射": "alarmMapping", "报警映射": "alarmMapping", /* 执行当前语句并推进处理流程。 */
		"description": "description", "说明": "description", "备注": "description", /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	headerRow := -1             /* 更新 headerRow 的值。 */
	columns := map[string]int{} /* 更新 columns 的值。 */
	for i, row := range rows {  /* 循环处理当前数据。 */
		candidate := map[string]int{}   /* 更新 candidate 的值。 */
		for column, cell := range row { /* 循环处理当前数据。 */
			key := normalizedPointHeader(cell)     /* 更新 key 的值。 */
			if canonical, ok := aliases[key]; ok { /* 判断条件并选择处理分支。 */
				candidate[canonical] = column /* 更新 candidate[canonical] 的值。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		if _, address := candidate["address"]; address { /* 判断条件并选择处理分支。 */
			if _, name := candidate["name"]; name { /* 判断条件并选择处理分支。 */
				headerRow, columns = i, candidate /* 更新 columns 的值。 */
				break                             /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if headerRow < 0 { /* 判断条件并选择处理分支。 */
		return nil, nil, errors.New("point table header must contain name/名称 and address/地址") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var points []model.ModbusPoint                  /* 声明 points。 */
	var warnings []string                           /* 声明 warnings。 */
	seen := map[string]int{}                        /* 更新 seen 的值。 */
	for rowIndex, row := range rows[headerRow+1:] { /* 循环处理当前数据。 */
		line := headerRow + rowIndex + 2                  /* 更新 line 的值。 */
		name := pointCell(row, columns, "name")           /* 更新 name 的值。 */
		addressText := pointCell(row, columns, "address") /* 更新 addressText 的值。 */
		if name == "" && addressText == "" {              /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if name == "" || addressText == "" { /* 判断条件并选择处理分支。 */
			return nil, warnings, fmt.Errorf("row %d: name and address are required", line) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		fc, _ := parsePointInt(pointCell(row, columns, "functionCode"))                                                            /* 更新 _ 的值。 */
		address, notation, inferredFC, parseErr := parseModbusAddress(addressText, pointCell(row, columns, "addressNotation"), fc) /* 更新 parseErr 的值。 */
		if parseErr != nil {                                                                                                       /* 判断条件并选择处理分支。 */
			return nil, warnings, fmt.Errorf("row %d: %w", line, parseErr) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if fc == 0 { /* 判断条件并选择处理分支。 */
			fc = inferredFC /* 更新 fc 的值。 */
		} /* 结束当前表达式或代码块。 */
		if fc < 1 || fc > 4 { /* 判断条件并选择处理分支。 */
			return nil, warnings, fmt.Errorf("row %d: functionCode must be 01, 02, 03 or 04", line) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		dataType := strings.ToLower(strings.TrimSpace(pointCell(row, columns, "dataType"))) /* 更新 dataType 的值。 */
		if dataType == "" {                                                                 /* 判断条件并选择处理分支。 */
			if fc == 1 || fc == 2 { /* 判断条件并选择处理分支。 */
				dataType = "bool" /* 更新 dataType 的值。 */
			} else { /* 结束当前表达式或代码块。 */
				dataType = "uint16" /* 更新 dataType 的值。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		registerCount, err := pointRegisterCount(dataType, pointCell(row, columns, "registerCount")) /* 更新 err 的值。 */
		if err != nil {                                                                              /* 判断条件并选择处理分支。 */
			return nil, warnings, fmt.Errorf("row %d: %w", line, err) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		bit, err := optionalPointInt(pointCell(row, columns, "bit")) /* 更新 err 的值。 */
		if err != nil || bit != nil && (*bit < 0 || *bit > 15) {     /* 判断条件并选择处理分支。 */
			return nil, warnings, fmt.Errorf("row %d: bit must be between 0 and 15", line) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		scale := 1.0                                              /* 更新 scale 的值。 */
		if text := pointCell(row, columns, "scale"); text != "" { /* 判断条件并选择处理分支。 */
			scale, err = strconv.ParseFloat(text, 64) /* 更新 err 的值。 */
			if err != nil {                           /* 判断条件并选择处理分支。 */
				return nil, warnings, fmt.Errorf("row %d: invalid scale", line) /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		offset := 0.0                                              /* 更新 offset 的值。 */
		if text := pointCell(row, columns, "offset"); text != "" { /* 判断条件并选择处理分支。 */
			offset, err = strconv.ParseFloat(text, 64) /* 更新 err 的值。 */
			if err != nil {                            /* 判断条件并选择处理分支。 */
				return nil, warnings, fmt.Errorf("row %d: invalid offset", line) /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		poll := defaultPoll                                                 /* 更新 poll 的值。 */
		if text := pointCell(row, columns, "pollIntervalSec"); text != "" { /* 判断条件并选择处理分支。 */
			poll, err = strconv.Atoi(text) /* 更新 err 的值。 */
			if err != nil || poll <= 0 {   /* 判断条件并选择处理分支。 */
				return nil, warnings, fmt.Errorf("row %d: pollIntervalSec must be positive", line) /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		identifier := strings.TrimSpace(pointCell(row, columns, "identifier")) /* 更新 identifier 的值。 */
		if identifier == "" {                                                  /* 判断条件并选择处理分支。 */
			identifier = generatedPointIdentifier(name, fc, address) /* 更新 identifier 的值。 */
		} /* 结束当前表达式或代码块。 */
		if previous, duplicate := seen[identifier]; duplicate { /* 判断条件并选择处理分支。 */
			return nil, warnings, fmt.Errorf("row %d: duplicate identifier %q (first at row %d)", line, identifier, previous) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		seen[identifier] = line                                                               /* 更新 seen[identifier] 的值。 */
		byteOrder := strings.ToLower(strings.TrimSpace(pointCell(row, columns, "byteOrder"))) /* 更新 byteOrder 的值。 */
		if byteOrder == "" {                                                                  /* 判断条件并选择处理分支。 */
			byteOrder = "big" /* 更新 byteOrder 的值。 */
		} /* 结束当前表达式或代码块。 */
		if byteOrder != "big" && byteOrder != "little" { /* 判断条件并选择处理分支。 */
			return nil, warnings, fmt.Errorf("row %d: byteOrder must be big or little", line) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		wordOrder := strings.ToUpper(strings.TrimSpace(pointCell(row, columns, "wordOrder"))) /* 更新 wordOrder 的值。 */
		if wordOrder == "" {                                                                  /* 判断条件并选择处理分支。 */
			wordOrder = "ABCD" /* 更新 wordOrder 的值。 */
		} /* 结束当前表达式或代码块。 */
		if registerCount > 1 && pointCell(row, columns, "wordOrder") == "" { /* 判断条件并选择处理分支。 */
			warnings = append(warnings, fmt.Sprintf("第 %d 行 %s 未填写字序，暂按 ABCD", line, name)) /* 更新 warnings 的值。 */
		} /* 结束当前表达式或代码块。 */
		if dataType != "string" && !validWordOrder(wordOrder, registerCount*2) { /* 判断条件并选择处理分支。 */
			return nil, warnings, fmt.Errorf("row %d: wordOrder %q does not match %d bytes", line, wordOrder, registerCount*2) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		access := strings.ToLower(strings.TrimSpace(pointCell(row, columns, "access"))) /* 更新 access 的值。 */
		if access == "" {                                                               /* 判断条件并选择处理分支。 */
			access = "read" /* 更新 access 的值。 */
		} /* 结束当前表达式或代码块。 */
		if access != "read" && access != "read_write" { /* 判断条件并选择处理分支。 */
			return nil, warnings, fmt.Errorf("row %d: access must be read or read_write", line) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		deadband := 0.0                                              /* 更新 deadband 的值。 */
		if text := pointCell(row, columns, "deadband"); text != "" { /* 判断条件并选择处理分支。 */
			deadband, err = strconv.ParseFloat(text, 64) /* 更新 err 的值。 */
			if err != nil || deadband < 0 {              /* 判断条件并选择处理分支。 */
				return nil, warnings, fmt.Errorf("row %d: invalid deadband", line) /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		alarmMapping, mapErr := parseAlarmMapping(pointCell(row, columns, "alarmMapping")) /* 更新 mapErr 的值。 */
		if mapErr != nil {                                                                 /* 判断条件并选择处理分支。 */
			return nil, warnings, fmt.Errorf("row %d: %w", line, mapErr) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		points = append(points, model.ModbusPoint{Identifier: identifier, Name: name, FunctionCode: fc, Address: address, AddressNotation: notation, DataType: dataType, RegisterCount: registerCount, ByteOrder: byteOrder, WordOrder: wordOrder, Bit: bit, Scale: scale, Offset: offset, Unit: pointCell(row, columns, "unit"), Access: access, PollIntervalSec: poll, Deadband: deadband, AlarmMapping: alarmMapping, Description: pointCell(row, columns, "description")}) /* 更新 points 的值。 */
	} /* 结束当前表达式或代码块。 */
	if len(points) == 0 { /* 判断条件并选择处理分支。 */
		return nil, warnings, errors.New("point table does not contain any data rows") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return points, warnings, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func CompileModbusReadBlocks(points []model.ModbusPoint) ([]model.ModbusReadBlock, error) { /* 定义 CompileModbusReadBlocks 函数。 */
	grouped := map[string][]model.ModbusPoint{} /* 更新 grouped 的值。 */
	for _, p := range points {                  /* 循环处理当前数据。 */
		if p.FunctionCode < 1 || p.FunctionCode > 4 { /* 判断条件并选择处理分支。 */
			return nil, fmt.Errorf("point %q has unsupported function code", p.Identifier) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		interval := p.PollIntervalSec /* 更新 interval 的值。 */
		if interval <= 0 {            /* 判断条件并选择处理分支。 */
			interval = 10 /* 更新 interval 的值。 */
		} /* 结束当前表达式或代码块。 */
		k := fmt.Sprintf("%d/%d", p.FunctionCode, interval) /* 更新 k 的值。 */
		grouped[k] = append(grouped[k], p)                  /* 更新 grouped[k] 的值。 */
	} /* 结束当前表达式或代码块。 */
	keys := make([]string, 0, len(grouped)) /* 更新 keys 的值。 */
	for k := range grouped {                /* 循环处理当前数据。 */
		keys = append(keys, k) /* 更新 keys 的值。 */
	} /* 结束当前表达式或代码块。 */
	sort.Strings(keys)              /* 执行当前语句并推进处理流程。 */
	var out []model.ModbusReadBlock /* 声明 out。 */
	for _, k := range keys {        /* 循环处理当前数据。 */
		ps := grouped[k]                                                             /* 更新 ps 的值。 */
		sort.Slice(ps, func(i, j int) bool { return ps[i].Address < ps[j].Address }) /* 执行当前语句并推进处理流程。 */
		fc, interval := ps[0].FunctionCode, ps[0].PollIntervalSec                    /* 更新 interval 的值。 */
		if interval <= 0 {                                                           /* 判断条件并选择处理分支。 */
			interval = 10 /* 更新 interval 的值。 */
		} /* 结束当前表达式或代码块。 */
		limit := 125 /* 更新 limit 的值。 */
		if fc <= 2 { /* 判断条件并选择处理分支。 */
			limit = 2000 /* 更新 limit 的值。 */
		} /* 结束当前表达式或代码块。 */
		start, end := ps[0].Address, ps[0].Address+pointWidth(ps[0]) /* 更新 end 的值。 */
		flush := func() {                                            /* 更新 flush 的值。 */
			out = append(out, model.ModbusReadBlock{ID: fmt.Sprintf("fc%02d_%d_%d", fc, start, end-start), FunctionCode: fc, StartAddress: start, Quantity: end - start, PollIntervalSec: interval}) /* 更新 out 的值。 */
		} /* 结束当前表达式或代码块。 */
		for _, p := range ps[1:] { /* 循环处理当前数据。 */
			pEnd := p.Address + pointWidth(p)          /* 更新 pEnd 的值。 */
			if p.Address > end || pEnd-start > limit { /* 判断条件并选择处理分支。 */
				flush()                      /* 执行当前语句并推进处理流程。 */
				start, end = p.Address, pEnd /* 更新 end 的值。 */
				continue                     /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
			if pEnd > end { /* 判断条件并选择处理分支。 */
				end = pEnd /* 更新 end 的值。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		flush() /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	return out, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func pointCell(row []string, columns map[string]int, name string) string { /* 定义 pointCell 函数。 */
	i, ok := columns[name]             /* 更新 ok 的值。 */
	if !ok || i < 0 || i >= len(row) { /* 判断条件并选择处理分支。 */
		return "" /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return strings.TrimSpace(row[i]) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func normalizedPointHeader(v string) string { /* 定义 normalizedPointHeader 函数。 */
	return strings.ToLower(strings.Map(func(r rune) rune { /* 返回当前处理结果。 */
		if unicode.IsSpace(r) || r == '_' || r == '-' || r == '/' { /* 判断条件并选择处理分支。 */
			return -1 /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return r /* 返回当前处理结果。 */
	}, strings.TrimSpace(v))) /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
func parsePointInt(v string) (int, error) { /* 定义 parsePointInt 函数。 */
	v = strings.TrimSpace(strings.TrimPrefix(strings.ToUpper(v), "FC")) /* 更新 v 的值。 */
	if v == "" {                                                        /* 判断条件并选择处理分支。 */
		return 0, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return strconv.Atoi(v) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func optionalPointInt(v string) (*int, error) { /* 定义 optionalPointInt 函数。 */
	if strings.TrimSpace(v) == "" { /* 判断条件并选择处理分支。 */
		return nil, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	n, e := strconv.Atoi(strings.TrimSpace(v)) /* 更新 e 的值。 */
	return &n, e                               /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func parseModbusAddress(text, notation string, fc int) (int, string, int, error) { /* 定义 parseModbusAddress 函数。 */
	clean := strings.TrimSpace(strings.ToUpper(text))           /* 更新 clean 的值。 */
	for _, prefix := range []string{"HR", "IR", "COIL", "DI"} { /* 循环处理当前数据。 */
		clean = strings.TrimPrefix(clean, prefix) /* 更新 clean 的值。 */
	} /* 结束当前表达式或代码块。 */
	clean = strings.TrimLeft(clean, " ") /* 更新 clean 的值。 */
	n, err := strconv.Atoi(clean)        /* 更新 err 的值。 */
	if err != nil || n < 0 {             /* 判断条件并选择处理分支。 */
		return 0, "", 0, fmt.Errorf("invalid Modbus address %q", text) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if strings.EqualFold(strings.TrimSpace(notation), "zero_based") { /* 判断条件并选择处理分支。 */
		if fc == 0 { /* 判断条件并选择处理分支。 */
			return 0, "", 0, errors.New("functionCode is required for zero/one based addresses") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if n > 65535 { /* 判断条件并选择处理分支。 */
			return 0, "", 0, errors.New("Modbus address must not exceed 65535") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return n, "zero_based", fc, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	notation = strings.ToLower(strings.TrimSpace(notation)) /* 更新 notation 的值。 */
	if notation == "" {                                     /* 判断条件并选择处理分支。 */
		notation = "zero_based" /* 更新 notation 的值。 */
	} /* 结束当前表达式或代码块。 */
	if n >= 40001 && n <= 49999 { /* 判断条件并选择处理分支。 */
		if fc != 0 && fc != 3 { /* 判断条件并选择处理分支。 */
			return 0, "", 0, errors.New("4xxxx address conflicts with functionCode") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return n - 40001, "4xxxx", 3, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if n >= 30001 && n <= 39999 { /* 判断条件并选择处理分支。 */
		if fc != 0 && fc != 4 { /* 判断条件并选择处理分支。 */
			return 0, "", 0, errors.New("3xxxx address conflicts with functionCode") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return n - 30001, "3xxxx", 4, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if n >= 10001 && n <= 19999 { /* 判断条件并选择处理分支。 */
		if fc != 0 && fc != 2 { /* 判断条件并选择处理分支。 */
			return 0, "", 0, errors.New("1xxxx address conflicts with functionCode") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return n - 10001, "1xxxx", 2, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if notation == "one_based" { /* 判断条件并选择处理分支。 */
		if n == 0 { /* 判断条件并选择处理分支。 */
			return 0, "", 0, errors.New("one_based address must start at 1") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		n-- /* 执行当前语句并推进处理流程。 */
	} else if notation != "zero_based" { /* 结束当前表达式或代码块。 */
		return 0, "", 0, fmt.Errorf("unsupported addressNotation %q", notation) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if fc == 0 { /* 判断条件并选择处理分支。 */
		return 0, "", 0, errors.New("functionCode is required for zero/one based addresses") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return n, notation, fc, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func pointRegisterCount(dataType, text string) (int, error) { /* 定义 pointRegisterCount 函数。 */
	if text != "" { /* 判断条件并选择处理分支。 */
		n, e := strconv.Atoi(text) /* 更新 e 的值。 */
		if e != nil || n <= 0 {    /* 判断条件并选择处理分支。 */
			return 0, errors.New("registerCount must be positive") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return n, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	switch dataType { /* 根据条件选择处理路径。 */
	case "bool", "uint16", "int16", "bits": /* 处理当前分支。 */
		return 1, nil /* 返回当前处理结果。 */
	case "uint32", "int32", "float32": /* 处理当前分支。 */
		return 2, nil /* 返回当前处理结果。 */
	case "uint64", "int64", "float64": /* 处理当前分支。 */
		return 4, nil /* 返回当前处理结果。 */
	case "string": /* 处理当前分支。 */
		return 0, errors.New("string requires registerCount") /* 返回当前处理结果。 */
	default: /* 处理当前分支。 */
		return 0, fmt.Errorf("unsupported dataType %q", dataType) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
func pointWidth(p model.ModbusPoint) int { /* 定义 pointWidth 函数。 */
	if p.FunctionCode <= 2 { /* 判断条件并选择处理分支。 */
		return 1 /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if p.RegisterCount > 0 { /* 判断条件并选择处理分支。 */
		return p.RegisterCount /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return 1 /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func generatedPointIdentifier(name string, fc, address int) string { /* 定义 generatedPointIdentifier 函数。 */
	var b strings.Builder                     /* 声明 b。 */
	for _, r := range strings.ToLower(name) { /* 循环处理当前数据。 */
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' { /* 判断条件并选择处理分支。 */
			b.WriteRune(r) /* 执行当前语句并推进处理流程。 */
		} else if b.Len() > 0 && b.String()[b.Len()-1] != '_' { /* 结束当前表达式或代码块。 */
			b.WriteByte('_') /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	v := strings.Trim(b.String(), "_") /* 更新 v 的值。 */
	if v == "" {                       /* 判断条件并选择处理分支。 */
		v = fmt.Sprintf("fc%02d_%d", fc, address) /* 更新 v 的值。 */
	} /* 结束当前表达式或代码块。 */
	return v /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func validWordOrder(value string, bytes int) bool { /* 定义 validWordOrder 函数。 */
	if bytes <= 2 { /* 判断条件并选择处理分支。 */
		return value == "ABCD" || value == "AB" /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	allowed := map[int]map[string]bool{4: {"ABCD": true, "CDAB": true, "BADC": true, "DCBA": true}, 8: {"ABCDEFGH": true, "GHEFCDAB": true, "BADCFEHG": true, "HGFEDCBA": true}} /* 更新 allowed 的值。 */
	return allowed[bytes][value]                                                                                                                                                 /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func parseAlarmMapping(value string) (map[string]any, error) { /* 定义 parseAlarmMapping 函数。 */
	value = strings.TrimSpace(value) /* 更新 value 的值。 */
	if value == "" {                 /* 判断条件并选择处理分支。 */
		return nil, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	out := map[string]any{}                                                                                                     /* 更新 out 的值。 */
	for _, item := range strings.FieldsFunc(value, func(r rune) bool { return r == ';' || r == '；' || r == ',' || r == '，' }) { /* 循环处理当前数据。 */
		parts := strings.SplitN(strings.TrimSpace(item), "=", 2)                                       /* 更新 parts 的值。 */
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" { /* 判断条件并选择处理分支。 */
			return nil, fmt.Errorf("alarmMapping %q must use value=alarmType", item) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		out[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1]) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	return out, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
