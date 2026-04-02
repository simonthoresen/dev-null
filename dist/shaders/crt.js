// CRT shader — tints the screen green on a dark background for a retro terminal look.
const Shader = {
    process(buf) {
        var green = "#33ff33";
        var darkBg = "#0a1a0a";
        for (var y = 0; y < buf.height; y++) {
            for (var x = 0; x < buf.width; x++) {
                var p = buf.getPixel(x, y);
                if (p) {
                    buf.setChar(x, y, p.char, green, darkBg, p.attr);
                }
            }
        }
    }
};
