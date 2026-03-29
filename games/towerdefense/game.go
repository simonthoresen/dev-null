package towerdefense

import (
	"fmt"
	"log/slog"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"null-space/common"
)

const (
	worldWidth  = 256
	worldHeight = 256
)

var (
	currentPlayerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFF8E1")).Background(lipgloss.Color("#F59E0B"))
	otherPlayerStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#F8FAFC")).Background(lipgloss.Color("#0F172A"))
	grassGlyph         = lipgloss.NewStyle().Foreground(lipgloss.Color("#D9F99D")).Background(lipgloss.Color("#5E8C31")).Render("·")
	forestGlyph        = lipgloss.NewStyle().Foreground(lipgloss.Color("#BBF7D0")).Background(lipgloss.Color("#25603B")).Render("•")
	trailGlyph         = lipgloss.NewStyle().Foreground(lipgloss.Color("#FEF3C7")).Background(lipgloss.Color("#9A6B3A")).Render("=")
	treeGlyph          = lipgloss.NewStyle().Foreground(lipgloss.Color("#DCFCE7")).Background(lipgloss.Color("#0F3D24")).Render("♣")
	voidGlyph          = lipgloss.NewStyle().Background(lipgloss.Color("#111827")).Render(" ")
)

type tileType uint8

const (
	grassTile tileType = iota
	forestTile
	trailTile
	treeTile
)

type Game struct {
	mu      sync.RWMutex
	tiles   []tileType
	players map[string]*playerState
	spawn   common.Point
}

type playerState struct {
	ID       string
	Name     string
	Position common.Point
	Color    string
	Inputs   int
	Applied  int
	Blocked  int
}

func New() *Game {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	game := &Game{
		tiles:   make([]tileType, worldWidth*worldHeight),
		players: make(map[string]*playerState),
		spawn:   common.Point{X: worldWidth / 2, Y: worldHeight / 2},
	}
	game.generateMap(rng)
	game.spawn = game.findNearestWalkable(game.spawn)
	return game
}

func (g *Game) Init() []tea.Cmd {
	return nil
}

func (g *Game) Update(msg tea.Msg, playerID string) []tea.Cmd {
	g.mu.Lock()
	defer g.mu.Unlock()

	switch msg := msg.(type) {
	case common.PlayerJoinedMsg:
		spawn := g.findSpawnPosition()
		g.players[msg.PlayerID] = &playerState{
			ID:       msg.PlayerID,
			Name:     msg.Name,
			Position: spawn,
			Color:    msg.Color,
		}
	case common.PlayerLeftMsg:
		delete(g.players, msg.PlayerID)
	case common.MoveMsg:
		g.applyMovement(playerID, msg.Direction)
	case tea.KeyPressMsg:
		g.applyMovement(playerID, msg.String())
	}

	return nil
}

func (g *Game) applyMovement(playerID, direction string) {
	player := g.players[playerID]
	if player == nil {
		slog.Debug("movement ignored for unknown player", "player_id", playerID, "key", direction)
		return
	}

	dx, dy := 0, 0
	switch direction {
	case "up":
		dy = -1
	case "down":
		dy = 1
	case "left":
		dx = -1
	case "right":
		dx = 1
	default:
		return
	}
	player.Inputs++

	next := common.Point{X: player.Position.X + dx, Y: player.Position.Y + dy}
	if !g.inBounds(next) || g.tileAt(next) == treeTile {
		player.Blocked++
		slog.Debug("movement blocked", "player_id", playerID, "from_x", player.Position.X, "from_y", player.Position.Y, "to_x", next.X, "to_y", next.Y, "key", direction)
		return
	}
	player.Position = next
	player.Applied++
	slog.Debug("movement applied", "player_id", playerID, "x", player.Position.X, "y", player.Position.Y, "key", direction)
}

