// Shared cube renderer for canvas harness tests.
// Draws a perspective-projected 3D cube with six colored faces on a white background.
//
// Usage: drawCube(ctx, w, h, ax, ay)
//   ctx — canvas context passed to renderCanvas
//   w, h — pixel dimensions of the canvas
//   ax   — X-axis rotation in radians
//   ay   — Y-axis rotation in radians
function drawCube(ctx, w, h, ax, ay) {
    ctx.setFillStyle("#ffffff");
    ctx.fillRect(0, 0, w, h);

    var cx = w / 2, cy = h / 2;
    var scale = Math.min(w, h) * 0.32;
    var cosX = Math.cos(ax), sinX = Math.sin(ax);
    var cosY = Math.cos(ay), sinY = Math.sin(ay);

    function rot(v) {
        var x = v[0], y = v[1], z = v[2];
        var rx = x * cosY + z * sinY;
        var rz = -x * sinY + z * cosY;
        return [rx, y * cosX - rz * sinX, y * sinX + rz * cosX];
    }

    function proj(v) {
        var d = 4.0 / (4.0 + v[2] + 2.0);
        return [cx + v[0] * scale * d, cy + v[1] * scale * d, v[2]];
    }

    var verts = [
        [-1,-1,-1],[1,-1,-1],[1,1,-1],[-1,1,-1],
        [-1,-1, 1],[1,-1, 1],[1,1, 1],[-1,1, 1]
    ];
    var pv = [];
    for (var i = 0; i < 8; i++) pv.push(proj(rot(verts[i])));

    var faces = [
        { idx: [0,1,2,3], color: "#dd2222" },  // back  — red
        { idx: [5,4,7,6], color: "#22dd22" },  // front — green
        { idx: [4,0,3,7], color: "#2222dd" },  // left  — blue
        { idx: [1,5,6,2], color: "#dddd22" },  // right — yellow
        { idx: [3,2,6,7], color: "#dd22dd" },  // top   — magenta
        { idx: [4,5,1,0], color: "#22dddd" }   // bottom— cyan
    ];

    faces.sort(function(a, b) {
        var za = 0, zb = 0;
        for (var i = 0; i < 4; i++) { za += pv[a.idx[i]][2]; zb += pv[b.idx[i]][2]; }
        return zb - za;
    });

    for (var f = 0; f < faces.length; f++) {
        var face = faces[f];
        ctx.setFillStyle(face.color);
        ctx.beginPath();
        ctx.moveTo(pv[face.idx[0]][0], pv[face.idx[0]][1]);
        for (var k = 1; k < 4; k++) ctx.lineTo(pv[face.idx[k]][0], pv[face.idx[k]][1]);
        ctx.closePath();
        ctx.fill();
    }
}
