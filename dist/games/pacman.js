// pacman.js — Multiplayer Pac-Man for dev-null
// Load with: /load pacman
//
// Supports both canvas rendering (Blocks/Pixels mode) and ASCII rendering.

// ============================================================
// Constants
// ============================================================
var WALL = 0, EMPTY = 1, DOT = 2, POWER = 3, DOOR = 4;
var UP = 0, DOWN = 1, LEFT = 2, RIGHT = 3;
var DX = [0, 0, -1, 1];
var DY = [-1, 1, 0, 0];
var OPPOSITE = [1, 0, 3, 2];

// ASCII colors (terminal palette)
var CWALL = "#0000AA";
var CDOT  = "#AA5500";
var CPOW  = "#FFFF55";
var CDOOR = "#555555";
var CEYES = "#555555";
var GCOL  = ["#AA0000", "#AA00AA", "#00AAAA", "#FF8700"];
var PCOL  = ["#AA5500", "#00AA00", "#00AAAA", "#AA00AA", "#AAAAAA", "#AA0000"];

// Canvas colors (RGB, full saturation)
var GCOL_CANVAS  = ["#FF0000", "#FFB8FF", "#00CCCC", "#FFB852"];
var PCOL_CANVAS  = ["#FFFF00", "#FF8800", "#00FF88", "#FF88FF", "#88FFFF", "#FF4444"];
var SCARED_COL       = "#2121DE";
var SCARED_FLASH_COL = "#FFFFFF";

// Canvas geometry
var TAU = Math.PI * 2;
var DIR_ANGLE  = [3 * Math.PI / 2, Math.PI / 2, Math.PI, 0]; // UP, DOWN, LEFT, RIGHT
var MOUTH_MAX  = Math.PI / 5;   // 36° half-angle
var MOUTH_FREQ = 8.0;           // oscillations per second
var VIEW_W = 19;                // cells visible around player (canvas)
var VIEW_H = 15;

// Player emoji sets: [invuln, normal, powered, dead]
var E_SETS = [
    ["\uD83D\uDE07", "\uD83D\uDE00", "\uD83D\uDE24", "\uD83D\uDC80"],  // 😇 😀 😤 💀
    ["\uD83D\uDE07", "\uD83D\uDE0E", "\uD83E\uDD2C", "\uD83D\uDC80"],  // 😇 😎 🤬 💀
    ["\uD83D\uDE07", "\uD83E\uDD20", "\uD83D\uDC79", "\uD83D\uDC80"],  // 😇 🤠 👹 💀
    ["\uD83D\uDE07", "\uD83E\uDD73", "\uD83D\uDE08", "\uD83D\uDC80"],  // 😇 🥳 😈 💀
    ["\uD83D\uDE07", "\uD83E\uDD29", "\uD83E\uDD75", "\uD83D\uDC80"],  // 😇 🤩 🥵 💀
    ["\uD83D\uDE07", "\uD83E\uDD13", "\uD83E\uDD2F", "\uD83D\uDC80"]   // 😇 🤓 🤯 💀
];
var E_INVULN = 0, E_NORMAL = 1, E_POWERED = 2, E_DEAD = 3;

// Ghost and item emojis
var E_GHOST  = "\uD83D\uDC7B";  // 👻
var E_EYES   = "\uD83D\uDC40";  // 👀
var E_CHERRY = "\uD83C\uDF52";  // 🍒

var GNAME = ["Blinky", "Pinky", "Inky", "Clyde"];

// Double-line box-drawing chars indexed by neighbor bitmask
// bit0=up(1), bit1=down(2), bit2=left(4), bit3=right(8)
var BOX = [
    "\u2550","\u2551","\u2551","\u2551","\u2550","\u255D","\u2557","\u2563",
    "\u2550","\u255A","\u2554","\u2560","\u2550","\u2569","\u2566","\u256C"
];

// Scoring & timing
var PTS_DOT = 10, PTS_POW = 50;
var PTS_GHOST = [200, 400, 800, 1600];
var POWER_DUR = 7.0;     // seconds
var RESPAWN_S = 2.0;     // seconds (short respawn)
var RESPAWN_L = 5.0;     // seconds (long respawn with penalty)
var INVULN = 3.0;        // seconds of invulnerability
var SPD_PAC = 0.2;       // seconds between pac moves
var SPD_GHOST = 0.2;     // seconds between ghost moves

// MIDI
var MIDI_NORMAL  = [60, 64, 67, 72, 67, 64, 60, 55];
var MIDI_POWER   = [79, 78, 77, 76, 75, 74, 73, 72, 71, 70, 69, 68];
var MIDI_STEP_DT = 0.18;   // seconds per step (normal)
var MIDI_POW_DT  = 0.09;   // faster when powered

