package excel

import (
	"archive/zip"
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsXLSXFile_NonExistent(t *testing.T) {
	if IsXLSXFile("/nonexistent/file.xlsx") {
		t.Error("expected false for nonexistent file")
	}
}

func TestIsXLSXFile_PlainFile(t *testing.T) {
	tmpDir := t.TempDir()
	plainPath := filepath.Join(tmpDir, "plain.txt")
	_ = os.WriteFile(plainPath, []byte("plain text content"), 0644)

	if IsXLSXFile(plainPath) {
		t.Error("expected false for plain text file")
	}
}

func TestFindTicker(t *testing.T) {
	// 1. Column D match
	r1 := sheetRow{"A": "Random", "D": "NVDA", "C": "Nvidia"}
	if tick := findTicker(r1, nil); tick != "NVDA" {
		t.Errorf("expected NVDA, got %q", tick)
	}

	// 2. Column A match
	r2 := sheetRow{"A": "AAPL", "B": "Apple Inc"}
	if tick := findTicker(r2, nil); tick != "AAPL" {
		t.Errorf("expected AAPL, got %q", tick)
	}

	// 3. Shifted next row Column A match
	r3 := sheetRow{"C": "Microsoft Corp"}
	nextR := sheetRow{"A": "MSFT"}
	if tick := findTicker(r3, nextR); tick != "MSFT" {
		t.Errorf("expected MSFT, got %q", tick)
	}

	// 4. Reserved word ignored
	r4 := sheetRow{"A": "CASH", "D": "USD"}
	if tick := findTicker(r4, nil); tick != "" {
		t.Errorf("expected empty ticker for reserved words, got %q", tick)
	}
}

func TestConvertXLSXToCSV_EndToEnd(t *testing.T) {
	tmpDir := t.TempDir()
	xlsxPath := filepath.Join(tmpDir, "test_portfolio.xlsx")
	csvPath := filepath.Join(tmpDir, "output.csv")

	// Construct a minimal valid xlsx zip archive
	f, err := os.Create(xlsxPath)
	if err != nil {
		t.Fatalf("creating test xlsx: %v", err)
	}
	zw := zip.NewWriter(f)

	// Add xl/sharedStrings.xml
	// Index 0: "AAPL", Index 1: "Apple Inc", Index 2: "MSFT", Index 3: "Microsoft"
	ssContent := `<?xml version="1.0" encoding="UTF-8"?>
<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" count="4" uniqueCount="4">
	<si><t>AAPL</t></si>
	<si><t>Apple Inc</t></si>
	<si><t>MSFT</t></si>
	<si><t>Microsoft Corp</t></si>
</sst>`
	ssFile, err := zw.Create("xl/sharedStrings.xml")
	if err != nil {
		t.Fatalf("creating sharedStrings: %v", err)
	}
	_, _ = ssFile.Write([]byte(ssContent))

	// Add xl/worksheets/sheet1.xml
	sheetContent := `<?xml version="1.0" encoding="UTF-8"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
	<sheetData>
		<row r="1">
			<c r="A1" t="s"><v>0</v></c>
			<c r="B1"><v>0.60</v></c>
			<c r="C1" t="s"><v>1</v></c>
			<c r="F1"><v>60000</v></c>
		</row>
		<row r="2">
			<c r="A2" t="s"><v>2</v></c>
			<c r="B2"><v>0.40</v></c>
			<c r="C2" t="s"><v>3</v></c>
			<c r="F2"><v>40000</v></c>
		</row>
	</sheetData>
</worksheet>`
	sFile, err := zw.Create("xl/worksheets/sheet1.xml")
	if err != nil {
		t.Fatalf("creating sheet1: %v", err)
	}
	_, _ = sFile.Write([]byte(sheetContent))

	_ = zw.Close()
	_ = f.Close()

	// Verify IsXLSXFile returns true
	if !IsXLSXFile(xlsxPath) {
		t.Fatal("expected IsXLSXFile to return true for created xlsx")
	}

	// Convert to CSV
	count, err := ConvertXLSXToCSV(xlsxPath, csvPath)
	if err != nil {
		t.Fatalf("ConvertXLSXToCSV failed: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 holdings converted, got %d", count)
	}

	// Validate output CSV
	outF, err := os.Open(csvPath)
	if err != nil {
		t.Fatalf("opening output csv: %v", err)
	}
	defer outF.Close()

	r := csv.NewReader(outF)
	records, err := r.ReadAll()
	if err != nil {
		t.Fatalf("reading output csv: %v", err)
	}

	if len(records) != 3 { // header + 2 rows
		t.Fatalf("expected 3 records in csv, got %d", len(records))
	}
	if records[0][0] != "ticker" || records[0][1] != "weight" {
		t.Errorf("header mismatch: %v", records[0])
	}
	if !strings.Contains(records[1][0], "AAPL") || !strings.Contains(records[2][0], "MSFT") {
		t.Errorf("holdings mismatch: %v, %v", records[1], records[2])
	}
}
