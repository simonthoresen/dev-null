package server

import (
	"fmt"
	"null-space/common"
	"sort"
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

// TabComplete performs context-aware tab completion on a slash-command input.
// input must start with '/'. playerNames is used for FirstArgIsPlayer commands.
// Repeated Tab presses cycle through candidates alphabetically.
// Returns (completed string, changed bool).
func (r *commandRegistry) TabComplete(input string, playerNames []string) (string, bool) {
	if !strings.HasPrefix(input, "/") {
		return input, false
	}
	trimmed := input[1:]

	// Split into words; detect whether there's a trailing space (empty partial).
	words := strings.Fields(trimmed)
	trailingSpace := strings.HasSuffix(trimmed, " ")

	if len(words) == 0 {
		return input, false
	}

	cmdName := words[0]

	// Still typing the command name — complete against registered command names.
	if len(words) == 1 && !trailingSpace {
		partial := cmdName
		var candidates []string
		for _, c := range r.All() {
			if strings.HasPrefix(strings.ToLower(c.Name), strings.ToLower(partial)) {
				candidates = append(candidates, c.Name)
			}
		}
		if len(candidates) == 0 {
			return input, false
		}
		sort.Strings(candidates)
		next := candidates[0]
		for i, c := range candidates {
			if strings.EqualFold(c, partial) {
				next = candidates[(i+1)%len(candidates)]
				break
			}
		}
		return "/" + next, true
	}

	cmd, ok := r.Get(cmdName)
	if !ok {
		return input, false
	}

	// Determine committed args (fully typed) and the partial being completed.
	var before []string
	var partial string
	if trailingSpace {
		before = words[1:]
		partial = ""
	} else {
		before = words[1 : len(words)-1]
		partial = words[len(words)-1]
	}

	// Gather candidates from the appropriate source.
	var candidates []string
	if cmd.Complete != nil {
		for _, c := range cmd.Complete(before) {
			if strings.HasPrefix(strings.ToLower(c), strings.ToLower(partial)) {
				candidates = append(candidates, c)
			}
		}
	} else if cmd.FirstArgIsPlayer && len(before) == 0 {
		for _, name := range playerNames {
			if strings.HasPrefix(strings.ToLower(name), strings.ToLower(partial)) {
				candidates = append(candidates, name)
			}
		}
	}

	if len(candidates) == 0 {
		return input, false
	}
	sort.Strings(candidates)

	// Cycle: if partial exactly matches a candidate, advance to the next.
	next := candidates[0]
	for i, c := range candidates {
		if strings.EqualFold(c, partial) {
			next = candidates[(i+1)%len(candidates)]
			break
		}
	}

	rebuilt := append([]string{cmdName}, before...)
	rebuilt = append(rebuilt, next)
	return "/" + strings.Join(rebuilt, " "), true
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
