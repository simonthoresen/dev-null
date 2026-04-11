// orbits.js — Solar System: all 8 planets with moons orbiting the sun.
// Load with: /game load orbits
//
// Controls:
//   WASD       — pan camera
//   ↑↓ arrows  — tilt view (look at orbital plane from an angle)
//   +/-        — zoom in/out

// ── Sun ────────────────────────────────────────────────────────────────────

var SUN = { radius: 18, color: "#FFD700", glowOuter: "#FF6600" };

// ── Planets ─────────────────────────────────────────────────────────────────
//
// orbitRadius: world units (arbitrary scale).
// radius:      visual radius in world units.
// speed:       angular velocity in rad/s. Earth = 0.3 rad/s ≈ 21 s/orbit.
//              Ratios match real Kepler periods: T ∝ a^(3/2).
// angle0:      initial orbital angle (spread planets apart at load time).
// orbitColor:  subtle colour for the orbit trail.
// glowOuter:   outer glow colour.
// symbol:      Unicode astronomical symbol used in the text fallback renderer.
//
// Moons: up to 2 per planet. Speed values are visually interesting, not to scale.

var PLANETS = [
    {
        name: "Mercury", symbol: "☿",
        orbitRadius: 55,  radius: 3,   color: "#A8A8A8", glowOuter: "#666666",
        orbitColor: "#1C1C1C", speed: 1.24, angle0: 0.0,
        moons: []
    },
    {
        name: "Venus", symbol: "♀",
        orbitRadius: 80,  radius: 6,   color: "#E8CC80", glowOuter: "#B09040",
        orbitColor: "#1E1E14", speed: 0.488, angle0: 2.1,
        moons: []
    },
    {
        name: "Earth", symbol: "⊕",
        orbitRadius: 110, radius: 7,   color: "#4488EE", glowOuter: "#2244AA",
        orbitColor: "#121838", speed: 0.3, angle0: 4.5,
        moons: [
            { name: "Moon",    orbitRadius: 22, radius: 2.5, color: "#C8C8C8", speed: 2.5, angle0: 1.0 }
        ]
    },
    {
        name: "Mars", symbol: "♂",
        orbitRadius: 155, radius: 4.5, color: "#CC4422", glowOuter: "#882211",
        orbitColor: "#1E1010", speed: 0.16, angle0: 1.0,
        moons: [
            { name: "Phobos", orbitRadius: 10, radius: 1.5, color: "#AA9988", speed: 4.5, angle0: 0.5 },
            { name: "Deimos", orbitRadius: 16, radius: 1.2, color: "#BBAAAA", speed: 2.8, angle0: 3.2 }
        ]
    },
    {
        name: "Jupiter", symbol: "♃",
        orbitRadius: 280, radius: 19,  color: "#D4A870", glowOuter: "#A07030",
        orbitColor: "#1E1A12", speed: 0.0253, angle0: 5.8,
        moons: [
            { name: "Io",     orbitRadius: 32, radius: 2.8, color: "#FFD050", speed: 6.0, angle0: 2.2 },
            { name: "Europa", orbitRadius: 44, radius: 2.4, color: "#C8D8FF", speed: 3.8, angle0: 4.8 }
        ]
    },
    {
        name: "Saturn", symbol: "♄",
        orbitRadius: 390, radius: 15,  color: "#E8D888", glowOuter: "#C0A840",
        orbitColor: "#1E1E10", speed: 0.0102, angle0: 3.2,
        ringInner: 21, ringOuter: 36, ringColor: "#C8A060",
        moons: [
            { name: "Titan",     orbitRadius: 48, radius: 3.0, color: "#E8A040", speed: 1.8, angle0: 0.8 },
            { name: "Enceladus", orbitRadius: 28, radius: 1.5, color: "#EEEEFF", speed: 4.5, angle0: 5.5 }
        ]
    },
    {
        name: "Uranus", symbol: "♅",
        orbitRadius: 510, radius: 11,  color: "#7EC8DC", glowOuter: "#407090",
        orbitColor: "#0E1E20", speed: 0.00357, angle0: 0.7,
        moons: [
            { name: "Titania", orbitRadius: 28, radius: 2.0, color: "#AACCCC", speed: 3.0, angle0: 2.5 },
            { name: "Oberon",  orbitRadius: 37, radius: 1.8, color: "#99AABB", speed: 2.1, angle0: 0.2 }
        ]
    },
    {
        name: "Neptune", symbol: "♆",
        orbitRadius: 620, radius: 10,  color: "#2855D8", glowOuter: "#1030A0",
        orbitColor: "#0E1020", speed: 0.00182, angle0: 5.1,
        moons: [
            { name: "Triton", orbitRadius: 26, radius: 2.2, color: "#AACCEE", speed: 2.4, angle0: 1.5 }
        ]
    }
];

