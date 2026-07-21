package excel

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// IsXLSXFile checks if a file exists and starts with the PK zip header or is a valid zip containing xl/worksheets.
func IsXLSXFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	header := make([]byte, 4)
	if _, err := f.Read(header); err != nil {
		return false
	}
	// PK zip magic header is PK\x03\x04
	if !bytes.Equal(header, []byte{'P', 'K', 0x03, 0x04}) {
		return false
	}

	zr, err := zip.OpenReader(path)
	if err != nil {
		return false
	}
	defer zr.Close()

	for _, file := range zr.File {
		if strings.HasPrefix(file.Name, "xl/") {
			return true
		}
	}
	return false
}

type sheetRow map[string]string

// ParseXLSXRows reads all rows from the primary worksheet in an xlsx file.
func ParseXLSXRows(path string) ([]sheetRow, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("opening xlsx zip reader: %w", err)
	}
	defer zr.Close()

	// 1. Read shared strings
	var sharedStrings []string
	for _, f := range zr.File {
		if f.Name == "xl/sharedStrings.xml" {
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("opening sharedStrings.xml: %w", err)
			}
			sharedStrings, _ = parseSharedStrings(rc)
			rc.Close()
			break
		}
	}

	// 2. Find first worksheet
	var sheetFile *zip.File
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "xl/worksheets/sheet") && strings.HasSuffix(f.Name, ".xml") {
			sheetFile = f
			break
		}
	}
	if sheetFile == nil {
		return nil, fmt.Errorf("no worksheet found in xlsx file")
	}

	rc, err := sheetFile.Open()
	if err != nil {
		return nil, fmt.Errorf("opening worksheet: %w", err)
	}
	defer rc.Close()

	return parseWorksheetXML(rc, sharedStrings)
}

func parseSharedStrings(r io.Reader) ([]string, error) {
	decoder := xml.NewDecoder(r)
	var stringsList []string
	var currentText strings.Builder
	inT := false

	for {
		t, err := decoder.Token()
		if err != nil {
			break
		}
		switch se := t.(type) {
		case xml.StartElement:
			if se.Name.Local == "t" {
				inT = true
				currentText.Reset()
			} else if se.Name.Local == "si" {
				currentText.Reset()
			}
		case xml.CharData:
			if inT {
				currentText.Write(se)
			}
		case xml.EndElement:
			if se.Name.Local == "t" {
				inT = false
			} else if se.Name.Local == "si" {
				stringsList = append(stringsList, currentText.String())
			}
		}
	}
	return stringsList, nil
}

func parseWorksheetXML(r io.Reader, sharedStrings []string) ([]sheetRow, error) {
	decoder := xml.NewDecoder(r)
	var rows []sheetRow
	var currentRow sheetRow
	var currentCellCol string
	var cellType string
	var valBuffer strings.Builder
	inV := false

	for {
		t, err := decoder.Token()
		if err != nil {
			break
		}
		switch se := t.(type) {
		case xml.StartElement:
			if se.Name.Local == "row" {
				currentRow = make(sheetRow)
			} else if se.Name.Local == "c" {
				ref := ""
				cellType = ""
				for _, attr := range se.Attr {
					if attr.Name.Local == "r" {
						ref = attr.Value
					} else if attr.Name.Local == "t" {
						cellType = attr.Value
					}
				}
				currentCellCol = extractColLetter(ref)
			} else if se.Name.Local == "v" {
				inV = true
				valBuffer.Reset()
			}
		case xml.CharData:
			if inV {
				valBuffer.Write(se)
			}
		case xml.EndElement:
			if se.Name.Local == "v" {
				inV = false
				val := valBuffer.String()
				if cellType == "s" {
					idx, err := strconv.Atoi(val)
					if err == nil && idx >= 0 && idx < len(sharedStrings) {
						val = sharedStrings[idx]
					}
				}
				if currentCellCol != "" && currentRow != nil {
					currentRow[currentCellCol] = val
				}
			} else if se.Name.Local == "row" {
				if len(currentRow) > 0 {
					rows = append(rows, currentRow)
				}
			}
		}
	}
	return rows, nil
}

func extractColLetter(ref string) string {
	var col strings.Builder
	for _, ch := range ref {
		if ch >= 'A' && ch <= 'Z' {
			col.WriteRune(ch)
		} else if ch >= 'a' && ch <= 'z' {
			col.WriteRune(ch - 'a' + 'A')
		} else {
			break
		}
	}
	return col.String()
}

