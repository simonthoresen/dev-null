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

    // 12 edges of the cube as pairs of vertex indices — pure wireframe, all always drawn.
    var edges = [
        [0,1], [1,2], [2,3], [3,0],  // back face
        [4,5], [5,6], [6,7], [7,4],  // front face
        [0,4], [1,5], [2,6], [3,7]   // connecting edges
    ];

    // Project all vertices
    var pv = [];
    for (var i = 0; i < 8; i++) pv.push(proj(rot(verts[i])));

    // Draw all edges as single-pixel marks.
    ctx.setFillStyle("#000000");
    for (var e = 0; e < edges.length; e++) {
        var edge = edges[e];
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
