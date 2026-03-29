package towerdefense

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"null-space/common"
)

const (
	worldWidth       = 200
	worldHeight      = 200
	defaultCredits   = 12
	towerCost        = 2
	enemyHealth      = 4
	spawnInterval    = 10
	enemyStepEvery   = 2
	towerFireEvery   = 4
	projectileTTL    = 2
	towerRange       = 6
	nearbyLegendRows = 1
)

type Game struct {
	mu          sync.RWMutex
	path        []common.Point
	pathCells   map[common.Point]struct{}
	players     map[string]*playerState
	towers      map[common.Point]*tower
	enemies     []*enemy
	projectiles []*projectile
	tickCount   int
	spawnBurst  int
}

type playerState struct {
	ID      string
	Name    string
	Position common.Point
	Color   string
	Credits int
}

type tower struct {
	Position common.Point
	Cooldown int
}

type enemy struct {
	PathIndex int
	Health    int
	Stride    int
}

type projectile struct {
	Position common.Point
	TTL      int
}

func New() *Game {
	path := buildPath()
	pathCells := make(map[common.Point]struct{}, len(path))
	for _, point := range path {
		pathCells[point] = struct{}{}
	}
	return &Game{
		path:      path,
		pathCells: pathCells,
		players:   make(map[string]*playerState),
		towers:    make(map[common.Point]*tower),
		enemies:   make([]*enemy, 0, 16),
	}
}

func (g *Game) Init() []tea.Cmd {
	return nil
}

func (g *Game) Update(msg tea.Msg, playerID string) []tea.Cmd {
	g.mu.Lock()
	defer g.mu.Unlock()

	switch msg := msg.(type) {
	case common.PlayerJoinedMsg:
		g.players[msg.PlayerID] = &playerState{
			ID:       msg.PlayerID,
			Name:     msg.Name,
			Position: msg.Position,
			Color:    msg.Color,
			Credits:  defaultCredits,
		}
	case common.PlayerLeftMsg:
		delete(g.players, msg.PlayerID)
	case common.TickMsg:
		g.tickCount++
		if g.tickCount%spawnInterval == 0 {
			g.spawnEnemy()
		}
		if g.spawnBurst > 0 {
			g.spawnEnemy()
			g.spawnBurst--
		}
		g.stepEnemies()
		g.fireTowers()
		g.stepProjectiles()
	case tea.KeyPressMsg:
		player := g.players[playerID]
		if player == nil {
			break
		}
		switch msg.String() {
		case "up":
			player.Position.Y = maxInt(0, player.Position.Y-1)
		case "down":
			player.Position.Y = minInt(worldHeight-1, player.Position.Y+1)
		case "left":
			player.Position.X = maxInt(0, player.Position.X-1)
		case "right":
			player.Position.X = minInt(worldWidth-1, player.Position.X+1)
		case "space":
			g.placeTower(player)
		}
	}

	return nil
}

func (g *Game) View(playerID string, width, height int) string {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if width <= 0 || height <= 0 {
		return ""
	}

	player := g.players[playerID]
	if player == nil {
		return strings.Repeat(" ", width)
	}

	boardHeight := maxInt(1, height-nearbyLegendRows)
	halfWidth := width / 2
	halfHeight := boardHeight / 2
	minX := clampInt(player.Position.X-halfWidth, 0, maxInt(0, worldWidth-width))
	minY := clampInt(player.Position.Y-halfHeight, 0, maxInt(0, worldHeight-boardHeight))
	maxX := minX + width
	maxY := minY + boardHeight

	enemyMap := make(map[common.Point]struct{}, len(g.enemies))
	for _, target := range g.enemies {
		point := g.path[target.PathIndex]
		enemyMap[point] = struct{}{}
	}

	projectileMap := make(map[common.Point]struct{}, len(g.projectiles))
	for _, shot := range g.projectiles {
		projectileMap[shot.Position] = struct{}{}
	}

	var rows []string
	for y := minY; y < maxY; y++ {
		var row strings.Builder
		for x := minX; x < maxX; x++ {
			point := common.Point{X: x, Y: y}
			cell := g.renderCell(point, playerID, enemyMap, projectileMap)
			row.WriteString(cell)
		}
		rows = append(rows, row.String())
	}

	legend := g.renderLegend(player, minX, minY, maxX, maxY, width)
	rows = append(rows, legend)
	if len(rows) > height {
		rows = rows[:height]
	}
	for len(rows) < height {
		rows = append(rows, strings.Repeat(" ", width))
	}
	return strings.Join(rows, "\n")
}

func (g *Game) GetCommands() []common.Command {
	return []common.Command{
		{
			Name:        "spawnwave",
			Usage:       "/spawnwave",
			Description: "Spawn a short enemy wave.",
			AdminOnly:   true,
			Handler: func(ctx common.CommandContext, args []string) error {
				g.mu.Lock()
				g.spawnBurst += 5
				g.mu.Unlock()
				ctx.AddSystemMessage("An admin forced a new wave.")
				ctx.RequestRefresh()
				return nil
			},
		},
		{
			Name:        "center",
			Usage:       "/center",
			Description: "Snap your camera anchor back to the center of the map.",
			Handler: func(ctx common.CommandContext, args []string) error {
				player := ctx.CurrentPlayer()
				if player == nil {
					return nil
				}
				g.mu.Lock()
				if state, ok := g.players[player.ID]; ok {
					state.Position = common.Point{X: worldWidth / 2, Y: worldHeight / 2}
				}
				g.mu.Unlock()
				ctx.AddPrivateMessage("Camera anchor reset to center.")
				ctx.RequestRefresh()
				return nil
			},
		},
	}
}

