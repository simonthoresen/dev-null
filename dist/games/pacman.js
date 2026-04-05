// pacman.js — Multiplayer Pac-Man for dev-null
// Load with: /load pacman

// ============================================================
// Constants
// ============================================================
var WALL = 0, EMPTY = 1, DOT = 2, POWER = 3, DOOR = 4;
var UP = 0, DOWN = 1, LEFT = 2, RIGHT = 3;
var DX = [0, 0, -1, 1];
var DY = [-1, 1, 0, 0];
var OPPOSITE = [1, 0, 3, 2];

// Hex colors
var CWALL = "#0000AA";
var CDOT  = "#AA5500";
var CPOW  = "#FFFF55";
var CDOOR = "#555555";
var CEYES = "#555555";
var GCOL  = ["#AA0000", "#AA00AA", "#00AAAA", "#FF8700"];
var PCOL  = ["#AA5500", "#00AA00", "#00AAAA", "#AA00AA", "#AAAAAA", "#AA0000"];

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
var animTimer = 0; // for animation frames (replaces frame counter in render)

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
}

function resetGhosts() {
    ghosts = [];
    for (var i = 0; i < 4; i++) {
        ghosts.push({
            x: GSPAWN[i].x, y: GSPAWN[i].y,
            dir: UP, inHouse: i > 0,
            releaseTimer: i * 2.0, eaten: false, returning: false
        });
    }
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
            // else wait a frame
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
    // Rank all valid directions by distance to target
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
    // Try best direction first; if blocked by another ghost, try next
    for (var r = 0; r < ranked.length; r++) {
        var d = ranked[r].d;
        var nx = wrapX(g.x + DX[d]), ny = g.y + DY[d];
        if (!ghostAt(nx, ny, i)) {
            g.x = nx; g.y = ny; g.dir = d;
            return;
        }
    }
    // All directions blocked by other ghosts — stay put, reverse direction
    g.dir = opp;
}

// ============================================================
// Player
// ============================================================
function newPlayer(id, name) {
    return {
        id: id, name: name,
        x: SPAWN.x, y: SPAWN.y, dir: LEFT, nextDir: LEFT,
        score: 0, lives: 3,
        dead: false, respawnTimer: 0, invulnTimer: 0,
        powerT: 0, geaten: 0,
        ci: plOrder.length % E_SETS.length
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
        maze[p.y][p.x] = EMPTY; p.score += PTS_DOT; dots--;
    } else if (cell === POWER) {
        maze[p.y][p.x] = EMPTY; p.score += PTS_POW; dots--;
        p.powerT = POWER_DUR; p.geaten = 0;
    }
}

// ============================================================
// Update
// ============================================================
function tick(dt) {
    animTimer += dt;

    // Decrement all timers by dt
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

    // Save previous positions for head-on collision detection
    for (var i = 0; i < plOrder.length; i++) {
        var p = pls[plOrder[i]];
        if (p) { p.px = p.x; p.py = p.y; }
    }
    for (var i = 0; i < ghosts.length; i++) {
        ghosts[i].px = ghosts[i].x; ghosts[i].py = ghosts[i].y;
    }

    // Accumulate movement timers
    pacMoveTimer += dt;
    ghostMoveTimer += dt;

    // Move players — powered players move every tick, others at SPD_PAC interval
    var pacShouldMove = pacMoveTimer >= SPD_PAC;
    for (var i = 0; i < plOrder.length; i++) {
        var p = pls[plOrder[i]];
        if (!p) continue;
        if (p.dead) {
            if (p.respawnTimer <= 0) {
                p.dead = false; p.x = SPAWN.x; p.y = SPAWN.y;
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
    // Ghosts move at normal speed
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
                // This player is powered — eat the ghost
                gh.eaten = true; gh.returning = true;
                var b = PTS_GHOST[Math.min(p.geaten, 3)];
                p.score += b; p.geaten++;
                chat(p.name + " ate " + GNAME[g] + "! +" + b);
            } else if (p.invulnTimer <= 0) {
                // Ghost kills this player
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
            p.dir = LEFT; p.nextDir = LEFT;
            p.dead = false; p.invulnTimer = INVULN;
            p.powerT = 0; p.geaten = 0;
        }
    }
}

// ============================================================
// Rendering — player-centered camera viewport
// ============================================================
function render(buf, pid, width, height) {
    var cw = (width >= 60) ? 2 : 1;
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

    // Entity map: {ch, fg, bg, emoji}
    var ents = {};

    // Ghosts — always 👻 (or 👀 when returning)
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

    // Players — 4-state emoji
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
                var poN = ["^", "v", "<", ">"];
                var ch = (Math.floor(animTimer * 10) % 6 < 3) ? poN[p.dir] : "o";
                ents[k] = {ch: ch, fg: col, bg: null, bold: (plOrder[i] === pid)};
            }
        }
    }

    // Render viewport
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
// Init
// ============================================================
parseMaze();
resetGhosts();

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
        parseMaze(); resetGhosts();
        for (var i = 0; i < plOrder.length; i++) {
            var p = pls[plOrder[i]];
            if (!p) continue;
            p.score = 0; p.lives = 3;
            p.x = SPAWN.x; p.y = SPAWN.y;
            p.dir = LEFT; p.nextDir = LEFT;
            p.dead = false; p.invulnTimer = INVULN;
            p.respawnTimer = 0;
            p.powerT = 0; p.geaten = 0;
        }
        chat("Game reset by admin!");
    }
});

// ============================================================
// Game API
// ============================================================
var Game = {
    onPlayerJoin: function(playerID, playerName) {
        pls[playerID] = newPlayer(playerID, playerName);
        plOrder.push(playerID);
        chat(playerName + " joined Pac-Man!");
    },

    onPlayerLeave: function(playerID) {
        var idx = plOrder.indexOf(playerID);
        if (idx >= 0) plOrder.splice(idx, 1);
        delete pls[playerID];
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

    render: function(buf, playerID, ox, oy, width, height) {
        render(buf, playerID, width, height);
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
