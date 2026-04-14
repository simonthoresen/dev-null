// voyage.js — Amiga-style space flythrough demo
// A single continuous camera travels from deep space through the solar system,
// approaches Earth, enters the atmosphere, and lands at the Egyptian pyramids.
//
// Load with: /game load voyage
//
// No player interaction — pure cinematic demo (~55 seconds).

// ═══════════════════════════════════════════════════════════════════════════
// TIMING (seconds)
// ═══════════════════════════════════════════════════════════════════════════

var T_FLY     = 4;   // Camera starts moving toward Earth
var T_LAND    = 44;  // Touchdown — dust kicks up
var T_SETTLE  = 50;  // Dust settling
var T_END     = 55;  // Game over

// ═══════════════════════════════════════════════════════════════════════════
// MATH
// ═══════════════════════════════════════════════════════════════════════════

function lerp(a, b, t) { return a + (b - a) * t; }
function clamp(v, lo, hi) { return v < lo ? lo : v > hi ? hi : v; }
function smooth(t) { t = clamp(t, 0, 1); return t * t * (3 - 2 * t); }
function progress(tNow, a, b) { return clamp((tNow - a) / (b - a), 0, 1); }

function hex(r, g, b, a) {
    r = clamp(Math.round(r), 0, 255);
    g = clamp(Math.round(g), 0, 255);
    b = clamp(Math.round(b), 0, 255);
    var s = "#";
    s += (r < 16 ? "0" : "") + r.toString(16);
    s += (g < 16 ? "0" : "") + g.toString(16);
    s += (b < 16 ? "0" : "") + b.toString(16);
    if (a !== undefined && a !== null) {
        a = clamp(Math.round(a), 0, 255);
        s += (a < 16 ? "0" : "") + a.toString(16);
    }
    return s;
}

function hashF(a, b) {
    var n = Math.sin(a * 127.1 + b * 311.7) * 43758.5453;
    return n - Math.floor(n);
}

// ═══════════════════════════════════════════════════════════════════════════
// PRE-GENERATED DATA
// ═══════════════════════════════════════════════════════════════════════════

var STARS = [];
for (var _i = 0; _i < 400; _i++) {
    STARS.push({
        x: Math.random() * 4000 - 2000,
        y: Math.random() * 4000 - 2000,
        z: Math.random() * 0.8 + 0.2,
        bright: Math.random() * 0.6 + 0.4,
        twinkle: Math.random() * 6.283
    });
}

var DUST = [];
for (var _i = 0; _i < 120; _i++) {
    DUST.push({
        x:     (Math.random() - 0.5) * 2.0,
        vy0:   -(Math.random() * 1.2 + 0.4),
        vx:    (Math.random() - 0.5) * 1.4,
        size:  Math.random() * 3 + 1.5,
        life:  Math.random() * 2.5 + 1.5,
        delay: Math.random() * 3.5,
        shade: Math.random() * 0.3
    });
}

var SUN_R = 25;
var PLANETS = [
    { name: "Mercury", orbit: 55,  r: 3,   col: "#A8A8A8", spd: 1.2,   a0: 0.5  },
    { name: "Venus",   orbit: 80,  r: 5.5, col: "#E8CC80", spd: 0.48,  a0: 2.1  },
    { name: "Earth",   orbit: 120, r: 7,   col: "#4488EE", spd: 0.3,   a0: 4.5  },
    { name: "Mars",    orbit: 170, r: 4.5, col: "#CC4422", spd: 0.16,  a0: 1.0  },
    { name: "Jupiter", orbit: 300, r: 20,  col: "#D4A870", spd: 0.025, a0: 5.8  },
    { name: "Saturn",  orbit: 400, r: 16,  col: "#E8D888", spd: 0.01,  a0: 3.2, ring: true },
    { name: "Uranus",  orbit: 520, r: 11,  col: "#7EC8DC", spd: 0.004, a0: 0.7  },
    { name: "Neptune", orbit: 640, r: 10,  col: "#2855D8", spd: 0.002, a0: 5.1  }
];