// ============================================================
// Maze — classic 28×26 layout
// ============================================================
var MAZE_RAW = [
    "############################",
    "#............##............#",
    "#.####.#####.##.#####.####.#",
    "#o####.#####.##.#####.####o#",
    "#..........................#",
    "#.####.##.########.##.####.#",
    "#......##....##....##......#",
    "######.#####.##.#####.######",
    "     #.#####.##.#####.#     ",
    "     #.##          ##.#     ",
    "     #.## ###--### ##.#     ",
    "######.## #      # ##.######",
    "      .   #      #   .      ",
    "######.## #      # ##.######",
    "     #.## ######## ##.#     ",
    "     #.##          ##.#     ",
    "     #.#####.##.#####.#     ",
    "######.#####.##.#####.######",
    "#............##............#",
    "#.####.#####.##.#####.####.#",
    "#o..##.......  .......##..o#",
    "###.##.##.########.##.##.###",
    "#......##....##....##......#",
    "#.##########.##.##########.#",
    "#..........................#",
    "############################"
];
var MW = 28, MH = MAZE_RAW.length;
var SPAWN = {x: 14, y: 20};
var GSPAWN = [{x:14,y:9},{x:13,y:12},{x:11,y:12},{x:15,y:12}];

// ============================================================
// State
// ============================================================
var maze = [], dots = 0, wallMask = [];
var pls = {}, plOrder = [];
var ghosts = [];
var round = 1;
var pacMoveTimer = 0, ghostMoveTimer = 0;
var animTimer = 0;

// MIDI sequencer
var midiTimer = 0, midiStep = 0, midiPowStep = 0, midiWakaAlt = 0;

// ============================================================
// Helpers
// ============================================================
function rep(s, n) { var r = ""; for (var i = 0; i < n; i++) r += s; return r; }
function wrapX(x) { return x < 0 ? MW - 1 : x >= MW ? 0 : x; }

function canPass(x, y, isGhost) {
    if (y < 0 || y >= MH) return false;
    if (x < 0 || x >= MW) return true;
    var c = maze[y][x];
    return c !== WALL && (c !== DOOR || isGhost);
}

function d2(x1, y1, x2, y2) { return (x1-x2)*(x1-x2) + (y1-y2)*(y1-y2); }

function nearest(x, y) {
    var best = null, bd = 999999;
    for (var i = 0; i < plOrder.length; i++) {
        var p = pls[plOrder[i]];
        if (!p || p.dead) continue;
        var dd = d2(x, y, p.x, p.y);
        if (dd < bd) { bd = dd; best = p; }
    }
    return best;
}

function anyPowered() {
    for (var i = 0; i < plOrder.length; i++) {
        var p = pls[plOrder[i]];
        if (p && !p.dead && p.powerT > 0) return true;
    }
    return false;
}

// ============================================================
// Maze init
// ============================================================
function parseMaze() {
    maze = []; dots = 0;
    for (var y = 0; y < MH; y++) {
        var row = [];
        for (var x = 0; x < MW; x++) {
            var ch = MAZE_RAW[y].charAt(x);
            if (ch === "#") row.push(WALL);
            else if (ch === ".") { row.push(DOT); dots++; }
            else if (ch === "o") { row.push(POWER); dots++; }
            else if (ch === "-") row.push(DOOR);
            else row.push(EMPTY);
        }
        maze.push(row);
    }
    wallMask = [];
    for (var y = 0; y < MH; y++) {
        var mrow = [];
        for (var x = 0; x < MW; x++) {
            if (maze[y][x] !== WALL) { mrow.push(0); continue; }
            var m = 0;
            if (y > 0     && (maze[y-1][x] === WALL || maze[y-1][x] === DOOR)) m |= 1;
            if (y < MH-1  && (maze[y+1][x] === WALL || maze[y+1][x] === DOOR)) m |= 2;
            if (x > 0     && (maze[y][x-1] === WALL || maze[y][x-1] === DOOR)) m |= 4;
            if (x < MW-1  && (maze[y][x+1] === WALL || maze[y][x+1] === DOOR)) m |= 8;
            mrow.push(m);
        }
        wallMask.push(mrow);
    }
    syncMazeToState();
}

function resetGhosts() {
    ghosts = [];
    for (var i = 0; i < 4; i++) {
        ghosts.push({
            x: GSPAWN[i].x, y: GSPAWN[i].y,
            px: GSPAWN[i].x, py: GSPAWN[i].y,
            dir: UP, inHouse: i > 0,
            releaseTimer: i * 2.0, eaten: false, returning: false
        });
    }
    syncGhostsToState();
}

// ============================================================
// Ghost AI
// ============================================================
function gTarget(g, i) {
    var np = nearest(g.x, g.y);
    if (!np) return {x: 14, y: 12};
    // Scatter when any player is powered
    if (anyPowered() && !g.returning) {
        var c = [{x:1,y:1},{x:26,y:1},{x:1,y:MH-2},{x:26,y:MH-2}];
        return c[i];
    }
    switch (i) {
        case 0: return {x: np.x, y: np.y};
        case 1: return {x: np.x + DX[np.dir]*4, y: np.y + DY[np.dir]*4};
        case 2:
            var b = ghosts[0];
            var ax = np.x + DX[np.dir]*2, ay = np.y + DY[np.dir]*2;
            return {x: 2*ax - b.x, y: 2*ay - b.y};
        case 3:
            return d2(g.x, g.y, np.x, np.y) > 64 ? {x:np.x, y:np.y} : {x:1, y:MH-2};
    }
    return {x: np.x, y: np.y};
}

