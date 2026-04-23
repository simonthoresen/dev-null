// orbits2.js — spike of the proposed game contract.
//
// What's different from orbits.js:
//
//   1. Single render(state, me, draw) function is used for BOTH pixels and
//      ascii. The existing Game.renderCanvas / Game.renderAscii hooks are
//      thin bridges that build the right `draw` adapter and dispatch — in
//      a real framework migration this bridging lives in Go, not here.
//
//   2. `me` is a resolved object (id, teamIdx, camera). The render function
//      never looks up players[pid] — that class of bug can't happen.
//
//   3. All gameplay state lives in Game.state. No module-level vars mutate
//      during gameplay. The planet/sun/star tables below are static data,
//      not state.
//
//   4. `draw` is polymorphic: draw.circle, draw.ellipse, draw.glow, etc.
//      Pixel and ASCII backends implement them differently; primitives
//      that don't make sense in ASCII (stars, glows, orbit trails) no-op.
//      Scale conversion is the one place the game must branch on mode,
//      because cells and pixels are fundamentally different units.

// ── Static world data (never mutated, never synced) ──────────────────────────

var SUN = { radius: 18, color: "#FFD700", glowOuter: "#FF6600", symbol: "☀" };

var PLANETS = [
    { name: "Mercury", symbol: "☿",
      orbitRadius: 55,  radius: 3,   color: "#A8A8A8", glowOuter: "#666666",
      orbitColor: "#1C1C1C", speed: 1.24,    angle0: 0.0, moons: [] },
    { name: "Venus", symbol: "♀",
      orbitRadius: 80,  radius: 6,   color: "#E8CC80", glowOuter: "#B09040",
      orbitColor: "#1E1E14", speed: 0.488,   angle0: 2.1, moons: [] },
    { name: "Earth", symbol: "⊕",
      orbitRadius: 110, radius: 7,   color: "#4488EE", glowOuter: "#2244AA",
      orbitColor: "#121838", speed: 0.3,     angle0: 4.5,
      moons: [ { name: "Moon", orbitRadius: 22, radius: 2.5, color: "#C8C8C8", speed: 2.5, angle0: 1.0 } ] },
    { name: "Mars", symbol: "♂",
      orbitRadius: 155, radius: 4.5, color: "#CC4422", glowOuter: "#882211",
      orbitColor: "#1E1010", speed: 0.16,    angle0: 1.0,
      moons: [ { name: "Phobos", orbitRadius: 10, radius: 1.5, color: "#AA9988", speed: 4.5, angle0: 0.5 },
               { name: "Deimos", orbitRadius: 16, radius: 1.2, color: "#BBAAAA", speed: 2.8, angle0: 3.2 } ] },
    { name: "Jupiter", symbol: "♃",
      orbitRadius: 280, radius: 19,  color: "#D4A870", glowOuter: "#A07030",
      orbitColor: "#1E1A12", speed: 0.0253,  angle0: 5.8,
      moons: [ { name: "Io",     orbitRadius: 32, radius: 2.8, color: "#FFD050", speed: 6.0, angle0: 2.2 },
               { name: "Europa", orbitRadius: 44, radius: 2.4, color: "#C8D8FF", speed: 3.8, angle0: 4.8 } ] },
    { name: "Saturn", symbol: "♄",
      orbitRadius: 390, radius: 15,  color: "#E8D888", glowOuter: "#C0A840",
      orbitColor: "#1E1E10", speed: 0.0102,  angle0: 3.2,
      ringInner: 21, ringOuter: 36, ringColor: "#C8A060",
      moons: [ { name: "Titan",     orbitRadius: 48, radius: 3.0, color: "#E8A040", speed: 1.8, angle0: 0.8 },
               { name: "Enceladus", orbitRadius: 28, radius: 1.5, color: "#EEEEFF", speed: 4.5, angle0: 5.5 } ] },
    { name: "Uranus", symbol: "♅",
      orbitRadius: 510, radius: 11,  color: "#7EC8DC", glowOuter: "#407090",
      orbitColor: "#0E1E20", speed: 0.00357, angle0: 0.7,
      moons: [ { name: "Titania", orbitRadius: 28, radius: 2.0, color: "#AACCCC", speed: 3.0, angle0: 2.5 },
               { name: "Oberon",  orbitRadius: 37, radius: 1.8, color: "#99AABB", speed: 2.1, angle0: 0.2 } ] },
    { name: "Neptune", symbol: "♆",
      orbitRadius: 620, radius: 10,  color: "#2855D8", glowOuter: "#1030A0",
      orbitColor: "#0E1020", speed: 0.00182, angle0: 5.1,
      moons: [ { name: "Triton", orbitRadius: 26, radius: 2.2, color: "#AACCEE", speed: 2.4, angle0: 1.5 } ] }
];