func (g *Game) View(playerID string, width, height int) string {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if width <= 0 || height <= 0 {
		return ""
	}

	player := g.players[playerID]
	if player == nil {
		return strings.Join(makeBlankRows(width, height), "\n")
	}

	minX, minY := g.cameraOrigin(player.Position, width, height)
	maxX := minInt(worldWidth, minX+width)
	maxY := minInt(worldHeight, minY+height)

	rows := make([]string, 0, height)
	for y := minY; y < maxY; y++ {
		var row strings.Builder
		for x := minX; x < maxX; x++ {
			point := common.Point{X: x, Y: y}
			row.WriteString(g.renderCell(point, playerID))
		}
		rows = append(rows, row.String())
	}

	for len(rows) < height {
		rows = append(rows, strings.Repeat(voidGlyph, width))
	}

	return strings.Join(rows, "\n")
}

func (g *Game) PlayerStatus(playerID string, width int) string {
	g.mu.RLock()
	defer g.mu.RUnlock()

	player := g.players[playerID]
	if player == nil {
		return truncateToWidth("world loading", width)
	}

	terrain := terrainName(g.tileAt(player.Position))
	nearby := make([]string, 0, len(g.players))
	for _, other := range g.players {
		if other.ID == player.ID {
			continue
		}
		if distance(player.Position, other.Position) <= 12 {
			nearby = append(nearby, other.Name)
		}
	}
	sort.Strings(nearby)
	nearbyText := "nearby none"
	if len(nearby) > 0 {
		nearbyText = "nearby " + strings.Join(nearby, ", ")
	}

	status := fmt.Sprintf("%s at %d,%d | terrain %s | map %dx%d | input %d/%d/%d rx/app/block | %s | Enter chats", player.Name, player.Position.X, player.Position.Y, terrain, worldWidth, worldHeight, player.Inputs, player.Applied, player.Blocked, nearbyText)
	return truncateToWidth(status, width)
}

func (g *Game) GetCommands() []common.Command {
	return nil
}

func (g *Game) generateMap(rng *rand.Rand) {
	for index := range g.tiles {
		g.tiles[index] = grassTile
	}

	for patch := 0; patch < 180; patch++ {
		center := common.Point{X: rng.Intn(worldWidth), Y: rng.Intn(worldHeight)}
		radius := rng.Intn(10) + 4
		g.paintPatch(center, radius, forestTile)
	}

	for trail := 0; trail < 18; trail++ {
		start := common.Point{X: rng.Intn(worldWidth), Y: rng.Intn(worldHeight)}
		end := common.Point{X: rng.Intn(worldWidth), Y: rng.Intn(worldHeight)}
		g.carveTrail(start, end, rng.Intn(2)+1)
	}

	g.clearArea(g.spawn, 8)

	for y := 0; y < worldHeight; y++ {
		for x := 0; x < worldWidth; x++ {
			point := common.Point{X: x, Y: y}
			tile := g.tileAt(point)
			if tile == trailTile {
				continue
			}
			chance := 0.05
			if tile == forestTile {
				chance = 0.24
			}
			if rng.Float64() < chance {
				g.setTile(point, treeTile)
			}
		}
	}

	g.clearArea(g.spawn, 4)
	g.clearBorder(1)
}

func (g *Game) clearBorder(thickness int) {
	for y := 0; y < worldHeight; y++ {
		for x := 0; x < worldWidth; x++ {
			if x >= thickness && x < worldWidth-thickness && y >= thickness && y < worldHeight-thickness {
				continue
			}
			g.setTile(common.Point{X: x, Y: y}, grassTile)
		}
	}
}

func (g *Game) paintPatch(center common.Point, radius int, tile tileType) {
	for y := center.Y - radius; y <= center.Y+radius; y++ {
		for x := center.X - radius; x <= center.X+radius; x++ {
			point := common.Point{X: x, Y: y}
			if !g.inBounds(point) {
				continue
			}
			if distance(center, point) <= radius+(radius/3) {
				g.setTile(point, tile)
			}
		}
	}
}

func (g *Game) carveTrail(start, end common.Point, halfWidth int) {
	current := start
	for current.X != end.X {
		step := 1
		if end.X < current.X {
			step = -1
		}
		g.paintTrailWidth(current, halfWidth)
		current.X += step
	}
	for current.Y != end.Y {
		step := 1
		if end.Y < current.Y {
			step = -1
		}
		g.paintTrailWidth(current, halfWidth)
		current.Y += step
	}
	g.paintTrailWidth(end, halfWidth)
}