function bfsDir(sx, sy, tx, ty) {
    if (sx === tx && sy === ty) return -1;
    var visited = {};
    visited[sx + "," + sy] = true;
    var queue = [];
    for (var d = 0; d < 4; d++) {
        var nx = wrapX(sx + DX[d]), ny = sy + DY[d];
        if (!canPass(nx, ny, true)) continue;
        var key = nx + "," + ny;
        if (visited[key]) continue;
        if (nx === tx && ny === ty) return d;
        visited[key] = true;
        queue.push({x: nx, y: ny, fd: d});
    }
    var head = 0;
    while (head < queue.length) {
        var cur = queue[head++];
        for (var d = 0; d < 4; d++) {
            var nx = wrapX(cur.x + DX[d]), ny = cur.y + DY[d];
            if (!canPass(nx, ny, true)) continue;
            var key = nx + "," + ny;
            if (visited[key]) continue;
            if (nx === tx && ny === ty) return cur.fd;
            visited[key] = true;
            queue.push({x: nx, y: ny, fd: cur.fd});
        }
    }
    return -1;
}

// Check if another ghost (excluding index ei) occupies (x,y)
function ghostAt(x, y, ei) {
    for (var j = 0; j < ghosts.length; j++) {
        if (j === ei) continue;
        var o = ghosts[j];
        if (o.eaten) continue;
        if (o.x === x && o.y === y) return true;
    }
    return false;
}

function moveGhost(g, i) {
    if (g.returning) {
        if (g.x === 14 && g.y === 12) {
            g.returning = false; g.eaten = false;
            g.inHouse = true; g.releaseTimer = 3.0;
            return;
        }
        var d = bfsDir(g.x, g.y, 14, 12);
        if (d >= 0) {
            var nx = wrapX(g.x + DX[d]), ny = g.y + DY[d];
            if (!ghostAt(nx, ny, i)) {
                g.x = nx; g.y = ny; g.dir = d;
            }
        }
        return;
    }
    if (g.inHouse) {
        if (g.releaseTimer > 0) return;
        if (g.x < 13) { g.x++; return; }
        if (g.x > 14) { g.x--; return; }
        if (g.y > 9)  { g.y--; return; }
        g.inHouse = false; g.dir = LEFT;
        return;
    }
    var t = gTarget(g, i);
    var opp = OPPOSITE[g.dir];
    var prio = [UP, LEFT, DOWN, RIGHT];
    var ranked = [];
    for (var p = 0; p < 4; p++) {
        var d = prio[p];
        if (d === opp) continue;
        var nx = wrapX(g.x + DX[d]), ny = g.y + DY[d];
        if (!canPass(nx, ny, false)) continue;
        ranked.push({d: d, dist: d2(nx, ny, t.x, t.y)});
    }
    ranked.sort(function(a, b) { return a.dist - b.dist; });
    for (var r = 0; r < ranked.length; r++) {
        var d = ranked[r].d;
        var nx = wrapX(g.x + DX[d]), ny = g.y + DY[d];
        if (!ghostAt(nx, ny, i)) {
            g.x = nx; g.y = ny; g.dir = d;
            return;
        }
    }
    g.dir = opp;
}

// ============================================================
// Player
// ============================================================
function newPlayer(id, name) {
    var idx = plOrder.length;
    return {
        id: id, name: name,
        x: SPAWN.x, y: SPAWN.y, px: SPAWN.x, py: SPAWN.y,
        dir: LEFT, nextDir: LEFT,
        score: 0, lives: 3,
        dead: false, respawnTimer: 0, invulnTimer: 0,
        powerT: 0, geaten: 0,
        ci: idx % E_SETS.length,
        color: PCOL_CANVAS[idx % PCOL_CANVAS.length]
    };
}

function movePac(p) {
    var nx = wrapX(p.x + DX[p.nextDir]), ny = p.y + DY[p.nextDir];
    if (canPass(nx, ny, false)) {
        p.x = nx; p.y = ny; p.dir = p.nextDir;
    } else {
        nx = wrapX(p.x + DX[p.dir]); ny = p.y + DY[p.dir];
        if (canPass(nx, ny, false)) { p.x = nx; p.y = ny; }
    }
    var cell = maze[p.y][p.x];
    if (cell === DOT) {
        maze[p.y][p.x] = EMPTY;
        Game.state.maze[p.y * MW + p.x] = EMPTY;
        p.score += PTS_DOT; dots--;
        midiNote(0, midiWakaAlt ? 72 : 69, 110, 60);
        midiWakaAlt = 1 - midiWakaAlt;
    } else if (cell === POWER) {
        maze[p.y][p.x] = EMPTY;
        Game.state.maze[p.y * MW + p.x] = EMPTY;
        p.score += PTS_POW; dots--;
        p.powerT = POWER_DUR; p.geaten = 0;
        midiNote(0, 60, 127, 60);
        midiNote(0, 64, 127, 80);
        midiNote(0, 67, 127, 100);
        midiNote(0, 72, 127, 250);
    }
}

