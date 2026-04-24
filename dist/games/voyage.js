// voyage.js — declarative solar system tour with team cubes
//
// Design notes
// ────────────
// The solar system is fixed, declarative data. Planet positions, moons,
// and stars are derived from `state._gameTime` (the framework's monotonic
// game clock); nothing about them lives in mutable state.
//
// Each team — and each bot, when there are fewer than MIN_TEAMS real
// teams — is an "entity" rendered as a small colored cube. An entity is
// either:
//
//   • orbiting a planet  (kind:"orbit",  planet, dist, spd, a0, t0, t1)
//   • travelling on a Bezier curve (kind:"travel", from, to, t0, t1, p0..p3)
//
// Position(entity, t) is a pure function of the descriptor and t. The
// server only mutates an entity at phase boundaries (orbit→travel,
// travel→orbit), so most ticks emit no state change at all. With
// per-frame _gameTime extrapolation on the client (see localrender.go),
// motion stays smooth at whatever fps the client renders at.
//
// Load with: /game-load voyage

// ═══════════════════════════════════════════════════════════════════════════
// SOLAR SYSTEM (static)
// ═══════════════════════════════════════════════════════════════════════════

var EARTH = 2;
var SUN_R = 20;

var PLANETS = [
    { name: "Mercury", orbit:  60, r:  2.5, col: "#A8A8A8", spd: 1.00, a0: 0.5, ring: false },
    { name: "Venus",   orbit:  85, r:  4.5, col: "#E8CC80", spd: 0.60, a0: 2.1, ring: false },
    { name: "Earth",   orbit: 120, r:  5.0, col: "#3A7FEE", spd: 0.45, a0: 4.5, ring: false },
    { name: "Mars",    orbit: 155, r:  3.5, col: "#CC4422", spd: 0.28, a0: 1.0, ring: false },
    { name: "Jupiter", orbit: 230, r: 15.0, col: "#D4A870", spd: 0.08, a0: 5.8, ring: false },
    { name: "Saturn",  orbit: 310, r: 13.0, col: "#E8D888", spd: 0.05, a0: 3.2, ring: true  },
    { name: "Uranus",  orbit: 380, r:  8.0, col: "#7EC8DC", spd: 0.03, a0: 0.7, ring: false },
    { name: "Neptune", orbit: 450, r:  7.5, col: "#2855D8", spd: 0.02, a0: 5.1, ring: false }
];

var MOONS = [
    { p: 2, dist: 12, r: 1.4, col: "#C8C8C8", spd: 1.00, a0: 0.0, inc: 0.10 },
    { p: 3, dist:  7, r: 0.6, col: "#9C8878", spd: 2.00, a0: 0.0, inc: 0.05 },
    { p: 3, dist: 10, r: 0.5, col: "#7C6858", spd: 1.20, a0: 2.1, inc: 0.15 },
    { p: 4, dist: 22, r: 1.3, col: "#E8D060", spd: 2.80, a0: 0.0, inc: 0.04 },
    { p: 4, dist: 32, r: 1.8, col: "#B0A898", spd: 1.30, a0: 2.0, inc: 0.18 },
    { p: 4, dist: 42, r: 1.7, col: "#887868", spd: 0.80, a0: 4.2, inc: 0.35 },
    { p: 5, dist: 38, r: 1.7, col: "#D8C080", spd: 1.10, a0: 0.0, inc: 0.08 },
    { p: 5, dist: 46, r: 0.9, col: "#A89898", spd: 0.70, a0: 2.5, inc: 0.20 },
    { p: 5, dist: 56, r: 0.9, col: "#655C52", spd: 0.40, a0: 4.7, inc: 0.45 },
    { p: 6, dist: 14, r: 1.0, col: "#A0A0B0", spd: 1.50, a0: 0.0, inc: 0.12 },
    { p: 6, dist: 19, r: 1.0, col: "#889098", spd: 1.00, a0: 2.1, inc: 0.35 },
    { p: 6, dist: 24, r: 0.8, col: "#606870", spd: 0.60, a0: 4.0, inc: 0.55 },
    { p: 7, dist: 14, r: 1.4, col: "#D0B098", spd: 1.20, a0: 0.0, inc: 0.50 }
];

