package sites

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"

	"iot-platform/internal/model"
)

// ImportColumns are the template headers; 单位名称 is required on every
// row, 设备编号 only when the row places a device.
var ImportColumns = []string{"单位名称", "单位编码", "单位地址", "建筑名称", "楼层名称", "楼层序号", "设备编号", "部件编号", "点位名称"}

const maxImportRows = 5000

// ImportRow is one spreadsheet line.
type ImportRow struct {
	Line                                         int
	Unit, UnitCode, UnitAddress, Building, Floor string
	Level                                        int
	DeviceID, ComponentID, PointName             string
}

type RowError struct {
	Line    int    `json:"line"`
	Message string `json:"message"`
}

type ImportResult struct {
	Rows          int        `json:"rows"`
	Units         int        `json:"units"`
	Buildings     int        `json:"buildings"`
	Floors        int        `json:"floors"`
	PointsCreated int        `json:"pointsCreated"`
	PointsUpdated int        `json:"pointsUpdated"`
	Errors        []RowError `json:"errors,omitempty"`
}

// ParseImport reads a CSV (UTF-8, or GB18030 as saved by Chinese Excel) or
// an XLSX workbook's first sheet.
func ParseImport(filename string, data []byte) ([]ImportRow, error) {
	var table [][]string
	var err error
	if bytes.HasPrefix(data, []byte("PK\x03\x04")) {
		table, err = readXLSX(data)
	} else {
		if !utf8.Valid(data) {
			if data, err = simplifiedchinese.GB18030.NewDecoder().Bytes(data); err != nil {
				return nil, invalid("文件编码无法识别，请另存为 UTF-8 CSV 或 XLSX")
			}
		}
		data = bytes.TrimPrefix(data, []byte("\ufeff"))
		reader := csv.NewReader(bytes.NewReader(data))
		reader.FieldsPerRecord = -1
		table, err = reader.ReadAll()
	}
	if err != nil {
		return nil, invalid("读取 %s 失败：%v", path.Base(filename), err)
	}
	if len(table) == 0 {
		return nil, invalid("文件没有内容")
	}
	columns := map[string]int{}
	for i, name := range table[0] {
		columns[strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(name), "*"))] = i
	}
	if _, ok := columns["单位名称"]; !ok {
		return nil, invalid("首行须为表头，至少包含“单位名称”列")
	}
	cell := func(row []string, name string) string {
		if i, ok := columns[name]; ok && i < len(row) {
			return strings.TrimSpace(row[i])
		}
		return ""
	}
	rows := []ImportRow{}
	for i, values := range table[1:] {
		if strings.TrimSpace(strings.Join(values, "")) == "" {
			continue
		}
		if len(rows) == maxImportRows {
			return nil, invalid("单次最多导入 %d 行", maxImportRows)
		}
		row := ImportRow{Line: i + 2, Unit: cell(values, "单位名称"), UnitCode: cell(values, "单位编码"), UnitAddress: cell(values, "单位地址"),
			Building: cell(values, "建筑名称"), Floor: cell(values, "楼层名称"), DeviceID: cell(values, "设备编号"), ComponentID: cell(values, "部件编号"), PointName: cell(values, "点位名称")}
		if level := cell(values, "楼层序号"); level != "" {
			n, err := strconv.ParseFloat(level, 64)
			if err != nil || n != float64(int(n)) {
				return nil, invalid("第 %d 行楼层序号须为整数", row.Line)
			}
			row.Level = int(n)
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return nil, invalid("文件没有数据行")
	}
	return rows, nil
}

