package commands

import (
	"context"
	"testing"
)

func TestNewRegistry(t *testing.T) {
	t.Run("with nil logger", func(t *testing.T) {
		r := NewRegistry(nil)
		if r == nil {
			t.Fatal("NewRegistry returned nil")
		}
		if r.commands == nil {
			t.Error("commands map not initialized")
		}
		if r.aliases == nil {
			t.Error("aliases map not initialized")
		}
		if r.categories == nil {
			t.Error("categories map not initialized")
		}
	})
}

func TestRegistry_Register_Errors(t *testing.T) {
	r := NewRegistry(nil)

	t.Run("nil command", func(t *testing.T) {
		err := r.Register(nil)
		if err == nil {
			t.Error("expected error for nil command")
		}
	})

	t.Run("empty name", func(t *testing.T) {
		err := r.Register(&Command{
			Name: "",
			Handler: func(ctx context.Context, inv *Invocation) (*Result, error) {
				return nil, nil
			},
		})
		if err == nil {
			t.Error("expected error for empty name")
		}
	})

	t.Run("nil handler", func(t *testing.T) {
		err := r.Register(&Command{
			Name:    "test",
			Handler: nil,
		})
		if err == nil {
			t.Error("expected error for nil handler")
		}
	})

	t.Run("alias conflicts with existing command", func(t *testing.T) {
		handler := func(ctx context.Context, inv *Invocation) (*Result, error) {
			return nil, nil
		}
		r := NewRegistry(nil)
		r.Register(&Command{Name: "existing", Handler: handler})

		// Register a command with alias that conflicts with existing command
		err := r.Register(&Command{
			Name:    "newcmd",
			Aliases: []string{"existing"}, // Conflicts with existing command name
			Handler: handler,
		})
		// This should succeed but the alias should be skipped (logged as warning)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("name conflicts with existing alias", func(t *testing.T) {
		handler := func(ctx context.Context, inv *Invocation) (*Result, error) {
			return nil, nil
		}
		r := NewRegistry(nil)
		r.Register(&Command{Name: "cmd1", Aliases: []string{"myalias"}, Handler: handler})

		err := r.Register(&Command{
			Name:    "myalias", // Same as existing alias
			Handler: handler,
		})
		if err == nil {
			t.Error("expected error when name conflicts with existing alias")
		}
	})
}

func TestRegistry_Unregister(t *testing.T) {
	handler := func(ctx context.Context, inv *Invocation) (*Result, error) {
		return nil, nil
	}

	t.Run("unregister existing command", func(t *testing.T) {
		r := NewRegistry(nil)
		r.Register(&Command{
			Name:     "test",
			Aliases:  []string{"t"},
			Category: "testing",
			Handler:  handler,
		})

		if !r.Unregister("test") {
			t.Error("Unregister returned false for existing command")
		}

		// Should no longer find by name or alias
		if _, found := r.Get("test"); found {
			t.Error("command still found after unregister")
		}
		if _, found := r.Get("t"); found {
			t.Error("alias still found after unregister")
		}
	})

	t.Run("unregister nonexistent command", func(t *testing.T) {
		r := NewRegistry(nil)
		if r.Unregister("nonexistent") {
			t.Error("Unregister returned true for nonexistent command")
		}
	})

	t.Run("unregister with empty category", func(t *testing.T) {
		r := NewRegistry(nil)
		r.Register(&Command{
			Name:    "nocategory",
			Handler: handler,
		})

		if !r.Unregister("nocategory") {
			t.Error("Unregister failed")
		}
	})
}

func TestRegistry_Get(t *testing.T) {
	handler := func(ctx context.Context, inv *Invocation) (*Result, error) {
		return nil, nil
	}
	r := NewRegistry(nil)
	r.Register(&Command{
		Name:    "test",
		Aliases: []string{"t", "tst"},
		Handler: handler,
	})

	t.Run("by name", func(t *testing.T) {
		cmd, found := r.Get("test")
		if !found || cmd == nil {
			t.Error("command not found by name")
		}
	})

	t.Run("by alias", func(t *testing.T) {
		cmd, found := r.Get("t")
		if !found || cmd == nil {
			t.Error("command not found by alias")
		}
		if cmd.Name != "test" {
			t.Error("wrong command returned for alias")
		}
	})

	t.Run("case insensitive", func(t *testing.T) {
		cmd, found := r.Get("TEST")
		if !found || cmd == nil {
			t.Error("command not found with uppercase")
		}
	})

	t.Run("with whitespace", func(t *testing.T) {
		cmd, found := r.Get("  test  ")
		if !found || cmd == nil {
			t.Error("command not found with surrounding whitespace")
		}
	})

	t.Run("nonexistent", func(t *testing.T) {
		_, found := r.Get("nonexistent")
		if found {
			t.Error("found nonexistent command")
		}
	})
}

