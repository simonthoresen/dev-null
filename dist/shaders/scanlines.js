// Scanlines shader — dims every other row to simulate a CRT scanline effect.
const Shader = {
    process(buf) {
        for (var y = 0; y < buf.height; y += 2) {
            buf.recolor(0, y, buf.width, 1, null, null, ATTR_FAINT);
        }
    }
};