var SCROLL_TEXT = "VOYAGE  ///  A DEV-NULL DEMO  ///  GREETINGS TO ALL PLAYERS AND CODERS  ///  FROM THE DEPTHS OF SPACE TO THE SANDS OF EGYPT  ///  THE JOURNEY IS THE DESTINATION  ///  2026  ///  ";

// ═══════════════════════════════════════════════════════════════════════════
// GAME STATE
// ═══════════════════════════════════════════════════════════════════════════

var time = 0;
var ended = false;
var midiCue = 0;

// ═══════════════════════════════════════════════════════════════════════════
// GAME OBJECT
// ═══════════════════════════════════════════════════════════════════════════

var Game = {
    gameName: "Voyage",

    load: function(saved) {},

    begin: function() {
        time = 0;
        ended = false;
        midiCue = 0;
    },

    update: function(dt) {
        time += dt;
        updateMusic();
        if (time >= T_END && !ended) {
            ended = true;
            gameOver([{ name: "Voyage", result: "Complete" }]);
        }
    },

    onInput: function(pid, key) {},

    // ── Canvas renderer ─────────────────────────────────────────────────
    renderCanvas: function(ctx, pid, w, h) {
        ctx.setFillStyle("#000011");
        ctx.fillRect(0, 0, w, h);

        // ── Continuous camera ───────────────────────────────────────────
        var flyT = smooth(progress(time, T_FLY, T_LAND));
        var earthAngle = time * PLANETS[2].spd + PLANETS[2].a0;
        var earthWX = Math.cos(earthAngle) * PLANETS[2].orbit;
        var earthWY = Math.sin(earthAngle) * PLANETS[2].orbit;

        // Camera arrives at Earth early, then stays locked on
        var posT = smooth(clamp(flyT * 3, 0, 1));
        var camX = lerp(0, earthWX, posT);
        var camY = lerp(0, earthWY, posT);

        // Exponential zoom: 0.35 → 525
        var zoom = 0.35 * Math.pow(1500, flyT);
        var scale = zoom * w / 1200;

        // Earth's projected size and screen position
        var earthScreenR = PLANETS[2].r * scale;
        var relR = earthScreenR / w;
        var cx = w / 2;
        var cy = h / 2;
        var earthSX = cx + (earthWX - camX) * scale;
        var earthSY = cy + (earthWY - camY) * scale;

        // ── Layer 1: Stars and solar system ─────────────────────────────
        if (relR < 0.20) {
            renderStarfield(ctx, w, h, cx, cy, camX, camY, scale);
            if (flyT > 0.03) {
                renderSpeedLines(ctx, w, h, cx, cy, flyT);
            }
            renderSolarSystem(ctx, w, h, cx, cy, camX, camY, scale);
            // Fade overlay as Earth grows
            if (relR > 0.04) {
                var spaceFade = smooth((relR - 0.04) / 0.16);
                ctx.setFillStyle(hex(0, 0, 17, Math.round(spaceFade * 255)));
                ctx.fillRect(0, 0, w, h);
            }
        }

        // ── Layer 2: Detailed globe ─────────────────────────────────────
        if (relR > 0.02 && relR < 1.05) {
            drawGlobe(ctx, earthSX, earthSY, earthScreenR, w, h);
            // Atmosphere rim glow
            if (earthScreenR > 20 && relR < 0.7) {
                for (var i = 3; i >= 1; i--) {
                    var gr = earthScreenR + i * Math.max(2, earthScreenR * 0.03);
                    ctx.setFillStyle(hex(80, 130, 255, Math.round(30 / i)));
                    ctx.beginPath();
                    ctx.arc(earthSX, earthSY, gr, 0, Math.PI * 2);
                    ctx.fill();
                }
            }
        }

        // ── Layer 3: Atmospheric entry glow ─────────────────────────────
        if (relR > 0.6 && relR < 1.5) {
            var entryT = clamp((relR - 0.6) / 0.9, 0, 1);
            var glow = Math.sin(entryT * Math.PI);
            if (glow > 0.01) {
                ctx.setFillStyle(hex(220, 160, 80, Math.round(glow * 250)));
                ctx.fillRect(0, 0, w, h);
            }
        }

        // ── Layer 4: Surface ────────────────────────────────────────────
        if (relR > 1.05) {
            renderSurfaceLayer(ctx, w, h);
        }

        // ── Copper bar ──────────────────────────────────────────────────
        renderCopperBar(ctx, w, h);

        // ── Fade in from black ──────────────────────────────────────────
        if (time < 2) {
            ctx.setFillStyle(hex(0, 0, 0, Math.round((1 - time / 2) * 255)));
            ctx.fillRect(0, 0, w, h);
        }
    },

    statusBar: function(pid) {
        var pct = Math.min(100, Math.floor(time / T_END * 100));
        return phaseLabel() + "  [" + pct + "%]";
    },

    commandBar: function(pid) {
        return "Sit back and enjoy the ride";
    }
};