// Import creates missing units, buildings and floors by name and creates or
// moves points. deviceAllowed rejects devices the importer may not place.
// Nothing is saved when any row is invalid.
func (s *Service) Import(ctx context.Context, tenant, actor string, rows []ImportRow, deviceAllowed func(string) bool) (ImportResult, error) {
	result := ImportResult{Rows: len(rows)}
	_, err := s.apply(ctx, tenant, func(state *model.SiteState, now int64) (any, error) {
		result = ImportResult{Rows: len(rows)}
		record := func() model.SiteRecord {
			var r model.SiteRecord
			_ = stamp(&r, nil, actor, now)
			return r
		}
		fail := func(line int, format string, args ...any) {
			result.Errors = append(result.Errors, RowError{Line: line, Message: fmt.Sprintf(format, args...)})
		}
		seen := map[string]int{}
		for _, row := range rows {
			if err := texts(func() error { return text(&row.Unit, "单位名称", true, 100) }, func() error { return text(&row.UnitCode, "单位编码", false, 64) },
				func() error { return text(&row.UnitAddress, "单位地址", false, 200) }, func() error { return text(&row.Building, "建筑名称", false, 100) },
				func() error { return text(&row.Floor, "楼层名称", false, 50) }, func() error { return text(&row.DeviceID, "设备编号", false, 128) },
				func() error { return text(&row.ComponentID, "部件编号", false, 128) }, func() error { return text(&row.PointName, "点位名称", false, 100) }); err != nil {
				fail(row.Line, "%s", strings.TrimPrefix(err.Error(), ErrValidation.Error()+": "))
				continue
			}
			if row.Floor != "" && row.Building == "" {
				fail(row.Line, "填写楼层时须填写建筑名称")
				continue
			}
			if row.Level < -20 || row.Level > 300 {
				fail(row.Line, "楼层序号为 -20 至 300")
				continue
			}
			if row.DeviceID == "" && (row.ComponentID != "" || row.PointName != "") {
				fail(row.Line, "填写部件或点位名称时须填写设备编号")
				continue
			}
			if row.DeviceID != "" {
				key := row.DeviceID + "\x00" + row.ComponentID
				if line, dup := seen[key]; dup {
					fail(row.Line, "与第 %d 行的设备部件重复", line)
					continue
				}
				seen[key] = row.Line
				if !deviceAllowed(row.DeviceID) {
					fail(row.Line, "设备 %s 不存在或无访问权限", row.DeviceID)
					continue
				}
			}
			unit := -1
			for i := range state.Units {
				if state.Units[i].Name == row.Unit {
					unit = i
				}
			}
			if unit < 0 {
				taken := ""
				for _, other := range state.Units {
					if row.UnitCode != "" && other.Code == row.UnitCode {
						taken = other.Name
					}
				}
				if taken != "" {
					fail(row.Line, "单位编码 %s 已被单位“%s”使用", row.UnitCode, taken)
					continue
				}
				state.Units = append(state.Units, model.SiteUnit{SiteRecord: record(), Name: row.Unit, Code: row.UnitCode, Address: row.UnitAddress})
				unit = len(state.Units) - 1
				result.Units++
			}
			point := model.SitePoint{UnitID: state.Units[unit].ID, DeviceID: row.DeviceID, ComponentID: row.ComponentID, Name: row.PointName}
			if row.Building != "" {
				building := -1
				for i := range state.Buildings {
					if state.Buildings[i].UnitID == point.UnitID && state.Buildings[i].Name == row.Building {
						building = i
					}
				}
				if building < 0 {
					state.Buildings = append(state.Buildings, model.SiteBuilding{SiteRecord: record(), UnitID: point.UnitID, Name: row.Building})
					building = len(state.Buildings) - 1
					result.Buildings++
				}
				point.BuildingID = state.Buildings[building].ID
			}
			if row.Floor != "" {
				floor := -1
				for i := range state.Floors {
					if state.Floors[i].BuildingID == point.BuildingID && state.Floors[i].Name == row.Floor {
						floor = i
					}
				}
				if floor < 0 {
					state.Floors = append(state.Floors, model.SiteFloor{SiteRecord: record(), BuildingID: point.BuildingID, Name: row.Floor, Level: row.Level})
					floor = len(state.Floors) - 1
					result.Floors++
				}
				point.FloorID = state.Floors[floor].ID
			}
			if row.DeviceID == "" {
				continue
			}
			existing := -1
			for i := range state.Points {
				if state.Points[i].DeviceID == row.DeviceID && state.Points[i].ComponentID == row.ComponentID {
					existing = i
				}
			}
			if existing < 0 {
				point.SiteRecord = record()
				state.Points = append(state.Points, point)
				result.PointsCreated++
				continue
			}
			old := state.Points[existing]
			if old.FloorID == point.FloorID {
				point.X, point.Y = old.X, old.Y
			}
			if point.Name == "" {
				point.Name = old.Name
			}
			point.SiteRecord = old.SiteRecord
			point.Version++
			point.UpdatedAt, point.UpdatedBy = now, actor
			state.Points[existing] = point
			result.PointsUpdated++
		}
		if len(result.Errors) > 0 {
			return nil, invalid("导入内容有 %d 处错误，未保存任何数据", len(result.Errors))
		}
		return nil, nil
	})
	return result, err
}

