package server

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"null-space/common"
)

type commandContext struct {
	app      *App
	playerID string
}

func (c commandContext) CurrentPlayer() *common.Player {
	return c.app.playerIdentity(c.playerID)
}

func (c commandContext) Players() []*common.Player {
	return c.app.state.ListPlayers()
}

func (c commandContext) PlayerByName(name string) *common.Player {
	return c.app.state.PlayerByName(name)
}

func (c commandContext) AddSystemMessage(text string) {
	c.app.addSystemMessage(text)
}

func (c commandContext) AddPrivateMessage(text string) {
	c.app.addPrivateMessage(c.playerID, text)
}

func (c commandContext) PasswordMatches(candidate string) bool {
	return candidate == c.app.adminPassword
}

func (c commandContext) KickPlayer(playerID string) error {
	return c.app.kickPlayer(playerID)
}

func (c commandContext) RequestRefresh() {
	c.app.broadcast(common.RefreshMsg{})
}

func (a *App) registerCommands(extra []common.Command) {
	a.registry = make(map[string]common.Command)
	for _, command := range append(a.coreCommands(), extra...) {
		a.registry[strings.ToLower(command.Name)] = command
	}
}

func (a *App) coreCommands() []common.Command {
	return []common.Command{
		{
			Name:        "admin",
			Usage:       "/admin <password>",
			Description: "Elevate the current session to admin.",
			Handler: func(ctx common.CommandContext, args []string) error {
				if len(args) != 1 {
					ctx.AddPrivateMessage("Usage: /admin <password>")
					return nil
				}
				if !ctx.PasswordMatches(args[0]) {
					ctx.AddPrivateMessage("Invalid admin password.")
					return nil
				}
				player := ctx.CurrentPlayer()
				if player == nil {
					return nil
				}
				a.state.SetPlayerAdmin(player.ID, true)
				ctx.AddPrivateMessage("Admin privileges granted.")
				ctx.RequestRefresh()
				return nil
			},
		},
		{
			Name:        "who",
			Usage:       "/who",
			Description: "List active players.",
			Handler: func(ctx common.CommandContext, args []string) error {
				players := ctx.Players()
				names := make([]string, 0, len(players))
				for _, player := range players {
					label := player.Name
					if player.IsAdmin {
						label += " (admin)"
					}
					names = append(names, label)
				}
				sort.Strings(names)
				ctx.AddPrivateMessage(fmt.Sprintf("Players online (%d): %s", len(names), strings.Join(names, ", ")))
				return nil
			},
		},
		{
			Name:        "help",
			Usage:       "/help",
			Description: "List available commands.",
			Handler: func(ctx common.CommandContext, args []string) error {
				player := ctx.CurrentPlayer()
				isAdmin := player != nil && player.IsAdmin
				commands := make([]common.Command, 0, len(a.registry))
				for _, command := range a.registry {
					if command.AdminOnly && !isAdmin {
						continue
					}
					commands = append(commands, command)
				}
				sort.Slice(commands, func(i, j int) bool {
					return commands[i].Name < commands[j].Name
				})
				for _, command := range commands {
					ctx.AddPrivateMessage(fmt.Sprintf("%s - %s", command.Usage, command.Description))
				}
				return nil
			},
		},
		{
			Name:        "kick",
			Usage:       "/kick <player>",
			Description: "Disconnect a player by name.",
			AdminOnly:   true,
			Handler: func(ctx common.CommandContext, args []string) error {
				if len(args) != 1 {
					ctx.AddPrivateMessage("Usage: /kick <player>")
					return nil
				}
				target := ctx.PlayerByName(args[0])
				if target == nil {
					ctx.AddPrivateMessage("Player not found.")
					return nil
				}
				if err := ctx.KickPlayer(target.ID); err != nil {
					return err
				}
				ctx.AddSystemMessage(fmt.Sprintf("%s was kicked from the tunnel.", target.Name))
				return nil
			},
		},
		{
			Name:        "quit",
			Usage:       "/quit",
			Description: "Stop the server and disconnect everyone.",
			AdminOnly:   true,
			Handler: func(ctx common.CommandContext, args []string) error {
				if len(args) != 0 {
					ctx.AddPrivateMessage("Usage: /quit")
					return nil
				}
				ctx.AddSystemMessage("Server shutdown requested. Disconnecting all players...")
				if !a.requestShutdown() {
					ctx.AddPrivateMessage("Shutdown handler is not available.")
				}
				return nil
			},
		},
		{
			Name:        "info",
			Usage:       "/info",
			Description: "Show connection strings for this server.",
			Handler: func(ctx common.CommandContext, args []string) error {
				localCmd, directCmd, tunnelJoin, oneLiner := a.connectionInfo()
				ctx.AddPrivateMessage(fmt.Sprintf("Local:   %s", localCmd))
				if directCmd != "" {
					ctx.AddPrivateMessage(fmt.Sprintf("Direct:  %s", directCmd))
				}
				if tunnelJoin != "" {
					ctx.AddPrivateMessage(fmt.Sprintf("Relay:   %s", ensureSSHFlag(tunnelJoin, "-t")))
				} else {
					ctx.AddPrivateMessage("Relay:   not available")
				}
				ctx.AddPrivateMessage(fmt.Sprintf("Connect: %s", oneLiner))
				return nil
			},
		},
	}
}

func (a *App) executeCommand(playerID, raw string) {
	parts := strings.Fields(strings.TrimPrefix(raw, "/"))
	if len(parts) == 0 {
		return
	}

	command, ok := a.registry[strings.ToLower(parts[0])]
	if !ok {
		slog.Warn("unknown command", "player_id", playerID, "raw", raw)
		a.addPrivateMessage(playerID, "Unknown command. Try /help.")
		return
	}

	player := a.playerIdentity(playerID)
	if command.AdminOnly && (player == nil || !player.IsAdmin) {
		slog.Warn("admin command denied", "player_id", playerID, "command", command.Name)
		a.addPrivateMessage(playerID, "Permission Denied")
		return
	}

	slog.Info("executing command", "player_id", playerID, "command", command.Name, "args", parts[1:])
	ctx := commandContext{app: a, playerID: playerID}
	if err := command.Handler(ctx, parts[1:]); err != nil {
		slog.Error("command failed", "player_id", playerID, "command", command.Name, "error", err)
		a.addPrivateMessage(playerID, fmt.Sprintf("Command failed: %v", err))
	}
}

func formatChatLine(author, body string) string {
	return fmt.Sprintf("[%s] <%s> %s", time.Now().Format("15:04:05"), author, body)
}

func formatSystemLine(body string) string {
	return fmt.Sprintf("[%s] [system] %s", time.Now().Format("15:04:05"), body)
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

func formatPinggyLine(body string) string {
	return fmt.Sprintf("[%s] [pinggy] %s", time.Now().Format("15:04:05"), body)
}

func formatPrivateLine(body string) string {
	return fmt.Sprintf("[%s] [local] %s", time.Now().Format("15:04:05"), body)
}