// ═══════════════════════════════════════════════════════════════════════════
// MUSIC — continuous evolution across the journey
// ═══════════════════════════════════════════════════════════════════════════

function updateMusic() {
    // Cue 1 (t=2): Deep space drone — low pad
    if (midiCue < 1 && time > 2) {
        midiCue = 1;
        midiProgram(0, 89);           // Pad 2 (Warm)
        midiNote(0, 36, 35, 52000);   // C2 — deep drone (plays whole demo)
        midiNote(0, 48, 30, 52000);   // C3
        midiNote(0, 55, 25, 52000);   // G3
    }

    // Cue 2 (t=10): Flythrough begins — add movement
    if (midiCue < 2 && time > 10) {
        midiCue = 2;
        midiProgram(1, 91);           // Pad 4 (Choir)
        midiNote(1, 60, 25, 18000);   // C4 — ethereal choir swell
        midiNote(1, 67, 20, 18000);   // G4
    }

    // Cue 3 (t=20): Approaching Earth — rising tension
    if (midiCue < 3 && time > 20) {
        midiCue = 3;
        midiProgram(2, 90);           // Pad 3 (Polysynth)
        midiNote(2, 64, 30, 12000);   // E4
        midiNote(2, 69, 25, 12000);   // A4
    }

    // Cue 4 (t=30): Globe fills view — build
    if (midiCue < 4 && time > 30) {
        midiCue = 4;
        midiNote(1, 72, 30, 8000);    // C5 — choir climbs
        midiNote(2, 71, 28, 8000);    // B4 — tension
    }

    // Cue 5 (t=35): Atmospheric entry — dramatic
    if (midiCue < 5 && time > 35) {
        midiCue = 5;
        midiProgram(3, 92);           // Pad 5 (Bowed)
        midiNote(3, 48, 45, 10000);   // C3 — dramatic entry
        midiNote(3, 55, 40, 10000);   // G3
        midiNote(3, 60, 38, 10000);   // C4
    }

    // Cue 6 (t=44): Landing — impact
    if (midiCue < 6 && time > T_LAND) {
        midiCue = 6;
        midiProgram(4, 47);           // Timpani
        midiNote(4, 36, 70, 2000);    // C2 — impact boom
        midiNote(4, 41, 55, 1500);    // F2
    }

    // Cue 7 (t=51): Resolution — final chord
    if (midiCue < 7 && time > T_SETTLE + 1) {
        midiCue = 7;
        midiProgram(5, 88);           // Pad 1 (New Age)
        midiNote(5, 60, 55, 6000);    // C4
        midiNote(5, 64, 50, 6000);    // E4
        midiNote(5, 67, 50, 6000);    // G4
        midiNote(5, 72, 45, 6000);    // C5
    }
}

// ═══════════════════════════════════════════════════════════════════════════
// HELPERS
// ═══════════════════════════════════════════════════════════════════════════

function phaseLabel() {
    if (time < T_FLY)    return "Deep Space";
    if (time < 20)       return "Solar System Flythrough";
    if (time < 32)       return "Approaching Earth";
    if (time < 36)       return "Entering Atmosphere";
    if (time < T_LAND)   return "Descending to Giza";
    if (time < T_SETTLE) return "Landing...";
    return "Voyage Complete";
}

