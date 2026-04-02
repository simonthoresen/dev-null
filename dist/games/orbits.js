// orbits.js — canvas rendering tech demo: moon orbiting planet orbiting sun
// Load with: /game load orbits
// Requires: enhanced client + /canvas scale 8 (or similar)
//
// Controls:
//   WASD        — move camera horizontally
//   Arrow Up/Down — tilt camera
//   +/-         — zoom in/out
//
// Each team gets its own camera. Players on the same team share a camera.


// Orbital parameters
var SUN = { radius: 30, color: "#FFD700" };
var PLANET = { orbitRadius: 120, radius: 12, color: "#4488FF", speed: 0.3 };
var MOON = { orbitRadius: 28, radius: 5, color: "#CCCCCC", speed: 1.2 };

// Stars (fixed background)
var STARS = [];
for (var i = 0; i < 200; i++) {
    STARS.push({
        x: Math.random() * 2000 - 1000,
        y: Math.random() * 2000 - 1000,
        brightness: Math.random() * 0.6 + 0.4
    });
}

function defaultCamera() {
    return { x: 0, y: 0, zoom: 1.0, tilt: 0 };
}

function getCamera(playerID) {
    var teamIdx = Game.state.players[playerID];
    if (teamIdx === undefined) teamIdx = 0;
    if (!Game.state.cameras[teamIdx]) {
        Game.state.cameras[teamIdx] = defaultCamera();
    }
    return Game.state.cameras[teamIdx];
}