// ============================================================
// MIDI sequencer
// ============================================================
function tickMidi(dt) {
    midiTimer += dt;
    var isPowered = anyPowered();
    var stepDt = isPowered ? MIDI_POW_DT : MIDI_STEP_DT;
    if (midiTimer >= stepDt) {
        midiTimer -= stepDt;
        if (isPowered) {
            var note = MIDI_POWER[midiPowStep % MIDI_POWER.length];
            midiPowStep++;
            midiNote(1, note, 95, Math.floor(stepDt * 950));
        } else {
            midiPowStep = 0;
            var note = MIDI_NORMAL[midiStep % MIDI_NORMAL.length];
            midiStep++;
            midiNote(1, note, 70, Math.floor(stepDt * 950));
        }
    }
}

// ============================================================
// State sync (for canvas rendering)
// ============================================================
function syncMazeToState() {
    var flat = [];
    for (var y = 0; y < MH; y++)
        for (var x = 0; x < MW; x++)
            flat.push(maze[y][x]);
    Game.state.maze = flat;
}

function syncGhostsToState() {
    var gs = [];
    for (var i = 0; i < ghosts.length; i++) {
        var g = ghosts[i];
        gs.push({
            x: g.x, y: g.y, px: g.px, py: g.py,
            dir: g.dir, inHouse: g.inHouse,
            eaten: g.eaten, returning: g.returning,
            color: GCOL_CANVAS[i]
        });
    }
    Game.state.ghosts = gs;
}

function syncPlayerToState(p) {
    Game.state.players[p.id] = {
        x: p.x, y: p.y, px: p.px, py: p.py,
        dir: p.dir, dead: p.dead,
        powerT: p.powerT, invulnTimer: p.invulnTimer,
        respawnTimer: p.respawnTimer,
        color: p.color, name: p.name,
        score: p.score, lives: p.lives
    };
}

// ============================================================
// Update
// ============================================================
function tick(dt) {
    animTimer += dt;

    for (var i = 0; i < plOrder.length; i++) {
        var p = pls[plOrder[i]];
        if (!p) continue;
        if (p.respawnTimer > 0) p.respawnTimer -= dt;
        if (p.invulnTimer > 0) p.invulnTimer -= dt;
        if (p.powerT > 0) {
            p.powerT -= dt;
            if (p.powerT <= 0) { p.powerT = 0; p.geaten = 0; }
        }
    }
    for (var i = 0; i < ghosts.length; i++) {
        if (ghosts[i].inHouse && ghosts[i].releaseTimer > 0) {
            ghosts[i].releaseTimer -= dt;
        }
    }

    // Save previous positions for interpolation and head-on collision detection
    for (var i = 0; i < plOrder.length; i++) {
        var p = pls[plOrder[i]];
        if (p) { p.px = p.x; p.py = p.y; }
    }
    for (var i = 0; i < ghosts.length; i++) {
        ghosts[i].px = ghosts[i].x; ghosts[i].py = ghosts[i].y;
    }

    pacMoveTimer += dt;
    ghostMoveTimer += dt;

    var pacShouldMove = pacMoveTimer >= SPD_PAC;
    for (var i = 0; i < plOrder.length; i++) {
        var p = pls[plOrder[i]];
        if (!p) continue;
        if (p.dead) {
            if (p.respawnTimer <= 0) {
                p.dead = false; p.x = SPAWN.x; p.y = SPAWN.y;
                p.px = SPAWN.x; p.py = SPAWN.y;
                p.dir = LEFT; p.nextDir = LEFT;
                p.invulnTimer = INVULN;
            }
            continue;
        }
        if (p.powerT > 0 || pacShouldMove) {
            movePac(p);
        }
    }
    if (pacShouldMove) pacMoveTimer -= SPD_PAC;

    // Returning eyes move every tick (fast!)
    for (var i = 0; i < ghosts.length; i++) {
        if (ghosts[i].returning) moveGhost(ghosts[i], i);
    }
    if (ghostMoveTimer >= SPD_GHOST) {
        for (var i = 0; i < ghosts.length; i++) {
            if (!ghosts[i].returning) moveGhost(ghosts[i], i);
        }
        ghostMoveTimer -= SPD_GHOST;
    }

    // Collisions: same-cell OR head-on swap
    for (var i = 0; i < plOrder.length; i++) {
        var p = pls[plOrder[i]];
        if (!p || p.dead) continue;
        for (var g = 0; g < ghosts.length; g++) {
            var gh = ghosts[g];
            if (gh.inHouse || gh.eaten || gh.returning) continue;
            var sameCell = (p.x === gh.x && p.y === gh.y);
            var swapped = (p.x === gh.px && p.y === gh.py &&
                           gh.x === p.px && gh.y === p.py);
            if (!sameCell && !swapped) continue;
            if (p.powerT > 0) {
                gh.eaten = true; gh.returning = true;
                var b = PTS_GHOST[Math.min(p.geaten, 3)];
                p.score += b; p.geaten++;
                chat(p.name + " ate " + GNAME[g] + "! +" + b);
                midiNote(0, 84, 127, 300);
            } else if (p.invulnTimer <= 0) {
                p.lives--;
                p.dead = true;
                if (p.lives <= 0) {
                    p.respawnTimer = RESPAWN_L;
                    p.lives = 3; p.score = Math.max(0, p.score - 500);
                    chat(p.name + " was caught! Respawning with penalty...");
                } else {
                    p.respawnTimer = RESPAWN_S;
                    chat(p.name + " was caught! " + p.lives + " lives left");
                }
                midiNote(0, 36, 127, 500);
            }
        }
    }

    if (dots <= 0) {
        round++;
        chat("Round " + round + "! Maze reset!");
        parseMaze(); resetGhosts();
        for (var i = 0; i < plOrder.length; i++) {
            var p = pls[plOrder[i]];
            if (!p) continue;
            p.x = SPAWN.x; p.y = SPAWN.y;
            p.px = SPAWN.x; p.py = SPAWN.y;
            p.dir = LEFT; p.nextDir = LEFT;
            p.dead = false; p.invulnTimer = INVULN;
            p.powerT = 0; p.geaten = 0;
        }
    }

    // Sync canvas state
    Game.state.animTimer      = animTimer;
    Game.state.pacMoveTimer   = pacMoveTimer;
    Game.state.ghostMoveTimer = ghostMoveTimer;
    Game.state.powered        = anyPowered();
    Game.state.round          = round;
    for (var i = 0; i < plOrder.length; i++) {
        var p = pls[plOrder[i]];
        if (p) syncPlayerToState(p);
    }
    syncGhostsToState();

    tickMidi(dt);
}