// ═══════════════════════════════════════════════════════════════════════════
// STARFIELD
// ═══════════════════════════════════════════════════════════════════════════

function renderStarfield(ctx, w, h, cx, cy, camX, camY, scale) {
    for (var i = 0; i < STARS.length; i++) {
        var s  = STARS[i];
        var sx = cx + (s.x - camX * s.z * 0.8) * scale * 0.25;
        var sy = cy + (s.y - camY * s.z * 0.8) * scale * 0.25;
        if (sx < 0 || sx >= w || sy < 0 || sy >= h) continue;

        var twk = Math.sin(time * 3 + s.twinkle) * 0.15 + 0.85;
        var v   = Math.round(s.bright * twk * 255);
        ctx.setFillStyle(hex(v, v, v));
        ctx.fillRect(sx, sy, Math.max(1, s.z * 1.2), Math.max(1, s.z * 1.2));
    }
}

// ═══════════════════════════════════════════════════════════════════════════
// SPEED LINES
// ═══════════════════════════════════════════════════════════════════════════

function renderSpeedLines(ctx, w, h, cx, cy, flyT) {
    var lineAlpha = Math.sin(flyT * Math.PI) * 0.4;
    if (lineAlpha < 0.01) return;
    for (var i = 0; i < 25; i++) {
        var lx = ((hashF(i, 42) * 2 - 1) * w * 0.8 + cx);
        var ly = ((hashF(i, 73) * 2 - 1) * h * 0.8 + cy);
        var ll = 4 + hashF(i, 99) * 12;
        var dx = (lx - cx); var dy = (ly - cy);
        var len = Math.sqrt(dx * dx + dy * dy) + 0.001;
        dx /= len; dy /= len;
        var a = Math.round(lineAlpha * 140);
        if (a > 2) {
            ctx.setStrokeStyle(hex(160, 180, 255, a));
            ctx.setLineWidth(1);
            ctx.beginPath();
            ctx.moveTo(lx, ly);
            ctx.lineTo(lx + dx * ll * flyT * 3, ly + dy * ll * flyT * 3);
            ctx.stroke();
        }
    }
}

// ═══════════════════════════════════════════════════════════════════════════
// SOLAR SYSTEM (orbits, sun, planets — Earth handled by globe renderer)
// ═══════════════════════════════════════════════════════════════════════════

function renderSolarSystem(ctx, w, h, cx, cy, camX, camY, scale) {
    // ── Orbit rings ──
    for (var i = 0; i < PLANETS.length; i++) {
        var orbitScreenR = PLANETS[i].orbit * scale;
        var ocx = cx - camX * scale;
        var ocy = cy - camY * scale;
        if (ocx + orbitScreenR < -20 || ocx - orbitScreenR > w + 20) continue;
        if (ocy + orbitScreenR < -20 || ocy - orbitScreenR > h + 20) continue;
        ctx.setStrokeStyle(hex(40, 40, 70, 50));
        ctx.setLineWidth(1);
        ctx.beginPath();
        ctx.arc(ocx, ocy, orbitScreenR, 0, Math.PI * 2);
        ctx.stroke();
    }

    // ── Sun ──
    var sunSX = cx - camX * scale;
    var sunSY = cy - camY * scale;
    var sunSR = Math.max(4, SUN_R * scale);
    if (sunSX > -sunSR * 4 && sunSX < w + sunSR * 4 &&
        sunSY > -sunSR * 4 && sunSY < h + sunSR * 4) {
        for (var i = 5; i >= 1; i--) {
            var gr = sunSR * (1 + i * 0.7);
            ctx.setFillStyle(hex(255, 100, 0, Math.round((6 - i) * 7)));
            ctx.fillCircle(sunSX, sunSY, gr);
        }
        ctx.setFillStyle("#FFD700");
        ctx.fillCircle(sunSX, sunSY, sunSR * 0.6);
    }

    // ── Planets (skip Earth — globe renderer handles it) ──
    for (var i = 0; i < PLANETS.length; i++) {
        if (i === 2) continue;
        var p     = PLANETS[i];
        var angle = time * p.spd + p.a0;
        var pwx   = Math.cos(angle) * p.orbit;
        var pwy   = Math.sin(angle) * p.orbit;
        var psx   = cx + (pwx - camX) * scale;
        var psy   = cy + (pwy - camY) * scale;
        var pr    = Math.max(1.5, p.r * scale);

        if (psx < -pr * 3 || psx > w + pr * 3) continue;
        if (psy < -pr * 3 || psy > h + pr * 3) continue;

        // Glow
        ctx.setFillStyle(p.col + "30");
        ctx.fillCircle(psx, psy, pr * 1.8);
        // Body
        ctx.setFillStyle(p.col);
        ctx.fillCircle(psx, psy, pr * 0.65);

        // Saturn ring
        if (p.ring && pr > 2) {
            ctx.setStrokeStyle("#C8A060C0");
            ctx.setLineWidth(Math.max(1, pr * 0.12));
            ctx.beginPath();
            ctx.arc(psx, psy, pr * 1.7, 0, Math.PI * 2);
            ctx.stroke();
        }
    }
}

