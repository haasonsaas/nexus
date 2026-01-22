// Package commands provides slash command detection and routing.
package commands

import (
	"context"
)

// Command represents a registered slash command.
type Command struct {
	// Name is the command name without the leading slash (e.g., "help")
	Name string `json:"name"`

	// Aliases are alternative names for the command
	Aliases []string `json:"aliases,omitempty"`

	// Description is a short description of what the command does
	Description string `json:"description,omitempty"`

	// Usage shows how to use the command
	Usage string `json:"usage,omitempty"`

	// AcceptsArgs indicates if the command accepts arguments
	AcceptsArgs bool `json:"accepts_args"`

	// Hidden hides the command from help listings
	Hidden bool `json:"hidden,omitempty"`

	// AdminOnly restricts the command to admin users
	AdminOnly bool `json:"admin_only,omitempty"`

	// Handler is the function that executes the command
	Handler CommandHandler `json:"-"`

	// Source identifies where this command came from (builtin, plugin, skill)
	Source string `json:"source,omitempty"`

	// Category groups commands in help output
	Category string `json:"category,omitempty"`
}

// CommandHandler processes a command invocation.
type CommandHandler func(ctx context.Context, inv *Invocation) (*Result, error)

// Invocation represents a parsed command invocation.
type Invocation struct {
	// Command is the matched command definition
	Command *Command

	// Name is the actual name/alias used to invoke
	Name string

	// Args is the text after the command name
	Args string

	// RawText is the original message text
	RawText string

	// SessionKey identifies the session
	SessionKey string

	// ChannelID identifies the channel
	ChannelID string

	// UserID identifies the user who invoked the command
	UserID string

	// IsAdmin indicates if the user has admin privileges
	IsAdmin bool

	// Context holds additional invocation data
	Context map[string]any
}

// Result is the output of a command execution.
type Result struct {
	// Text is the response message to send
	Text string `json:"text,omitempty"`

	// Markdown indicates if Text should be rendered as markdown
	Markdown bool `json:"markdown,omitempty"`

	// Private indicates the response should only be visible to the invoker
	Private bool `json:"private,omitempty"`

	// Suppress indicates no response should be sent
	Suppress bool `json:"suppress,omitempty"`

	// Data holds structured data for programmatic consumption
	Data map[string]any `json:"data,omitempty"`

	// Error is set if the command failed
	Error string `json:"error,omitempty"`
}

// ParsedCommand represents a detected command in a message.
type ParsedCommand struct {
	// Name is the command name (without prefix)
	Name string

	// Args is the argument text
	Args string

	// Prefix is the command prefix used (/, !, etc)
	Prefix string

	// StartPos is the position in the original text
	StartPos int

	// EndPos is the end position in the original text
	EndPos int

	// Inline indicates this was an inline command (not at start of message)
	Inline bool
}

// Detection holds the result of command detection.
type Detection struct {
	// HasCommand indicates if any command was found
	HasCommand bool

	// Commands are all detected commands in the message
	Commands []ParsedCommand

	// Primary is the first/main command (usually at message start)
	Primary *ParsedCommand

	// IsControlCommand indicates this is a system control command
	IsControlCommand bool
}
