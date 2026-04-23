// orbits.js — Solar System: all 8 planets with moons orbiting the sun.
// Load with: /game load orbits
//
// Game contract v2. Cameras are per-team (the whole team shares one view),
// so we provide a custom resolveMe that maps playerID → team camera.
//
// Controls:
//   WASD       — pan camera
//   ↑↓ arrows  — tilt view (look at orbital plane from an angle)
//   +/-        — zoom in/out

// ── Static data ─────────────────────────────────────────────────────────────

var SUN = { radius: 18, color: "#FFD700", glowOuter: "#FF6600" };

var PLANETS = [
    { name: "Mercury", symbol: "☿",
      orbitRadius: 55,  radius: 3,   color: "#A8A8A8", glowOuter: "#666666",
      orbitColor: "#1C1C1C", speed: 1.24, angle0: 0.0, moons: [] },
    { name: "Venus", symbol: "♀",
      orbitRadius: 80,  radius: 6,   color: "#E8CC80", glowOuter: "#B09040",
      orbitColor: "#1E1E14", speed: 0.488, angle0: 2.1, moons: [] },
    { name: "Earth", symbol: "⊕",
      orbitRadius: 110, radius: 7,   color: "#4488EE", glowOuter: "#2244AA",
      orbitColor: "#121838", speed: 0.3, angle0: 4.5,
      moons: [ { name: "Moon", orbitRadius: 22, radius: 2.5, color: "#C8C8C8", speed: 2.5, angle0: 1.0 } ] },
    { name: "Mars", symbol: "♂",
      orbitRadius: 155, radius: 4.5, color: "#CC4422", glowOuter: "#882211",
      orbitColor: "#1E1010", speed: 0.16, angle0: 1.0,
      moons: [ { name: "Phobos", orbitRadius: 10, radius: 1.5, color: "#AA9988", speed: 4.5, angle0: 0.5 },
               { name: "Deimos", orbitRadius: 16, radius: 1.2, color: "#BBAAAA", speed: 2.8, angle0: 3.2 } ] },
    { name: "Jupiter", symbol: "♃",
      orbitRadius: 280, radius: 19,  color: "#D4A870", glowOuter: "#A07030",
      orbitColor: "#1E1A12", speed: 0.0253, angle0: 5.8,
      moons: [ { name: "Io",     orbitRadius: 32, radius: 2.8, color: "#FFD050", speed: 6.0, angle0: 2.2 },
               { name: "Europa", orbitRadius: 44, radius: 2.4, color: "#C8D8FF", speed: 3.8, angle0: 4.8 } ] },
    { name: "Saturn", symbol: "♄",
      orbitRadius: 390, radius: 15,  color: "#E8D888", glowOuter: "#C0A840",
      orbitColor: "#1E1E10", speed: 0.0102, angle0: 3.2,
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

function defaultCamera() { return { x: 0, y: 0, zoom: 0.4, tilt: 0 }; }

// ── Canvas helpers ──────────────────────────────────────────────────────────

function drawOrbitEllipse(canvas, centerWX, centerWY, orbitR, color, lineWidth, scale, tiltFactor, cam, cx, cy) {
    canvas.setStrokeStyle(color);
    canvas.setLineWidth(lineWidth || 1);
    canvas.beginPath();
    var segs = 96;
    for (var i = 0; i <= segs; i++) {
        var a  = (i / segs) * Math.PI * 2;
        var sx = cx + (centerWX + Math.cos(a) * orbitR - cam.x) * scale;
        var sy = cy + (centerWY + Math.sin(a) * orbitR - cam.y) * scale * tiltFactor;
        if (i === 0) canvas.moveTo(sx, sy);
        else         canvas.lineTo(sx, sy);
    }
    canvas.stroke();
}

function drawRingHalf(canvas, pPos, planet, scale, tiltFactor, fromAngle, toAngle) {
    var ringMid   = (planet.ringInner + planet.ringOuter) / 2;
    var ringThick = Math.max(1.5, (planet.ringOuter - planet.ringInner) * scale);
    var rx        = ringMid * scale;
    var ry        = ringMid * scale * tiltFactor;
    canvas.setStrokeStyle(planet.ringColor + "C0");
    canvas.setLineWidth(ringThick);
    canvas.beginPath();
    var segs  = 48;
    var first = true;
    for (var i = 0; i <= segs; i++) {
        var a  = fromAngle + (i / segs) * (toAngle - fromAngle);
        var sx = pPos.x + Math.cos(a) * rx;
        var sy = pPos.y + Math.sin(a) * ry;
        if (first) { canvas.moveTo(sx, sy); first = false; }
        else        canvas.lineTo(sx, sy);
    }
    canvas.stroke();
}

function drawGlow(canvas, x, y, radius, outerColor) {
    var layers = 5;
    for (var i = layers; i >= 1; i--) {
        var r     = radius * (i / layers);
        var alpha = Math.round((1 - i / layers) * 55 + 10);
        var hex   = alpha.toString(16);
        if (hex.length < 2) hex = "0" + hex;
        canvas.setFillStyle(outerColor + hex);
        canvas.fillCircle(x, y, r);
    }
}

// ── Game contract v2 ────────────────────────────────────────────────────────

var Game = {
    gameName: "Orbits",
    contract: 2,
    teamRange: { min: 1, max: 8 },

    init: function(ctx) {
        return {
            time:        0,
            cameras:     {},  // teamIndex → {x, y, zoom, tilt}
            playerTeams: {}   // playerID → teamIndex
        };
    },

    // Custom resolveMe: orbits' "me" is the player's team and their team's
    // shared camera, not a per-player state.players[pid] record.
    resolveMe: function(state, playerID) {
        var teamIdx = state.playerTeams[playerID];
        if (teamIdx === undefined) teamIdx = 0;
        if (!state.cameras[teamIdx]) state.cameras[teamIdx] = defaultCamera();
        return { id: playerID, teamIdx: teamIdx, camera: state.cameras[teamIdx] };
    },

    begin: function(state, ctx) {
        var t = state.teams || [];
        for (var i = 0; i < t.length; i++) {
            state.cameras[i] = defaultCamera();
            for (var j = 0; j < t[i].players.length; j++) {
                state.playerTeams[t[i].players[j].id] = i;
            }
        }
    },

    update: function(state, dt, events, ctx) {
        state.time += dt;
        for (var i = 0; i < events.length; i++) {
            var e = events[i];
            if (e.type === "leave") {
                delete state.playerTeams[e.playerID];
                continue;
            }
            if (e.type !== "input") continue;
            var teamIdx = state.playerTeams[e.playerID];
            if (teamIdx === undefined) continue;
            var cam = state.cameras[teamIdx];
            if (!cam) continue;
            var step = 12 / cam.zoom;
            switch (e.key) {
                case "w": cam.y -= step; break;
                case "s": cam.y += step; break;
                case "a": cam.x -= step; break;
                case "d": cam.x += step; break;
                case "up":          cam.tilt = Math.max(-0.8, cam.tilt - 0.05); break;
                case "down":        cam.tilt = Math.min( 0.8, cam.tilt + 0.05); break;
                case "+": case "=": cam.zoom = Math.min( 4.0, cam.zoom * 1.15); break;
                case "-":           cam.zoom = Math.max( 0.1, cam.zoom / 1.15); break;
            }
        }
    },

    // ── Canvas renderer ────────────────────────────────────────────────────
    renderCanvas: function(state, me, canvas) {
        var cam        = me.camera;
        var w          = canvas.width;
        var h          = canvas.height;
        var cx         = w / 2;
        var cy         = h / 2;
        var scale      = cam.zoom * w / 1200;
        var tiltFactor = 1 - Math.abs(cam.tilt) * 0.6;
        var t          = state.time;

        function toScreen(wx, wy) {
            return { x: cx + (wx - cam.x) * scale,
                     y: cy + (wy - cam.y) * scale * tiltFactor };
        }

        canvas.setFillStyle("#000011");
        canvas.fillRect(0, 0, w, h);

        for (var i = 0; i < STARS.length; i++) {
            var sp = toScreen(STARS[i].x, STARS[i].y);
            if (sp.x < 0 || sp.x >= w || sp.y < 0 || sp.y >= h) continue;
            var v  = Math.round(STARS[i].brightness * 200 + 55);
            var hx = v.toString(16); if (hx.length < 2) hx = "0" + hx;
            canvas.setFillStyle("#" + hx + hx + hx);
            var sz = STARS[i].brightness * 1.5 + 0.5;
            canvas.fillRect(sp.x - sz/2, sp.y - sz/2, sz, sz);
        }

        for (var i = 0; i < PLANETS.length; i++) {
            drawOrbitEllipse(canvas, 0, 0, PLANETS[i].orbitRadius,
                PLANETS[i].orbitColor, 1, scale, tiltFactor, cam, cx, cy);
        }

        var sunPos = toScreen(0, 0);
        drawGlow(canvas, sunPos.x, sunPos.y, SUN.radius * scale, SUN.glowOuter);
        canvas.setFillStyle(SUN.color);
        canvas.fillCircle(sunPos.x, sunPos.y, SUN.radius * scale * 0.55);

        for (var i = 0; i < PLANETS.length; i++) {
            var planet = PLANETS[i];
            var pAngle = t * planet.speed + planet.angle0;
            var pwx    = Math.cos(pAngle) * planet.orbitRadius;
            var pwy    = Math.sin(pAngle) * planet.orbitRadius;
            var pPos   = toScreen(pwx, pwy);

            for (var j = 0; j < planet.moons.length; j++) {
                drawOrbitEllipse(canvas, pwx, pwy, planet.moons[j].orbitRadius,
                    "#1A1A2A", 1, scale, tiltFactor, cam, cx, cy);
            }

            if (planet.ringInner) {
                drawRingHalf(canvas, pPos, planet, scale, tiltFactor, Math.PI, 2 * Math.PI);
            }

            drawGlow(canvas, pPos.x, pPos.y, planet.radius * scale * 1.0, planet.glowOuter);
            canvas.setFillStyle(planet.color);
            canvas.fillCircle(pPos.x, pPos.y, planet.radius * scale * 0.55);

            if (planet.ringInner) {
                drawRingHalf(canvas, pPos, planet, scale, tiltFactor, 0, Math.PI);
            }

            for (var j = 0; j < planet.moons.length; j++) {
                var moon   = planet.moons[j];
                var mAngle = t * moon.speed + moon.angle0;
                var mPos   = toScreen(
                    pwx + Math.cos(mAngle) * moon.orbitRadius,
                    pwy + Math.sin(mAngle) * moon.orbitRadius
                );
                canvas.setFillStyle(moon.color);
                canvas.fillCircle(mPos.x, mPos.y, moon.radius * scale * 0.6);
            }
        }
    },

    // ── ASCII fallback: top-down symbol view ─────────────────────────────────
    renderAscii: function(state, me, cells) {
        var cam        = me.camera;
        var w          = cells.width;
        var h          = cells.height;
        var t          = state.time;
        var cellScale  = cam.zoom * 0.1;
        var tiltFactor = 1 - Math.abs(cam.tilt) * 0.6;

        cells.fill(0, 0, w, h, " ", "#333333", "#000011");

        function toCell(wx, wy) {
            return { x: Math.round(w / 2 + (wx - cam.x) * cellScale),
                     y: Math.round(h / 2 + (wy - cam.y) * cellScale * tiltFactor) };
        }

        var sun = toCell(0, 0);
        if (sun.x >= 0 && sun.x < w && sun.y >= 0 && sun.y < h) {
            cells.setChar(sun.x, sun.y, "☀", "#FFD700", null);
        }
        for (var i = 0; i < PLANETS.length; i++) {
            var p     = PLANETS[i];
            var angle = t * p.speed + p.angle0;
            var sc    = toCell(Math.cos(angle) * p.orbitRadius, Math.sin(angle) * p.orbitRadius);
            if (sc.x >= 0 && sc.x < w && sc.y >= 0 && sc.y < h) {
                cells.setChar(sc.x, sc.y, p.symbol, p.color, null);
            }
        }
        cells.writeString(0, 0, "WASD:pan  ↑↓:tilt  +/-:zoom", "#555555", null);
    },

    statusBar: function(state, me) {
        return "Team " + (me.teamIdx + 1)
            + "  Zoom: " + me.camera.zoom.toFixed(2) + "x"
            + "  Tilt: " + (me.camera.tilt * 100).toFixed(0) + "%";
    },

    commandBar: function(state, me) {
        return "[WASD] pan  [↑↓] tilt  [+/-] zoom";
    }
};
