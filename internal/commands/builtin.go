package commands

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// RegisterBuiltins registers the built-in commands.
func RegisterBuiltins(r *Registry) {
	mustRegister := func(cmd *Command) {
		if err := r.Register(cmd); err != nil {
			panic(fmt.Sprintf("failed to register builtin command %q: %v", cmd.Name, err))
		}
	}

	// Help command
	mustRegister(&Command{
		Name:        "help",
		Aliases:     []string{"h", "?"},
		Description: "Show available commands",
		Usage:       "/help [command]",
		AcceptsArgs: true,
		Category:    "system",
		Source:      "builtin",
		Handler:     helpHandler(r),
	})

	// Status command
	mustRegister(&Command{
		Name:        "status",
		Description: "Show current session status",
		Category:    "system",
		Source:      "builtin",
		Handler: func(ctx context.Context, inv *Invocation) (*Result, error) {
			return &Result{
				Text:     "Session active",
				Markdown: false,
			}, nil
		},
	})

	// New session command
	mustRegister(&Command{
		Name:        "new",
		Aliases:     []string{"reset", "clear"},
		Description: "Start a new conversation",
		Category:    "session",
		Source:      "builtin",
		Handler: func(ctx context.Context, inv *Invocation) (*Result, error) {
			return &Result{
				Text: "Starting new conversation...",
				Data: map[string]any{
					"action": "new_session",
				},
			}, nil
		},
	})

	// Model command
	mustRegister(&Command{
		Name:        "model",
		Description: "Show or change the current model",
		Usage:       "/model [model_name]",
		AcceptsArgs: true,
		Category:    "config",
		Source:      "builtin",
		Handler: func(ctx context.Context, inv *Invocation) (*Result, error) {
			if inv.Args == "" {
				return &Result{
					Text: "Current model: (use /model <name> to change)",
					Data: map[string]any{
						"action": "get_model",
					},
				}, nil
			}
			return &Result{
				Text: fmt.Sprintf("Model changed to: %s", inv.Args),
				Data: map[string]any{
					"action": "set_model",
					"model":  inv.Args,
				},
			}, nil
		},
	})

	// Stop/abort command
	mustRegister(&Command{
		Name:        "stop",
		Aliases:     []string{"abort", "cancel"},
		Description: "Stop the current operation",
		Category:    "control",
		Source:      "builtin",
		Handler: func(ctx context.Context, inv *Invocation) (*Result, error) {
			return &Result{
				Text: "Stopping...",
				Data: map[string]any{
					"action": "abort",
				},
			}, nil
		},
	})

	// Undo command
	mustRegister(&Command{
		Name:        "undo",
		Description: "Undo the last message",
		Category:    "session",
		Source:      "builtin",
		Handler: func(ctx context.Context, inv *Invocation) (*Result, error) {
			return &Result{
				Text: "Undoing last message...",
				Data: map[string]any{
					"action": "undo",
				},
			}, nil
		},
	})

	// Memory command
	mustRegister(&Command{
		Name:        "memory",
		Aliases:     []string{"mem"},
		Description: "Search or manage memory",
		Usage:       "/memory [query]",
		AcceptsArgs: true,
		Category:    "memory",
		Source:      "builtin",
		Handler: func(ctx context.Context, inv *Invocation) (*Result, error) {
			if inv.Args == "" {
				return &Result{
					Text: "Memory search. Usage: /memory <query>",
				}, nil
			}
			return &Result{
				Text: fmt.Sprintf("Searching memory for: %s", inv.Args),
				Data: map[string]any{
					"action": "memory_search",
					"query":  inv.Args,
				},
			}, nil
		},
	})

	// Compact/summarize command
	mustRegister(&Command{
		Name:        "compact",
		Aliases:     []string{"summarize"},
		Description: "Summarize and compact the conversation",
		Category:    "session",
		Source:      "builtin",
		Handler: func(ctx context.Context, inv *Invocation) (*Result, error) {
			return &Result{
				Text: "Compacting conversation...",
				Data: map[string]any{
					"action": "compact",
				},
			}, nil
		},
	})
}

// titleCase converts the first letter to uppercase.
func titleCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func helpHandler(r *Registry) CommandHandler {
	return func(ctx context.Context, inv *Invocation) (*Result, error) {
		// If specific command requested
		if inv.Args != "" {
			cmdName := strings.ToLower(strings.TrimSpace(inv.Args))
			cmdName = strings.TrimPrefix(cmdName, "/")

			cmd, exists := r.Get(cmdName)
			if !exists {
				return &Result{
					Text: fmt.Sprintf("Unknown command: %s\n\nUse /help to see available commands.", cmdName),
				}, nil
			}

			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("**/%s**\n", cmd.Name))
			if cmd.Description != "" {
				sb.WriteString(fmt.Sprintf("%s\n", cmd.Description))
			}
			if cmd.Usage != "" {
				sb.WriteString(fmt.Sprintf("\nUsage: `%s`\n", cmd.Usage))
			}
			if len(cmd.Aliases) > 0 {
				aliases := make([]string, len(cmd.Aliases))
				for i, a := range cmd.Aliases {
					aliases[i] = "/" + a
				}
				sb.WriteString(fmt.Sprintf("\nAliases: %s\n", strings.Join(aliases, ", ")))
			}
			if cmd.AdminOnly {
				sb.WriteString("\n⚠️ Admin only\n")
			}

			return &Result{
				Text:     sb.String(),
				Markdown: true,
			}, nil
		}

		// List all commands by category
		byCategory := r.ListByCategory()
		categories := make([]string, 0, len(byCategory))
		for cat := range byCategory {
			categories = append(categories, cat)
		}
		sort.Strings(categories)

		var sb strings.Builder
		sb.WriteString("**Available Commands**\n\n")

		for _, category := range categories {
			commands := byCategory[category]
			if len(commands) == 0 {
				continue
			}

			sb.WriteString(fmt.Sprintf("**%s**\n", titleCase(category)))
			for _, cmd := range commands {
				desc := cmd.Description
				if desc == "" {
					desc = "No description"
				}
				sb.WriteString(fmt.Sprintf("  `/%s` - %s\n", cmd.Name, desc))
			}
			sb.WriteString("\n")
		}

		sb.WriteString("Use `/help <command>` for more details.")

		return &Result{
			Text:     sb.String(),
			Markdown: true,
		}, nil
	}
}
