package common

import tea "charm.land/bubbletea/v2"

type Game interface {
	Init() []tea.Cmd
	Update(msg tea.Msg, playerID string) []tea.Cmd
	View(playerID string, width, height int) string
	GetCommands() []Command
}

type CommandHandler func(ctx CommandContext, args []string) error

type Command struct {
	Name        string
	Usage       string
	Description string
	AdminOnly   bool
	Handler     CommandHandler
}

type CommandContext interface {
	CurrentPlayer() *Player
	Players() []*Player
	PlayerByName(name string) *Player
	AddSystemMessage(text string)
	AddPrivateMessage(text string)
	PasswordMatches(candidate string) bool
	KickPlayer(playerID string) error
	RequestRefresh()
}