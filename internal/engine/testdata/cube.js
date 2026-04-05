// Shared cube renderer for canvas harness tests.
// Draws a wireframe 3D cube with back-face edge culling on a white background.
//
// Coordinate system (as seen by renderCanvas after the engine's scaleY=2 correction):
//   w = 2 × cellWidth   (2 logical X units per terminal cell)
//   h = cellHeight      (1 logical Y unit per terminal cell)
//
// For a visually square cube on a 2:1-aspect terminal font (8×16 px cells):
//   projX uses sx = 4 × base  (2× for cell isotropy, 2× more for font aspect ratio)
//   projY uses sy = base
// where base = (h/2) × 0.65, chosen so the cube fits within both axes with margin.
// This reproduces the same on-screen pixel size as the previous code that applied
// a "* 0.5" Y correction directly inside the projection function.
//
// Lines are drawn as single-pixel marks (1×1 logical px) so sub-cell edges produce
// half-block quadrant characters and the wireframe structure is clearly visible.
//
// Usage: drawCube(ctx, w, h, ax, ay)
//   ctx — canvas context passed to renderCanvas
//   w, h — logical dimensions of the canvas
//   ax   — X-axis rotation in radians
//   ay   — Y-axis rotation in radians
function drawCube(ctx, w, h, ax, ay) {
    ctx.setFillStyle("#ffffff");
    ctx.fillRect(0, 0, w, h);

    var cx = w / 2, cy = h / 2;
    var base = h / 2 * 0.65;
    var sx = base * 4;   // X projection scale
    var sy = base;       // Y projection scale

    var cosX = Math.cos(ax), sinX = Math.sin(ax);
    var cosY = Math.cos(ay), sinY = Math.sin(ay);

    function rot(v) {
        var x = v[0], y = v[1], z = v[2];
        var rx = x * cosY + z * sinY;
        var rz = -x * sinY + z * cosY;
        return [rx, y * cosX - rz * sinX, y * sinX + rz * cosX];
    }

    function proj(v) {
        var d = 4.0 / (4.0 + v[2] + 2.5);
        return [cx + v[0] * sx * d, cy + v[1] * sy * d];
    }

    var verts = [
        [-1,-1,-1],[1,-1,-1],[1,1,-1],[-1,1,-1],
        [-1,-1, 1],[1,-1, 1],[1,1, 1],[-1,1, 1]
    ];

    // 6 face normals (outward) and their vertex indices
    var faces = [
        { normal: [ 0,  0, -1], idx: [0,1,2,3] },  // back
        { normal: [ 0,  0,  1], idx: [4,5,6,7] },  // front
        { normal: [-1,  0,  0], idx: [4,0,3,7] },  // left
        { normal: [ 1,  0,  0], idx: [1,5,6,2] },  // right
        { normal: [ 0, -1,  0], idx: [4,5,1,0] },  // bottom
        { normal: [ 0,  1,  0], idx: [3,2,6,7] }   // top
    ];

    // Determine which faces are front-facing (rotated normal z > 0 means facing camera)
    var vis = [];
    for (var f = 0; f < faces.length; f++) {
        var rn = rot(faces[f].normal);
        vis.push(rn[2] > 0);
    }

    // 12 edges: each edge has [v0, v1, faceA, faceB]
    // Draw edge only if at least one adjacent face is front-facing
    var edges = [
        // back face edges
        [0,1,0,4], [1,2,0,3], [2,3,0,5], [3,0,0,2],
        // front face edges
        [4,5,1,4], [5,6,1,3], [6,7,1,5], [7,4,1,2],
        // connecting edges (back to front)
        [0,4,2,4], [1,5,3,4], [2,6,3,5], [3,7,2,5]
    ];

    // Project all vertices
    var pv = [];
    for (var i = 0; i < 8; i++) pv.push(proj(rot(verts[i])));

    // Draw visible edges as single-pixel marks so sub-cell positions produce
    // half-block quadrant characters at diagonal edges.
    ctx.setFillStyle("#000000");
    for (var e = 0; e < edges.length; e++) {
        var edge = edges[e];
        if (!vis[edge[2]] && !vis[edge[3]]) continue;
        var p0 = pv[edge[0]], p1 = pv[edge[1]];
        var dx = p1[0] - p0[0], dy = p1[1] - p0[1];
        var steps = Math.ceil(Math.sqrt(dx*dx + dy*dy));
        if (steps < 1) steps = 1;
        for (var s = 0; s <= steps; s++) {
            var t = s / steps;
            var px = Math.round(p0[0] + dx * t);
            var py = Math.round(p0[1] + dy * t);
            ctx.fillRect(px, py, 1, 1);
        }
    }
}