var STARS = [];
(function() {
    for (var i = 0; i < 600; i++) {
        var u = Math.random() * 2 - 1;
        var a = Math.random() * Math.PI * 2;
        var r = Math.sqrt(1 - u * u);
        STARS.push({ x: r * Math.cos(a), y: u, z: r * Math.sin(a),
                     b: 0.35 + Math.random() * 0.65 });
    }
})();

// ═══════════════════════════════════════════════════════════════════════════
// ENTITIES (teams + bots)
// ═══════════════════════════════════════════════════════════════════════════

var MIN_TEAMS    = 5;
var ORBIT_SPEED  = 0.45;       // rad/sec around the planet
var TRAVEL_SECS  = 7.0;        // bezier travel duration
var CUBE_SIZE    = 1.6;        // half-side of the marker cube

var BOT_NAMES  = ["Cygnus", "Lyra", "Orion", "Pegasus", "Phoenix",
                  "Vega", "Andromeda", "Rigel"];
var BOT_COLORS = ["#C0C0FF", "#FFB0B0", "#B0FFB0", "#FFE0A0",
                  "#D0A0FF", "#A0FFE0", "#FFA0D0", "#A0D0FF"];

// orbitDistFor returns the per-entity orbit distance for a given planet.
// Larger planets → wider orbit so the cube doesn't poke into the surface.
function orbitDistFor(planetIdx) {
    return PLANETS[planetIdx].r * 4 + 8;
}

// orbitDuration is one full revolution at ORBIT_SPEED.
function orbitDuration() { return (2 * Math.PI) / ORBIT_SPEED; }

// pickDestination picks a random planet other than `currentPlanet`.
function pickDestination(currentPlanet) {
    var n = PLANETS.length;
    var pick = Math.floor(Math.random() * (n - 1));
    return pick >= currentPlanet ? pick + 1 : pick;
}

// makeOrbit builds a fresh orbit descriptor anchored at time t, around
// planet `planet`, starting at angle `a0`. The orbit phase ends after one
// full revolution.
function makeOrbit(t, planet, a0) {
    return {
        kind:   "orbit",
        planet: planet,
        dist:   orbitDistFor(planet),
        spd:    ORBIT_SPEED,
        a0:     a0,
        t0:     t,
        t1:     t + orbitDuration()
    };
}

// makeTravel builds a travel descriptor from current position p0 to a
// fresh orbit anchor at planet `to`. The destination orbit's a0 is
// returned so the orbit phase that follows can be created without a
// visible jump.
function makeTravel(t, fromPlanet, p0, to) {
    var t1   = t + TRAVEL_SECS;
    var a0   = Math.random() * Math.PI * 2;
    var dist = orbitDistFor(to);
    // End point = where the destination orbit places the entity at t1
    // (we sample planet position at t1 so the bezier endpoint lands on
    // the correct moving target).
    var dpp  = planetPos(to, t1);
    var endX = dpp[0] + Math.cos(a0) * dist;
    var endZ = dpp[2] + Math.sin(a0) * dist;
    var endY = dist * 0.3;
    var p3   = [endX, endY, endZ];

    var line = vSub(p3, p0);
    var d    = vLen(line);
    var perp = vNorm([-line[2], 0, line[0]]);
    var sa   = Math.random() < 0.5 ? -1 : 1;
    var sb   = Math.random() < 0.5 ? -1 : 1;
    var p1   = vAdd(vAdd(p0, vScale(line, 0.30)), vScale(perp, d * 0.25 * sa));
    p1[1] += d * 0.20 * sa;
    var p2   = vAdd(vSub(p3, vScale(line, 0.30)), vScale(perp, d * 0.25 * sb));
    p2[1] += d * 0.20 * sb;

    return {
        kind: "travel",
        from: fromPlanet, to: to,
        t0: t, t1: t1,
        p0: p0, p1: p1, p2: p2, p3: p3,
        // Carry the destination orbit's a0 so the orbit-on-arrival phase
        // matches exactly without recomputing trig.
        nextA0: a0
    };
}

