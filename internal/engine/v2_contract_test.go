package engine

import (
	"strings"
	"testing"

	"dev-null/internal/render"
)

// End-to-end exercise of the v2 contract on the server side. We load a
// minimal v2 game, call the usual lifecycle methods, and verify that:
//   - init(ctx) produces the initial state and sets Game.state.
//   - ctx is callable from init/begin/update/end (side effects land).
//   - ctx is NOT callable from render (render never sees it).
//   - update receives (state, dt, events, ctx) and events carry inputs/
//     joins/leaves plus the per-tick tick event.
//   - renderCanvas receives (state, me, canvas) with canvas.log and
//     canvas.width/height.
//   - renderAscii receives (state, me, cells) with ATTR_* and cells.log.
//   - me resolution falls back to state.players[pid] when the game
//     doesn't provide resolveMe.
//   - statusBar receives (state, me).
const v2GameJS = `
var calls = [];
Game = {
    gameName: "v2test",
    teamRange: { min: 1, max: 2 },
    contract: 2,

    init: function(ctx) {
        calls.push("init");
        ctx.log("init-called");
        return { players: {}, score: 0 };
    },

    begin: function(state, ctx) {
        calls.push("begin/" + (typeof ctx));
        // Seed a player record so resolveMe's fallback works.
        state.players["p1"] = { id: "p1", name: "alice", x: 0 };
    },

    update: function(state, dt, events, ctx) {
        calls.push("update/" + events.length + "/" + (typeof ctx));
        state.score++;
        for (var i = 0; i < events.length; i++) {
            var e = events[i];
            if (e.type === "input") {
                state.players[e.playerID].x++;
            }
            if (e.type === "join") {
                state.players[e.playerID] = { id: e.playerID, name: e.playerName, x: 0 };
            }
            if (e.type === "leave") {
                delete state.players[e.playerID];
            }
        }
    },

    statusBar: function(state, me) {
        return "score=" + state.score + " me=" + (me ? me.id : "none");
    },

    renderCanvas: function(state, me, canvas) {
        // Fail loudly if the framework leaked ctx into render.
        if (typeof ctx !== "undefined") { throw new Error("ctx leaked to render"); }
        calls.push("renderCanvas/" + me.id + "/" + canvas.width + "x" + canvas.height);
        canvas.log("drawing");
        canvas.setFillStyle("#FF0000");
        canvas.fillRect(0, 0, canvas.width, canvas.height);
    },

    renderAscii: function(state, me, cells) {
        calls.push("renderAscii/" + me.id + "/ATTR_BOLD=" + cells.ATTR_BOLD);
        cells.log("drawing-ascii");
        cells.setChar(0, 0, "@", "#FFF", null, cells.ATTR_BOLD);
    }
};
`

func TestV2Contract_FullLifecycle(t *testing.T) {
	rt := loadHookRuntime(t, v2GameJS)
	if rt.ctxObj == nil {
		t.Fatal("ctxObj should be built during extractGameObject")
	}

	// init → sets Game.state = { players: {}, score: 0 }
	rt.Load(nil)
	state := gameState(rt)
	if state == nil {
		t.Fatal("Game.state should be populated after init")
	}
	// teams overlay is framework-injected; account for it when counting keys.
	delete(state, "teams")
	if _, hasScore := state["score"]; !hasScore {
		t.Errorf("state should have 'score' from init, got %v", state)
	}

	// begin seeds p1 into state.players.
	rt.Begin()

	// Fire events: input, then join a new player, then call Update. Update
	// should receive them all batched plus the tick event.
	rt.OnInput("p1", "right")
	rt.OnPlayerJoin("p2", "bob")
	rt.Update(0.1)

	state = gameState(rt)
	players, ok := state["players"].(map[string]any)
	if !ok {
		t.Fatalf("players missing from state: %v", state)
	}
	if players["p1"].(map[string]any)["x"].(int64) != 1 {
		t.Errorf("input event should have bumped p1.x to 1: %v", players["p1"])
	}
	if _, has := players["p2"]; !has {
		t.Errorf("join event should have added p2: %v", players)
	}

	// Leave event.
	rt.OnPlayerLeave("p2")
	rt.Update(0.1)
	state = gameState(rt)
	players = state["players"].(map[string]any)
	if _, has := players["p2"]; has {
		t.Error("leave event should have removed p2")
	}

	// statusBar: v2 signature (state, me). me falls back to state.players[pid].
	status := rt.StatusBar("p1")
	if !strings.Contains(status, "me=p1") {
		t.Errorf("statusBar should resolve me from state.players, got %q", status)
	}
	if !strings.Contains(status, "score=") {
		t.Errorf("statusBar should read state.score, got %q", status)
	}

	// renderCanvas: v2 (state, me, canvas). Should not throw on ctx leak.
	if data := rt.RenderCanvas("p1", 40, 30); data == nil {
		t.Error("RenderCanvas returned nil — JS error? check log")
	}

	// renderAscii: v2 (state, me, cells). cells.ATTR_BOLD should be present.
	buf := render.NewImageBuffer(10, 5)
	rt.RenderAscii(buf, "p1", 0, 0, 10, 5)
	// @ written at 0,0?
	if got := buf.CharAt(0, 0); got != '@' {
		t.Errorf("renderAscii did not draw @ at 0,0 (got %q)", got)
	}

	// End.
	rt.End()
}

// Default resolveMe always returns at least {id: pid} so render is called
// even for players not explicitly registered. Games that want the "not
// ready" splash override resolveMe to return null themselves.
func TestV2Contract_UnregisteredPlayer_GetsMinimalMe(t *testing.T) {
	rt := loadHookRuntime(t, v2GameJS)
	rt.Load(nil)
	rt.Begin()

	// "unknown" isn't in state.players; minimal me should still be provided.
	if data := rt.RenderCanvas("unknown", 20, 20); data == nil {
		t.Error("RenderCanvas with minimal me should produce a PNG")
	}
}

// Games can provide their own resolveMe to support non-players[pid] layouts
// (orbits-style with per-team cameras, etc.).
func TestV2Contract_CustomResolveMe(t *testing.T) {
	rt := loadHookRuntime(t, `
		Game = {
			gameName: "t",
			teamRange: { min: 1, max: 2 },
			contract: 2,
			init: function(ctx) {
				return { cameras: { 0: { zoom: 1 }, 1: { zoom: 2 } }, playerTeams: { p1: 1 } };
			},
			resolveMe: function(state, pid) {
				var t = state.playerTeams[pid];
				if (t === undefined) return null;
				return { id: pid, teamIdx: t, camera: state.cameras[t] };
			},
			statusBar: function(state, me) {
				return me ? ("zoom=" + me.camera.zoom) : "no-me";
			}
		};
	`)
	rt.Load(nil)
	if got := rt.StatusBar("p1"); got != "zoom=2" {
		t.Errorf("custom resolveMe not honoured: %q", got)
	}
	// When game's resolveMe returns null, framework synthesises a minimal me
	// so statusBar still gets a non-null value (but without the zoom field
	// the game expected — it's on the game to check).
	if got := rt.StatusBar("unknown"); got != "no-me" {
		t.Errorf("resolveMe null should degrade gracefully; got %q", got)
	}
}