// ── Stars ───────────────────────────────────────────────────────────────────

var STARS = [];
for (var i = 0; i < 300; i++) {
    STARS.push({
        x:          Math.random() * 2000 - 1000,
        y:          Math.random() * 2000 - 1000,
        brightness: Math.random() * 0.6 + 0.4
    });
}

// ── Camera ──────────────────────────────────────────────────────────────────

function defaultCamera() {
    // zoom=0.4 shows the full solar system (Neptune at ~620 world units) in a
    // typical 120-column terminal.
    return { x: 0, y: 0, zoom: 0.4, tilt: 0 };
}

function getCamera(playerID) {
    var teamIdx = Game.state.players[playerID];
    if (teamIdx === undefined) teamIdx = 0;
    if (!Game.state.cameras[teamIdx]) {
        Game.state.cameras[teamIdx] = defaultCamera();
    }
    return Game.state.cameras[teamIdx];
}

// ── Game object ─────────────────────────────────────────────────────────────

var Game = {
    gameName: "Orbits",
    teamRange: { min: 1, max: 8 },

    state: {
        time:       0,
        cameras:    {},   // teamIndex → {x, y, zoom, tilt}
        players:    {},   // playerID  → teamIndex
        teamColors: []
    },

    load: function(savedState) {},

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

    onPlayerLeave: function(playerID) {
        delete Game.state.players[playerID];
    },

    onInput: function(playerID, key) {
        var cam = getCamera(playerID);
        var moveSpeed = 12 / cam.zoom;
        switch (key) {
            case "w": cam.y -= moveSpeed; break;
            case "s": cam.y += moveSpeed; break;
            case "a": cam.x -= moveSpeed; break;
            case "d": cam.x += moveSpeed; break;
            case "up":    cam.tilt = Math.max(-0.8, cam.tilt - 0.05); break;
            case "down":  cam.tilt = Math.min(0.8,  cam.tilt + 0.05); break;
            case "+": case "=": cam.zoom = Math.min(4.0, cam.zoom * 1.15); break;
            case "-":           cam.zoom = Math.max(0.1, cam.zoom / 1.15); break;
        }
    },

    update: function(dt) {
        Game.state.time += dt;
    },

    // ── Ascii fallback renderer (SSH without canvas support) ─────────────────

    renderAscii: function(buf, playerID, ox, oy, w, h) {
        var cam = getCamera(playerID);
        var t   = Game.state.time;

        // Cell-space scale: multiply world coords by this to get cells from center.
        var cellScale  = cam.zoom * 0.1;
        var tiltFactor = 1 - Math.abs(cam.tilt) * 0.6;

        buf.fill(ox, oy, w, h, " ", "#333333", "#000011");

        function toCell(wx, wy) {
            return {
                x: Math.round(ox + w/2 + (wx - cam.x) * cellScale),
                y: Math.round(oy + h/2 + (wy - cam.y) * cellScale * tiltFactor)
            };
        }

        // Sun
        var sun = toCell(0, 0);
        if (sun.x >= ox && sun.x < ox+w && sun.y >= oy && sun.y < oy+h) {
            buf.setChar(sun.x, sun.y, "☀", "#FFD700", null);
        }

        // Planets
        for (var i = 0; i < PLANETS.length; i++) {
            var p     = PLANETS[i];
            var angle = t * p.speed + p.angle0;
            var sc    = toCell(Math.cos(angle) * p.orbitRadius, Math.sin(angle) * p.orbitRadius);
            if (sc.x >= ox && sc.x < ox+w && sc.y >= oy && sc.y < oy+h) {
                buf.setChar(sc.x, sc.y, p.symbol, p.color, null);
            }
        }

        // HUD
        buf.writeString(ox, oy, "WASD:pan  ↑↓:tilt  +/-:zoom", "#555555", null);
    },

    // ── Canvas renderer ──────────────────────────────────────────────────────
    //
    // Canvas is w×h pixels where each pixel is visually square.
    // For Quadrant (SSH) mode the engine passes w=cols*2, h=rows*4.
    // For Canvas HD (GUI) mode the engine passes the actual window pixel size.
    // In both cases scale = cam.zoom * w / 1200 normalises to a virtual 1200-px
    // wide viewport so zoom levels are consistent across client types.

    renderCanvas: function(ctx, playerID, w, h) {
        var cam        = getCamera(playerID);
        var cx         = w / 2;
        var cy         = h / 2;
        var scale      = cam.zoom * w / 1200;
        var tiltFactor = 1 - Math.abs(cam.tilt) * 0.6;
        var t          = Game.state.time;

        function toScreen(wx, wy) {
            return {
                x: cx + (wx - cam.x) * scale,
                y: cy + (wy - cam.y) * scale * tiltFactor
            };
        }

        // Background
        ctx.setFillStyle("#000011");
        ctx.fillRect(0, 0, w, h);

        // Stars
        for (var i = 0; i < STARS.length; i++) {
            var sp = toScreen(STARS[i].x, STARS[i].y);
            if (sp.x >= 0 && sp.x < w && sp.y >= 0 && sp.y < h) {
                var v   = Math.round(STARS[i].brightness * 200 + 55);
                var hx  = v.toString(16);
                if (hx.length < 2) hx = "0" + hx;
                ctx.setFillStyle("#" + hx + hx + hx);
                var sz = STARS[i].brightness * 1.5 + 0.5;
                ctx.fillRect(sp.x - sz/2, sp.y - sz/2, sz, sz);
            }
        }

        // Planet orbit trails
        for (var i = 0; i < PLANETS.length; i++) {
            drawOrbitEllipse(ctx, 0, 0, PLANETS[i].orbitRadius,
                PLANETS[i].orbitColor, 1, scale, tiltFactor, cam, cx, cy);
        }

        // Sun glow + body
        var sunPos = toScreen(0, 0);
        drawGlow(ctx, sunPos.x, sunPos.y, SUN.radius * scale, SUN.glowOuter);
        ctx.setFillStyle(SUN.color);
        ctx.fillCircle(sunPos.x, sunPos.y, SUN.radius * scale * 0.55);

        // Planets and their moons
        for (var i = 0; i < PLANETS.length; i++) {
            var planet = PLANETS[i];
            var pAngle = t * planet.speed + planet.angle0;
            var pwx    = Math.cos(pAngle) * planet.orbitRadius;
            var pwy    = Math.sin(pAngle) * planet.orbitRadius;
            var pPos   = toScreen(pwx, pwy);

            // Moon orbit trails
            for (var j = 0; j < planet.moons.length; j++) {
                drawOrbitEllipse(ctx, pwx, pwy, planet.moons[j].orbitRadius,
                    "#1A1A2A", 1, scale, tiltFactor, cam, cx, cy);
            }

            // Saturn rings — back half (behind planet, drawn first)
            if (planet.ringInner) {
                drawRingHalf(ctx, pPos, planet, scale, tiltFactor, Math.PI, 2 * Math.PI);
            }

            // Planet body + glow
            drawGlow(ctx, pPos.x, pPos.y, planet.radius * scale * 1.0, planet.glowOuter);
            ctx.setFillStyle(planet.color);
            ctx.fillCircle(pPos.x, pPos.y, planet.radius * scale * 0.55);

            // Saturn rings — front half (in front of planet, drawn after)
            if (planet.ringInner) {
                drawRingHalf(ctx, pPos, planet, scale, tiltFactor, 0, Math.PI);
            }

            // Moons
            for (var j = 0; j < planet.moons.length; j++) {
                var moon   = planet.moons[j];
                var mAngle = t * moon.speed + moon.angle0;
                var mPos   = toScreen(
                    pwx + Math.cos(mAngle) * moon.orbitRadius,
                    pwy + Math.sin(mAngle) * moon.orbitRadius
                );
                ctx.setFillStyle(moon.color);
                ctx.fillCircle(mPos.x, mPos.y, moon.radius * scale * 0.6);
            }
        }
    },

    statusBar: function(playerID) {
        var cam      = getCamera(playerID);
        var teamIdx  = Game.state.players[playerID] || 0;
        return "Team " + (teamIdx + 1)
            + "  Zoom: " + cam.zoom.toFixed(2) + "x"
            + "  Tilt: " + (cam.tilt * 100).toFixed(0) + "%";
    },

    commandBar: function(playerID) {
        return "[WASD] pan  [↑↓] tilt  [+/-] zoom";
    }
};