var Game = {
    gameName: "Orbits",
    teamRange: { min: 1, max: 8 },

    state: {
        time: 0,
        cameras: {},  // teamIndex → {x, y, zoom, tilt}
        players: {},  // playerID → teamIndex
        teamColors: []
    },

    init: function(savedState) {},

    start: function() {
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
        var moveSpeed = 8 / cam.zoom;

        switch (key) {
            case "w": cam.y -= moveSpeed; break;
            case "s": cam.y += moveSpeed; break;
            case "a": cam.x -= moveSpeed; break;
            case "d": cam.x += moveSpeed; break;
            case "up":   cam.tilt = Math.max(-0.8, cam.tilt - 0.05); break;
            case "down": cam.tilt = Math.min(0.8, cam.tilt + 0.05); break;
            case "+": case "=": cam.zoom = Math.min(4.0, cam.zoom * 1.1); break;
            case "-": cam.zoom = Math.max(0.2, cam.zoom / 1.1); break;
        }
    },

    update: function(dt) {
        Game.state.time += dt;
    },

    // Cell-based render: fallback for regular SSH clients.
    render: function(buf, playerID, ox, oy, w, h) {
        var cam = getCamera(playerID);
        var cx = w / 2, cy = h / 2;

        // Background
        buf.fill(ox, oy, w, h, " ", "#333333", "#000011");

        // Sun position (world origin)
        var sunX = Math.round(cx - cam.x * cam.zoom * 0.1);
        var sunY = Math.round(cy - cam.y * cam.zoom * 0.1);

        // Planet orbit
        var planetAngle = Game.state.time * PLANET.speed;
        var planetWX = Math.cos(planetAngle) * PLANET.orbitRadius * 0.1;
        var planetWY = Math.sin(planetAngle) * PLANET.orbitRadius * 0.1 * (1 - Math.abs(cam.tilt) * 0.5);
        var planetX = Math.round(sunX + planetWX * cam.zoom);
        var planetY = Math.round(sunY + planetWY * cam.zoom);

        // Moon orbit around planet
        var moonAngle = Game.state.time * MOON.speed;
        var moonWX = Math.cos(moonAngle) * MOON.orbitRadius * 0.1;
        var moonWY = Math.sin(moonAngle) * MOON.orbitRadius * 0.1 * (1 - Math.abs(cam.tilt) * 0.5);
        var moonX = Math.round(planetX + moonWX * cam.zoom);
        var moonY = Math.round(planetY + moonWY * cam.zoom);

        // Draw sun
        if (sunX >= ox && sunX < ox + w && sunY >= oy && sunY < oy + h) {
            buf.setChar(sunX, sunY, "☀", SUN.color, null);
        }
        // Draw planet
        if (planetX >= ox && planetX < ox + w && planetY >= oy && planetY < oy + h) {
            buf.setChar(planetX, planetY, "●", PLANET.color, null);
        }
        // Draw moon
        if (moonX >= ox && moonX < ox + w && moonY >= oy && moonY < oy + h) {
            buf.setChar(moonX, moonY, "○", MOON.color, null);
        }

        // HUD
        var teamIdx = Game.state.players[playerID] || 0;
        buf.writeString(ox, oy, "Team " + (teamIdx + 1) + " | WASD:move ↑↓:tilt +/-:zoom", "#888888", null);
    },

    // Canvas render: graphical version for enhanced clients.
    renderCanvas: function(ctx, playerID, w, h) {
        var cam = getCamera(playerID);

        // World-to-screen transform
        var cx = w / 2;
        var cy = h / 2;
        var scale = cam.zoom;
        var tiltFactor = 1 - Math.abs(cam.tilt) * 0.6;

        function toScreen(wx, wy) {
            return {
                x: cx + (wx - cam.x) * scale,
                y: cy + (wy - cam.y) * scale * tiltFactor
            };
        }

        // Background: deep space
        ctx.setFillStyle("#000011");
        ctx.fillRect(0, 0, w, h);

        // Stars
        ctx.setFillStyle("#FFFFFF");
        for (var i = 0; i < STARS.length; i++) {
            var sp = toScreen(STARS[i].x, STARS[i].y);
            if (sp.x >= 0 && sp.x < w && sp.y >= 0 && sp.y < h) {
                var size = 1 + STARS[i].brightness;
                var alpha = STARS[i].brightness;
                var gray = Math.round(alpha * 255);
                var hex = gray.toString(16);
                if (hex.length < 2) hex = "0" + hex;
                ctx.setFillStyle("#" + hex + hex + hex);
                ctx.fillRect(sp.x - size / 2, sp.y - size / 2, size, size);
            }
        }

        // Orbit trail: planet
        drawOrbitTrail(ctx, 0, 0, PLANET.orbitRadius, "#333355", scale, tiltFactor, cam, cx, cy);

        // Sun glow
        var sunPos = toScreen(0, 0);
        drawGlow(ctx, sunPos.x, sunPos.y, SUN.radius * scale, "#FFD700", "#FF8800");

        // Sun body
        ctx.setFillStyle(SUN.color);
        ctx.fillCircle(sunPos.x, sunPos.y, SUN.radius * scale * 0.6);

        // Planet position
        var planetAngle = Game.state.time * PLANET.speed;
        var planetWX = Math.cos(planetAngle) * PLANET.orbitRadius;
        var planetWY = Math.sin(planetAngle) * PLANET.orbitRadius;

        // Moon orbit trail around planet
        drawOrbitTrail(ctx, planetWX, planetWY, MOON.orbitRadius, "#222244", scale, tiltFactor, cam, cx, cy);

        // Planet
        var planetPos = toScreen(planetWX, planetWY);
        drawGlow(ctx, planetPos.x, planetPos.y, PLANET.radius * scale * 0.8, "#4488FF", "#2244AA");
        ctx.setFillStyle(PLANET.color);
        ctx.fillCircle(planetPos.x, planetPos.y, PLANET.radius * scale * 0.5);

        // Moon position
        var moonAngle = Game.state.time * MOON.speed;
        var moonWX = planetWX + Math.cos(moonAngle) * MOON.orbitRadius;
        var moonWY = planetWY + Math.sin(moonAngle) * MOON.orbitRadius;

        var moonPos = toScreen(moonWX, moonWY);
        ctx.setFillStyle(MOON.color);
        ctx.fillCircle(moonPos.x, moonPos.y, MOON.radius * scale * 0.5);

        // Shadow on moon (simple: darken half facing away from sun)
        var moonSunAngle = Math.atan2(moonWY, moonWX);
        ctx.setFillStyle("#00001180");
        // Approximate shadow with a half-circle offset
        ctx.fillCircle(
            moonPos.x + Math.cos(moonSunAngle) * MOON.radius * scale * 0.2,
            moonPos.y + Math.sin(moonSunAngle) * MOON.radius * scale * 0.2 * tiltFactor,
            MOON.radius * scale * 0.45
        );

        // HUD overlay
        var teamIdx = Game.state.players[playerID] || 0;
        var teamColor = Game.state.teamColors[teamIdx] || "#FFFFFF";
        ctx.setFillStyle("#000000AA");
        ctx.fillRect(4, 4, 260, 22);
        ctx.setFillStyle(teamColor);
        ctx.fillText("Team " + (teamIdx + 1) + " | WASD:move  Up/Down:tilt  +/-:zoom", 8, 20);

        // Minimap (bottom-right corner)
        var mmSize = 80;
        var mmX = w - mmSize - 8;
        var mmY = h - mmSize - 8;
        ctx.setFillStyle("#00001180");
        ctx.fillRect(mmX, mmY, mmSize, mmSize);
        ctx.setStrokeStyle("#333366");
        ctx.strokeRect(mmX, mmY, mmSize, mmSize);

        var mmScale = mmSize / (PLANET.orbitRadius * 3);
        var mmCx = mmX + mmSize / 2;
        var mmCy = mmY + mmSize / 2;

        // Minimap sun
        ctx.setFillStyle(SUN.color);
        ctx.fillCircle(mmCx, mmCy, 3);

        // Minimap planet
        ctx.setFillStyle(PLANET.color);
        ctx.fillCircle(mmCx + planetWX * mmScale, mmCy + planetWY * mmScale, 2);

        // Minimap moon
        ctx.setFillStyle(MOON.color);
        ctx.fillCircle(mmCx + moonWX * mmScale, mmCy + moonWY * mmScale, 1);

        // Minimap camera viewport indicator
        ctx.setStrokeStyle(teamColor);
        ctx.setLineWidth(1);
        var vpHalfW = (w / 2) / scale * mmScale;
        var vpHalfH = (h / 2) / scale * mmScale;
        ctx.strokeRect(
            mmCx + cam.x * mmScale - vpHalfW,
            mmCy + cam.y * mmScale - vpHalfH,
            vpHalfW * 2,
            vpHalfH * 2
        );
    },

    statusBar: function(playerID) {
        var cam = getCamera(playerID);
        var teamIdx = Game.state.players[playerID] || 0;
        return "Team " + (teamIdx + 1)
            + " | Zoom: " + cam.zoom.toFixed(1) + "x"
            + " | Tilt: " + (cam.tilt * 100).toFixed(0) + "%"
            + " | Time: " + Game.state.time.toFixed(1) + "s";
    },

    commandBar: function(playerID) {
        return "[WASD] move  [↑↓] tilt  [+/-] zoom";
    }
};