var STARS = [];
(function() {
    for (var i = 0; i < 300; i++) {
        STARS.push({ x: Math.random() * 2000 - 1000,
                     y: Math.random() * 2000 - 1000,
                     brightness: Math.random() * 0.6 + 0.4 });
    }
})();

// ── Draw adapters (framework-emulated; would be Go in the real migration) ────
//
// Both adapters expose the same surface. The game calls draw.circle(x, y, r,
// color), draw.ellipse(...), etc. Each adapter knows how to render the
// primitive in its target medium — or to skip it entirely when the primitive
// is meaningless (an orbit trail in a 100-cell terminal is visual noise).

function makePixelDraw(ctx, w, h) {
    return {
        mode: "pixels",
        width: w,
        height: h,
        unitScale: function(zoom) { return zoom * w / 1200; },  // 1 world unit → pixels

        clear: function(color) {
            ctx.setFillStyle(color);
            ctx.fillRect(0, 0, w, h);
        },
        point: function(x, y, brightness, color) {
            var sz = brightness * 1.5 + 0.5;
            ctx.setFillStyle(color);
            ctx.fillRect(x - sz / 2, y - sz / 2, sz, sz);
        },
        circle: function(x, y, r, color) {
            if (r < 0.4) { this.point(x, y, 1, color); return; }
            ctx.setFillStyle(color);
            ctx.fillCircle(x, y, r);
        },
        glow: function(x, y, radius, color) {
            var layers = 5;
            for (var i = layers; i >= 1; i--) {
                var r = radius * (i / layers);
                var a = Math.round((1 - i / layers) * 55 + 10).toString(16);
                if (a.length < 2) a = "0" + a;
                ctx.setFillStyle(color + a);
                ctx.fillCircle(x, y, r);
            }
        },
        ellipse: function(cx, cy, rx, ry, color, width) {
            ctx.setStrokeStyle(color);
            ctx.setLineWidth(width || 1);
            ctx.beginPath();
            var segs = 96;
            for (var i = 0; i <= segs; i++) {
                var a = (i / segs) * Math.PI * 2;
                var x = cx + Math.cos(a) * rx;
                var y = cy + Math.sin(a) * ry;
                if (i === 0) ctx.moveTo(x, y); else ctx.lineTo(x, y);
            }
            ctx.stroke();
        },
        arc: function(cx, cy, rx, ry, fromA, toA, color, width) {
            ctx.setStrokeStyle(color);
            ctx.setLineWidth(width || 1);
            ctx.beginPath();
            var segs = 48;
            for (var i = 0; i <= segs; i++) {
                var a = fromA + (i / segs) * (toA - fromA);
                var x = cx + Math.cos(a) * rx;
                var y = cy + Math.sin(a) * ry;
                if (i === 0) ctx.moveTo(x, y); else ctx.lineTo(x, y);
            }
            ctx.stroke();
        },
        // Body is a named "thing" — planets, the sun, etc. In pixels it's a
        // circle with glow. In ASCII it's the symbol. The primitive picks
        // the right representation automatically.
        body: function(x, y, worldR, color, glow, symbol) {
            if (glow) this.glow(x, y, worldR, glow);
            this.circle(x, y, worldR * 0.55, color);
        },
        text: function(x, y, str, color) {
            ctx.setFillStyle(color);
            ctx.fillText(str, x, y);
        }
    };
}