// ═══════════════════════════════════════════════════════════════════════════
// GLOBE RENDERING
// ═══════════════════════════════════════════════════════════════════════════

function drawGlobe(ctx, cx, cy, radius, w, h) {
    if (radius < 4) {
        ctx.setFillStyle("#4488EE");
        ctx.fillCircle(cx, cy, radius);
        return;
    }

    // Ocean base
    ctx.setFillStyle("#1A3580");
    ctx.fillCircle(cx, cy, radius);

    // Specular highlight
    ctx.setFillStyle("#2255AA30");
    ctx.fillCircle(cx - radius * 0.15, cy - radius * 0.15, radius * 0.75);

    // Adaptive pixel step (keeps iteration count bounded)
    var step = Math.max(1, Math.ceil(radius / 55));
    var r2   = radius * radius;

    // Rotation: center view on ~25°E longitude (faces Africa/Egypt toward camera)
    var rotLon = 25;

    var yStart = Math.max(Math.ceil(-radius), Math.ceil(-cy));
    var yEnd   = Math.min(Math.floor(radius), Math.floor(h - 1 - cy));

    for (var py = yStart; py <= yEnd; py += step) {
        var rowHalf = Math.sqrt(r2 - py * py);
        var xStart  = Math.max(Math.ceil(-rowHalf), Math.ceil(-cx));
        var xEnd    = Math.min(Math.floor(rowHalf), Math.floor(w - 1 - cx));

        for (var px = xStart; px <= xEnd; px += step) {
            var d2 = px * px + py * py;
            if (d2 >= r2) continue;

            var nz  = Math.sqrt(1 - d2 / r2);
            var nx  = px / radius;
            var ny  = -py / radius;
            var lat = Math.asin(ny) * 57.2958;            // rad→deg
            var lon = Math.atan2(nx, nz) * 57.2958 + rotLon;

            if (!isLand(lat, lon)) continue;

            // Diffuse lighting from upper-right
            var light = clamp(nz * 0.65 + nx * 0.25 + ny * 0.1, 0.18, 1.0);

            var cr, cg, cb;
            if (lat > 70 || lat < -65) {
                // Ice caps
                cr = 230 * light; cg = 240 * light; cb = 250 * light;
            } else if (lat > 15 && lat < 36 && lon > -10 && lon < 55) {
                // Sahara / Arabian desert
                cr = 215 * light; cg = 185 * light; cb = 120 * light;
            } else if (Math.abs(lat) < 15) {
                // Tropical — dark green
                cr = 25 * light; cg = 115 * light; cb = 30 * light;
            } else {
                // Temperate — green
                cr = 45 * light; cg = 145 * light; cb = 45 * light;
            }

            ctx.setFillStyle(hex(cr, cg, cb));
            ctx.fillRect(cx + px, cy + py, step, step);
        }
    }

    // Cloud wisps
    ctx.setFillStyle("#FFFFFF30");
    for (var i = 0; i < 10; i++) {
        var cla = (Math.sin(i * 1.7 + 0.5) * 50) * 0.01745; // deg→rad
        var clo = ((i * 40 + time * 1.5) % 360 - 180) * 0.01745;
        var cnz = Math.cos(cla) * Math.cos(clo);
        if (cnz < 0.1) continue;
        var cpx = cx + Math.cos(cla) * Math.sin(clo) * radius;
        var cpy = cy - Math.sin(cla) * radius;
        var csz = radius * 0.12 * cnz;
        ctx.fillCircle(cpx, cpy, csz);
    }
}