// ============================================================
// Canvas rendering helpers
// ============================================================

// Interpolate entity world position (handles horizontal tunnel wrap).
function interpPos(ex, ey, epx, epy, frac) {
    var dx = ex - epx;
    if (dx >  MW / 2) dx -= MW;
    if (dx < -MW / 2) dx += MW;
    return {
        wx: epx + dx * frac,
        wy: epy + (ey - epy) * frac
    };
}

// Draw a pacman: circle with a wedge-shaped mouth cut out.
function drawPacman(ctx, cx, cy, r, color, dir, mouthAngle, dead, blinkOn) {
    if (dead) {
        ctx.setFillStyle("#555555");
        ctx.fillCircle(cx, cy, r * 0.3);
        return;
    }
    var bodyColor = blinkOn ? "#FFFFFF" : color;
    ctx.setFillStyle(bodyColor);
    ctx.beginPath();
    if (mouthAngle > 0.02) {
        var a = DIR_ANGLE[dir];
        ctx.arc(cx, cy, r, a + mouthAngle, a - mouthAngle + TAU);
        ctx.lineTo(cx, cy);
        ctx.closePath();
    } else {
        ctx.arc(cx, cy, r, 0, TAU);
    }
    ctx.fill();
    if (!dead) {
        var eyeA = DIR_ANGLE[dir] - Math.PI / 2;
        ctx.setFillStyle("#000000");
        ctx.fillCircle(
            cx + Math.cos(eyeA) * r * 0.45,
            cy + Math.sin(eyeA) * r * 0.45,
            r * 0.13
        );
    }
}

// Draw a ghost: top semicircle + body + wavy skirt + eyes.
function drawGhost(ctx, cx, cy, r, color, scared, flashWhite, dir) {
    var bodyColor = scared ? (flashWhite ? SCARED_FLASH_COL : SCARED_COL) : color;
    ctx.setFillStyle(bodyColor);
    ctx.beginPath();
    ctx.arc(cx, cy, r, Math.PI, TAU);
    ctx.lineTo(cx + r, cy + r * 0.95);
    var bw = (r * 2) / 3;
    for (var b = 2; b >= 0; b--) {
        var tipX = cx - r + b * bw;
        ctx.quadraticCurveTo(
            tipX + bw * 0.5, cy + r * 0.45,
            tipX,            cy + r * 0.95
        );
    }
    ctx.lineTo(cx - r, cy);
    ctx.closePath();
    ctx.fill();
    if (scared) {
        ctx.setFillStyle(flashWhite ? "#0000AA" : "#FFFFFF");
        ctx.fillCircle(cx - r * 0.35, cy - r * 0.05, r * 0.13);
        ctx.fillCircle(cx + r * 0.35, cy - r * 0.05, r * 0.13);
        ctx.setFillStyle(flashWhite ? "#0000AA" : "#FFFFFF");
        ctx.fillCircle(cx - r * 0.4,  cy + r * 0.22, r * 0.09);
        ctx.fillCircle(cx - r * 0.15, cy + r * 0.32, r * 0.09);
        ctx.fillCircle(cx + r * 0.15, cy + r * 0.22, r * 0.09);
        ctx.fillCircle(cx + r * 0.4,  cy + r * 0.32, r * 0.09);
    } else {
        ctx.setFillStyle("#FFFFFF");
        ctx.fillCircle(cx - r * 0.35, cy - r * 0.08, r * 0.23);
        ctx.fillCircle(cx + r * 0.35, cy - r * 0.08, r * 0.23);
        ctx.setFillStyle("#0000CC");
        var pdx = DX[dir] * r * 0.10;
        var pdy = DY[dir] * r * 0.10;
        ctx.fillCircle(cx - r * 0.35 + pdx, cy - r * 0.08 + pdy, r * 0.13);
        ctx.fillCircle(cx + r * 0.35 + pdx, cy - r * 0.08 + pdy, r * 0.13);
    }
}