func findTicker(r sheetRow, nextR sheetRow) string {
	reserved := map[string]bool{
		"IDENTIFIER": true, "SYMBOL": true, "TICKER": true, "CUSIP": true,
		"QTUM": true, "CASH": true, "USD": true, "EUR": true, "TWD": true,
		"FGXXX": true, "NAME": true, "SHARES": true, "MARKET": true, "VALUE": true,
		"CASH & OTHER": true,
	}

	isValidTicker := func(s string) bool {
		s = strings.TrimSpace(s)
		if s == "" || len(s) > 5 || reserved[strings.ToUpper(s)] {
			return false
		}
		for _, ch := range s {
			if ch < 'A' || ch > 'Z' {
				return false
			}
		}
		return true
	}

	// 1. Column D (Identifier)
	if dVal := strings.TrimSpace(r["D"]); isValidTicker(dVal) {
		return dVal
	}
	// 2. Column A
	if aVal := strings.TrimSpace(r["A"]); isValidTicker(aVal) {
		return aVal
	}
	// 3. Column A of next row (for shifted rows in ETF spreadsheets)
	if nextR != nil {
		if nextA := strings.TrimSpace(nextR["A"]); isValidTicker(nextA) {
			return nextA
		}
	}
	// 4. Any cell in current row
	for _, val := range r {
		if isValidTicker(val) {
			return val
		}
	}
	return ""
}

// ConvertXLSXToCSV converts an Excel file to a clean portfolio CSV (ticker,weight).
func ConvertXLSXToCSV(inputPath, outputPath string) (int, error) {
	rows, err := ParseXLSXRows(inputPath)
	if err != nil {
		return 0, err
	}

	type constituent struct {
		ticker string
		name   string
		mv     float64
		weight float64
	}

	var holdings []constituent
	var totalMV float64
	seenTickers := make(map[string]bool)

	for i := 0; i < len(rows); i++ {
		r := rows[i]
		var nextR sheetRow
		if i+1 < len(rows) {
			nextR = rows[i+1]
		}

		ticker := findTicker(r, nextR)
		if ticker == "" {
			continue
		}

		name := strings.TrimSpace(r["C"])
		if name == "" {
			name = strings.TrimSpace(r["B"])
		}

		mvStr := strings.TrimSpace(r["F"])
		weightStr := strings.TrimSpace(r["B"])

		mv, _ := strconv.ParseFloat(mvStr, 64)
		w := 0.0
		if strings.Contains(weightStr, "%") {
			wClean := strings.ReplaceAll(weightStr, "%", "")
			if parsedW, err := strconv.ParseFloat(strings.TrimSpace(wClean), 64); err == nil {
				w = parsedW / 100.0
			}
		}

		if seenTickers[ticker] {
			continue
		}
		seenTickers[ticker] = true

		if mv > 0 {
			totalMV += mv
		}

		holdings = append(holdings, constituent{
			ticker: ticker,
			name:   name,
			mv:     mv,
			weight: w,
		})
	}

	if len(holdings) == 0 {
		return 0, fmt.Errorf("no valid stock constituents found in Excel file")
	}

	if outputPath == "" {
		ext := filepath.Ext(inputPath)
		outputPath = strings.TrimSuffix(inputPath, ext) + ".csv"
	}

	outFile, err := os.Create(outputPath)
	if err != nil {
		return 0, fmt.Errorf("creating output CSV: %w", err)
	}
	defer outFile.Close()

	w := csv.NewWriter(outFile)
	defer w.Flush()

	if err := w.Write([]string{"ticker", "weight"}); err != nil {
		return 0, err
	}

	writtenCount := 0
	for _, h := range holdings {
		finalWeight := h.weight
		if finalWeight == 0 && totalMV > 0 && h.mv > 0 {
			finalWeight = h.mv / totalMV
		}

		formattedTicker := h.ticker
		if !strings.HasPrefix(formattedTicker, "US:") && !strings.HasPrefix(formattedTicker, "NSE:") && !strings.HasPrefix(formattedTicker, "BSE:") {
			formattedTicker = "US:" + formattedTicker
		}

		weightFormatted := fmt.Sprintf("%.6f", finalWeight)
		if err := w.Write([]string{formattedTicker, weightFormatted}); err != nil {
			return 0, err
		}
		writtenCount++
	}

	return writtenCount, nil
}

func isNumeric(s string) bool {
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}