// ── Continent map (simplified, recognisable at low res) ─────────────────

function isLand(lat, lon) {
    while (lon > 180)  lon -= 360;
    while (lon < -180) lon += 360;

    // Coastline noise for organic edges
    var noise = Math.sin(lat * 0.31 + 1.2) * Math.sin(lon * 0.43 + 0.7) * 4;

    // Africa
    if (lat > -36 && lat < 38 && lon > -20 && lon < 55) {
        var halfW = 24 + noise;
        if (lat > 28)  halfW = 18 - (lat - 28) * 1.2 + noise;
        if (lat < -22) halfW = 14 + (lat + 22) * 0.5 + noise;
        var clon = 22;
        if (halfW > 3 && Math.abs(lon - clon) < halfW) {
            // Mediterranean cutout
            if (lat > 31 && lat < 42 && lon > 0 && lon < 32) return false;
            return true;
        }
    }
    // Horn of Africa
    if (lat > 0 && lat < 14 && lon > 38 && lon < 52) return true;

    // Europe
    if (lat > 37 && lat < 72 && lon > -12 && lon < 42 + noise) {
        if (lat < 42 && lon < -2) return false;
        return true;
    }
    // Scandinavia
    if (lat > 55 && lat < 72 && lon > 5 && lon < 32) return true;
    // British Isles
    if (lat > 50 && lat < 60 && lon > -10 && lon < 2) return true;

    // Middle East / Arabia
    if (lat > 12 && lat < 38 && lon > 33 && lon < 60 + noise) {
        if (lat < 18 && lon > 52) return false;
        return true;
    }

    // India
    if (lat > 8 && lat < 35 && lon > 68 && lon < 90) {
        var iw = clamp((35 - lat) * 0.6 + noise, 0, 20);
        if (Math.abs(lon - 79) < iw) return true;
    }

    // East Asia (broad)
    if (lat > 20 && lat < 65 && lon > 80 && lon < 135 + noise) return true;

    // Greenland
    if (lat > 60 && lat < 84 && lon > -58 && lon < -12) return true;

    // South America
    if (lat > -56 && lat < 14 && lon > -82 && lon < -34) {
        var sw = 22 + noise - Math.abs(lat + 12) * 0.35;
        if (sw > 0 && Math.abs(lon + 58) < sw) return true;
    }

    // North America
    if (lat > 25 && lat < 72 && lon > -135 && lon < -55) {
        var nw = 30 + noise - Math.abs(lat - 48) * 0.45;
        if (nw > 0 && Math.abs(lon + 95) < nw) return true;
    }

    // Australia
    if (lat > -40 && lat < -12 && lon > 113 && lon < 155) return true;

    // Antarctica
    if (lat < -68) return true;

    return false;
}

// ═══════════════════════════════════════════════════════════════════════════
// SURFACE — Descent to the pyramids
// ═══════════════════════════════════════════════════════════════════════════