// entityPos returns [x,y,z] for an entity at time t, derived purely from
// the descriptor — no state lookup, no caching.
function entityPos(ent, t) {
    var m = ent.mode;
    if (m.kind === "orbit") {
        var pp = planetPos(m.planet, t);
        var a  = (t - m.t0) * m.spd + m.a0;
        return [pp[0] + Math.cos(a) * m.dist, m.dist * 0.3, pp[2] + Math.sin(a) * m.dist];
    }
    // travel
    var s = (t - m.t0) / (m.t1 - m.t0);
    if (s < 0) return m.p0.slice();
    if (s > 1) return m.p3.slice();
    return bezier3(m.p0, m.p1, m.p2, m.p3, smooth(s));
}

// entityVel returns a unit forward vector for the entity (numerical
// derivative of position over a tiny dt). Used to orient the cube and
// place a chase camera behind it.
function entityVel(ent, t) {
    var dt = 0.05;
    var p0 = entityPos(ent, t);
    var p1 = entityPos(ent, t + dt);
    var v  = vSub(p1, p0);
    if (vLen(v) < 1e-6) return [1, 0, 0];
    return vNorm(v);
}

// initEntity returns a fresh entity orbiting a random planet at time t.
function initEntity(id, name, color, isBot, t) {
    var startPlanet = Math.floor(Math.random() * PLANETS.length);
    return {
        id: id, name: name, color: color, isBot: isBot,
        mode: makeOrbit(t, startPlanet, Math.random() * Math.PI * 2)
    };
}

// buildEntities constructs the full entity list: one per real team plus
// bots to reach MIN_TEAMS.
function buildEntities(teams, t) {
    teams = teams || [];
    var list = [];
    for (var i = 0; i < teams.length; i++) {
        var t0 = teams[i];
        list.push(initEntity("team-" + i, t0.name || ("Team " + (i + 1)),
                             t0.color || "#CCCCCC", false, t));
    }
    var botIdx = 0;
    while (list.length < MIN_TEAMS) {
        var name = BOT_NAMES[botIdx % BOT_NAMES.length] + " Bot";
        var col  = BOT_COLORS[botIdx % BOT_COLORS.length];
        list.push(initEntity("bot-" + botIdx, name, col, true, t));
        botIdx++;
    }
    return list;
}

// stepEntity advances a single entity through phase boundaries given the
// current time. Returns true if the entity's mode changed (state diff).
function stepEntity(ent, t) {
    var m = ent.mode;
    if (m.kind === "orbit") {
        if (t < m.t1) return false;
        var p0       = entityPos(ent, m.t1);
        var dest     = pickDestination(m.planet);
        ent.mode     = makeTravel(m.t1, m.planet, p0, dest);
        return true;
    }
    // travel
    if (t < m.t1) return false;
    ent.mode = makeOrbit(m.t1, m.to, m.nextA0);
    return true;
}

// ═══════════════════════════════════════════════════════════════════════════
// MATH
// ═══════════════════════════════════════════════════════════════════════════

function clamp(v, lo, hi) { return v < lo ? lo : v > hi ? hi : v; }
function lerp(a, b, t)    { return a + (b - a) * t; }
function smooth(t)        { t = clamp(t, 0, 1); return t * t * (3 - 2 * t); }

function vAdd(a, b)       { return [a[0]+b[0], a[1]+b[1], a[2]+b[2]]; }
function vSub(a, b)       { return [a[0]-b[0], a[1]-b[1], a[2]-b[2]]; }
function vScale(a, s)     { return [a[0]*s, a[1]*s, a[2]*s]; }
function vDot(a, b)       { return a[0]*b[0] + a[1]*b[1] + a[2]*b[2]; }
function vCross(a, b) {
    return [a[1]*b[2]-a[2]*b[1], a[2]*b[0]-a[0]*b[2], a[0]*b[1]-a[1]*b[0]];
}
function vLen(a)          { return Math.sqrt(vDot(a, a)); }
function vNorm(a)         { var L = vLen(a) || 1; return [a[0]/L, a[1]/L, a[2]/L]; }
function vLerp(a, b, t)   { return [lerp(a[0],b[0],t), lerp(a[1],b[1],t), lerp(a[2],b[2],t)]; }