function makeAsciiDraw(buf, ox, oy, w, h) {
    return {
        mode: "ascii",
        width: w,
        height: h,
        unitScale: function(zoom) { return zoom * 0.1; },  // 1 world unit → cells

        clear: function(color) {
            buf.fill(ox, oy, w, h, " ", "#333333", color);
        },
        // Stars / orbit trails / glows are all visual noise in a terminal;
        // the primitive is present for API parity but draws nothing.
        point:   function() {},
        ellipse: function() {},
        arc:     function() {},
        glow:    function() {},
        circle: function(x, y, r, color) {
            var cx = Math.round(ox + x), cy = Math.round(oy + y);
            if (cx < ox || cx >= ox + w || cy < oy || cy >= oy + h) return;
            var ch = r < 0.6 ? "·" : r < 1.4 ? "•" : r < 3 ? "●" : "◉";
            buf.setChar(cx, cy, ch, color, null);
        },
        body: function(x, y, worldR, color, glow, symbol) {
            var cx = Math.round(ox + x), cy = Math.round(oy + y);
            if (cx < ox || cx >= ox + w || cy < oy || cy >= oy + h) return;
            buf.setChar(cx, cy, symbol || "●", color, null);
        },
        text: function(x, y, str, color) {
            buf.writeString(ox + Math.round(x), oy + Math.round(y), str, color, null);
        }
    };
}

// ── Render: one function for both modes ──────────────────────────────────────

function render(state, me, draw) {
    var cam   = me.camera;
    var t     = state.time;
    var cx    = draw.width  / 2;
    var cy    = draw.height / 2;
    var scale = draw.unitScale(cam.zoom);
    var tilt  = 1 - Math.abs(cam.tilt) * 0.6;

    function toScreen(wx, wy) {
        return {
            x: cx + (wx - cam.x) * scale,
            y: cy + (wy - cam.y) * scale * tilt
        };
    }

    draw.clear("#000011");

    // Background stars (no-ops in ASCII).
    for (var i = 0; i < STARS.length; i++) {
        var sp = toScreen(STARS[i].x, STARS[i].y);
        if (sp.x < 0 || sp.x >= draw.width || sp.y < 0 || sp.y >= draw.height) continue;
        var v  = Math.round(STARS[i].brightness * 200 + 55);
        var hx = v.toString(16); if (hx.length < 2) hx = "0" + hx;
        draw.point(sp.x, sp.y, STARS[i].brightness, "#" + hx + hx + hx);
    }

    // Planet orbit trails.
    var origin = toScreen(0, 0);
    for (var i = 0; i < PLANETS.length; i++) {
        var r = PLANETS[i].orbitRadius * scale;
        draw.ellipse(origin.x, origin.y, r, r * tilt, PLANETS[i].orbitColor, 1);
    }

    // Sun.
    draw.body(origin.x, origin.y, SUN.radius * scale, SUN.color, SUN.glowOuter, SUN.symbol);

    // Planets and their moons.
    for (var i = 0; i < PLANETS.length; i++) {
        var planet = PLANETS[i];
        var pa     = t * planet.speed + planet.angle0;
        var pwx    = Math.cos(pa) * planet.orbitRadius;
        var pwy    = Math.sin(pa) * planet.orbitRadius;
        var pPos   = toScreen(pwx, pwy);

        for (var j = 0; j < planet.moons.length; j++) {
            var mr = planet.moons[j].orbitRadius * scale;
            draw.ellipse(pPos.x, pPos.y, mr, mr * tilt, "#1A1A2A", 1);
        }

        if (planet.ringInner) {
            var ringMid   = (planet.ringInner + planet.ringOuter) / 2;
            var ringThick = Math.max(1.5, (planet.ringOuter - planet.ringInner) * scale);
            draw.arc(pPos.x, pPos.y, ringMid * scale, ringMid * scale * tilt,
                     Math.PI, 2 * Math.PI, planet.ringColor + "C0", ringThick);
        }

        draw.body(pPos.x, pPos.y, planet.radius * scale, planet.color,
                  planet.glowOuter, planet.symbol);

        if (planet.ringInner) {
            var ringMid2   = (planet.ringInner + planet.ringOuter) / 2;
            var ringThick2 = Math.max(1.5, (planet.ringOuter - planet.ringInner) * scale);
            draw.arc(pPos.x, pPos.y, ringMid2 * scale, ringMid2 * scale * tilt,
                     0, Math.PI, planet.ringColor + "C0", ringThick2);
        }

        for (var k = 0; k < planet.moons.length; k++) {
            var moon = planet.moons[k];
            var ma   = t * moon.speed + moon.angle0;
            var mPos = toScreen(pwx + Math.cos(ma) * moon.orbitRadius,
                                pwy + Math.sin(ma) * moon.orbitRadius);
            draw.circle(mPos.x, mPos.y, moon.radius * scale * 0.6, moon.color);
        }
    }

    if (draw.mode === "ascii") {
        draw.text(0, 0, "WASD:pan  ↑↓:tilt  +/-:zoom", "#555555");
    }
}

