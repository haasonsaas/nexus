package commands

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
)

// Registry manages command registrations and execution.
type Registry struct {
	commands   map[string]*Command // name -> command
	aliases    map[string]string   // alias -> name
	categories map[string][]*Command
	logger     *slog.Logger
	mu         sync.RWMutex
}

// NewRegistry creates a new command registry.
func NewRegistry(logger *slog.Logger) *Registry {
	if logger == nil {
		logger = slog.Default()
	}
	return &Registry{
		commands:   make(map[string]*Command),
		aliases:    make(map[string]string),
		categories: make(map[string][]*Command),
		logger:     logger.With("component", "commands"),
	}
}

// Register adds a command to the registry.
func (r *Registry) Register(cmd *Command) error {
	if cmd == nil {
		return fmt.Errorf("command is nil")
	}
	if cmd.Name == "" {
		return fmt.Errorf("command name is required")
	}
	if cmd.Handler == nil {
		return fmt.Errorf("command handler is required")
	}

	name := strings.ToLower(strings.TrimSpace(cmd.Name))

	r.mu.Lock()
	defer r.mu.Unlock()

	// Check for conflicts
	if _, exists := r.commands[name]; exists {
		return fmt.Errorf("command %q already registered", name)
	}
	if existingName, exists := r.aliases[name]; exists {
		return fmt.Errorf("command name %q conflicts with alias for %q", name, existingName)
	}

	// Register command
	r.commands[name] = cmd

	// Register aliases
	for _, alias := range cmd.Aliases {
		aliasLower := strings.ToLower(strings.TrimSpace(alias))
		if aliasLower == "" || aliasLower == name {
			continue
		}
		if _, exists := r.commands[aliasLower]; exists {
			r.logger.Warn("alias conflicts with command", "alias", aliasLower, "command", name)
			continue
		}
		if _, exists := r.aliases[aliasLower]; exists {
			r.logger.Warn("alias already registered", "alias", aliasLower, "command", name)
			continue
		}
		r.aliases[aliasLower] = name
	}

	// Add to category
	category := cmd.Category
	if category == "" {
		category = "general"
	}
	r.categories[category] = append(r.categories[category], cmd)

	r.logger.Debug("registered command",
		"name", name,
		"aliases", cmd.Aliases,
		"category", category,
		"source", cmd.Source)

	return nil
}

// Unregister removes a command from the registry.
func (r *Registry) Unregister(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))

	r.mu.Lock()
	defer r.mu.Unlock()

	cmd, exists := r.commands[name]
	if !exists {
		return false
	}

	// Remove aliases
	for _, alias := range cmd.Aliases {
		delete(r.aliases, strings.ToLower(alias))
	}

	// Remove from category
	category := cmd.Category
	if category == "" {
		category = "general"
	}
	commands := r.categories[category]
	for i, c := range commands {
		if c.Name == name {
			r.categories[category] = append(commands[:i], commands[i+1:]...)
			break
		}
	}

	delete(r.commands, name)
	r.logger.Debug("unregistered command", "name", name)
	return true
}

// Get retrieves a command by name or alias.
func (r *Registry) Get(name string) (*Command, bool) {
	name = strings.ToLower(strings.TrimSpace(name))

	r.mu.RLock()
	defer r.mu.RUnlock()

	// Direct lookup
	if cmd, exists := r.commands[name]; exists {
		return cmd, true
	}

	// Alias lookup
	if realName, exists := r.aliases[name]; exists {
		if cmd, exists := r.commands[realName]; exists {
			return cmd, true
		}
	}

	return nil, false
}

// List returns all registered commands.
func (r *Registry) List() []*Command {
	r.mu.RLock()
	defer r.mu.RUnlock()

	commands := make([]*Command, 0, len(r.commands))
	for _, cmd := range r.commands {
		commands = append(commands, cmd)
	}

	sort.Slice(commands, func(i, j int) bool {
		return commands[i].Name < commands[j].Name
	})

	return commands
}

// ListVisible returns commands that should be shown in help.
func (r *Registry) ListVisible() []*Command {
	all := r.List()
	visible := make([]*Command, 0, len(all))
	for _, cmd := range all {
		if !cmd.Hidden {
			visible = append(visible, cmd)
		}
	}
	return visible
}

// ListByCategory returns commands grouped by category.
func (r *Registry) ListByCategory() map[string][]*Command {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string][]*Command)
	for category, commands := range r.categories {
		visible := make([]*Command, 0)
		for _, cmd := range commands {
			if !cmd.Hidden {
				visible = append(visible, cmd)
			}
		}
		if len(visible) > 0 {
			result[category] = visible
		}
	}
	return result
}

// Execute runs a command by name with arguments.
func (r *Registry) Execute(ctx context.Context, inv *Invocation) (*Result, error) {
	if inv == nil {
		return nil, fmt.Errorf("invocation is nil")
	}

	cmd, exists := r.Get(inv.Name)
	if !exists {
		return nil, fmt.Errorf("command %q not found", inv.Name)
	}

	// Check admin restriction
	if cmd.AdminOnly && !inv.IsAdmin {
		return &Result{
			Error: "This command requires admin privileges",
		}, nil
	}

	// Check args requirement
	if !cmd.AcceptsArgs && strings.TrimSpace(inv.Args) != "" {
		return &Result{
			Error: fmt.Sprintf("Command /%s does not accept arguments", cmd.Name),
		}, nil
	}

	inv.Command = cmd
	return cmd.Handler(ctx, inv)
}

// Names returns all registered command names (not aliases).
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.commands))
	for name := range r.commands {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