function bezier3(p0, p1, p2, p3, t) {
    var u = 1 - t;
    var w0 = u*u*u, w1 = 3*u*u*t, w2 = 3*u*t*t, w3 = t*t*t;
    return [
        p0[0]*w0 + p1[0]*w1 + p2[0]*w2 + p3[0]*w3,
        p0[1]*w0 + p1[1]*w1 + p2[1]*w2 + p3[1]*w3,
        p0[2]*w0 + p1[2]*w1 + p2[2]*w2 + p3[2]*w3
    ];
}

function hexByte(n) {
    n = clamp(Math.round(n), 0, 255);
    return (n < 16 ? "0" : "") + n.toString(16);
}

function hex(r, g, b, a) {
    var s = "#" + hexByte(r) + hexByte(g) + hexByte(b);
    if (a !== undefined && a !== null) s += hexByte(a);
    return s;
}

// parseHexColor returns [r,g,b] from "#RRGGBB" or [200,200,200] if it
// can't be parsed. Used to tint cube faces by team color.
function parseHexColor(c) {
    if (typeof c !== "string" || c.charAt(0) !== "#" || c.length < 7) {
        return [200, 200, 200];
    }
    return [
        parseInt(c.substr(1, 2), 16) || 0,
        parseInt(c.substr(3, 2), 16) || 0,
        parseInt(c.substr(5, 2), 16) || 0
    ];
}

// ═══════════════════════════════════════════════════════════════════════════
// SOLAR SYSTEM GEOMETRY (pure functions of time)
// ═══════════════════════════════════════════════════════════════════════════

function planetPos(idx, t) {
    var p = PLANETS[idx];
    var a = t * p.spd + p.a0;
    return [Math.cos(a) * p.orbit, 0, Math.sin(a) * p.orbit];
}

function moonPos(m, t) {
    var pp = planetPos(m.p, t);
    var a  = t * m.spd + m.a0;
    var x  = Math.cos(a) * m.dist;
    var z  = Math.sin(a) * m.dist;
    var ci = Math.cos(m.inc || 0);
    var si = Math.sin(m.inc || 0);
    return [pp[0] + x, pp[1] - z * si, pp[2] + z * ci];
}

// ═══════════════════════════════════════════════════════════════════════════
// CAMERA
// ═══════════════════════════════════════════════════════════════════════════

// chaseCamera: third-person rig — sit behind/above the entity, look ahead.
function chaseCamera(ent, t) {
    var pos = entityPos(ent, t);
    var fwd = entityVel(ent, t);
    var camPos = [
        pos[0] - fwd[0] * 18,
        pos[1] + 9,
        pos[2] - fwd[2] * 18
    ];
    var lookAt = [
        pos[0] + fwd[0] * 6,
        pos[1] + 1,
        pos[2] + fwd[2] * 6
    ];
    return { pos: camPos, look: lookAt };
}

// freeCamera: spectator orbit when the player has no team. Slowly spins
// around the system above the ecliptic.
function freeCamera(t) {
    var R = 520;
    var a = t * 0.05;
    return {
        pos:  [Math.cos(a) * R, 180, Math.sin(a) * R],
        look: [0, 0, 0]
    };
}

// ═══════════════════════════════════════════════════════════════════════════
// PROJECTION
// ═══════════════════════════════════════════════════════════════════════════

function makeProjector(pos, look, w, h) {
    var fwd   = vNorm(vSub(look, pos));
    var right = vNorm(vCross(fwd, [0, 1, 0]));
    var up    = vCross(right, fwd);
    var focal = w * 0.9;
    var cx = w * 0.5, cy = h * 0.5;
    function proj(p) {
        var d  = vSub(p, pos);
        var lz = vDot(d, fwd);
        if (lz < 0.01) return null;
        return {
            x: cx + vDot(d, right) / lz * focal,
            y: cy - vDot(d, up)    / lz * focal,
            z: lz,
            scale: focal / lz
        };
    }
    function projDir(d) {
        var lz = vDot(d, fwd);
        if (lz < 0.05) return null;
        return { x: cx + vDot(d, right) / lz * focal, y: cy - vDot(d, up) / lz * focal };
    }
    return { project: proj, projectDir: projDir };
}

