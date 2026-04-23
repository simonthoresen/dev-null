// voyage.js — 3D solar system tour
//
// A cinematic camera orbits Earth, then flies along a smooth curved path
// to a random unvisited planet, orbits it, and continues until every
// planet has been visited. It then returns to Earth and starts over.
//
// Load with: /game load voyage

// ═══════════════════════════════════════════════════════════════════════════
// SOLAR SYSTEM
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

// Moons — up to 3 per planet, biggest real ones, exaggerated for visibility.
// p: planet index, dist: world units from planet, inc: orbital tilt (rad around world X).
var MOONS = [
    // Earth
    { p: 2, dist: 12, r: 1.4, col: "#C8C8C8", spd: 1.00, a0: 0.0, inc: 0.10 }, // Luna
    // Mars
    { p: 3, dist:  7, r: 0.6, col: "#9C8878", spd: 2.00, a0: 0.0, inc: 0.05 }, // Phobos
    { p: 3, dist: 10, r: 0.5, col: "#7C6858", spd: 1.20, a0: 2.1, inc: 0.15 }, // Deimos
    // Jupiter
    { p: 4, dist: 22, r: 1.3, col: "#E8D060", spd: 2.80, a0: 0.0, inc: 0.04 }, // Io
    { p: 4, dist: 32, r: 1.8, col: "#B0A898", spd: 1.30, a0: 2.0, inc: 0.18 }, // Ganymede
    { p: 4, dist: 42, r: 1.7, col: "#887868", spd: 0.80, a0: 4.2, inc: 0.35 }, // Callisto
    // Saturn (outside rings; outer ring ~30)
    { p: 5, dist: 38, r: 1.7, col: "#D8C080", spd: 1.10, a0: 0.0, inc: 0.08 }, // Titan
    { p: 5, dist: 46, r: 0.9, col: "#A89898", spd: 0.70, a0: 2.5, inc: 0.20 }, // Rhea
    { p: 5, dist: 56, r: 0.9, col: "#655C52", spd: 0.40, a0: 4.7, inc: 0.45 }, // Iapetus
    // Uranus (famously tilted)
    { p: 6, dist: 14, r: 1.0, col: "#A0A0B0", spd: 1.50, a0: 0.0, inc: 0.12 }, // Titania
    { p: 6, dist: 19, r: 1.0, col: "#889098", spd: 1.00, a0: 2.1, inc: 0.35 }, // Oberon
    { p: 6, dist: 24, r: 0.8, col: "#606870", spd: 0.60, a0: 4.0, inc: 0.55 }, // Umbriel
    // Neptune
    { p: 7, dist: 14, r: 1.4, col: "#D0B098", spd: 1.20, a0: 0.0, inc: 0.50 }  // Triton
];

// Star directions as points on the unit sphere.
var STARS = [];
(function() {
    for (var i = 0; i < 600; i++) {
        var u = Math.random() * 2 - 1;
        var a = Math.random() * Math.PI * 2;
        var r = Math.sqrt(1 - u * u);
        STARS.push({
            x: r * Math.cos(a),
            y: u,
            z: r * Math.sin(a),
            b: 0.35 + Math.random() * 0.65
        });
    }
})();

var ORBIT_DURATION  = 4.0;
var TRAVEL_DURATION = 6.0;

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

// ═══════════════════════════════════════════════════════════════════════════
// SOLAR SYSTEM GEOMETRY
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

function orbitRadiusFor(idx) {
    return PLANETS[idx].r * 5;
}

function orbitPosition(idx, t, phaseStart) {
    var pp = planetPos(idx, t);
    var rr = orbitRadiusFor(idx);
    var a  = (t - phaseStart) * 0.5;
    return [pp[0] + Math.cos(a) * rr, rr * 0.3, pp[2] + Math.sin(a) * rr];
}

// ═══════════════════════════════════════════════════════════════════════════
// TOUR / CAMERA STATE
// ═══════════════════════════════════════════════════════════════════════════

function newTour() {
    // Earth first, the other 7 planets in random order, then Earth again.
    var others = [];
    for (var i = 0; i < PLANETS.length; i++) if (i !== EARTH) others.push(i);
    for (var i = others.length - 1; i > 0; i--) {
        var j = Math.floor(Math.random() * (i + 1));
        var tmp = others[i]; others[i] = others[j]; others[j] = tmp;
    }
    return [EARTH].concat(others).concat([EARTH]);
}

function beginOrbit(st, t) {
    st.phase = "orbit";
    st.phaseStart = t;
    st.travel = null;
}