// Ghost eyes only (returning state)
function drawEyes(ctx, cx, cy, r) {
    ctx.setFillStyle("#FFFFFF");
    ctx.fillCircle(cx - r * 0.35, cy - r * 0.15, r * 0.28);
    ctx.fillCircle(cx + r * 0.35, cy - r * 0.15, r * 0.28);
    ctx.setFillStyle("#0055FF");
    ctx.fillCircle(cx - r * 0.35, cy - r * 0.15, r * 0.16);
    ctx.fillCircle(cx + r * 0.35, cy - r * 0.15, r * 0.16);
}

// ============================================================
// ASCII rendering — player-centered camera viewport
// ============================================================
function renderAscii(buf, pid, width, height, cw) {
    if (cw === undefined) cw = (width >= 60) ? 2 : 1;
    var viewCols = Math.floor(width / cw);
    var viewRows = height;

    var me = pls[pid];
    var cx = me && !me.dead ? me.x : Math.floor(MW / 2);
    var cy = me && !me.dead ? me.y : Math.floor(MH / 2);

    var startX = cx - Math.floor(viewCols / 2);
    var startY = cy - Math.floor(viewRows / 2);

    if (viewCols >= MW) {
        startX = -Math.floor((viewCols - MW) / 2);
    } else {
        if (startX < 0) startX = 0;
        if (startX + viewCols > MW) startX = MW - viewCols;
    }
    if (viewRows >= MH) {
        startY = -Math.floor((viewRows - MH) / 2);
    } else {
        if (startY < 0) startY = 0;
        if (startY + viewRows > MH) startY = MH - viewRows;
    }

    var ents = {};

    for (var g = 0; g < ghosts.length; g++) {
        var gh = ghosts[g];
        if (gh.eaten && !gh.returning) continue;
        var k = gh.x + "," + gh.y;
        if (gh.returning) {
            if (cw === 2) {
                ents[k] = {ch: E_EYES, fg: CEYES, bg: null, emoji: true};
            } else {
                ents[k] = {ch: ".", fg: CEYES, bg: null};
            }
        } else {
            if (cw === 2) {
                ents[k] = {ch: E_GHOST, fg: GCOL[g], bg: null, emoji: true};
            } else {
                ents[k] = {ch: "M", fg: GCOL[g], bg: null};
            }
        }
    }

    for (var i = 0; i < plOrder.length; i++) {
        var p = pls[plOrder[i]];
        if (!p) continue;
        var k = p.x + "," + p.y;
        var set = E_SETS[p.ci % E_SETS.length];
        if (cw === 2) {
            var emoji;
            if (p.dead) {
                emoji = set[E_DEAD];
            } else if (p.invulnTimer > 0) {
                emoji = set[E_INVULN];
            } else if (p.powerT > 0) {
                emoji = set[E_POWERED];
            } else {
                emoji = set[E_NORMAL];
            }
            ents[k] = {ch: emoji, fg: null, bg: null, emoji: true};
        } else {
            if (p.dead) {
                ents[k] = {ch: "X", fg: "#555555", bg: null};
            } else {
                var col = PCOL[p.ci % PCOL.length];
                ents[k] = {ch: "@", fg: col, bg: null, bold: (plOrder[i] === pid)};
            }
        }
    }

    for (var row = 0; row < viewRows; row++) {
        var my = startY + row;
        if (my < 0 || my >= MH) continue;

        for (var col = 0; col < viewCols; col++) {
            var mx = startX + col;
            var screenCol = col * cw;
            if (mx < 0 || mx >= MW) continue;

            var k = mx + "," + my;
            var e = ents[k];
            if (e) {
                if (e.emoji) {
                    buf.writeString(screenCol, row, e.ch, e.fg, e.bg || null);
                } else if (e.bold) {
                    buf.setChar(screenCol, row, e.ch, e.fg, e.bg || null, ATTR_BOLD);
                    if (cw === 2) buf.setChar(screenCol + 1, row, " ", e.fg, e.bg || null);
                } else {
                    buf.setChar(screenCol, row, e.ch, e.fg, e.bg || null);
                    if (cw === 2) buf.setChar(screenCol + 1, row, " ", e.fg, e.bg || null);
                }
                continue;
            }

            var c = maze[my][mx];
            if (cw === 2) {
                if (c === WALL) {
                    buf.setChar(screenCol, row, "\u2588", CWALL, null);
                    buf.setChar(screenCol + 1, row, "\u2588", CWALL, null);
                } else if (c === DOT) {
                    buf.setChar(screenCol, row, "\u2810", CDOT, null);
                    buf.setChar(screenCol + 1, row, "\u2802", CDOT, null);
                } else if (c === POWER) {
                    buf.writeString(screenCol, row, E_CHERRY, null, null);
                } else if (c === DOOR) {
                    buf.setChar(screenCol, row, "\u2500", CDOOR, null);
                    buf.setChar(screenCol + 1, row, "\u2500", CDOOR, null);
                }
            } else {
                if (c === WALL) {
                    buf.setChar(screenCol, row, BOX[wallMask[my][mx]], CWALL, null);
                } else if (c === DOT) {
                    buf.setChar(screenCol, row, "\u2022", CDOT, null);
                } else if (c === POWER) {
                    buf.setChar(screenCol, row, "\u25cf", CPOW, null);
                } else if (c === DOOR) {
                    buf.setChar(screenCol, row, "\u2500", CDOOR, null);
                }
            }
        }
    }
}