// ═══════════════════════════════════════════════════════════════════════════
// PLANET / MOON / SUN RENDERING
// ═══════════════════════════════════════════════════════════════════════════

function renderStars(ctx, proj, w, h) {
    for (var i = 0; i < STARS.length; i++) {
        var s = STARS[i];
        var p = proj.projectDir([s.x, s.y, s.z]);
        if (!p) continue;
        if (p.x < 0 || p.x >= w || p.y < 0 || p.y >= h) continue;
        var v = Math.round(s.b * 220);
        ctx.setFillStyle(hex(v, v, v));
        ctx.fillRect(p.x, p.y, 1, 1);
    }
}

function renderOrbitRings(ctx, proj) {
    var SEG = 96;
    for (var i = 0; i < PLANETS.length; i++) {
        var R = PLANETS[i].orbit;
        ctx.setStrokeStyle("#283050");
        ctx.setLineWidth(1);
        ctx.beginPath();
        var started = false;
        for (var j = 0; j <= SEG; j++) {
            var a = j / SEG * Math.PI * 2;
            var p = proj.project([Math.cos(a) * R, 0, Math.sin(a) * R]);
            if (!p) { started = false; continue; }
            if (!started) { ctx.moveTo(p.x, p.y); started = true; }
            else           ctx.lineTo(p.x, p.y);
        }
        ctx.stroke();
    }
}

function renderSun(ctx, p) {
    var r = Math.max(2, SUN_R * p.scale);
    for (var i = 5; i >= 1; i--) {
        ctx.setFillStyle(hex(255, 170, 60, Math.round((6 - i) * 12)));
        ctx.fillCircle(p.x, p.y, r * (1 + i * 0.7));
    }
    ctx.setFillStyle("#FFE288");
    ctx.fillCircle(p.x, p.y, r);
    ctx.setFillStyle("#FFFFEE");
    ctx.fillCircle(p.x, p.y, r * 0.6);
}

var SPHERE_LATS  = 10;
var SPHERE_LONGS = 16;

var UNIT_SPHERE = (function makeUnitSphere(lats, longs) {
    var verts = [[0, 1, 0]];
    for (var i = 1; i < lats; i++) {
        var theta = i * Math.PI / lats;
        var y = Math.cos(theta);
        var r = Math.sin(theta);
        for (var j = 0; j < longs; j++) {
            var phi = j * 2 * Math.PI / longs;
            verts.push([r * Math.cos(phi), y, r * Math.sin(phi)]);
        }
    }
    verts.push([0, -1, 0]);
    var bottom = verts.length - 1;
    var tris = [];
    for (var j = 0; j < longs; j++) {
        tris.push([0, 1 + ((j + 1) % longs), 1 + j]);
    }
    for (var i = 0; i < lats - 2; i++) {
        var r1 = 1 + i * longs, r2 = 1 + (i + 1) * longs;
        for (var j = 0; j < longs; j++) {
            var jN = (j + 1) % longs;
            tris.push([r1 + j, r2 + j, r1 + jN]);
            tris.push([r1 + jN, r2 + j, r2 + jN]);
        }
    }
    var lastRing = 1 + (lats - 2) * longs;
    for (var j = 0; j < longs; j++) {
        tris.push([bottom, lastRing + j, lastRing + ((j + 1) % longs)]);
    }
    return { verts: verts, tris: tris };
})(SPHERE_LATS, SPHERE_LONGS);