// readXLSX returns the cell text of the workbook's first worksheet.
func readXLSX(data []byte) ([][]string, error) {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	files := map[string]*zip.File{}
	for _, f := range archive.File {
		files[f.Name] = f
	}
	read := func(name string, out any) error {
		f := files[name]
		if f == nil {
			return fmt.Errorf("缺少 %s", name)
		}
		if f.UncompressedSize64 > 64<<20 {
			return fmt.Errorf("%s 过大", name)
		}
		r, err := f.Open()
		if err != nil {
			return err
		}
		defer r.Close()
		return xml.NewDecoder(io.LimitReader(r, 64<<20)).Decode(out)
	}
	var workbook struct {
		Sheets []struct {
			RID string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
		} `xml:"sheets>sheet"`
	}
	var rels struct {
		Items []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
		} `xml:"Relationship"`
	}
	if err = read("xl/workbook.xml", &workbook); err != nil {
		return nil, err
	}
	if err = read("xl/_rels/workbook.xml.rels", &rels); err != nil {
		return nil, err
	}
	if len(workbook.Sheets) == 0 {
		return nil, fmt.Errorf("工作簿没有工作表")
	}
	sheet := ""
	for _, rel := range rels.Items {
		if rel.ID == workbook.Sheets[0].RID {
			sheet = strings.TrimPrefix(rel.Target, "/")
			if !strings.HasPrefix(sheet, "xl/") {
				sheet = path.Join("xl", sheet)
			}
		}
	}
	var shared struct {
		Items []struct {
			Text string `xml:"t"`
			Runs []struct {
				Text string `xml:"t"`
			} `xml:"r"`
		} `xml:"si"`
	}
	if files["xl/sharedStrings.xml"] != nil {
		if err = read("xl/sharedStrings.xml", &shared); err != nil {
			return nil, err
		}
	}
	strings_ := make([]string, len(shared.Items))
	for i, item := range shared.Items {
		strings_[i] = item.Text
		for _, run := range item.Runs {
			strings_[i] += run.Text
		}
	}
	var worksheet struct {
		Rows []struct {
			Cells []struct {
				Ref    string `xml:"r,attr"`
				Type   string `xml:"t,attr"`
				Value  string `xml:"v"`
				Inline string `xml:"is>t"`
			} `xml:"c"`
		} `xml:"sheetData>row"`
	}
	if err = read(sheet, &worksheet); err != nil {
		return nil, err
	}
	table := make([][]string, 0, len(worksheet.Rows))
	for _, row := range worksheet.Rows {
		values := []string{}
		for position, c := range row.Cells {
			column := position
			if letters := strings.TrimRight(c.Ref, "0123456789"); letters != "" {
				column = 0
				for _, ch := range letters {
					column = column*26 + int(ch-'A'+1)
				}
				column--
			}
			if column < 0 || column > 256 {
				continue
			}
			for len(values) <= column {
				values = append(values, "")
			}
			switch c.Type {
			case "s":
				if i, err := strconv.Atoi(c.Value); err == nil && i >= 0 && i < len(strings_) {
					values[column] = strings_[i]
				}
			case "inlineStr":
				values[column] = c.Inline
			default:
				values[column] = c.Value
			}
		}
		table = append(table, values)
	}
	return table, nil
}
