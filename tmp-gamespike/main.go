package main
import (
	"fmt"
	"os"
	"path/filepath"
	"time"
	"dev-null/internal/domain"
	"dev-null/internal/engine"
	"dev-null/internal/render"
)
func main() {
	path := os.Args[1]
	abs, _ := filepath.Abs(path)
	chatCh := make(chan domain.Message, 256)
	go func() { for range chatCh {} }()
	clock := &domain.MockClock{T: time.Now()}
	g, err := engine.LoadGame(abs, func(s string){fmt.Println("[log]", s)}, chatCh, clock, filepath.Dir(abs))
	if err != nil { fmt.Println("LoadGame err:", err); os.Exit(1) }
	g.Load(nil)
	g.(interface{ SetTeamsCache([]map[string]any) }).SetTeamsCache([]map[string]any{
		{"name":"Red","color":"#FF0000","players":[]any{map[string]any{"id":"p1","name":"alice"}}},
	})
	g.OnPlayerJoin("p1","alice")
	g.Begin()
	for i := 0; i < 30; i++ { g.Update(0.1) }
	fmt.Println("statusBar:", g.StatusBar("p1"))
	fmt.Println("commandBar:", g.CommandBar("p1"))
	buf := render.NewImageBuffer(40, 12)
	g.RenderAscii(buf, "p1", 0, 0, 40, 12)
	fmt.Println("renderAscii output (may be blank — voyage has no renderAscii hook):")
	for y := 0; y < 3; y++ {
		var line string
		for x := 0; x < 40; x++ { line += string(buf.CharAt(x, y)) }
		fmt.Println(line)
	}
	if png := g.RenderCanvas("p1", 120, 80); png != nil {
		fmt.Println("canvas PNG bytes:", len(png))
	}
	fmt.Println("OK")
}