func (g *Game) renderCell(point common.Point, currentPlayerID string, enemyMap, projectileMap map[common.Point]struct{}) string {
	for _, player := range g.players {
		if player.Position == point {
			glyph := "X"
			if player.ID == currentPlayerID {
				glyph = "@"
			}
			return lipgloss.NewStyle().Foreground(lipgloss.Color(player.Color)).Render(glyph)
		}
	}
	if _, ok := projectileMap[point]; ok {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#F4A261")).Render("*")
	}
	if _, ok := enemyMap[point]; ok {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#FF4D6D")).Render("e")
	}
	if _, ok := g.towers[point]; ok {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#FFD166")).Render("T")
	}
	if _, ok := g.pathCells[point]; ok {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#5C677D")).Render("░")
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("#1F2933")).Render("·")
}

func (g *Game) renderLegend(player *playerState, minX, minY, maxX, maxY, width int) string {
	nearby := make([]string, 0, len(g.players))
	for _, other := range g.players {
		if other.ID == player.ID {
			continue
		}
		if other.Position.X >= minX && other.Position.X < maxX && other.Position.Y >= minY && other.Position.Y < maxY {
			nearby = append(nearby, other.Name)
		}
	}
	sort.Strings(nearby)
	legend := fmt.Sprintf("@ %d,%d | credits %d | towers [T] | enemies e | nearby %s", player.Position.X, player.Position.Y, player.Credits, strings.Join(nearby, ", "))
	if len(nearby) == 0 {
		legend = fmt.Sprintf("@ %d,%d | credits %d | towers [T] | enemies e | nearby none", player.Position.X, player.Position.Y, player.Credits)
	}
	return lipgloss.NewStyle().Faint(true).Width(width).Render(truncateToWidth(legend, width))
}

func (g *Game) placeTower(player *playerState) {
	if player.Credits < towerCost {
		return
	}
	if _, onPath := g.pathCells[player.Position]; onPath {
		return
	}
	if _, exists := g.towers[player.Position]; exists {
		return
	}
	g.towers[player.Position] = &tower{Position: player.Position}
	player.Credits -= towerCost
}

func (g *Game) spawnEnemy() {
	g.enemies = append(g.enemies, &enemy{Health: enemyHealth})
}

func (g *Game) stepEnemies() {
	nextEnemies := make([]*enemy, 0, len(g.enemies))
	for _, target := range g.enemies {
		target.Stride++
		if target.Stride%enemyStepEvery == 0 {
			target.PathIndex++
		}
		if target.Health <= 0 {
			continue
		}
		if target.PathIndex >= len(g.path) {
			continue
		}
		nextEnemies = append(nextEnemies, target)
	}
	g.enemies = nextEnemies
}

func (g *Game) fireTowers() {
	if len(g.enemies) == 0 {
		return
	}
	for _, structure := range g.towers {
		structure.Cooldown++
		if structure.Cooldown%towerFireEvery != 0 {
			continue
		}
		for _, target := range g.enemies {
			point := g.path[target.PathIndex]
			if distance(structure.Position, point) > towerRange {
				continue
			}
			target.Health--
			g.projectiles = append(g.projectiles, &projectile{Position: point, TTL: projectileTTL})
			if target.Health <= 0 {
				for _, player := range g.players {
					player.Credits++
				}
			}
			break
		}
	}

	survivors := make([]*enemy, 0, len(g.enemies))
	for _, target := range g.enemies {
		if target.Health > 0 {
			survivors = append(survivors, target)
		}
	}
	g.enemies = survivors
}

func (g *Game) stepProjectiles() {
	active := make([]*projectile, 0, len(g.projectiles))
	for _, shot := range g.projectiles {
		shot.TTL--
		if shot.TTL > 0 {
			active = append(active, shot)
		}
	}
	g.projectiles = active
}

func buildPath() []common.Point {
	waypoints := []common.Point{{10, 20}, {190, 20}, {190, 60}, {20, 60}, {20, 110}, {180, 110}, {180, 160}, {30, 160}}
	path := make([]common.Point, 0, 400)
	for index := 0; index < len(waypoints)-1; index++ {
		start := waypoints[index]
		end := waypoints[index+1]
		if start.X == end.X {
			step := 1
			if end.Y < start.Y {
				step = -1
			}
			for y := start.Y; y != end.Y; y += step {
				path = append(path, common.Point{X: start.X, Y: y})
			}
		} else {
			step := 1
			if end.X < start.X {
				step = -1
			}
			for x := start.X; x != end.X; x += step {
				path = append(path, common.Point{X: x, Y: start.Y})
			}
		}
	}
	path = append(path, waypoints[len(waypoints)-1])
	return path
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