function renderSurfaceLayer(ctx, w, h) {
    var barH     = Math.max(3, Math.round(h * 0.05));
    var usableH  = h - barH;
    var descentT = smooth(progress(time, 30, T_LAND));
    var horizonY = lerp(usableH * 0.3, usableH * 0.5, descentT);

    // ── Sky ──
    var skyBands = 16;
    for (var i = 0; i < skyBands; i++) {
        var t  = i / skyBands;
        var by = t * horizonY;
        var bh = horizonY / skyBands + 1;

        var sr = lerp(8,   160, t * t);
        var sg = lerp(12,  140, t);
        var sb = lerp(50,  200, Math.sqrt(t));
        if (t > 0.75) {
            var glow = (t - 0.75) / 0.25;
            sr = lerp(sr, 240, glow * 0.55);
            sg = lerp(sg, 190, glow * 0.35);
            sb = lerp(sb, 140, glow * 0.15);
        }
        ctx.setFillStyle(hex(sr, sg, sb));
        ctx.fillRect(0, by, w, bh);
    }

    // ── Sun in sky ──
    var sunX = w * 0.78;
    var sunY = horizonY * 0.22;
    var sunR = lerp(8, 18, descentT);
    for (var i = 4; i >= 1; i--) {
        ctx.setFillStyle(hex(255, 220, 50, Math.round(25 / i)));
        ctx.fillCircle(sunX, sunY, sunR * (1 + i * 0.6));
    }
    ctx.setFillStyle("#FFEE88");
    ctx.fillCircle(sunX, sunY, sunR);

    // ── Ground (desert) ──
    var groundH = usableH - horizonY;
    var gBands  = 12;
    for (var i = 0; i < gBands; i++) {
        var t  = i / gBands;
        var gy = horizonY + t * groundH;
        var gh = groundH / gBands + 1;

        var gr = lerp(205, 215, t);
        var gg = lerp(190, 175, t);
        var gb = lerp(165, 110, t);
        if (t < 0.25) {
            var haze = 1 - t / 0.25;
            gr = lerp(gr, 195, haze * 0.5);
            gg = lerp(gg, 185, haze * 0.5);
            gb = lerp(gb, 175, haze * 0.5);
        }
        ctx.setFillStyle(hex(gr, gg, gb));
        ctx.fillRect(0, gy, w, gh);
    }

    // ── Dune ripple lines ──
    ctx.setStrokeStyle("#BFA87040");
    ctx.setLineWidth(1);
    for (var i = 0; i < 8; i++) {
        var ry  = horizonY + groundH * (0.2 + i * 0.1);
        var amp = 2 + i * 0.5;
        ctx.beginPath();
        for (var x = 0; x <= w; x += 4) {
            var dy = Math.sin(x * 0.08 + i * 2 + time * 0.3) * amp;
            if (x === 0) ctx.moveTo(x, ry + dy);
            else         ctx.lineTo(x, ry + dy);
        }
        ctx.stroke();
    }

    // ── Pyramids ──
    drawPyramids(ctx, w, usableH, horizonY, descentT);

    // ── Dust ──
    if (time > T_LAND - 1) {
        var dustIntensity = smooth(progress(time, T_LAND - 1, T_LAND + 2));
        var dustFade      = 1 - smooth(progress(time, T_SETTLE - 2, T_SETTLE + 1));
        drawDust(ctx, w, usableH, horizonY + groundH * 0.3, dustIntensity * dustFade);

        // General haze during peak dust
        if (dustIntensity > 0.3 && dustFade > 0.2) {
            var hazeA = Math.sin(dustIntensity * Math.PI) * dustFade * 0.45;
            ctx.setFillStyle(hex(210, 195, 155, Math.round(hazeA * 180)));
            ctx.fillRect(0, horizonY, w, usableH - horizonY);
        }
    }

    // ── "VOYAGE COMPLETE" text ──
    if (time > T_SETTLE + 1) {
        var textAlpha = smooth(progress(time, T_SETTLE + 1, T_SETTLE + 3));
        ctx.setFillStyle(hex(255, 255, 255, Math.round(textAlpha * 240)));
        ctx.fillText("V O Y A G E   C O M P L E T E", w / 2 - 60, horizonY * 0.4);
    }
}

// ── Pyramids of Giza ────────────────────────────────────────────────────

