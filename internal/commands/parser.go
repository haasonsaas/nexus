package commands

import (
	"regexp"
	"strings"
)

// DefaultPrefixes are the default command prefixes.
var DefaultPrefixes = []string{"/", "!"}

// Parser detects and parses commands from message text.
type Parser struct {
	prefixes   []string
	registry   *Registry
	inlineRe   *regexp.Regexp
	controlRe  *regexp.Regexp
}

// NewParser creates a new command parser.
func NewParser(registry *Registry, prefixes ...string) *Parser {
	if len(prefixes) == 0 {
		prefixes = DefaultPrefixes
	}

	// Build regex for inline command detection
	// Matches /command or !command anywhere in text
	escapedPrefixes := make([]string, len(prefixes))
	for i, p := range prefixes {
		escapedPrefixes[i] = regexp.QuoteMeta(p)
	}
	prefixPattern := strings.Join(escapedPrefixes, "|")
	inlinePattern := `(?:^|\s)(` + prefixPattern + `)([a-zA-Z][a-zA-Z0-9_-]*)`

	return &Parser{
		prefixes:  prefixes,
		registry:  registry,
		inlineRe:  regexp.MustCompile(inlinePattern),
		controlRe: regexp.MustCompile(`^(?:` + prefixPattern + `)([a-zA-Z][a-zA-Z0-9_-]*)(?:\s+(.*))?$`),
	}
}

// Parse detects commands in message text.
func (p *Parser) Parse(text string) *Detection {
	text = strings.TrimSpace(text)
	if text == "" {
		return &Detection{}
	}

	detection := &Detection{
		Commands: make([]ParsedCommand, 0),
	}

	// Check for control command at start of message
	if p.isCommandPrefix(text) {
		if match := p.controlRe.FindStringSubmatch(text); match != nil {
			cmdName := strings.ToLower(match[1])
			args := ""
			if len(match) > 2 {
				args = strings.TrimSpace(match[2])
			}

			parsed := ParsedCommand{
				Name:     cmdName,
				Args:     args,
				Prefix:   text[:1],
				StartPos: 0,
				EndPos:   len(text),
				Inline:   false,
			}

			detection.Commands = append(detection.Commands, parsed)
			detection.Primary = &detection.Commands[0]
			detection.HasCommand = true

			// Check if it's a registered command
			if p.registry != nil {
				if _, exists := p.registry.Get(cmdName); exists {
					detection.IsControlCommand = true
				}
			}
		}
	}

	// Find inline commands
	matches := p.inlineRe.FindAllStringSubmatchIndex(text, -1)
	for _, match := range matches {
		if len(match) < 6 {
			continue
		}

		// Skip if this overlaps with the primary command
		if detection.Primary != nil && match[0] == 0 {
			continue
		}

		prefix := text[match[2]:match[3]]
		name := strings.ToLower(text[match[4]:match[5]])
		startPos := match[0]
		if text[startPos] == ' ' {
			startPos++
		}

		// Find args - text after command until next whitespace or end
		endPos := match[5]
		args := ""
		if endPos < len(text) && text[endPos] == ' ' {
			// Look for args
			argsStart := endPos + 1
			argsEnd := strings.Index(text[argsStart:], "  ") // Double space ends args
			if argsEnd == -1 {
				args = strings.TrimSpace(text[argsStart:])
				endPos = len(text)
			} else {
				args = strings.TrimSpace(text[argsStart : argsStart+argsEnd])
				endPos = argsStart + argsEnd
			}
		}

		parsed := ParsedCommand{
			Name:     name,
			Args:     args,
			Prefix:   prefix,
			StartPos: startPos,
			EndPos:   endPos,
			Inline:   true,
		}

		detection.Commands = append(detection.Commands, parsed)
		detection.HasCommand = true
	}

	return detection
}

// ParseCommand parses a command invocation from text.
// Returns nil if the text is not a valid command.
func (p *Parser) ParseCommand(text string) *ParsedCommand {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	if !p.isCommandPrefix(text) {
		return nil
	}

	match := p.controlRe.FindStringSubmatch(text)
	if match == nil {
		return nil
	}

	cmdName := strings.ToLower(match[1])
	args := ""
	if len(match) > 2 {
		args = strings.TrimSpace(match[2])
	}

	return &ParsedCommand{
		Name:     cmdName,
		Args:     args,
		Prefix:   text[:1],
		StartPos: 0,
		EndPos:   len(text),
		Inline:   false,
	}
}

// IsCommand checks if text starts with a command.
func (p *Parser) IsCommand(text string) bool {
	text = strings.TrimSpace(text)
	return p.isCommandPrefix(text)
}

// HasInlineCommands checks if text contains inline commands.
func (p *Parser) HasInlineCommands(text string) bool {
	return p.inlineRe.MatchString(text)
}

// isCommandPrefix checks if text starts with a command prefix.
func (p *Parser) isCommandPrefix(text string) bool {
	for _, prefix := range p.prefixes {
		if strings.HasPrefix(text, prefix) {
			// Must be followed by a letter
			if len(text) > len(prefix) {
				next := text[len(prefix)]
				if (next >= 'a' && next <= 'z') || (next >= 'A' && next <= 'Z') {
					return true
				}
			}
		}
	}
	return false
}

// NormalizeCommandText extracts the command portion from text.
// For "/help foo bar", returns "help foo bar".
func NormalizeCommandText(text string, prefixes ...string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}

	if len(prefixes) == 0 {
		prefixes = DefaultPrefixes
	}

	for _, prefix := range prefixes {
		if strings.HasPrefix(text, prefix) {
			return strings.TrimSpace(text[len(prefix):])
		}
	}

	return text
}

// SplitCommandArgs splits command text into name and args.
func SplitCommandArgs(text string) (name, args string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", ""
	}

	parts := strings.SplitN(text, " ", 2)
	name = strings.ToLower(strings.TrimSpace(parts[0]))
	if len(parts) > 1 {
		args = strings.TrimSpace(parts[1])
	}
	return name, args
}
