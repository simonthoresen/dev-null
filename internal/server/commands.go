package server

import (
	"fmt"
	"null-space/internal/domain"
	"sort"
	"strings"
	"sync"
)

type commandRegistry struct {
	mu       sync.RWMutex
	commands map[string]domain.Command
}

func newCommandRegistry() *commandRegistry {
	return &commandRegistry{commands: make(map[string]domain.Command)}
}

func (r *commandRegistry) Register(cmd domain.Command) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commands[strings.ToLower(cmd.Name)] = cmd
}

func (r *commandRegistry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.commands, strings.ToLower(name))
}

func (r *commandRegistry) Get(name string) (domain.Command, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cmd, ok := r.commands[strings.ToLower(name)]
	return cmd, ok
}

func (r *commandRegistry) All() []domain.Command {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]domain.Command, 0, len(r.commands))
	for _, cmd := range r.commands {
		out = append(out, cmd)
	}
	return out
}

// Dispatch parses and runs a slash command. input must start with '/'.
func (r *commandRegistry) Dispatch(input string, ctx domain.CommandContext) {
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

// TabCandidates computes completion candidates for the current slash-command
// input. Returns the fixed prefix (everything before the partial token) and
// the sorted list of matching candidates. The caller is responsible for
// cycling through the list across repeated Tab presses.
func (r *commandRegistry) TabCandidates(input string, playerNames []string) (prefix string, candidates []string) {
	if !strings.HasPrefix(input, "/") {
		return
	}
	trimmed := input[1:]
	words := strings.Fields(trimmed)
	trailingSpace := strings.HasSuffix(trimmed, " ")

	if len(words) == 0 {
		return
	}

	cmdName := words[0]

	// Still typing the command name — complete against registered command names.
	if len(words) == 1 && !trailingSpace {
		partial := cmdName
		for _, c := range r.All() {
			if strings.HasPrefix(strings.ToLower(c.Name), strings.ToLower(partial)) {
				candidates = append(candidates, c.Name)
			}
		}
		sort.Strings(candidates)
		prefix = "/"
		return
	}

	cmd, ok := r.Get(cmdName)
	if !ok {
		return
	}

	var before []string
	var partial string
	if trailingSpace {
		before = words[1:]
		partial = ""
	} else {
		before = words[1 : len(words)-1]
		partial = words[len(words)-1]
	}

	var all []string
	if cmd.Complete != nil {
		all = cmd.Complete(before)
	} else if cmd.FirstArgIsPlayer && len(before) == 0 {
		all = playerNames
	}
	for _, c := range all {
		if strings.HasPrefix(strings.ToLower(c), strings.ToLower(partial)) {
			candidates = append(candidates, c)
		}
	}
	sort.Strings(candidates)

	rebuilt := append([]string{cmdName}, before...)
	prefix = "/" + strings.Join(rebuilt, " ") + " "
	return
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