func TestRegistry_List(t *testing.T) {
	handler := func(ctx context.Context, inv *Invocation) (*Result, error) {
		return nil, nil
	}
	r := NewRegistry(nil)
	r.Register(&Command{Name: "cmd2", Handler: handler})
	r.Register(&Command{Name: "cmd1", Handler: handler})
	r.Register(&Command{Name: "cmd3", Handler: handler})

	list := r.List()
	if len(list) != 3 {
		t.Errorf("List returned %d commands, want 3", len(list))
	}

	// Should be sorted alphabetically
	if list[0].Name != "cmd1" || list[1].Name != "cmd2" || list[2].Name != "cmd3" {
		t.Error("List is not sorted alphabetically")
	}
}

func TestRegistry_ListVisible(t *testing.T) {
	handler := func(ctx context.Context, inv *Invocation) (*Result, error) {
		return nil, nil
	}
	r := NewRegistry(nil)
	r.Register(&Command{Name: "visible1", Handler: handler})
	r.Register(&Command{Name: "hidden", Hidden: true, Handler: handler})
	r.Register(&Command{Name: "visible2", Handler: handler})

	visible := r.ListVisible()
	if len(visible) != 2 {
		t.Errorf("ListVisible returned %d commands, want 2", len(visible))
	}

	for _, cmd := range visible {
		if cmd.Hidden {
			t.Errorf("Hidden command %q in visible list", cmd.Name)
		}
	}
}

func TestRegistry_ListByCategory(t *testing.T) {
	handler := func(ctx context.Context, inv *Invocation) (*Result, error) {
		return nil, nil
	}
	r := NewRegistry(nil)
	r.Register(&Command{Name: "cmd1", Category: "cat1", Handler: handler})
	r.Register(&Command{Name: "cmd2", Category: "cat1", Handler: handler})
	r.Register(&Command{Name: "cmd3", Category: "cat2", Handler: handler})
	r.Register(&Command{Name: "hidden", Category: "cat1", Hidden: true, Handler: handler})

	byCategory := r.ListByCategory()

	if len(byCategory["cat1"]) != 2 {
		t.Errorf("cat1 has %d visible commands, want 2", len(byCategory["cat1"]))
	}
	if len(byCategory["cat2"]) != 1 {
		t.Errorf("cat2 has %d visible commands, want 1", len(byCategory["cat2"]))
	}
}

func TestRegistry_Names(t *testing.T) {
	handler := func(ctx context.Context, inv *Invocation) (*Result, error) {
		return nil, nil
	}
	r := NewRegistry(nil)
	r.Register(&Command{Name: "beta", Aliases: []string{"b"}, Handler: handler})
	r.Register(&Command{Name: "alpha", Handler: handler})

	names := r.Names()
	if len(names) != 2 {
		t.Errorf("Names returned %d names, want 2", len(names))
	}

	// Should be sorted
	if names[0] != "alpha" || names[1] != "beta" {
		t.Error("Names is not sorted")
	}

	// Should not include aliases
	for _, name := range names {
		if name == "b" {
			t.Error("Names includes alias")
		}
	}
}

func TestRegistry_Execute_EdgeCases(t *testing.T) {
	handler := func(ctx context.Context, inv *Invocation) (*Result, error) {
		return &Result{Text: "ok"}, nil
	}
	r := NewRegistry(nil)
	r.Register(&Command{Name: "test", AcceptsArgs: false, Handler: handler})

	t.Run("nil invocation", func(t *testing.T) {
		_, err := r.Execute(context.Background(), nil)
		if err == nil {
			t.Error("expected error for nil invocation")
		}
	})

	t.Run("command not found", func(t *testing.T) {
		_, err := r.Execute(context.Background(), &Invocation{Name: "nonexistent"})
		if err == nil {
			t.Error("expected error for nonexistent command")
		}
	})

	t.Run("args rejected when AcceptsArgs is false", func(t *testing.T) {
		result, err := r.Execute(context.Background(), &Invocation{
			Name: "test",
			Args: "some args",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Error == "" {
			t.Error("expected error in result for rejected args")
		}
	})
}