func (g *Game) paintTrailWidth(center common.Point, halfWidth int) {
	for y := center.Y - halfWidth; y <= center.Y+halfWidth; y++ {
		for x := center.X - halfWidth; x <= center.X+halfWidth; x++ {
			point := common.Point{X: x, Y: y}
			if g.inBounds(point) {
				g.setTile(point, trailTile)
			}
		}
	}
}

func (g *Game) clearArea(center common.Point, radius int) {
	for y := center.Y - radius; y <= center.Y+radius; y++ {
		for x := center.X - radius; x <= center.X+radius; x++ {
			point := common.Point{X: x, Y: y}
			if !g.inBounds(point) {
				continue
			}
			if distance(center, point) <= radius {
				g.setTile(point, grassTile)
			}
		}
	}
}

func (g *Game) renderCell(point common.Point, currentPlayerID string) string {
	for _, player := range g.players {
		if player.Position != point {
			continue
		}
		if player.ID == currentPlayerID {
			return currentPlayerStyle.Render("☺")
		}
		return otherPlayerStyle.Foreground(lipgloss.Color(player.Color)).Render("☻")
	}

	switch g.tileAt(point) {
	case grassTile:
		return grassGlyph
	case forestTile:
		return forestGlyph
	case trailTile:
		return trailGlyph
	case treeTile:
		return treeGlyph
	default:
		return voidGlyph
	}
}

func (g *Game) findSpawnPosition() common.Point {
	spawn := g.findNearestWalkable(g.spawn)
	for _, player := range g.players {
		if player.Position == spawn {
			return g.findNearestWalkable(common.Point{X: spawn.X + 1, Y: spawn.Y + 1})
		}
	}
	return spawn
}

func (g *Game) findNearestWalkable(origin common.Point) common.Point {
	if g.inBounds(origin) && g.tileAt(origin) != treeTile {
		return origin
	}

	for radius := 1; radius < maxInt(worldWidth, worldHeight); radius++ {
		for y := origin.Y - radius; y <= origin.Y+radius; y++ {
			for x := origin.X - radius; x <= origin.X+radius; x++ {
				point := common.Point{X: x, Y: y}
				if !g.inBounds(point) || g.tileAt(point) == treeTile {
					continue
				}
				return point
			}
		}
	}

	return common.Point{X: 0, Y: 0}
}

func (g *Game) cameraOrigin(position common.Point, width, height int) (int, int) {
	halfWidth := width / 2
	halfHeight := height / 2
	minX := clampInt(position.X-halfWidth, 0, maxInt(0, worldWidth-width))
	minY := clampInt(position.Y-halfHeight, 0, maxInt(0, worldHeight-height))
	return minX, minY
}

func (g *Game) inBounds(point common.Point) bool {
	return point.X >= 0 && point.X < worldWidth && point.Y >= 0 && point.Y < worldHeight
}

func (g *Game) tileAt(point common.Point) tileType {
	return g.tiles[g.index(point)]
}

func (g *Game) setTile(point common.Point, tile tileType) {
	g.tiles[g.index(point)] = tile
}

func (g *Game) index(point common.Point) int {
	return point.Y*worldWidth + point.X
}

func terrainName(tile tileType) string {
	switch tile {
	case grassTile:
		return "grass"
	case forestTile:
		return "forest"
	case trailTile:
		return "trail"
	case treeTile:
		return "trees"
	default:
		return "unknown"
	}
}

func makeBlankRows(width, height int) []string {
	rows := make([]string, 0, height)
	for i := 0; i < height; i++ {
		rows = append(rows, strings.Repeat(" ", width))
	}
	return rows
}

func distance(a, b common.Point) int {
	dx := a.X - b.X
	if dx < 0 {
		dx = -dx
	}
	dy := a.Y - b.Y
	if dy < 0 {
		dy = -dy
	}
	return dx + dy
}

func clampInt(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func minInt(aValue, bValue int) int {
	if aValue < bValue {
		return aValue
	}
	return bValue
}

func maxInt(aValue, bValue int) int {
	if aValue > bValue {
		return aValue
	}
	return bValue
}

func truncateToWidth(value string, width int) string {
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	return string(runes[:width])
}
