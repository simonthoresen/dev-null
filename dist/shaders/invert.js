// Invert shader — swaps foreground and background colors for every pixel.
const Shader = {
    process(buf) {
        for (var y = 0; y < buf.height; y++) {
            for (var x = 0; x < buf.width; x++) {
                var p = buf.getPixel(x, y);
                if (p) {
                    buf.setChar(x, y, p.char, p.bg, p.fg, p.attr);
                }
            }
        }
    }
};