// Draw a dotted orbit trail as an ellipse
function drawOrbitTrail(ctx, centerWX, centerWY, orbitR, color, scale, tiltFactor, cam, cx, cy) {
    ctx.setStrokeStyle(color);
    ctx.setLineWidth(1);
    ctx.beginPath();
    var segments = 64;
    for (var i = 0; i <= segments; i++) {
        var a = (i / segments) * Math.PI * 2;
        var wx = centerWX + Math.cos(a) * orbitR;
        var wy = centerWY + Math.sin(a) * orbitR;
        var sx = cx + (wx - cam.x) * scale;
        var sy = cy + (wy - cam.y) * scale * tiltFactor;
        if (i === 0) {
            ctx.moveTo(sx, sy);
        } else {
            ctx.lineTo(sx, sy);
        }
    }
    ctx.stroke();
}

// Draw a radial glow effect (concentric circles with decreasing opacity)
function drawGlow(ctx, x, y, radius, innerColor, outerColor) {
    // Simple approximation: a few layers
    var layers = 5;
    for (var i = layers; i >= 1; i--) {
        var r = radius * (i / layers);
        var alpha = Math.round((1 - i / layers) * 60 + 10);
        var hex = alpha.toString(16);
        if (hex.length < 2) hex = "0" + hex;
        ctx.setFillStyle(outerColor + hex);
        ctx.fillCircle(x, y, r);
    }
}