function beginTravel(st, t) {
    var from = st.tour[st.tourIdx];
    var to   = st.tour[st.tourIdx + 1];
    var tArr = t + TRAVEL_DURATION;

    var start = orbitPosition(from, t, st.phaseStart);
    var dpp   = planetPos(to, tArr);
    var rr    = orbitRadiusFor(to);
    // End matches orbitPosition(to, tArr, tArr) — so the hand-off to the
    // orbit phase has no visible jump.
    var end   = [dpp[0] + rr, rr * 0.3, dpp[2]];

    var line = vSub(end, start);
    var dist = vLen(line);
    var perp = vNorm([-line[2], 0, line[0]]); // perpendicular in the xz-plane
    var sa = Math.random() < 0.5 ? -1 : 1;
    var sb = Math.random() < 0.5 ? -1 : 1;

    var cp1 = vAdd(vAdd(start, vScale(line, 0.30)), vScale(perp, dist * 0.25 * sa));
    cp1[1] += dist * 0.15 * sa;
    var cp2 = vAdd(vSub(end, vScale(line, 0.30)), vScale(perp, dist * 0.25 * sb));
    cp2[1] += dist * 0.15 * sb;

    st.phase = "travel";
    st.phaseStart = t;
    st.travel = { from: from, to: to, p0: start, p1: cp1, p2: cp2, p3: end };
}

function advanceTour(st, t) {
    if (st.tourIdx + 1 >= st.tour.length) {
        // Back at Earth — reshuffle and start a new cycle.
        st.tour = newTour();
        st.tourIdx = 0;
        beginOrbit(st, t);
        return;
    }
    beginTravel(st, t);
}

function updateCamera(st, t) {
    var dur = st.phase === "orbit" ? ORBIT_DURATION : TRAVEL_DURATION;
    if (t - st.phaseStart < dur) return;
    if (st.phase === "orbit") {
        advanceTour(st, t);
    } else {
        st.tourIdx += 1;
        beginOrbit(st, t);
    }
}