function renderBody(ctx, proj, wp, Rworld, col, spinAngle) {
    var cosS = Math.cos(spinAngle), sinS = Math.sin(spinAngle);
    var N = UNIT_SPHERE.verts.length;
    var sx = new Array(N), sy = new Array(N), sz = new Array(N);
    var nx = new Array(N), ny = new Array(N), nz = new Array(N);
    for (var i = 0; i < N; i++) {
        var v  = UNIT_SPHERE.verts[i];
        var rx = v[0] * cosS + v[2] * sinS;
        var ry = v[1];
        var rz = -v[0] * sinS + v[2] * cosS;
        nx[i] = rx; ny[i] = ry; nz[i] = rz;
        var sp = proj.project([wp[0] + rx*Rworld, wp[1] + ry*Rworld, wp[2] + rz*Rworld]);
        if (sp) { sx[i] = sp.x; sy[i] = sp.y; sz[i] = sp.z; }
        else    { sz[i] = -1; }
    }
    var lightDir = vNorm(vSub([0, 0, 0], wp));
    var ambient = 0.08;
    ctx.clearDepth();
    for (var k = 0; k < UNIT_SPHERE.tris.length; k++) {
        var tri = UNIT_SPHERE.tris[k];
        var a = tri[0], b = tri[1], c = tri[2];
        if (sz[a] < 0 || sz[b] < 0 || sz[c] < 0) continue;
        ctx.fillTriangle3DLit(
            [sx[a], sy[a], sz[a]],
            [sx[b], sy[b], sz[b]],
            [sx[c], sy[c], sz[c]],
            [nx[a], ny[a], nz[a]],
            [nx[b], ny[b], nz[b]],
            [nx[c], ny[c], nz[c]],
            lightDir, col, ambient
        );
    }
}

function renderPlanet(ctx, proj, idx, wp, t) {
    var spin = t * (0.15 + 0.35 * PLANETS[idx].spd);
    renderBody(ctx, proj, wp, PLANETS[idx].r, PLANETS[idx].col, spin);
    if (PLANETS[idx].ring) renderRings(ctx, proj, wp, idx);
}

function renderMoon(ctx, proj, m, wp, t) {
    renderBody(ctx, proj, wp, m.r, m.col, t * m.spd);
}

function renderRings(ctx, proj, planetWP, idx) {
    var inner = PLANETS[idx].r * 1.35;
    var outer = PLANETS[idx].r * 2.30;
    var SEG = 72;
    for (var k = 0; k < 4; k++) {
        var rad = lerp(inner, outer, k / 3);
        var g   = lerp(0.5, 0.95, k / 3);
        ctx.setStrokeStyle(hex(220*g, 195*g, 140*g, 220));
        ctx.setLineWidth(1);
        ctx.beginPath();
        var started = false;
        for (var j = 0; j <= SEG; j++) {
            var a = j / SEG * Math.PI * 2;
            var pp = proj.project(vAdd(planetWP, [Math.cos(a)*rad, 0, Math.sin(a)*rad]));
            if (!pp) { started = false; continue; }
            if (!started) { ctx.moveTo(pp.x, pp.y); started = true; }
            else          ctx.lineTo(pp.x, pp.y);
        }
        ctx.stroke();
    }
}

// ═══════════════════════════════════════════════════════════════════════════
// CUBE RENDERING
// ═══════════════════════════════════════════════════════════════════════════

// Unit-cube vertices (range [-1, +1]).
var CUBE_VERTS = [
    [-1,-1,-1], [+1,-1,-1], [+1,+1,-1], [-1,+1,-1],
    [-1,-1,+1], [+1,-1,+1], [+1,+1,+1], [-1,+1,+1]
];

// Face = [v0, v1, v2, v3, normal]. Triangulated as (v0,v1,v2) + (v0,v2,v3).
var CUBE_FACES = [
    [0,1,5,4, [ 0,-1, 0]], // bottom
    [3,7,6,2, [ 0,+1, 0]], // top
    [0,4,7,3, [-1, 0, 0]], // left
    [1,2,6,5, [+1, 0, 0]], // right
    [0,3,2,1, [ 0, 0,-1]], // back
    [4,5,6,7, [ 0, 0,+1]]  // front
];