// ============================================================
// Commands
// ============================================================
registerCommand({
    name: "score",
    description: "Show the Pac-Man scoreboard",
    handler: function(pid, isAdmin, args) {
        var sorted = plOrder.slice().sort(function(a, b) {
            return (pls[b] ? pls[b].score : 0) - (pls[a] ? pls[a].score : 0);
        });
        var lines = ["--- PAC-MAN SCOREBOARD ---"];
        for (var i = 0; i < sorted.length; i++) {
            var p = pls[sorted[i]];
            if (!p) continue;
            lines.push((i+1) + ". " + p.name + ": " + p.score + " pts (" + rep("\u2665", p.lives) + ")");
        }
        if (sorted.length === 0) lines.push("No players yet!");
        for (var i = 0; i < lines.length; i++) chatPlayer(pid, lines[i]);
    }
});

registerCommand({
    name: "reset",
    description: "Reset the Pac-Man game",
    adminOnly: true,
    handler: function(pid, isAdmin, args) {
        round = 1;
        pacMoveTimer = 0; ghostMoveTimer = 0; animTimer = 0;
        midiTimer = 0; midiStep = 0; midiPowStep = 0; midiWakaAlt = 0;
        parseMaze(); resetGhosts();
        for (var i = 0; i < plOrder.length; i++) {
            var p = pls[plOrder[i]];
            if (!p) continue;
            p.score = 0; p.lives = 3;
            p.x = SPAWN.x; p.y = SPAWN.y;
            p.px = SPAWN.x; p.py = SPAWN.y;
            p.dir = LEFT; p.nextDir = LEFT;
            p.dead = false; p.invulnTimer = INVULN;
            p.respawnTimer = 0;
            p.powerT = 0; p.geaten = 0;
            syncPlayerToState(p);
        }
        Game.state.round = 1;
        chat("Game reset by admin!");
    }
});

