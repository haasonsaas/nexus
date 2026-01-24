package markdown

import (
	"strings"
	"testing"
)

func TestFindTables(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantCount int
	}{
		{
			name:      "no tables",
			input:     "Just some text\nwithout tables",
			wantCount: 0,
		},
		{
			name: "simple table",
			input: `| Header 1 | Header 2 |
|----------|----------|
| Cell 1   | Cell 2   |`,
			wantCount: 1,
		},
		{
			name: "table with multiple rows",
			input: `| Name | Age |
|------|-----|
| Alice | 25 |
| Bob | 30 |
| Carol | 35 |`,
			wantCount: 1,
		},
		{
			name: "table in text",
			input: `Some text before

| Column A | Column B |
|----------|----------|
| Value 1  | Value 2  |

Some text after`,
			wantCount: 1,
		},
		{
			name: "multiple tables",
			input: `First table:

| A | B |
|---|---|
| 1 | 2 |

Second table:

| X | Y |
|---|---|
| 3 | 4 |`,
			wantCount: 2,
		},
		{
			name: "not a table - missing separator",
			input: `| Header 1 | Header 2 |
| Cell 1   | Cell 2   |`,
			wantCount: 0,
		},
		{
			name: "not a table - no data rows",
			input: `| Header 1 | Header 2 |
|----------|----------|`,
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tables := FindTables(tt.input)
			if len(tables) != tt.wantCount {
				t.Errorf("FindTables() found %d tables, want %d", len(tables), tt.wantCount)
			}
		})
	}
}

func TestConvertTables_Bullets(t *testing.T) {
	input := `Here is a table:

| Name | Role |
|------|------|
| Alice | Developer |
| Bob | Designer |

End of text.`

	result := ConvertTables(input, TableModeBullets)

	// Should not contain pipe characters from table
	if strings.Contains(result, "|---") {
		t.Error("table separator should be removed")
	}

	// Should contain bullet points
	if !strings.Contains(result, "• ") {
		t.Error("expected bullet points in output")
	}

	// Should contain header-value pairs
	if !strings.Contains(result, "Name: Alice") {
		t.Error("expected 'Name: Alice' in output")
	}

	// Should preserve surrounding text
	if !strings.Contains(result, "Here is a table:") {
		t.Error("text before table should be preserved")
	}
	if !strings.Contains(result, "End of text.") {
		t.Error("text after table should be preserved")
	}
}

func TestConvertTables_Code(t *testing.T) {
	input := `Table:

| A | B |
|---|---|
| 1 | 2 |`

	result := ConvertTables(input, TableModeCode)

	// Should be wrapped in code block
	if !strings.Contains(result, "```\n| A | B |") {
		t.Error("expected table to be wrapped in code block")
	}
	if !strings.Contains(result, "| 2 |\n```") {
		t.Error("expected closing code block")
	}
}

func TestConvertTables_Off(t *testing.T) {
	input := `| A | B |
|---|---|
| 1 | 2 |`

	result := ConvertTables(input, TableModeOff)

	if result != input {
		t.Errorf("TableModeOff should leave input unchanged, got: %s", result)
	}
}

func TestConvertTables_MultipleTablesBullets(t *testing.T) {
	input := `Table 1:
| X | Y |
|---|---|
| a | b |

Table 2:
| P | Q |
|---|---|
| c | d |`

	result := ConvertTables(input, TableModeBullets)

	// Should have two bullet points (one per table row)
	count := strings.Count(result, "• ")
	if count != 2 {
		t.Errorf("expected 2 bullet points, got %d", count)
	}

	// Both tables should be converted
	if !strings.Contains(result, "X: a") {
		t.Error("first table not converted")
	}
	if !strings.Contains(result, "P: c") {
		t.Error("second table not converted")
	}
}

