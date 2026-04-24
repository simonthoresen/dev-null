// cube.js — spinning 3D cube with shaded ASCII block graphics
// Load with: /game load cube

// ── Static data ────────────────────────────────────────────────────────────

// Shading characters from darkest to brightest
var SHADES = [" ", ".", ":", "-", "=", "+", "*", "#", "%", "@"];
// Block shading: light to dark
var BLOCKS = [" ", " ", "░", "░", "▒", "▒", "▓", "▓", "█", "█"];

// Cube vertices (unit cube centered at origin)
var VERTICES = [
    [-1, -1, -1], [ 1, -1, -1], [ 1,  1, -1], [-1,  1, -1],
    [-1, -1,  1], [ 1, -1,  1], [ 1,  1,  1], [-1,  1,  1]
];

// Each face: [v0, v1, v2, v3] indices (CCW winding when viewed from outside)
var FACES = [
    [0, 1, 2, 3], // front  (z = -1)
    [5, 4, 7, 6], // back   (z = +1)
    [4, 0, 3, 7], // left   (x = -1)
    [1, 5, 6, 2], // right  (x = +1)
    [4, 5, 1, 0], // top    (y = -1)
    [3, 2, 6, 7]  // bottom (y = +1)
];

// Face normals (before rotation)
var NORMALS = [
    [ 0,  0, -1],
    [ 0,  0,  1],
    [-1,  0,  0],
    [ 1,  0,  0],
    [ 0, -1,  0],
    [ 0,  1,  0]
];

// Light direction (normalized-ish, pointing upper-right-front)
var LIGHT = normalize([0.6, -0.8, -0.5]);

var GAME_DURATION = 30;

// ── Pure helpers ───────────────────────────────────────────────────────────

function normalize(v) {
    var len = Math.sqrt(v[0]*v[0] + v[1]*v[1] + v[2]*v[2]);
    if (len === 0) return [0, 0, 0];
    return [v[0]/len, v[1]/len, v[2]/len];
}

function dot(a, b) {
    return a[0]*b[0] + a[1]*b[1] + a[2]*b[2];
}

function rotateX(v, a) {
    var c = Math.cos(a), s = Math.sin(a);
    return [v[0], v[1]*c - v[2]*s, v[1]*s + v[2]*c];
}
function rotateY(v, a) {
    var c = Math.cos(a), s = Math.sin(a);
    return [v[0]*c + v[2]*s, v[1], -v[0]*s + v[2]*c];
}
function rotateZ(v, a) {
    var c = Math.cos(a), s = Math.sin(a);
    return [v[0]*c - v[1]*s, v[0]*s + v[1]*c, v[2]];
}
function rotatePoint(v, ax, ay, az) {
    var r = rotateX(v, ax);
    r = rotateY(r, ay);
    r = rotateZ(r, az);
    return r;
}

function project(v, cx, cy, scale) {
    var z = v[2] + 4;
    if (z < 0.1) z = 0.1;
    var factor = scale / z;
    return [Math.round(cx + v[0] * factor * 2), Math.round(cy + v[1] * factor)];
}

function fillTriangle(cells, zbuf, w, h, p0, p1, p2, z0, z1, z2, shade) {
    var pts = [{x: p0[0], y: p0[1], z: z0}, {x: p1[0], y: p1[1], z: z1}, {x: p2[0], y: p2[1], z: z2}];
    pts.sort(function(a, b) { return a.y - b.y; });
    var a = pts[0], b = pts[1], c = pts[2];
    if (a.y === c.y) return;

    for (var y = Math.max(0, a.y); y <= Math.min(h - 1, c.y); y++) {
        var x1, x2, zl, zr;
        if (y < b.y || a.y === b.y) {
            var t1 = (c.y === a.y) ? 0 : (y - a.y) / (c.y - a.y);
            var t2;
            if (a.y === b.y) {
                t2 = (c.y === b.y) ? 0 : (y - b.y) / (c.y - b.y);
                x1 = a.x + (c.x - a.x) * t1;
                x2 = b.x + (c.x - b.x) * t2;
                zl = a.z + (c.z - a.z) * t1;
                zr = b.z + (c.z - b.z) * t2;
            } else {
                t2 = (b.y === a.y) ? 0 : (y - a.y) / (b.y - a.y);
                x1 = a.x + (c.x - a.x) * t1;
                x2 = a.x + (b.x - a.x) * t2;
                zl = a.z + (c.z - a.z) * t1;
                zr = a.z + (b.z - a.z) * t2;
            }
        } else {
            var t1b = (c.y === a.y) ? 0 : (y - a.y) / (c.y - a.y);
            var t2b = (c.y === b.y) ? 0 : (y - b.y) / (c.y - b.y);
            x1 = a.x + (c.x - a.x) * t1b;
            x2 = b.x + (c.x - b.x) * t2b;
            zl = a.z + (c.z - a.z) * t1b;
            zr = b.z + (c.z - b.z) * t2b;
        }
        if (x1 > x2) { var tmp = x1; x1 = x2; x2 = tmp; tmp = zl; zl = zr; zr = tmp; }

        var sx = Math.max(0, Math.floor(x1));
        var ex = Math.min(w - 1, Math.ceil(x2));
        for (var x = sx; x <= ex; x++) {
            var t = (x2 === x1) ? 0 : (x - x1) / (x2 - x1);
            var zz = zl + (zr - zl) * t;
            var idx = y * w + x;
            if (zz < zbuf[idx]) {
                zbuf[idx] = zz;
                cells.setChar(x, y, shade, null, null);
            }
        }
    }
}

