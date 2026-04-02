// cube.js — spinning 3D cube with shaded ASCII block graphics
// Load with: /game load cube

var state = {
    tick: 0,
    maxTicks: 300, // 30 seconds at 100ms per tick
    angleX: 0,
    angleY: 0,
    angleZ: 0
};

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

// Project 3D point to 2D screen coords with perspective
function project(v, cx, cy, scale) {
    var z = v[2] + 4; // camera distance
    if (z < 0.1) z = 0.1;
    var factor = scale / z;
    // Characters are ~2x taller than wide, so scale x by 2
    return [Math.round(cx + v[0] * factor * 2), Math.round(cy + v[1] * factor)];
}

// Fill a triangle on the screen buffer using scanline
function fillTriangle(imgBuf, zbuf, w, h, p0, p1, p2, z0, z1, z2, shade) {
    // Sort by y
    var pts = [{x: p0[0], y: p0[1], z: z0}, {x: p1[0], y: p1[1], z: z1}, {x: p2[0], y: p2[1], z: z2}];
    pts.sort(function(a, b) { return a.y - b.y; });

    var a = pts[0], b = pts[1], c = pts[2];
    if (a.y === c.y) return; // degenerate

    for (var y = Math.max(0, a.y); y <= Math.min(h - 1, c.y); y++) {
        var x1, x2, zl, zr;

        // Interpolate edges
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
            var t1 = (c.y === a.y) ? 0 : (y - a.y) / (c.y - a.y);
            var t2 = (c.y === b.y) ? 0 : (y - b.y) / (c.y - b.y);
            x1 = a.x + (c.x - a.x) * t1;
            x2 = b.x + (c.x - b.x) * t2;
            zl = a.z + (c.z - a.z) * t1;
            zr = b.z + (c.z - b.z) * t2;
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
                imgBuf.setChar(x, y, shade, null, null);
            }
        }
    }
}

function renderCube(imgBuf, width, height) {
    var ax = state.angleX;
    var ay = state.angleY;
    var az = state.angleZ;

    // Transform all vertices
    var transformed = [];
    for (var i = 0; i < VERTICES.length; i++) {
        transformed.push(rotatePoint(VERTICES[i], ax, ay, az));
    }

    // Transform normals
    var rotNormals = [];
    for (var i = 0; i < NORMALS.length; i++) {
        rotNormals.push(rotatePoint(NORMALS[i], ax, ay, az));
    }

    var cx = Math.floor(width / 2);
    var cy = Math.floor(height / 2);
    var scale = Math.min(width / 4, height / 1.5);

    // Initialize z-buffer only (image buffer is the output)
    var zbuf = [];
    for (var i = 0; i < width * height; i++) {
        zbuf.push(9999);
    }

    // Sort faces by average z (painter's algorithm backup, but we use z-buffer)
    var faceOrder = [];
    for (var f = 0; f < FACES.length; f++) {
        var avgZ = 0;
        for (var v = 0; v < 4; v++) {
            avgZ += transformed[FACES[f][v]][2];
        }
        faceOrder.push({ idx: f, z: avgZ / 4 });
    }
    faceOrder.sort(function(a, b) { return b.z - a.z; });

    // Render each face
    for (var fi = 0; fi < faceOrder.length; fi++) {
        var f = faceOrder[fi].idx;
        var n = rotNormals[f];

        // Backface culling: skip faces pointing away from camera (z > 0 = facing away)
        if (n[2] > 0.05) continue;

        // Lighting: dot product with light direction
        var d = dot(n, LIGHT);
        // Map from [-1, 1] to shade index
        var brightness = Math.max(0, Math.min(1, (d + 1) / 2));
        // Add some ambient light
        brightness = 0.15 + brightness * 0.85;
        var shadeIdx = Math.floor(brightness * (BLOCKS.length - 1));
        var shade = BLOCKS[shadeIdx];

        // Project face vertices to 2D
        var face = FACES[f];
        var pts = [];
        var zvals = [];
        for (var v = 0; v < 4; v++) {
            var tv = transformed[face[v]];
            pts.push(project(tv, cx, cy, scale));
            zvals.push(tv[2]);
        }

        // Fill two triangles per quad
        fillTriangle(imgBuf, zbuf, width, height, pts[0], pts[1], pts[2], zvals[0], zvals[1], zvals[2], shade);
        fillTriangle(imgBuf, zbuf, width, height, pts[0], pts[2], pts[3], zvals[0], zvals[2], zvals[3], shade);
    }

    // Draw edges on top for crisp outline
    for (var fi = 0; fi < faceOrder.length; fi++) {
        var f = faceOrder[fi].idx;
        var n = rotNormals[f];
        if (n[2] > 0.05) continue;

        var face = FACES[f];
        var pts = [];
        for (var v = 0; v < 4; v++) {
            pts.push(project(transformed[face[v]], cx, cy, scale));
        }

        for (var e = 0; e < 4; e++) {
            var p0 = pts[e];
            var p1 = pts[(e + 1) % 4];
            drawLine(imgBuf, width, height, p0[0], p0[1], p1[0], p1[1]);
        }
    }

}

function drawLine(imgBuf, w, h, x0, y0, x1, y1) {
    var dx = Math.abs(x1 - x0);
    var dy = Math.abs(y1 - y0);
    var sx = x0 < x1 ? 1 : -1;
    var sy = y0 < y1 ? 1 : -1;
    var err = dx - dy;

    var steps = 0;
    var maxSteps = dx + dy + 1;

    while (steps < maxSteps) {
        if (x0 >= 0 && x0 < w && y0 >= 0 && y0 < h) {
            imgBuf.setChar(x0, y0, "·", null, null);
        }
        if (x0 === x1 && y0 === y1) break;
        var e2 = 2 * err;
        if (e2 > -dy) { err -= dy; x0 += sx; }
        if (e2 < dx) { err += dx; y0 += sy; }
        steps++;
    }
}

// Progress bar
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

var Game = {
    gameName: "Spinning Cube",

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

    init: function(savedState) {
        state.tick = 0;
        state.angleX = 0;
        state.angleY = 0;
        state.angleZ = 0;
    },

    start: function() {
        state.tick = 0;
        log("Spinning Cube started");
    },

    onInput: function(playerID, key) {
        // No input needed — it's a screensaver!
    },

    update: function(dt) {
        state.tick++;

        if (state.tick >= state.maxTicks) {
            gameOver();
        }

        // Smooth rotation speeds (different per axis for interesting tumble)
        var t = state.tick * 0.02;
        state.angleX = t * 1.0;
        state.angleY = t * 1.3;
        state.angleZ = t * 0.7;
    },

    render: function(buf, playerID, ox, oy, width, height) {
        renderCube(buf, width, height);
    },

    statusBar: function(playerID) {
        var remaining = Math.max(0, Math.ceil((state.maxTicks - state.tick) / 10));
        var elapsed = Math.floor(state.tick / 10);
        return "Spinning Cube  |  " + elapsed + "s / 30s  |  " + remaining + "s remaining";
    },

    commandBar: function(playerID) {
        var fraction = Math.min(1, state.tick / state.maxTicks);
        return progressBar(40, fraction) + "  [Enter] Chat";
    }
};