// ── Drawing helpers ──────────────────────────────────────────────────────────

// Draw a complete orbit ellipse as a single closed path.
function drawOrbitEllipse(ctx, centerWX, centerWY, orbitR, color, lineWidth, scale, tiltFactor, cam, cx, cy) {
    ctx.setStrokeStyle(color);
    ctx.setLineWidth(lineWidth || 1);
    ctx.beginPath();
    var segs = 96;
    for (var i = 0; i <= segs; i++) {
        var a  = (i / segs) * Math.PI * 2;
        var sx = cx + (centerWX + Math.cos(a) * orbitR - cam.x) * scale;
        var sy = cy + (centerWY + Math.sin(a) * orbitR - cam.y) * scale * tiltFactor;
        if (i === 0) ctx.moveTo(sx, sy);
        else         ctx.lineTo(sx, sy);
    }
    ctx.stroke();
}

// Draw one half of Saturn's ring arc (fromAngle → toAngle).
// Angles 0→π = front half (in front of planet), π→2π = back half (behind planet).
function drawRingHalf(ctx, pPos, planet, scale, tiltFactor, fromAngle, toAngle) {
    var ringMid   = (planet.ringInner + planet.ringOuter) / 2;
    var ringThick = Math.max(1.5, (planet.ringOuter - planet.ringInner) * scale);
    var rx        = ringMid * scale;
    var ry        = ringMid * scale * tiltFactor;

    ctx.setStrokeStyle(planet.ringColor + "C0");
    ctx.setLineWidth(ringThick);
    ctx.beginPath();
    var segs  = 48;
    var first = true;
    for (var i = 0; i <= segs; i++) {
        var a  = fromAngle + (i / segs) * (toAngle - fromAngle);
        var sx = pPos.x + Math.cos(a) * rx;
        var sy = pPos.y + Math.sin(a) * ry;
        if (first) { ctx.moveTo(sx, sy); first = false; }
        else        ctx.lineTo(sx, sy);
    }
    ctx.stroke();
}

// Radial glow: concentric filled circles fading outward.
function drawGlow(ctx, x, y, radius, outerColor) {
    var layers = 5;
    for (var i = layers; i >= 1; i--) {
        var r     = radius * (i / layers);
        var alpha = Math.round((1 - i / layers) * 55 + 10);
        var hex   = alpha.toString(16);
        if (hex.length < 2) hex = "0" + hex;
        ctx.setFillStyle(outerColor + hex);
        ctx.fillCircle(x, y, r);
    }
}