function drawLine(cells, w, h, x0, y0, x1, y1) {
    var dx = Math.abs(x1 - x0);
    var dy = Math.abs(y1 - y0);
    var sx = x0 < x1 ? 1 : -1;
    var sy = y0 < y1 ? 1 : -1;
    var err = dx - dy;
    var steps = 0;
    var maxSteps = dx + dy + 1;
    while (steps < maxSteps) {
        if (x0 >= 0 && x0 < w && y0 >= 0 && y0 < h) {
            cells.setChar(x0, y0, "·", null, null);
        }
        if (x0 === x1 && y0 === y1) break;
        var e2 = 2 * err;
        if (e2 > -dy) { err -= dy; x0 += sx; }
        if (e2 < dx) { err += dx; y0 += sy; }
        steps++;
    }
}

function progressBar(width, fraction) {
    var barW = width - 2;
    var filled = Math.round(barW * fraction);
    var bar = "[";
    for (var i = 0; i < barW; i++) {
        bar += i < filled ? "█" : "░";
    }
    bar += "]";
    return bar;
}

function renderCube(state, cells) {
    var width = cells.width;
    var height = cells.height;
    var ax = state.angleX, ay = state.angleY, az = state.angleZ;

    var transformed = [];
    for (var i = 0; i < VERTICES.length; i++) {
        transformed.push(rotatePoint(VERTICES[i], ax, ay, az));
    }
    var rotNormals = [];
    for (var i = 0; i < NORMALS.length; i++) {
        rotNormals.push(rotatePoint(NORMALS[i], ax, ay, az));
    }

    var cx = Math.floor(width / 2);
    var cy = Math.floor(height / 2);
    var scale = Math.min(width / 4, height / 1.5);

    var zbuf = [];
    for (var i = 0; i < width * height; i++) zbuf.push(9999);

    var faceOrder = [];
    for (var f = 0; f < FACES.length; f++) {
        var avgZ = 0;
        for (var v = 0; v < 4; v++) avgZ += transformed[FACES[f][v]][2];
        faceOrder.push({ idx: f, z: avgZ / 4 });
    }
    faceOrder.sort(function(a, b) { return b.z - a.z; });

    for (var fi = 0; fi < faceOrder.length; fi++) {
        var fi0 = faceOrder[fi].idx;
        var n = rotNormals[fi0];
        if (n[2] > 0.05) continue;

        var d = dot(n, LIGHT);
        var brightness = Math.max(0, Math.min(1, (d + 1) / 2));
        brightness = 0.15 + brightness * 0.85;
        var shadeIdx = Math.floor(brightness * (BLOCKS.length - 1));
        var shade = BLOCKS[shadeIdx];

        var face = FACES[fi0];
        var pts = [];
        var zvals = [];
        for (var v = 0; v < 4; v++) {
            var tv = transformed[face[v]];
            pts.push(project(tv, cx, cy, scale));
            zvals.push(tv[2]);
        }
        fillTriangle(cells, zbuf, width, height, pts[0], pts[1], pts[2], zvals[0], zvals[1], zvals[2], shade);
        fillTriangle(cells, zbuf, width, height, pts[0], pts[2], pts[3], zvals[0], zvals[2], zvals[3], shade);
    }

    for (var fi2 = 0; fi2 < faceOrder.length; fi2++) {
        var f2 = faceOrder[fi2].idx;
        var n2 = rotNormals[f2];
        if (n2[2] > 0.05) continue;
        var face2 = FACES[f2];
        var pts2 = [];
        for (var v = 0; v < 4; v++) pts2.push(project(transformed[face2[v]], cx, cy, scale));
        for (var e = 0; e < 4; e++) {
            var p0 = pts2[e];
            var p1 = pts2[(e + 1) % 4];
            drawLine(cells, width, height, p0[0], p0[1], p1[0], p1[1]);
        }
    }
}

var Game = {
    gameName: "Spinning Cube",
    teamRange: { min: 1, max: 8 },

    splashScreen: "╔═══════════════════════════╗\n"
               +  "║     SPINNING CUBE         ║\n"
               +  "║                           ║\n"
               +  "║   A 3D cube rendered in   ║\n"
               +  "║   glorious ASCII art      ║\n"
               +  "║   with shaded blocks      ║\n"
               +  "║                           ║\n"
               +  "║   Sit back and enjoy      ║\n"
               +  "║   the show for 30s...     ║\n"
               +  "║                           ║\n"
               +  "╚═══════════════════════════╝",

    init: function(ctx) {
        return {
            elapsed: 0,
            angleX: 0,
            angleY: 0,
            angleZ: 0
        };
    },

    begin: function(state, ctx) {
        state.elapsed = 0;
        ctx.log("Spinning Cube started");
    },

    update: function(state, dt, events, ctx) {
        state.elapsed += dt;
        if (state.elapsed >= GAME_DURATION) {
            ctx.gameOver([{ name: "Spinning Cube", result: "completed" }]);
        }
        var t = state.elapsed * 0.2;
        state.angleX = t * 1.0;
        state.angleY = t * 1.3;
        state.angleZ = t * 0.7;
    },

    renderAscii: function(state, me, cells) {
        renderCube(state, cells);
    },

    statusBar: function(state, me) {
        var remaining = Math.max(0, Math.ceil(GAME_DURATION - state.elapsed));
        var elapsed = Math.floor(state.elapsed);
        return "Spinning Cube  |  " + elapsed + "s / " + GAME_DURATION + "s  |  " + remaining + "s remaining";
    },

    commandBar: function(state, me) {
        var fraction = Math.min(1, state.elapsed / GAME_DURATION);
        return progressBar(40, fraction) + "  [Enter] Chat";
    }
};
