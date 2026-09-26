package core

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ledongthuc/pdf"
)

var markupTag = regexp.MustCompile(`<[^>]+>`)

func ExtractKnowledgeText(filename string, data []byte) (string, error) {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".pdf":
		r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return "", fmt.Errorf("parse PDF: %w", err)
		}
		plain, err := r.GetPlainText()
		if err != nil {
			return "", fmt.Errorf("extract PDF text: %w", err)
		}
		b, err := io.ReadAll(io.LimitReader(plain, 64<<20))
		if err != nil {
			return "", err
		}
		return cleanText(string(b)), nil
	case ".xlsx":
		return extractSpreadsheetXML(data)
	case ".docx", ".pptx", ".odt", ".odp", ".ods":
		return extractOfficeXML(data)
	case ".html", ".htm", ".xml":
		return cleanText(string(data)), nil
	default:
		if !utf8.Valid(data) {
			return "", fmt.Errorf("unsupported binary document %s", ext)
		}
		return cleanText(string(data)), nil
	}
}

// spreadsheetRows reads the displayed cell values from an OOXML workbook.
// Excel commonly stores text in xl/sharedStrings.xml and leaves only an index
// in the worksheet; treating the index as text loses the point-table meaning.
// The returned rows preserve empty cells so column positions remain stable.
func spreadsheetRows(data []byte) ([][]string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open spreadsheet: %w", err)
	}
	shared := []string{}
	for _, f := range zr.File {
		if strings.EqualFold(f.Name, "xl/sharedStrings.xml") {
			content, readErr := readZipEntry(f, 32<<20)
			if readErr != nil {
				return nil, readErr
			}
			var document spreadsheetSharedStrings
			if unmarshalErr := xml.Unmarshal(content, &document); unmarshalErr != nil {
				return nil, fmt.Errorf("parse spreadsheet shared strings: %w", unmarshalErr)
			}
			for _, item := range document.Items {
				shared = append(shared, item.Text())
			}
			break
		}
	}
	var files []*zip.File
	for _, f := range zr.File {
		name := strings.ToLower(f.Name)
		if strings.HasPrefix(name, "xl/worksheets/") && strings.HasSuffix(name, ".xml") {
			files = append(files, f)
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	var rows [][]string
	for _, f := range files {
		content, readErr := readZipEntry(f, 32<<20)
		if readErr != nil {
			return nil, readErr
		}
		var sheet spreadsheetWorksheet
		if unmarshalErr := xml.Unmarshal(content, &sheet); unmarshalErr != nil {
			return nil, fmt.Errorf("parse spreadsheet worksheet %s: %w", f.Name, unmarshalErr)
		}
		for _, row := range sheet.Rows {
			values := make([]string, 0, len(row.Cells))
			positions := make([]int, 0, len(row.Cells))
			maxColumn := -1
			for index, cell := range row.Cells {
				column := spreadsheetColumnIndex(cell.Ref)
				if column < 0 {
					column = index
				}
				positions = append(positions, column)
				if column > maxColumn {
					maxColumn = column
				}
			}
			if maxColumn < 0 {
				continue
			}
			values = make([]string, maxColumn+1)
			for index, cell := range row.Cells {
				value, valueErr := spreadsheetCellText(cell, shared)
				if valueErr != nil {
					return nil, valueErr
				}
				values[positions[index]] = value
			}
			rows = append(rows, values)
		}
	}
	if len(rows) == 0 {
		return nil, errors.New("spreadsheet contains no worksheet rows")
	}
	return rows, nil
}

func extractSpreadsheetXML(data []byte) (string, error) {
	rows, err := spreadsheetRows(data)
	if err != nil {
		return "", err
	}
	var lines []string
	for _, row := range rows {
		values := make([]string, len(row))
		hasValue := false
		for index, value := range row {
			values[index] = strings.TrimSpace(value)
			if values[index] != "" {
				hasValue = true
			}
		}
		if hasValue {
			lines = append(lines, strings.TrimSpace(strings.Join(values, "\t")))
		}
	}
	text := strings.TrimSpace(strings.Join(lines, "\n"))
	if text == "" {
		return "", errors.New("document contains no extractable text")
	}
	return cleanText(text), nil
}

type spreadsheetSharedStrings struct {
	Items []spreadsheetStringItem `xml:"si"`
}

type spreadsheetStringItem struct {
	Plain string                 `xml:"t"`
	Runs  []spreadsheetStringRun `xml:"r"`
}

type spreadsheetStringRun struct {
	Text string `xml:"t"`
}

func (item spreadsheetStringItem) Text() string {
	if item.Plain != "" {
		return item.Plain
	}
	var b strings.Builder
	for _, run := range item.Runs {
		b.WriteString(run.Text)
	}
	return b.String()
}

type spreadsheetWorksheet struct {
	Rows []spreadsheetRow `xml:"sheetData>row"`
}

type spreadsheetRow struct {
	Cells []spreadsheetCell `xml:"c"`
}

type spreadsheetCell struct {
	Ref    string                  `xml:"r,attr"`
	Type   string                  `xml:"t,attr"`
	Value  string                  `xml:"v"`
	Inline spreadsheetInlineString `xml:"is"`
}

type spreadsheetInlineString struct {
	Plain string                 `xml:"t"`
	Runs  []spreadsheetStringRun `xml:"r"`
}

func (value spreadsheetInlineString) Text() string {
	if value.Plain != "" {
		return value.Plain
	}
	var b strings.Builder
	for _, run := range value.Runs {
		b.WriteString(run.Text)
	}
	return b.String()
}

func spreadsheetCellText(cell spreadsheetCell, shared []string) (string, error) {
	if strings.EqualFold(cell.Type, "s") {
		index, err := strconv.Atoi(strings.TrimSpace(cell.Value))
		if err != nil || index < 0 || index >= len(shared) {
			return "", fmt.Errorf("spreadsheet shared-string index %q is invalid", cell.Value)
		}
		return shared[index], nil
	}
	if strings.EqualFold(cell.Type, "inlineStr") {
		return cell.Inline.Text(), nil
	}
	return cell.Value, nil
}

func spreadsheetColumnIndex(ref string) int {
	ref = strings.TrimSpace(ref)
	index := 0
	found := false
	for _, character := range ref {
		if character < 'A' || character > 'Z' {
			break
		}
		found = true
		index = index*26 + int(character-'A'+1)
	}
	if !found {
		return -1
	}
	return index - 1
}

func readZipEntry(file *zip.File, maximum int64) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maximum {
		return nil, fmt.Errorf("spreadsheet entry %s exceeds %d bytes", file.Name, maximum)
	}
	return data, nil
}

