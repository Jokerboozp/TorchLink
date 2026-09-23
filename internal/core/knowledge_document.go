package core /* 声明 core 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"archive/zip"   /* 执行当前语句并推进处理流程。 */
	"bytes"         /* 执行当前语句并推进处理流程。 */
	"encoding/xml"  /* 执行当前语句并推进处理流程。 */
	"errors"        /* 执行当前语句并推进处理流程。 */
	"fmt"           /* 执行当前语句并推进处理流程。 */
	"html"          /* 执行当前语句并推进处理流程。 */
	"io"            /* 执行当前语句并推进处理流程。 */
	"path/filepath" /* 执行当前语句并推进处理流程。 */
	"regexp"        /* 执行当前语句并推进处理流程。 */
	"sort"          /* 执行当前语句并推进处理流程。 */
	"strconv"       /* 执行当前语句并推进处理流程。 */
	"strings"       /* 执行当前语句并推进处理流程。 */
	"unicode"       /* 执行当前语句并推进处理流程。 */
	"unicode/utf8"  /* 执行当前语句并推进处理流程。 */

	"github.com/ledongthuc/pdf" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

var markupTag = regexp.MustCompile(`<[^>]+>`) /* 声明 markupTag。 */

func ExtractKnowledgeText(filename string, data []byte) (string, error) { /* 定义 ExtractKnowledgeText 函数。 */
	ext := strings.ToLower(filepath.Ext(filename)) /* 更新 ext 的值。 */
	switch ext {                                   /* 根据条件选择处理路径。 */
	case ".pdf": /* 处理当前分支。 */
		r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data))) /* 更新 err 的值。 */
		if err != nil {                                                  /* 判断条件并选择处理分支。 */
			return "", fmt.Errorf("parse PDF: %w", err) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		plain, err := r.GetPlainText() /* 更新 err 的值。 */
		if err != nil {                /* 判断条件并选择处理分支。 */
			return "", fmt.Errorf("extract PDF text: %w", err) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		b, err := io.ReadAll(io.LimitReader(plain, 64<<20)) /* 更新 err 的值。 */
		if err != nil {                                     /* 判断条件并选择处理分支。 */
			return "", err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return cleanText(string(b)), nil /* 返回当前处理结果。 */
	case ".xlsx": /* 处理当前分支。 */
		return extractSpreadsheetXML(data) /* 返回当前处理结果。 */
	case ".docx", ".pptx", ".odt", ".odp", ".ods": /* 处理当前分支。 */
		return extractOfficeXML(data) /* 返回当前处理结果。 */
	case ".html", ".htm", ".xml": /* 处理当前分支。 */
		return cleanText(string(data)), nil /* 返回当前处理结果。 */
	default: /* 处理当前分支。 */
		if !utf8.Valid(data) { /* 判断条件并选择处理分支。 */
			return "", fmt.Errorf("unsupported binary document %s", ext) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return cleanText(string(data)), nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

// spreadsheetRows reads the displayed cell values from an OOXML workbook.
// Excel commonly stores text in xl/sharedStrings.xml and leaves only an index
// in the worksheet; treating the index as text loses the point-table meaning.
// The returned rows preserve empty cells so column positions remain stable.
func spreadsheetRows(data []byte) ([][]string, error) { /* 定义 spreadsheetRows 函数。 */
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data))) /* 更新 err 的值。 */
	if err != nil {                                                   /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("open spreadsheet: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	shared := []string{}        /* 更新 shared 的值。 */
	for _, f := range zr.File { /* 循环处理当前数据。 */
		if strings.EqualFold(f.Name, "xl/sharedStrings.xml") { /* 判断条件并选择处理分支。 */
			content, readErr := readZipEntry(f, 32<<20) /* 更新 readErr 的值。 */
			if readErr != nil {                         /* 判断条件并选择处理分支。 */
				return nil, readErr /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			var document spreadsheetSharedStrings                                       /* 声明 document。 */
			if unmarshalErr := xml.Unmarshal(content, &document); unmarshalErr != nil { /* 判断条件并选择处理分支。 */
				return nil, fmt.Errorf("parse spreadsheet shared strings: %w", unmarshalErr) /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			for _, item := range document.Items { /* 循环处理当前数据。 */
				shared = append(shared, item.Text()) /* 更新 shared 的值。 */
			} /* 结束当前表达式或代码块。 */
			break /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	var files []*zip.File       /* 声明 files。 */
	for _, f := range zr.File { /* 循环处理当前数据。 */
		name := strings.ToLower(f.Name)                                                   /* 更新 name 的值。 */
		if strings.HasPrefix(name, "xl/worksheets/") && strings.HasSuffix(name, ".xml") { /* 判断条件并选择处理分支。 */
			files = append(files, f) /* 更新 files 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name }) /* 执行当前语句并推进处理流程。 */
	var rows [][]string                                                             /* 声明 rows。 */
	for _, f := range files {                                                       /* 循环处理当前数据。 */
		content, readErr := readZipEntry(f, 32<<20) /* 更新 readErr 的值。 */
		if readErr != nil {                         /* 判断条件并选择处理分支。 */
			return nil, readErr /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		var sheet spreadsheetWorksheet                                           /* 声明 sheet。 */
		if unmarshalErr := xml.Unmarshal(content, &sheet); unmarshalErr != nil { /* 判断条件并选择处理分支。 */
			return nil, fmt.Errorf("parse spreadsheet worksheet %s: %w", f.Name, unmarshalErr) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		for _, row := range sheet.Rows { /* 循环处理当前数据。 */
			values := make([]string, 0, len(row.Cells)) /* 更新 values 的值。 */
			positions := make([]int, 0, len(row.Cells)) /* 更新 positions 的值。 */
			maxColumn := -1                             /* 更新 maxColumn 的值。 */
			for index, cell := range row.Cells {        /* 循环处理当前数据。 */
				column := spreadsheetColumnIndex(cell.Ref) /* 更新 column 的值。 */
				if column < 0 {                            /* 判断条件并选择处理分支。 */
					column = index /* 更新 column 的值。 */
				} /* 结束当前表达式或代码块。 */
				positions = append(positions, column) /* 更新 positions 的值。 */
				if column > maxColumn {               /* 判断条件并选择处理分支。 */
					maxColumn = column /* 更新 maxColumn 的值。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
			if maxColumn < 0 { /* 判断条件并选择处理分支。 */
				continue /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
			values = make([]string, maxColumn+1) /* 更新 values 的值。 */
			for index, cell := range row.Cells { /* 循环处理当前数据。 */
				value, valueErr := spreadsheetCellText(cell, shared) /* 更新 valueErr 的值。 */
				if valueErr != nil {                                 /* 判断条件并选择处理分支。 */
					return nil, valueErr /* 返回当前处理结果。 */
				} /* 结束当前表达式或代码块。 */
				values[positions[index]] = value /* 更新 values[positions[index]] 的值。 */
			} /* 结束当前表达式或代码块。 */
			rows = append(rows, values) /* 更新 rows 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if len(rows) == 0 { /* 判断条件并选择处理分支。 */
		return nil, errors.New("spreadsheet contains no worksheet rows") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return rows, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func extractSpreadsheetXML(data []byte) (string, error) { /* 定义 extractSpreadsheetXML 函数。 */
	rows, err := spreadsheetRows(data) /* 更新 err 的值。 */
	if err != nil {                    /* 判断条件并选择处理分支。 */
		return "", err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var lines []string         /* 声明 lines。 */
	for _, row := range rows { /* 循环处理当前数据。 */
		values := make([]string, len(row)) /* 更新 values 的值。 */
		hasValue := false                  /* 更新 hasValue 的值。 */
		for index, value := range row {    /* 循环处理当前数据。 */
			values[index] = strings.TrimSpace(value) /* 更新 values[index] 的值。 */
			if values[index] != "" {                 /* 判断条件并选择处理分支。 */
				hasValue = true /* 更新 hasValue 的值。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		if hasValue { /* 判断条件并选择处理分支。 */
			lines = append(lines, strings.TrimSpace(strings.Join(values, "\t"))) /* 更新 lines 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	text := strings.TrimSpace(strings.Join(lines, "\n")) /* 更新 text 的值。 */
	if text == "" {                                      /* 判断条件并选择处理分支。 */
		return "", errors.New("document contains no extractable text") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return cleanText(text), nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

type spreadsheetSharedStrings struct { /* 定义 spreadsheetSharedStrings 类型。 */
	Items []spreadsheetStringItem `xml:"si"` /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type spreadsheetStringItem struct { /* 定义 spreadsheetStringItem 类型。 */
	Plain string                 `xml:"t"` /* 执行当前语句并推进处理流程。 */
	Runs  []spreadsheetStringRun `xml:"r"` /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type spreadsheetStringRun struct { /* 定义 spreadsheetStringRun 类型。 */
	Text string `xml:"t"` /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (item spreadsheetStringItem) Text() string { /* 定义 Text 函数。 */
	if item.Plain != "" { /* 判断条件并选择处理分支。 */
		return item.Plain /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var b strings.Builder           /* 声明 b。 */
	for _, run := range item.Runs { /* 循环处理当前数据。 */
		b.WriteString(run.Text) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	return b.String() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

type spreadsheetWorksheet struct { /* 定义 spreadsheetWorksheet 类型。 */
	Rows []spreadsheetRow `xml:"sheetData>row"` /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type spreadsheetRow struct { /* 定义 spreadsheetRow 类型。 */
	Cells []spreadsheetCell `xml:"c"` /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type spreadsheetCell struct { /* 定义 spreadsheetCell 类型。 */
	Ref    string                  `xml:"r,attr"` /* 执行当前语句并推进处理流程。 */
	Type   string                  `xml:"t,attr"` /* 执行当前语句并推进处理流程。 */
	Value  string                  `xml:"v"`      /* 执行当前语句并推进处理流程。 */
	Inline spreadsheetInlineString `xml:"is"`     /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type spreadsheetInlineString struct { /* 定义 spreadsheetInlineString 类型。 */
	Plain string                 `xml:"t"` /* 执行当前语句并推进处理流程。 */
	Runs  []spreadsheetStringRun `xml:"r"` /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (value spreadsheetInlineString) Text() string { /* 定义 Text 函数。 */
	if value.Plain != "" { /* 判断条件并选择处理分支。 */
		return value.Plain /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var b strings.Builder            /* 声明 b。 */
	for _, run := range value.Runs { /* 循环处理当前数据。 */
		b.WriteString(run.Text) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	return b.String() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func spreadsheetCellText(cell spreadsheetCell, shared []string) (string, error) { /* 定义 spreadsheetCellText 函数。 */
	if strings.EqualFold(cell.Type, "s") { /* 判断条件并选择处理分支。 */
		index, err := strconv.Atoi(strings.TrimSpace(cell.Value)) /* 更新 err 的值。 */
		if err != nil || index < 0 || index >= len(shared) {      /* 判断条件并选择处理分支。 */
			return "", fmt.Errorf("spreadsheet shared-string index %q is invalid", cell.Value) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return shared[index], nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if strings.EqualFold(cell.Type, "inlineStr") { /* 判断条件并选择处理分支。 */
		return cell.Inline.Text(), nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return cell.Value, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func spreadsheetColumnIndex(ref string) int { /* 定义 spreadsheetColumnIndex 函数。 */
	ref = strings.TrimSpace(ref)    /* 更新 ref 的值。 */
	index := 0                      /* 更新 index 的值。 */
	found := false                  /* 更新 found 的值。 */
	for _, character := range ref { /* 循环处理当前数据。 */
		if character < 'A' || character > 'Z' { /* 判断条件并选择处理分支。 */
			break /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		found = true                            /* 更新 found 的值。 */
		index = index*26 + int(character-'A'+1) /* 更新 index 的值。 */
	} /* 结束当前表达式或代码块。 */
	if !found { /* 判断条件并选择处理分支。 */
		return -1 /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return index - 1 /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func readZipEntry(file *zip.File, maximum int64) ([]byte, error) { /* 定义 readZipEntry 函数。 */
	reader, err := file.Open() /* 更新 err 的值。 */
	if err != nil {            /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer reader.Close()                                       /* 安排函数结束时执行清理。 */
	data, err := io.ReadAll(io.LimitReader(reader, maximum+1)) /* 更新 err 的值。 */
	if err != nil {                                            /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if int64(len(data)) > maximum { /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("spreadsheet entry %s exceeds %d bytes", file.Name, maximum) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return data, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func extractOfficeXML(data []byte) (string, error) { /* 定义 extractOfficeXML 函数。 */
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data))) /* 更新 err 的值。 */
	if err != nil {                                                   /* 判断条件并选择处理分支。 */
		return "", fmt.Errorf("open office document: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	files := append([]*zip.File(nil), zr.File...)                                   /* 更新 files 的值。 */
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name }) /* 执行当前语句并推进处理流程。 */
	var out strings.Builder                                                         /* 声明 out。 */
	for _, f := range files {                                                       /* 循环处理当前数据。 */
		name := strings.ToLower(f.Name)                                                                                                                                                                                                                              /* 更新 name 的值。 */
		if !strings.HasSuffix(name, ".xml") || !(strings.HasPrefix(name, "word/") || strings.HasPrefix(name, "ppt/slides/") || strings.HasPrefix(name, "xl/sharedstrings") || strings.HasPrefix(name, "xl/worksheets/") || strings.HasPrefix(name, "content.xml")) { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		r, openErr := f.Open() /* 更新 openErr 的值。 */
		if openErr != nil {    /* 判断条件并选择处理分支。 */
			return "", openErr /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		b, readErr := io.ReadAll(io.LimitReader(r, 32<<20)) /* 更新 readErr 的值。 */
		_ = r.Close()                                       /* 更新 _ 的值。 */
		if readErr != nil {                                 /* 判断条件并选择处理分支。 */
			return "", readErr /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		content := string(b)                /* 更新 content 的值。 */
		if strings.HasPrefix(name, "xl/") { /* 判断条件并选择处理分支。 */
			// Keep spreadsheet cells distinguishable after XML tags are removed;
			// this gives the protocol assistant row/column boundaries to reason
			// about instead of one concatenated string.
			content = strings.NewReplacer("</c>", "\n", "</v>", "\t", "</t>", "\t").Replace(content) /* 更新 content 的值。 */
		} /* 结束当前表达式或代码块。 */
		out.WriteString(cleanText(content)) /* 执行当前语句并推进处理流程。 */
		out.WriteByte('\n')                 /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	text := strings.TrimSpace(out.String()) /* 更新 text 的值。 */
	if text == "" {                         /* 判断条件并选择处理分支。 */
		return "", fmt.Errorf("document contains no extractable text") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return text, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func cleanText(s string) string { /* 定义 cleanText 函数。 */
	s = strings.NewReplacer("</p>", "\n", "</w:p>", "\n", "</a:p>", "\n", "<br>", "\n", "<br/>", "\n").Replace(s) /* 更新 s 的值。 */
	s = html.UnescapeString(markupTag.ReplaceAllString(s, " "))                                                   /* 更新 s 的值。 */
	lines := strings.Split(s, "\n")                                                                               /* 更新 lines 的值。 */
	for i, line := range lines {                                                                                  /* 循环处理当前数据。 */
		lines[i] = strings.Join(strings.Fields(line), " ") /* 更新 lines[i] 的值。 */
	} /* 结束当前表达式或代码块。 */
	return strings.TrimSpace(strings.Join(lines, "\n")) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

type KnowledgeTextChunk struct { /* 定义 KnowledgeTextChunk 类型。 */
	Index          int    /* 执行当前语句并推进处理流程。 */
	StartChar      int    /* 执行当前语句并推进处理流程。 */
	EndChar        int    /* 执行当前语句并推进处理流程。 */
	CharacterCount int    /* 执行当前语句并推进处理流程。 */
	OverlapChars   int    /* 执行当前语句并推进处理流程。 */
	Text           string /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

// ChunkKnowledgeTextDetailed splits normalized extracted text into fixed
// Unicode-code-point windows. The end offset is exclusive; adjacent windows
// overlap by the requested number of code points.
func ChunkKnowledgeTextDetailed(text string, size, overlap int) []KnowledgeTextChunk { /* 定义 ChunkKnowledgeTextDetailed 函数。 */
	if size <= 0 { /* 判断条件并选择处理分支。 */
		size = 1200 /* 更新 size 的值。 */
	} /* 结束当前表达式或代码块。 */
	if overlap < 0 || overlap >= size { /* 判断条件并选择处理分支。 */
		overlap = 200 /* 更新 overlap 的值。 */
	} /* 结束当前表达式或代码块。 */
	r := []rune(strings.TrimSpace(text)) /* 更新 r 的值。 */
	if len(r) == 0 {                     /* 判断条件并选择处理分支。 */
		return nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	out := []KnowledgeTextChunk{}     /* 更新 out 的值。 */
	previousRawEnd := 0               /* 更新 previousRawEnd 的值。 */
	for start := 0; start < len(r); { /* 循环处理当前数据。 */
		rawEnd := start + size /* 更新 rawEnd 的值。 */
		if rawEnd > len(r) {   /* 判断条件并选择处理分支。 */
			rawEnd = len(r) /* 更新 rawEnd 的值。 */
		} /* 结束当前表达式或代码块。 */
		contentStart, contentEnd := start, rawEnd                           /* 更新 contentEnd 的值。 */
		for contentStart < contentEnd && unicode.IsSpace(r[contentStart]) { /* 循环处理当前数据。 */
			contentStart++ /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		for contentEnd > contentStart && unicode.IsSpace(r[contentEnd-1]) { /* 循环处理当前数据。 */
			contentEnd-- /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if contentStart < contentEnd { /* 判断条件并选择处理分支。 */
			overlapChars := 0                  /* 更新 overlapChars 的值。 */
			if previousRawEnd > contentStart { /* 判断条件并选择处理分支。 */
				overlapChars = previousRawEnd - contentStart /* 更新 overlapChars 的值。 */
			} /* 结束当前表达式或代码块。 */
			out = append(out, KnowledgeTextChunk{ /* 更新 out 的值。 */
				Index:          len(out) + 1,                       /* 执行当前语句并推进处理流程。 */
				StartChar:      contentStart,                       /* 执行当前语句并推进处理流程。 */
				EndChar:        contentEnd,                         /* 执行当前语句并推进处理流程。 */
				CharacterCount: contentEnd - contentStart,          /* 执行当前语句并推进处理流程。 */
				OverlapChars:   overlapChars,                       /* 执行当前语句并推进处理流程。 */
				Text:           string(r[contentStart:contentEnd]), /* 执行当前语句并推进处理流程。 */
			}) /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		if rawEnd == len(r) { /* 判断条件并选择处理分支。 */
			break /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		previousRawEnd = rawEnd  /* 更新 previousRawEnd 的值。 */
		start = rawEnd - overlap /* 更新 start 的值。 */
	} /* 结束当前表达式或代码块。 */
	return out /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func ChunkKnowledgeText(text string, size, overlap int) []string { /* 定义 ChunkKnowledgeText 函数。 */
	detailed := ChunkKnowledgeTextDetailed(text, size, overlap) /* 更新 detailed 的值。 */
	out := make([]string, 0, len(detailed))                     /* 更新 out 的值。 */
	for _, chunk := range detailed {                            /* 循环处理当前数据。 */
		out = append(out, chunk.Text) /* 更新 out 的值。 */
	} /* 结束当前表达式或代码块。 */
	return out /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
