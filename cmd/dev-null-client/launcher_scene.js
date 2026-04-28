var Game = {
  resolveMe: function(state, pid) { return { id: pid }; },
  renderCanvas: function(state, me, canvas) {
    var w = canvas.width, h = canvas.height;
    var t = state._gameTime || 0;
    var cx = w * 0.5, cy = h * 0.5;
    var cam = t * 0.08;
    var tilt = 0.30;

    canvas.setFillStyle("#03050d");
    canvas.fillRect(0, 0, w, h);

    // Star field.
    var stars = 180;
    for (var i = 0; i < stars; i++) {
      var sx = ((i * 137 + 53) % w);
      var sy = ((i * 89 + 17) % h);
      var tw = 0.55 + 0.45 * Math.sin(t * 1.7 + i * 0.37);
      var v = Math.floor(110 + tw * 120);
      canvas.setFillStyle("rgba(" + v + "," + v + "," + (v + 20) + ",0.9)");
      canvas.fillRect(sx, sy, 1, 1);
    }

    function orbitRing(r, col) {
      canvas.setStrokeStyle(col);
      canvas.setLineWidth(1);
      canvas.beginPath();
      var started = false;
      for (var a = 0; a <= 6.283; a += 0.08) {
        var x = Math.cos(a) * r;
        var z = Math.sin(a) * r;
        var px = cx + x * Math.cos(cam) + z * Math.sin(cam) * 0.45;
        var py = cy + z * Math.cos(cam) * tilt;
        if (!started) { canvas.moveTo(px, py); started = true; }
        else { canvas.lineTo(px, py); }
      }
      canvas.stroke();
    }

    function planet(r, speed, phase, size, col) {
      var a = phase + t * speed;
      var x = Math.cos(a) * r;
      var z = Math.sin(a) * r;
      var px = cx + x * Math.cos(cam) + z * Math.sin(cam) * 0.45;
      var py = cy + z * Math.cos(cam) * tilt;
      var depth = 0.55 + 0.45 * Math.sin(a - cam);
      var pr = size * (0.65 + depth * 0.7);
      canvas.setFillStyle(col);
      canvas.fillCircle(px, py, pr);
    }

    // Sun glow.
    for (var g = 5; g >= 1; g--) {
      canvas.setFillStyle("rgba(255,190,80," + (0.05 * g) + ")");
      canvas.fillCircle(cx, cy, 20 + g * 11);
    }
    canvas.setFillStyle("#ffe08a");
    canvas.fillCircle(cx, cy, 18);
    canvas.setFillStyle("#fff6d0");
    canvas.fillCircle(cx - 3, cy - 3, 7);

    orbitRing(70, "rgba(80,110,180,0.35)");
    orbitRing(110, "rgba(90,120,180,0.28)");
    orbitRing(150, "rgba(100,130,170,0.24)");
    orbitRing(200, "rgba(110,140,160,0.20)");

    planet(70, 1.15, 0.9, 4, "#a8a8a8");
    planet(110, 0.75, 2.4, 6, "#d9c082");
    planet(150, 0.52, 4.8, 6, "#3f7de2");
    planet(200, 0.28, 1.7, 10, "#d4a66f");
  }
};
