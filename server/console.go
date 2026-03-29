package server

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"null-space/common"
)

const localConsolePlayerID = "local-admin"

func (a *App) EnableLocalConsole(ctx context.Context, reader io.Reader, writer io.Writer) {
	player := &common.Player{
		ID:          localConsolePlayerID,
		Name:        "admin",
		Position:    common.Point{X: 100, Y: 100},
		IsAdmin:     true,
		Color:       "#FFFFFF",
		ConnectedAt: time.Now(),
	}

	a.mu.Lock()
	a.consolePlayer = player.ID
	a.consoleWriter = writer
	a.privateHistory[player.ID] = nil
	a.mu.Unlock()

	a.state.AddPlayer(player)
	a.handleGameMessage(common.PlayerJoinedMsg{
		PlayerID: player.ID,
		Name:     player.Name,
		Position: player.Position,
		Color:    player.Color,
	}, player.ID)
	a.addSystemMessage("admin joined from the local console.")
	a.writeConsoleLine(formatPrivateLine("local admin console ready. Type chat text or /commands. Press Ctrl+C to stop."))

	go func() {
		<-ctx.Done()
		a.disableLocalConsole(player.ID, false)
	}()

	go a.runLocalConsoleInput(ctx, reader, player.ID)
}

func (a *App) runLocalConsoleInput(ctx context.Context, reader io.Reader, playerID string) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 1024), 1024*1024)

	for scanner.Scan() {
		if ctx.Err() != nil {
			return
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "/") {
			a.executeCommand(playerID, line)
			continue
		}

		a.addChatMessage(playerID, line)
	}

	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		a.writeConsoleLine(formatPrivateLine(fmt.Sprintf("console input error: %v", err)))
	}

	a.disableLocalConsole(playerID, true)
}

func (a *App) disableLocalConsole(playerID string, announce bool) {
	player := a.state.GetPlayer(playerID)
	if player == nil {
		return
	}

	if announce {
		a.appendChatLine(formatSystemLine(fmt.Sprintf("%s left the local console.", player.Name)))
	}

	a.handleGameMessage(common.PlayerLeftMsg{PlayerID: playerID}, playerID)
	a.state.RemovePlayer(playerID)

	a.mu.Lock()
	if a.consolePlayer == playerID {
		a.consolePlayer = ""
		a.consoleWriter = nil
	}
	delete(a.privateHistory, playerID)
	a.mu.Unlock()

	a.broadcast(common.RefreshMsg{})
}
