package server

import (
	"fmt"
	"null-space/common"
	"strings"
	"sync"
)

type commandRegistry struct {
	mu       sync.RWMutex
	commands map[string]common.Command
}

func newCommandRegistry() *commandRegistry {
	return &commandRegistry{commands: make(map[string]common.Command)}
}

func (r *commandRegistry) Register(cmd common.Command) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commands[strings.ToLower(cmd.Name)] = cmd
}

func (r *commandRegistry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.commands, strings.ToLower(name))
}

func (r *commandRegistry) Get(name string) (common.Command, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cmd, ok := r.commands[strings.ToLower(name)]
	return cmd, ok
}

func (r *commandRegistry) All() []common.Command {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]common.Command, 0, len(r.commands))
	for _, cmd := range r.commands {
		out = append(out, cmd)
	}
	return out
}

// Dispatch parses and runs a slash command. input must start with '/'.
func (r *commandRegistry) Dispatch(input string, ctx common.CommandContext) {
	input = strings.TrimPrefix(input, "/")
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return
	}
	name := parts[0]
	args := parts[1:]

	cmd, ok := r.Get(name)
	if !ok {
		ctx.Reply(fmt.Sprintf("Unknown command: /%s — type /help for a list", name))
		return
	}
	if cmd.AdminOnly && !ctx.IsAdmin {
		ctx.Reply("Permission denied (admin only)")
		return
	}
	cmd.Handler(ctx, args)
}

// TabComplete returns player name completions for commands with FirstArgIsPlayer.
// input is the current text in the input field (including leading '/').
// Returns (completed string, changed bool).
func (r *commandRegistry) TabComplete(input string, playerNames []string) (string, bool) {
	if !strings.HasPrefix(input, "/") {
		return input, false
	}
	parts := strings.SplitN(input[1:], " ", 3)
	if len(parts) < 2 {
		return input, false
	}
	cmdName := parts[0]
	partial := parts[1]

	cmd, ok := r.Get(cmdName)
	if !ok || !cmd.FirstArgIsPlayer {
		return input, false
	}

	var matches []string
	for _, name := range playerNames {
		if strings.HasPrefix(strings.ToLower(name), strings.ToLower(partial)) {
			matches = append(matches, name)
		}
	}
	if len(matches) == 0 {
		return input, false
	}
	// cycle: if partial already matches first exactly, go to next
	completed := "/" + cmdName + " " + matches[0]
	if len(parts) == 3 {
		completed += " " + parts[2]
	}
	return completed, true
}

func ensureSSHFlag(cmd, flag string) string {
	if strings.Contains(cmd, " "+flag+" ") || strings.HasSuffix(cmd, " "+flag) {
		return cmd
	}
	if strings.HasPrefix(cmd, "ssh ") {
		return "ssh " + flag + " " + cmd[4:]
	}
	return cmd
}