// renderCube draws a unit-size colored cube at world position wp, oriented
// so its +Z axis points along `forward`. Lit by the sun (origin) with a
// generous ambient floor — entities are gameplay markers, not stellar
// bodies, so legibility wins over realism.
function renderCube(ctx, proj, wp, forward, halfSide, col) {
    var fwd   = vNorm(forward);
    // Avoid the degenerate forward ≈ ±worldUp case.
    var up0   = Math.abs(fwd[1]) > 0.95 ? [1, 0, 0] : [0, 1, 0];
    var right = vNorm(vCross(fwd, up0));
    var up    = vCross(right, fwd);

    // Project all 8 verts once.
    var sx = new Array(8), sy = new Array(8), sz = new Array(8);
    var wx = new Array(8), wy = new Array(8), wz = new Array(8);
    for (var i = 0; i < 8; i++) {
        var v = CUBE_VERTS[i];
        var lx = v[0] * halfSide, ly = v[1] * halfSide, lz = v[2] * halfSide;
        wx[i] = wp[0] + right[0]*lx + up[0]*ly + fwd[0]*lz;
        wy[i] = wp[1] + right[1]*lx + up[1]*ly + fwd[1]*lz;
        wz[i] = wp[2] + right[2]*lx + up[2]*ly + fwd[2]*lz;
        var sp = proj.project([wx[i], wy[i], wz[i]]);
        if (sp) { sx[i] = sp.x; sy[i] = sp.y; sz[i] = sp.z; }
        else    { sz[i] = -1; }
    }

    var rgb       = parseHexColor(col);
    var lightDir  = vNorm(vSub([0, 0, 0], wp));
    var ambient   = 0.35;

    ctx.clearDepth();
    for (var f = 0; f < CUBE_FACES.length; f++) {
        var face = CUBE_FACES[f];
        var a = face[0], b = face[1], c = face[2], d = face[3], n = face[4];
        if (sz[a] < 0 || sz[b] < 0 || sz[c] < 0 || sz[d] < 0) continue;
        // Rotate face normal into world space.
        var nx = right[0]*n[0] + up[0]*n[1] + fwd[0]*n[2];
        var ny = right[1]*n[0] + up[1]*n[1] + fwd[1]*n[2];
        var nz = right[2]*n[0] + up[2]*n[1] + fwd[2]*n[2];
        // Triangulate the quad. Each tri shares the same per-vertex normal;
        // fillTriangle3DLit handles the Lambert term.
        ctx.fillTriangle3DLit(
            [sx[a], sy[a], sz[a]],
            [sx[b], sy[b], sz[b]],
            [sx[c], sy[c], sz[c]],
            [nx, ny, nz], [nx, ny, nz], [nx, ny, nz],
            lightDir, hex(rgb[0], rgb[1], rgb[2]), ambient
        );
        ctx.fillTriangle3DLit(
            [sx[a], sy[a], sz[a]],
            [sx[c], sy[c], sz[c]],
            [sx[d], sy[d], sz[d]],
            [nx, ny, nz], [nx, ny, nz], [nx, ny, nz],
            lightDir, hex(rgb[0], rgb[1], rgb[2]), ambient
        );
    }
}

// ═══════════════════════════════════════════════════════════════════════════
// SCENE
// ═══════════════════════════════════════════════════════════════════════════