// ── Framework contract (current hooks; bridges to the new shape) ─────────────
//
// In the real migration everything below this line (except update/onInput/
// init/begin) becomes framework code. The game would only export update,
// input handlers, and render.

function defaultCamera() { return { x: 0, y: 0, zoom: 0.4, tilt: 0 }; }

// resolveMe mirrors what the framework would do before calling render: it
// turns a raw playerID into a "me" object the game can use directly. If the
// player isn't on any team yet, the framework draws a "connecting…" splash
// and doesn't invoke the game's render at all.
function resolveMe(pid) {
    var teamIdx = Game.state.players[pid];
    if (teamIdx === undefined) return null;
    if (!Game.state.cameras[teamIdx]) Game.state.cameras[teamIdx] = defaultCamera();
    return { id: pid, teamIdx: teamIdx, camera: Game.state.cameras[teamIdx] };
}

var Game = {
    gameName: "Orbits v2",
    teamRange: { min: 1, max: 8 },

    state: { time: 0, cameras: {}, players: {}, teamColors: [] },

    load:  function() {},

    begin: function() {
        var t = teams();
        Game.state.teamColors = [];
        for (var i = 0; i < t.length; i++) {
            Game.state.teamColors.push(t[i].color || "#FFFFFF");
            Game.state.cameras[i] = defaultCamera();
            for (var j = 0; j < t[i].players.length; j++) {
                Game.state.players[t[i].players[j].id] = i;
            }
        }
    },

    onPlayerLeave: function(pid) { delete Game.state.players[pid]; },

    onInput: function(pid, key) {
        var me = resolveMe(pid); if (!me) return;
        var cam = me.camera;
        var step = 12 / cam.zoom;
        switch (key) {
            case "w": cam.y -= step; break;
            case "s": cam.y += step; break;
            case "a": cam.x -= step; break;
            case "d": cam.x += step; break;
            case "up":          cam.tilt = Math.max(-0.8, cam.tilt - 0.05); break;
            case "down":        cam.tilt = Math.min( 0.8, cam.tilt + 0.05); break;
            case "+": case "=": cam.zoom = Math.min( 4.0, cam.zoom * 1.15); break;
            case "-":           cam.zoom = Math.max( 0.1, cam.zoom / 1.15); break;
        }
    },

    update: function(dt) { Game.state.time += dt; },

    renderCanvas: function(ctx, pid, w, h) {
        var me = resolveMe(pid);
        if (!me) { ctx.setFillStyle("#000011"); ctx.fillRect(0, 0, w, h); return; }
        render(Game.state, me, makePixelDraw(ctx, w, h));
    },

    renderAscii: function(buf, pid, ox, oy, w, h) {
        var me = resolveMe(pid);
        if (!me) { buf.fill(ox, oy, w, h, " ", "#333333", "#000011"); return; }
        render(Game.state, me, makeAsciiDraw(buf, ox, oy, w, h));
    },

    statusBar: function(pid) {
        var me = resolveMe(pid);
        if (!me) return "Orbits v2";
        return "Team " + (me.teamIdx + 1)
            + "  Zoom: " + me.camera.zoom.toFixed(2) + "x"
            + "  Tilt: " + (me.camera.tilt * 100).toFixed(0) + "%";
    },

    commandBar: function() { return "[WASD] pan  [↑↓] tilt  [+/-] zoom"; }
};