// ============================================================
// Game API
// ============================================================
var Game = {
    state: {
        players: {},
        ghosts: [],
        maze: [],
        round: 1,
        pacMoveTimer: 0,
        ghostMoveTimer: 0,
        animTimer: 0,
        SPD_PAC: SPD_PAC,
        SPD_GHOST: SPD_GHOST,
        powered: false
    },

    load: function(state) {},

    onPlayerJoin: function(playerID, playerName) {
        pls[playerID] = newPlayer(playerID, playerName);
        plOrder.push(playerID);
        syncPlayerToState(pls[playerID]);
        chat(playerName + " joined Pac-Man!");
    },

    onPlayerLeave: function(playerID) {
        var idx = plOrder.indexOf(playerID);
        if (idx >= 0) plOrder.splice(idx, 1);
        delete pls[playerID];
        delete Game.state.players[playerID];
    },

    onInput: function(playerID, key) {
        var p = pls[playerID];
        if (!p || p.dead) return;
        if (key === "up") p.nextDir = UP;
        else if (key === "down") p.nextDir = DOWN;
        else if (key === "left") p.nextDir = LEFT;
        else if (key === "right") p.nextDir = RIGHT;
    },

    update: function(dt) {
        tick(dt);
    },

    renderCanvas: function(ctx, playerID, w, h) {
        var st = Game.state;

        // Use module-level constants — st.SPD_PAC/SPD_GHOST are not in the
        // synced JSON so reading them from state would give undefined → NaN.
        var pacFrac   = Math.min(st.pacMoveTimer   / SPD_PAC,   1.0);
        var ghostFrac = Math.min(st.ghostMoveTimer / SPD_GHOST, 1.0);
        var t         = st.animTimer;

        // Choose a cell size that fills the canvas as much as possible.
        // Fit the full maze (MW×MH) if there's enough room; otherwise zoom in.
        var CELL  = Math.max(4, Math.floor(Math.min(w / MW, h / MH)));
        var halfC = CELL * 0.5;

        // How many maze cells fit in the canvas at this cell size
        var viewW = Math.min(MW, Math.floor(w / CELL));
        var viewH = Math.min(MH, Math.floor(h / CELL));

        var me = st.players[playerID];
        var camWX, camWY;
        if (me && !me.dead) {
            var ip = interpPos(me.x, me.y, me.px, me.py, pacFrac);
            camWX = ip.wx; camWY = ip.wy;
        } else if (me && me.dead) {
            camWX = SPAWN.x; camWY = SPAWN.y;
        } else {
            camWX = MW / 2; camWY = MH / 2;
        }

        // Camera: show full maze if it fits, otherwise follow the player
        var startWX, startWY;
        if (viewW >= MW) {
            startWX = 0;
        } else {
            startWX = camWX - viewW * 0.5;
            if (startWX < 0) startWX = 0;
            if (startWX + viewW > MW) startWX = MW - viewW;
        }
        if (viewH >= MH) {
            startWY = 0;
        } else {
            startWY = camWY - viewH * 0.5;
            if (startWY < 0) startWY = 0;
            if (startWY + viewH > MH) startWY = MH - viewH;
        }

        var offX = (w - viewW * CELL) * 0.5;
        var offY = (h - viewH * CELL) * 0.5;

        function toSX(wx) { return offX + (wx - startWX) * CELL; }
        function toSY(wy) { return offY + (wy - startWY) * CELL; }

        // Black background
        ctx.setFillStyle("#000000");
        ctx.fillRect(0, 0, w, h);

        // Maze tiles
        var mx0 = Math.floor(startWX) - 1;
        var my0 = Math.floor(startWY) - 1;
        var mx1 = mx0 + viewW + 3;
        var my1 = my0 + viewH + 3;

        for (var my = my0; my <= my1; my++) {
            if (my < 0 || my >= MH) continue;
            for (var mx = mx0; mx <= mx1; mx++) {
                var wmx  = ((mx % MW) + MW) % MW;
                var cell = st.maze[my * MW + wmx];
                var sx   = toSX(mx);
                var sy   = toSY(my);

                if (cell === WALL) {
                    ctx.setFillStyle("#0033BB");
                    ctx.fillRect(sx, sy, CELL + 1, CELL + 1);
                    ctx.setFillStyle("#0022AA");
                    ctx.fillRect(sx + 1, sy + 1, CELL - 1, CELL - 1);
                } else if (cell === DOT) {
                    var r = CELL * 0.11;
                    ctx.setFillStyle("#FFFFFF");
                    ctx.fillCircle(sx + halfC, sy + halfC, r);
                } else if (cell === POWER) {
                    var pulse = 0.82 + Math.sin(t * 5.0) * 0.18;
                    var r = CELL * 0.30 * pulse;
                    ctx.setFillStyle("#CC0000");
                    ctx.fillCircle(sx + halfC, sy + halfC, r);
                    ctx.setFillStyle("#FF6666");
                    ctx.fillCircle(sx + halfC - r * 0.28, sy + halfC - r * 0.28, r * 0.32);
                } else if (cell === DOOR) {
                    ctx.setFillStyle("#FF99FF");
                    ctx.fillRect(sx + 1, sy + CELL * 0.42, CELL - 2, CELL * 0.16);
                }
            }
        }

        // Ghosts
        var isPowered = st.powered;
        for (var i = 0; i < st.ghosts.length; i++) {
            var g  = st.ghosts[i];
            if (g.eaten && !g.returning) continue;
            var gfrac = g.returning ? 1.0 : ghostFrac;
            var ip = interpPos(g.x, g.y, g.px, g.py, gfrac);
            var sx = toSX(ip.wx + 0.5);
            var sy = toSY(ip.wy + 0.5);
            var r  = CELL * 0.44;

            if (g.returning) {
                drawEyes(ctx, sx, sy, r);
            } else if (isPowered) {
                var flash = (me && me.powerT > 0 && me.powerT < 2.0)
                    ? (Math.floor(t * 6) % 2 === 1) : false;
                drawGhost(ctx, sx, sy, r, g.color, true, flash, g.dir);
            } else {
                drawGhost(ctx, sx, sy, r, g.color, false, false, g.dir);
            }
        }

        // Players
        for (var pid in st.players) {
            var p  = st.players[pid];
            var ip = interpPos(p.x, p.y, p.px, p.py, pacFrac);
            var sx = toSX(ip.wx + 0.5);
            var sy = toSY(ip.wy + 0.5);
            var r  = CELL * 0.44;

            var mouthAngle = 0;
            if (!p.dead) {
                mouthAngle = Math.abs(Math.sin(t * MOUTH_FREQ)) * MOUTH_MAX;
            }
            var blinkOn = (p.invulnTimer > 0) && (Math.floor(t * 8) % 2 === 0);

            drawPacman(ctx, sx, sy, r, p.color, p.dir, mouthAngle, p.dead, blinkOn);
        }
    },

    renderAscii: function(buf, playerID, ox, oy, width, height) {
        renderAscii(buf, playerID, width, height, 1);
    },

    statusBar: function(playerID) {
        var p = pls[playerID];
        if (!p) return "PAC-MAN";
        var h = rep("\u2665", p.lives);
        var pw = p.powerT > 0 ? "  POWER!" : "";
        return "PAC-MAN | Score: " + p.score + " | " + h + " | Rnd " + round + pw;
    },

    commandBar: function(playerID) {
        var p = pls[playerID];
        if (p && p.dead) return "Respawning...  /score for scoreboard";
        return "[\u2191\u2193\u2190\u2192] Move  [Enter] Chat  /score Scoreboard";
    }
};

// ============================================================
// Init — after Game is defined so state sync works
// ============================================================
parseMaze();
resetGhosts();
midiProgram(0, 80);   // Ch0: Square Lead — SFX (pellets, hits)
midiProgram(1, 80);   // Ch1: Square Lead — background chiptune
midiCC(0, 7, 110);    // Ch0 volume
midiCC(1, 7,  65);    // Ch1 volume (quieter background)
