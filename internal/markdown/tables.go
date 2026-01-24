// Package markdown provides utilities for processing markdown content,
// including table conversion for channels that don't support markdown tables.
package markdown

import (
	"regexp"
	"strings"
)

// TableMode specifies how to handle markdown tables.
type TableMode string

const (
	// TableModeOff leaves tables unchanged.
	TableModeOff TableMode = "off"
	// TableModeBullets converts tables to bullet lists.
	TableModeBullets TableMode = "bullets"
	// TableModeCode wraps tables in code blocks.
	TableModeCode TableMode = "code"
)

// IsValidTableMode checks if a mode string is valid.
func IsValidTableMode(mode string) bool {
	switch TableMode(strings.ToLower(mode)) {
	case TableModeOff, TableModeBullets, TableModeCode, "":
		return true
	default:
		return false
	}
}

// ParseTableMode parses a table mode string, returning the default if invalid.
func ParseTableMode(mode string, defaultMode TableMode) TableMode {
	m := TableMode(strings.ToLower(mode))
	switch m {
	case TableModeOff, TableModeBullets, TableModeCode:
		return m
	default:
		return defaultMode
	}
}

// Table represents a parsed markdown table.
type Table struct {
	Headers []string
	Rows    [][]string
	// Raw is the original table text
	Raw string
	// StartIndex is where the table starts in the original text
	StartIndex int
	// EndIndex is where the table ends in the original text
	EndIndex int
}

// tableRowRegex matches markdown table rows
var tableRowRegex = regexp.MustCompile(`^\s*\|(.+)\|\s*$`)

// separatorRegex matches table separator rows (|---|---|)
var separatorRegex = regexp.MustCompile(`^\s*\|[\s\-:|]+\|\s*$`)

// ConvertTables converts markdown tables in the text according to the mode.
func ConvertTables(text string, mode TableMode) string {
	if mode == TableModeOff || mode == "" {
		return text
	}

	tables := FindTables(text)
	if len(tables) == 0 {
		return text
	}

	// Process tables in reverse order to preserve indices
	result := text
	for i := len(tables) - 1; i >= 0; i-- {
		table := tables[i]
		var converted string

		switch mode {
		case TableModeBullets:
			converted = tableToBullets(table)
		case TableModeCode:
			converted = tableToCode(table)
		default:
			continue
		}

		result = result[:table.StartIndex] + converted + result[table.EndIndex:]
	}

	return result
}

// FindTables finds all markdown tables in the text.
func FindTables(text string) []Table {
	var tables []Table
	lines := strings.Split(text, "\n")

	i := 0
	for i < len(lines) {
		// Look for potential table start
		if tableRowRegex.MatchString(lines[i]) {
			table, endLine := parseTable(lines, i)
			if table != nil {
				// Calculate the raw table content
				raw := strings.Join(lines[i:endLine], "\n")

				// Find the start position in the original text
				startIdx := 0
				for j := 0; j < i; j++ {
					startIdx += len(lines[j]) + 1 // +1 for newline
				}

				// End index is start + length of raw content
				endIdx := startIdx + len(raw)

				// Ensure we don't exceed text bounds
				if endIdx > len(text) {
					endIdx = len(text)
				}

				table.StartIndex = startIdx
				table.EndIndex = endIdx
				table.Raw = raw
				tables = append(tables, *table)
				i = endLine
				continue
			}
		}
		i++
	}

	return tables
}

// parseTable attempts to parse a markdown table starting at lineIdx.
// Returns the table and the line index after the table, or nil if not a valid table.
func parseTable(lines []string, lineIdx int) (*Table, int) {
	if lineIdx >= len(lines) {
		return nil, lineIdx
	}

	// First line should be headers
	headers := parseCells(lines[lineIdx])
	if len(headers) == 0 {
		return nil, lineIdx
	}

	// Second line should be separator
	if lineIdx+1 >= len(lines) || !separatorRegex.MatchString(lines[lineIdx+1]) {
		return nil, lineIdx
	}

	table := &Table{
		Headers: headers,
	}

	// Parse data rows
	endLine := lineIdx + 2
	for endLine < len(lines) {
		if !tableRowRegex.MatchString(lines[endLine]) {
			break
		}
		cells := parseCells(lines[endLine])
		// Ensure row has same number of columns (or pad with empty)
		for len(cells) < len(headers) {
			cells = append(cells, "")
		}
		table.Rows = append(table.Rows, cells)
		endLine++
	}

	// Must have at least one data row to be a valid table
	if len(table.Rows) == 0 {
		return nil, lineIdx
	}

	return table, endLine
}

// parseCells extracts cells from a table row.
func parseCells(row string) []string {
	row = strings.TrimSpace(row)
	// Remove leading/trailing pipes
	row = strings.TrimPrefix(row, "|")
	row = strings.TrimSuffix(row, "|")

	parts := strings.Split(row, "|")
	var cells []string
	for _, part := range parts {
		cells = append(cells, strings.TrimSpace(part))
	}
	return cells
}

// tableToBullets converts a table to a bullet list format.
func tableToBullets(table Table) string {
	var lines []string

	for _, row := range table.Rows {
		var parts []string
		for i, cell := range row {
			if cell == "" {
				continue
			}
			header := ""
			if i < len(table.Headers) && table.Headers[i] != "" {
				header = table.Headers[i] + ": "
			}
			parts = append(parts, header+cell)
		}
		if len(parts) > 0 {
			lines = append(lines, "• "+strings.Join(parts, " | "))
		}
	}

	return strings.Join(lines, "\n")
}

// tableToCode wraps a table in a code block.
func tableToCode(table Table) string {
	return "```\n" + table.Raw + "\n```"
}

// HasTables checks if the text contains any markdown tables.
func HasTables(text string) bool {
	return len(FindTables(text)) > 0
}

// DefaultTableModeForChannel returns the default table mode for a channel.
// Some channels like Signal and WhatsApp don't support markdown tables well.
func DefaultTableModeForChannel(channel string) TableMode {
	switch strings.ToLower(channel) {
	case "signal", "whatsapp", "sms":
		return TableModeBullets
	case "slack", "discord", "telegram", "matrix":
		return TableModeCode
	default:
		return TableModeOff
	}
}