function currentCamera(st, t) {
    if (st.phase === "orbit") {
        var idx = st.tour[st.tourIdx];
        return { pos: orbitPosition(idx, t, st.phaseStart), look: planetPos(idx, t) };
    }
    var s    = clamp((t - st.phaseStart) / TRAVEL_DURATION, 0, 1);
    var ease = smooth(s);
    var pos  = bezier3(st.travel.p0, st.travel.p1, st.travel.p2, st.travel.p3, ease);
    var look = vLerp(planetPos(st.travel.from, t), planetPos(st.travel.to, t), ease);
    return { pos: pos, look: look };
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
// RENDERING
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

// ── Unit sphere mesh (generated once) ──────────────────────────────────────
// Lat-long tessellation. For a unit sphere, a vertex position IS its
// outward normal, so we store one array and use it as both.
var SPHERE_LATS  = 10;
var SPHERE_LONGS = 16;

var UNIT_SPHERE = (function makeUnitSphere(lats, longs) {
    var verts = [[0, 1, 0]];                               // top pole
    for (var i = 1; i < lats; i++) {
        var theta = i * Math.PI / lats;
        var y = Math.cos(theta);
        var r = Math.sin(theta);
        for (var j = 0; j < longs; j++) {
            var phi = j * 2 * Math.PI / longs;
            verts.push([r * Math.cos(phi), y, r * Math.sin(phi)]);
        }
    }
    verts.push([0, -1, 0]);                                // bottom pole
    var bottom = verts.length - 1;
    var tris = [];
    // Top cap.
    for (var j = 0; j < longs; j++) {
        tris.push([0, 1 + ((j + 1) % longs), 1 + j]);
    }
    // Mid rings.
    for (var i = 0; i < lats - 2; i++) {
        var r1 = 1 + i * longs, r2 = 1 + (i + 1) * longs;
        for (var j = 0; j < longs; j++) {
            var jN = (j + 1) % longs;
            tris.push([r1 + j, r2 + j, r1 + jN]);
            tris.push([r1 + jN, r2 + j, r2 + jN]);
        }
    }
    // Bottom cap.
    var lastRing = 1 + (lats - 2) * longs;
    for (var j = 0; j < longs; j++) {
        tris.push([bottom, lastRing + j, lastRing + ((j + 1) % longs)]);
    }
    return { verts: verts, tris: tris };
})(SPHERE_LATS, SPHERE_LONGS);

// Draw a Sun-lit body (planet or moon) as a depth-tested triangle mesh.
// Rotation around Y gives the surface a slow spin even on untextured
// spheres — without it the Lambert term is constant and spheres look
// painted. Light direction is approximated as constant across the body
// (sun is far away relative to body size).
function renderBody(ctx, proj, wp, Rworld, col, spinAngle) {
    var cosS = Math.cos(spinAngle), sinS = Math.sin(spinAngle);

    // Precompute screen positions and world-space normals for every mesh
    // vertex, then iterate triangles. Each vertex is projected once even
    // when it belongs to several triangles.
    var N = UNIT_SPHERE.verts.length;
    var sx = new Array(N), sy = new Array(N), sz = new Array(N);
    var nx = new Array(N), ny = new Array(N), nz = new Array(N);
    for (var i = 0; i < N; i++) {
        var v  = UNIT_SPHERE.verts[i];
        // Rotate around Y (the spin axis). For a unit sphere this is also
        // the rotated normal.
        var rx = v[0] * cosS + v[2] * sinS;
        var ry = v[1];
        var rz = -v[0] * sinS + v[2] * cosS;
        nx[i] = rx; ny[i] = ry; nz[i] = rz;
        var sp = proj.project([wp[0] + rx*Rworld, wp[1] + ry*Rworld, wp[2] + rz*Rworld]);
        if (sp) {
            sx[i] = sp.x; sy[i] = sp.y; sz[i] = sp.z;
        } else {
            sz[i] = -1; // flag: behind camera
        }
    }

    var lightDir = vNorm(vSub([0, 0, 0], wp));
    var ambient = 0.08;

    // Self-occlusion is handled by the depth buffer; the outer painter's
    // sort in renderScene handles inter-body ordering.
    ctx.clearDepth();
    for (var k = 0; k < UNIT_SPHERE.tris.length; k++) {
        var tri = UNIT_SPHERE.tris[k];
        var a = tri[0], b = tri[1], c = tri[2];
        if (sz[a] < 0 || sz[b] < 0 || sz[c] < 0) continue;  // any vertex behind camera
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
    // Each planet spins on its Y axis at a rate loosely tied to its orbit
    // period for visual variety.
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

function renderScene(ctx, cam, t, w, h) {
    ctx.setFillStyle("#000008");
    ctx.fillRect(0, 0, w, h);

    var proj = makeProjector(cam.pos, cam.look, w, h);

    renderStars(ctx, proj, w, h);
    renderOrbitRings(ctx, proj);

    // Z-sort Sun, planets, and moons back-to-front (painter's algorithm).
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
    items.sort(function(a, b) { return b.z - a.z; });

    for (var k = 0; k < items.length; k++) {
        var it = items[k];
        if      (it.kind === "sun")    renderSun(ctx, it.p);
        else if (it.kind === "planet") renderPlanet(ctx, proj, it.idx, it.wp, t);
        else                           renderMoon(ctx, proj, it.m, it.wp, t);
    }
}

// ═══════════════════════════════════════════════════════════════════════════
// GAME OBJECT
// ═══════════════════════════════════════════════════════════════════════════

var Game = {
    gameName: "Voyage",
    contract: 2,

    init: function(ctx) {
        return { tour: [], tourIdx: 0, phase: "orbit", phaseStart: 0, travel: null };
    },

    begin: function(state, ctx) {
        state.tour = newTour();
        state.tourIdx = 0;
        state.phase = "orbit";
        state.phaseStart = 0;
        state.travel = null;
    },

    update: function(state, dt, events, ctx) {
        // state._gameTime is auto-injected by the engine; use it as our tour clock.
        updateCamera(state, state._gameTime || 0);
    },

    renderCanvas: function(state, me, canvas) {
        var w = canvas.width;
        var h = canvas.height;
        var t = state._gameTime || 0;
        if (!state.tour || state.tour.length < 2) {
            canvas.setFillStyle("#000008");
            canvas.fillRect(0, 0, w, h);
            return;
        }
        var cam = currentCamera(state, t);
        renderScene(canvas, cam, t, w, h);
    },

    statusBar: function(state, me) {
        if (!state.tour || state.tour.length === 0) return "Voyage";
        if (state.phase === "orbit") {
            return "Orbiting " + PLANETS[state.tour[state.tourIdx]].name;
        }
        var s = clamp(((state._gameTime || 0) - state.phaseStart) / TRAVEL_DURATION, 0, 1);
        return PLANETS[state.travel.from].name + " → " + PLANETS[state.travel.to].name
             + "  [" + Math.round(s * 100) + "%]";
    },

    commandBar: function(state, me) {
        return "Sit back and enjoy the ride";
    }
};