function drawPyramids(ctx, w, h, horizonY, approach) {
    var groundH = h - horizonY;

    // Three pyramids: Khafre (left), Khufu (center, largest), Menkaure (right)
    var pyrs = [
        { xOff: -0.13, size: 0.88 },
        { xOff:  0.0,  size: 1.0  },
        { xOff:  0.16, size: 0.55 },
    ];

    var spread = lerp(0.6, 1.0, approach);

    for (var i = 0; i < pyrs.length; i++) {
        var p   = pyrs[i];
        var px  = w / 2 + p.xOff * w * spread;
        var baseScale = lerp(0.4, 1.0, approach);
        var pyrH = lerp(12, 80, approach) * p.size * baseScale;
        var pyrW = pyrH * 1.35;

        var baseY = horizonY + groundH * lerp(0.05, 0.35, approach) * (1 + (1 - p.size) * 0.3);

        // Shadow side (right half)
        ctx.setFillStyle("#7A6545");
        ctx.beginPath();
        ctx.moveTo(px, baseY - pyrH);
        ctx.lineTo(px + pyrW / 2, baseY);
        ctx.lineTo(px, baseY);
        ctx.closePath();
        ctx.fill();

        // Lit side (left half)
        ctx.setFillStyle("#D4B87A");
        ctx.beginPath();
        ctx.moveTo(px, baseY - pyrH);
        ctx.lineTo(px - pyrW / 2, baseY);
        ctx.lineTo(px, baseY);
        ctx.closePath();
        ctx.fill();

        // Center edge highlight
        ctx.setStrokeStyle("#C8A86090");
        ctx.setLineWidth(1);
        ctx.beginPath();
        ctx.moveTo(px, baseY - pyrH);
        ctx.lineTo(px, baseY);
        ctx.stroke();

        // Apex highlight
        if (pyrH > 20) {
            ctx.setFillStyle("#FFE8B040");
            ctx.fillCircle(px, baseY - pyrH, 2);
        }
    }
}

// ── Dust particles ──────────────────────────────────────────────────────

function drawDust(ctx, w, h, groundY, intensity) {
    if (intensity <= 0.01) return;
    var age0 = time - T_LAND;

    for (var i = 0; i < DUST.length; i++) {
        var d   = DUST[i];
        var age = age0 - d.delay;
        if (age < 0) continue;

        var gravity = 0.25;
        var dx = d.x * w * 0.35 + d.vx * age * w * 0.08;
        var dy = d.vy0 * age * h * 0.12 + gravity * age * age * h * 0.04;

        var sx = w / 2 + dx;
        var sy = groundY + dy;
        sy = Math.min(sy, h - 2);

        var alpha = clamp(1 - age / d.life, 0, 1) * intensity;
        if (alpha < 0.02) continue;

        var shade = d.shade;
        var cr = lerp(215, 190, shade);
        var cg = lerp(195, 165, shade);
        var cb = lerp(155, 120, shade);
        var ca = Math.round(alpha * 200);

        ctx.setFillStyle(hex(cr, cg, cb, ca));
        var sz = d.size * (1 + age * 0.4) * intensity;
        ctx.fillCircle(sx, sy, sz);
    }
}

// ═══════════════════════════════════════════════════════════════════════════
// COPPER BAR (Amiga demo homage)
// ═══════════════════════════════════════════════════════════════════════════

function renderCopperBar(ctx, w, h) {
    var barH = Math.max(3, Math.round(h * 0.05));
    var barY = h - barH;

    for (var i = 0; i < barH; i++) {
        var t  = Math.sin(i / (barH - 1 || 1) * Math.PI);        // 0→1→0
        var wave = Math.sin(time * 2.5 + i * 0.4) * 0.15;
        var cr = lerp(15,  100, t + wave);
        var cg = lerp(8,   50,  t);
        var cb = lerp(50,  190, t + wave * 0.5);
        ctx.setFillStyle(hex(cr, cg, cb));
        ctx.fillRect(0, barY + i, w, 1);
    }

    // Scrolling text
    var charW   = Math.max(4, Math.round(w / 50));
    var totalW  = SCROLL_TEXT.length * charW;
    var scrollX = w - ((time * 55) % (totalW + w));
    ctx.setFillStyle("#FFFFFFD0");
    ctx.fillText(SCROLL_TEXT, scrollX, barY + barH - 1);
}