function renderScene(ctx, cam, entities, t, w, h) {
    ctx.setFillStyle("#000008");
    ctx.fillRect(0, 0, w, h);

    var proj = makeProjector(cam.pos, cam.look, w, h);

    renderStars(ctx, proj, w, h);
    renderOrbitRings(ctx, proj);

    // Painter's sort: sun, planets, moons, then cubes — all together so
    // back-to-front compositing works across kinds.
    var items = [];
    var sp = proj.project([0, 0, 0]);
    if (sp) items.push({ z: sp.z, kind: "sun", p: sp });
    for (var i = 0; i < PLANETS.length; i++) {
        var wp = planetPos(i, t);
        var pp = proj.project(wp);
        if (pp) items.push({ z: pp.z, kind: "planet", idx: i, wp: wp, p: pp });
    }
    for (var i = 0; i < MOONS.length; i++) {
        var mwp = moonPos(MOONS[i], t);
        var mpp = proj.project(mwp);
        if (mpp) items.push({ z: mpp.z, kind: "moon", m: MOONS[i], wp: mwp, p: mpp });
    }
    for (var i = 0; i < entities.length; i++) {
        var ewp = entityPos(entities[i], t);
        var epp = proj.project(ewp);
        if (!epp) continue;
        items.push({
            z: epp.z, kind: "cube",
            wp: ewp, fwd: entityVel(entities[i], t),
            color: entities[i].color, p: epp
        });
    }
    items.sort(function(a, b) { return b.z - a.z; });

    for (var k = 0; k < items.length; k++) {
        var it = items[k];
        if      (it.kind === "sun")    renderSun(ctx, it.p);
        else if (it.kind === "planet") renderPlanet(ctx, proj, it.idx, it.wp, t);
        else if (it.kind === "moon")   renderMoon(ctx, proj, it.m, it.wp, t);
        else                            renderCube(ctx, proj, it.wp, it.fwd, CUBE_SIZE, it.color);
    }
}

// ═══════════════════════════════════════════════════════════════════════════
// GAME OBJECT
// ═══════════════════════════════════════════════════════════════════════════

// findEntityIdxForPlayer scans state.teams to locate the entity that
// belongs to playerID. Real teams own entities[0..teams.length-1]; bots
// have no owner and are returned as -1 ("spectator").
function findEntityIdxForPlayer(state, pid) {
    var teams = state.teams || [];
    for (var i = 0; i < teams.length; i++) {
        var ps = teams[i].players || [];
        for (var j = 0; j < ps.length; j++) {
            if (ps[j].id === pid) return i;
        }
    }
    return -1;
}

var Game = {
    gameName: "Voyage",

    init: function(ctx) {
        // Entities are seeded in begin() once teams are known; init just
        // sets the empty container so the diff transport has a baseline.
        return { entities: [] };
    },

    begin: function(state, ctx) {
        var t = state._gameTime || 0;
        state.entities = buildEntities(state.teams, t);
    },

    update: function(state, dt, events, ctx) {
        var t = state._gameTime || 0;
        // Phase advancement only — no per-tick mutation.
        for (var i = 0; i < state.entities.length; i++) {
            stepEntity(state.entities[i], t);
        }
    },

    resolveMe: function(state, pid) {
        return { id: pid, entityIdx: findEntityIdxForPlayer(state, pid) };
    },

    renderCanvas: function(state, me, canvas) {
        var w = canvas.width, h = canvas.height;
        var t = state._gameTime || 0;
        var ents = state.entities || [];
        if (ents.length === 0) {
            canvas.setFillStyle("#000008");
            canvas.fillRect(0, 0, w, h);
            return;
        }
        var idx = me ? me.entityIdx : -1;
        var cam = (idx >= 0 && idx < ents.length)
            ? chaseCamera(ents[idx], t)
            : freeCamera(t);
        renderScene(canvas, cam, ents, t, w, h);
    },

    statusBar: function(state, me) {
        var ents = state.entities || [];
        if (ents.length === 0) return "Voyage";
        var idx = me ? me.entityIdx : -1;
        if (idx < 0 || idx >= ents.length) {
            return "Spectating " + ents.length + " ship" + (ents.length === 1 ? "" : "s");
        }
        var ent = ents[idx];
        var m   = ent.mode;
        if (m.kind === "orbit") {
            var rem = Math.max(0, m.t1 - (state._gameTime || 0));
            return ent.name + " — orbiting " + PLANETS[m.planet].name +
                   "  (departs in " + rem.toFixed(1) + "s)";
        }
        var s = clamp(((state._gameTime || 0) - m.t0) / (m.t1 - m.t0), 0, 1);
        return ent.name + " — " + PLANETS[m.from].name + " → " + PLANETS[m.to].name +
               "  [" + Math.round(s * 100) + "%]";
    },

    commandBar: function(state, me) {
        return "Sit back and enjoy the ride";
    }
};