func extractOfficeXML(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("open office document: %w", err)
	}
	files := append([]*zip.File(nil), zr.File...)
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	var out strings.Builder
	for _, f := range files {
		name := strings.ToLower(f.Name)
		if !strings.HasSuffix(name, ".xml") || !(strings.HasPrefix(name, "word/") || strings.HasPrefix(name, "ppt/slides/") || strings.HasPrefix(name, "xl/sharedstrings") || strings.HasPrefix(name, "xl/worksheets/") || strings.HasPrefix(name, "content.xml")) {
			continue
		}
		r, openErr := f.Open()
		if openErr != nil {
			return "", openErr
		}
		b, readErr := io.ReadAll(io.LimitReader(r, 32<<20))
		_ = r.Close()
		if readErr != nil {
			return "", readErr
		}
		content := string(b)
		if strings.HasPrefix(name, "xl/") {
			// Keep spreadsheet cells distinguishable after XML tags are removed;
			// this gives the protocol assistant row/column boundaries to reason
			// about instead of one concatenated string.
			content = strings.NewReplacer("</c>", "\n", "</v>", "\t", "</t>", "\t").Replace(content)
		}
		out.WriteString(cleanText(content))
		out.WriteByte('\n')
	}
	text := strings.TrimSpace(out.String())
	if text == "" {
		return "", fmt.Errorf("document contains no extractable text")
	}
	return text, nil
}

func cleanText(s string) string {
	s = strings.NewReplacer("</p>", "\n", "</w:p>", "\n", "</a:p>", "\n", "<br>", "\n", "<br/>", "\n").Replace(s)
	s = html.UnescapeString(markupTag.ReplaceAllString(s, " "))
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.Join(strings.Fields(line), " ")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

type KnowledgeTextChunk struct {
	Index          int
	StartChar      int
	EndChar        int
	CharacterCount int
	OverlapChars   int
	Text           string
}

// ChunkKnowledgeTextDetailed splits normalized extracted text into fixed
// Unicode-code-point windows. The end offset is exclusive; adjacent windows
// overlap by the requested number of code points.
func ChunkKnowledgeTextDetailed(text string, size, overlap int) []KnowledgeTextChunk {
	if size <= 0 {
		size = 1200
	}
	if overlap < 0 || overlap >= size {
		overlap = 200
	}
	r := []rune(strings.TrimSpace(text))
	if len(r) == 0 {
		return nil
	}
	out := []KnowledgeTextChunk{}
	previousRawEnd := 0
	for start := 0; start < len(r); {
		rawEnd := start + size
		if rawEnd > len(r) {
			rawEnd = len(r)
		}
		contentStart, contentEnd := start, rawEnd
		for contentStart < contentEnd && unicode.IsSpace(r[contentStart]) {
			contentStart++
		}
		for contentEnd > contentStart && unicode.IsSpace(r[contentEnd-1]) {
			contentEnd--
		}
		if contentStart < contentEnd {
			overlapChars := 0
			if previousRawEnd > contentStart {
				overlapChars = previousRawEnd - contentStart
			}
			out = append(out, KnowledgeTextChunk{
				Index:          len(out) + 1,
				StartChar:      contentStart,
				EndChar:        contentEnd,
				CharacterCount: contentEnd - contentStart,
				OverlapChars:   overlapChars,
				Text:           string(r[contentStart:contentEnd]),
			})
		}
		if rawEnd == len(r) {
			break
		}
		previousRawEnd = rawEnd
		start = rawEnd - overlap
	}
	return out
}

func ChunkKnowledgeText(text string, size, overlap int) []string {
	detailed := ChunkKnowledgeTextDetailed(text, size, overlap)
	out := make([]string, 0, len(detailed))
	for _, chunk := range detailed {
		out = append(out, chunk.Text)
	}
	return out
}