func TestParseCells(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{
			input: "| A | B | C |",
			want:  []string{"A", "B", "C"},
		},
		{
			input: "|A|B|C|",
			want:  []string{"A", "B", "C"},
		},
		{
			input: "| First cell | Second cell |",
			want:  []string{"First cell", "Second cell"},
		},
		{
			input: "|  Padded  |  Content  |",
			want:  []string{"Padded", "Content"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseCells(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("parseCells() got %d cells, want %d", len(got), len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("cell %d: got %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestTableToBullets(t *testing.T) {
	table := Table{
		Headers: []string{"Name", "Age", "City"},
		Rows: [][]string{
			{"Alice", "25", "NYC"},
			{"Bob", "30", "LA"},
		},
	}

	result := tableToBullets(table)

	// Should have two bullet points
	if strings.Count(result, "• ") != 2 {
		t.Errorf("expected 2 bullet points, got: %s", result)
	}

	// First row
	if !strings.Contains(result, "Name: Alice") {
		t.Error("missing 'Name: Alice'")
	}
	if !strings.Contains(result, "Age: 25") {
		t.Error("missing 'Age: 25'")
	}

	// Second row
	if !strings.Contains(result, "Name: Bob") {
		t.Error("missing 'Name: Bob'")
	}
}

func TestTableToBullets_EmptyCells(t *testing.T) {
	table := Table{
		Headers: []string{"Name", "Notes"},
		Rows: [][]string{
			{"Alice", ""},
			{"", "Some note"},
		},
	}

	result := tableToBullets(table)

	// Empty cells should be skipped
	if strings.Contains(result, "Notes: |") {
		t.Error("empty cell should not appear")
	}
}

func TestHasTables(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name:  "has table",
			input: "| A | B |\n|---|---|\n| 1 | 2 |",
			want:  true,
		},
		{
			name:  "no table",
			input: "Just regular text",
			want:  false,
		},
		{
			name:  "pipe but not table",
			input: "This | is | not | a | table",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasTables(tt.input)
			if got != tt.want {
				t.Errorf("HasTables() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDefaultTableModeForChannel(t *testing.T) {
	tests := []struct {
		channel string
		want    TableMode
	}{
		{"signal", TableModeBullets},
		{"Signal", TableModeBullets},
		{"whatsapp", TableModeBullets},
		{"WhatsApp", TableModeBullets},
		{"sms", TableModeBullets},
		{"slack", TableModeCode},
		{"discord", TableModeCode},
		{"telegram", TableModeCode},
		{"matrix", TableModeCode},
		{"email", TableModeOff},
		{"unknown", TableModeOff},
	}

	for _, tt := range tests {
		t.Run(tt.channel, func(t *testing.T) {
			got := DefaultTableModeForChannel(tt.channel)
			if got != tt.want {
				t.Errorf("DefaultTableModeForChannel(%q) = %v, want %v", tt.channel, got, tt.want)
			}
		})
	}
}

func TestParseTableMode(t *testing.T) {
	tests := []struct {
		input   string
		def     TableMode
		want    TableMode
	}{
		{"off", TableModeCode, TableModeOff},
		{"OFF", TableModeCode, TableModeOff},
		{"bullets", TableModeOff, TableModeBullets},
		{"BULLETS", TableModeOff, TableModeBullets},
		{"code", TableModeOff, TableModeCode},
		{"CODE", TableModeOff, TableModeCode},
		{"invalid", TableModeCode, TableModeCode},
		{"", TableModeBullets, TableModeBullets},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ParseTableMode(tt.input, tt.def)
			if got != tt.want {
				t.Errorf("ParseTableMode(%q, %v) = %v, want %v", tt.input, tt.def, got, tt.want)
			}
		})
	}
}

func TestIsValidTableMode(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"off", true},
		{"OFF", true},
		{"bullets", true},
		{"code", true},
		{"", true},
		{"invalid", false},
		{"bullet", false},
		{"plain", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := IsValidTableMode(tt.input)
			if got != tt.want {
				t.Errorf("IsValidTableMode(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestTableIndices(t *testing.T) {
	input := `Before table

| A | B |
|---|---|
| 1 | 2 |

After table`

	tables := FindTables(input)
	if len(tables) != 1 {
		t.Fatalf("expected 1 table, got %d", len(tables))
	}

	table := tables[0]

	// Verify the raw content
	expectedRaw := "| A | B |\n|---|---|\n| 1 | 2 |"
	if table.Raw != expectedRaw {
		t.Errorf("table.Raw = %q, want %q", table.Raw, expectedRaw)
	}

	// Verify indices allow correct extraction
	extracted := input[table.StartIndex:table.EndIndex]
	if extracted != expectedRaw {
		t.Errorf("extracted = %q, want %q", extracted, expectedRaw)
	}
}
