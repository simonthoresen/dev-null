package server

import (
	"testing"

	"dev-null/internal/domain"
)

func TestRegisterAndGet(t *testing.T) {
	r := newCommandRegistry()
	r.Register(domain.Command{Name: "test", Description: "a test command"})

	cmd, ok := r.Get("test")
	if !ok {
		t.Fatal("expected command to be found")
	}
	if cmd.Description != "a test command" {
		t.Fatalf("expected description, got %q", cmd.Description)
	}
}

func TestGetCaseInsensitive(t *testing.T) {
	r := newCommandRegistry()
	r.Register(domain.Command{Name: "Help"})

	if _, ok := r.Get("help"); !ok {
		t.Fatal("expected case-insensitive get to work")
	}
	if _, ok := r.Get("HELP"); !ok {
		t.Fatal("expected uppercase get to work")
	}
}

func TestUnregister(t *testing.T) {
	r := newCommandRegistry()
	r.Register(domain.Command{Name: "temp"})
	r.Unregister("temp")

	if _, ok := r.Get("temp"); ok {
		t.Fatal("expected command to be removed")
	}
}

func TestAll(t *testing.T) {
	r := newCommandRegistry()
	r.Register(domain.Command{Name: "alpha"})
	r.Register(domain.Command{Name: "beta"})

	all := r.All()
	if len(all) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(all))
	}
}

func TestDispatchCallsHandler(t *testing.T) {
	r := newCommandRegistry()
	called := false
	r.Register(domain.Command{
		Name: "ping",
		Handler: func(ctx domain.CommandContext, args []string) {
			called = true
			if len(args) != 1 || args[0] != "hello" {
				t.Fatalf("unexpected args: %v", args)
			}
		},
	})

	ctx := domain.CommandContext{IsAdmin: true, Reply: func(string) {}}
	r.Dispatch("/ping hello", ctx)
	if !called {
		t.Fatal("expected handler to be called")
	}
}

func TestDispatchUnknownCommand(t *testing.T) {
	r := newCommandRegistry()
	var reply string
	ctx := domain.CommandContext{
		Reply: func(s string) { reply = s },
	}
	r.Dispatch("/nonexistent", ctx)
	if reply == "" {
		t.Fatal("expected error reply for unknown command")
	}
}

func TestDispatchAdminOnly(t *testing.T) {
	r := newCommandRegistry()
	called := false
	r.Register(domain.Command{
		Name:      "secret",
		AdminOnly: true,
		Handler:   func(ctx domain.CommandContext, args []string) { called = true },
	})

	// Non-admin should be rejected.
	var reply string
	ctx := domain.CommandContext{
		IsAdmin: false,
		Reply:   func(s string) { reply = s },
	}
	r.Dispatch("/secret", ctx)
	if called {
		t.Fatal("handler should not be called for non-admin")
	}
	if reply == "" {
		t.Fatal("expected permission denied reply")
	}

	// Admin should succeed.
	ctx.IsAdmin = true
	r.Dispatch("/secret", ctx)
	if !called {
		t.Fatal("expected handler to be called for admin")
	}
}

func TestDispatchEmptyInput(t *testing.T) {
	r := newCommandRegistry()
	// Should not panic.
	r.Dispatch("/", domain.CommandContext{Reply: func(string) {}})
	r.Dispatch("", domain.CommandContext{Reply: func(string) {}})
}

func TestTabCandidatesCommandName(t *testing.T) {
	r := newCommandRegistry()
	r.Register(domain.Command{Name: "help"})
	r.Register(domain.Command{Name: "hello"})
	r.Register(domain.Command{Name: "kick"})

	prefix, candidates := r.TabCandidates("/hel", nil)
	if prefix != "/" {
		t.Fatalf("expected prefix '/', got %q", prefix)
	}
	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d: %v", len(candidates), candidates)
	}
	// Should be sorted.
	if candidates[0] != "hello" || candidates[1] != "help" {
		t.Fatalf("unexpected candidates: %v", candidates)
	}
}

func TestTabCandidatesWithComplete(t *testing.T) {
	r := newCommandRegistry()
	r.Register(domain.Command{
		Name: "game",
		Complete: func(before []string) []string {
			if len(before) == 0 {
				return []string{"load", "list", "unload"}
			}
			return nil
		},
	})

	prefix, candidates := r.TabCandidates("/game l", nil)
	if prefix != "/game " {
		t.Fatalf("expected prefix '/game ', got %q", prefix)
	}
	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates (list, load), got %d: %v", len(candidates), candidates)
	}
}

func TestTabCandidatesFirstArgIsPlayer(t *testing.T) {
	r := newCommandRegistry()
	r.Register(domain.Command{
		Name:             "msg",
		FirstArgIsPlayer: true,
	})

	players := []string{"alice", "bob", "adam"}
	prefix, candidates := r.TabCandidates("/msg a", players)
	if prefix != "/msg " {
		t.Fatalf("expected prefix '/msg ', got %q", prefix)
	}
	if len(candidates) != 2 {
		t.Fatalf("expected 2 (adam, alice), got %d: %v", len(candidates), candidates)
	}
}

func TestTabCandidatesNonSlash(t *testing.T) {
	r := newCommandRegistry()
	prefix, candidates := r.TabCandidates("hello", nil)
	if prefix != "" || candidates != nil {
		t.Fatal("expected empty results for non-slash input")
	}
}

func TestTabCandidatesTrailingSpace(t *testing.T) {
	r := newCommandRegistry()
	r.Register(domain.Command{
		Name: "game",
		Complete: func(before []string) []string {
			return []string{"load", "list"}
		},
	})

	prefix, candidates := r.TabCandidates("/game ", nil)
	if prefix != "/game " {
		t.Fatalf("expected prefix '/game ', got %q", prefix)
	}
	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d: %v", len(candidates), candidates)
	}
}

func TestEnsureSSHFlag(t *testing.T) {
	tests := []struct {
		cmd, flag, want string
	}{
		{"ssh user@host", "-o StrictHostKeyChecking=no", "ssh -o StrictHostKeyChecking=no user@host"},
		{"ssh -o StrictHostKeyChecking=no user@host", "-o StrictHostKeyChecking=no", "ssh -o StrictHostKeyChecking=no user@host"},
		{"other command", "-v", "other command"},
	}
	for _, tt := range tests {
		got := ensureSSHFlag(tt.cmd, tt.flag)
		if got != tt.want {
			t.Errorf("ensureSSHFlag(%q, %q) = %q, want %q", tt.cmd, tt.flag, got, tt.want)
		}
	}
}
