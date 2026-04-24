// pacman.js — Multiplayer Pac-Man for dev-null
// Load with: /game load pacman
//
// Module-level constants only; all mutable game data lives in state and
// is threaded into helpers that need it.

// ── Constants ──────────────────────────────────────────────────────────────
var WALL = 0, EMPTY = 1, DOT = 2, POWER = 3, DOOR = 4;
var UP = 0, DOWN = 1, LEFT = 2, RIGHT = 3;
var DX = [0, 0, -1, 1];
var DY = [-1, 1, 0, 0];
var OPPOSITE = [1, 0, 3, 2];

var CWALL = "#0000AA";
var CDOT  = "#AA5500";
var CPOW  = "#FFFF55";
var CDOOR = "#555555";
var CEYES = "#555555";
var GCOL  = ["#AA0000", "#AA00AA", "#00AAAA", "#FF8700"];
var PCOL  = ["#AA5500", "#00AA00", "#00AAAA", "#AA00AA", "#AAAAAA", "#AA0000"];

var GCOL_CANVAS = ["#FF0000", "#FFB8FF", "#00CCCC", "#FFB852"];
var PCOL_CANVAS = ["#FFFF00", "#FF8800", "#00FF88", "#FF88FF", "#88FFFF", "#FF4444"];
var SCARED_COL       = "#2121DE";
var SCARED_FLASH_COL = "#FFFFFF";

var TAU = Math.PI * 2;
var DIR_ANGLE = [3 * Math.PI / 2, Math.PI / 2, Math.PI, 0];
var MOUTH_MAX  = Math.PI / 5;
var MOUTH_FREQ = 8.0;

var E_SETS = [
    ["😇","😀","😤","💀"],
    ["😇","😎","🤬","💀"],
    ["😇","🤠","👹","💀"],
    ["😇","🥳","😈","💀"],
    ["😇","🤩","🥵","💀"],
    ["😇","🤓","🤯","💀"]
];
var E_INVULN = 0, E_NORMAL = 1, E_POWERED = 2, E_DEAD = 3;
var E_GHOST  = "👻";
var E_EYES   = "👀";
var E_CHERRY = "🍒";
var GNAME = ["Blinky", "Pinky", "Inky", "Clyde"];

var BOX = ["═","║","║","║","═","╝","╗","╣","═","╚","╔","╠","═","╩","╦","╬"];

var PTS_DOT = 10, PTS_POW = 50;
var PTS_GHOST = [200, 400, 800, 1600];
var POWER_DUR = 7.0;
var RESPAWN_S = 2.0;
var RESPAWN_L = 5.0;
var INVULN = 3.0;
var SPD_PAC = 0.2;
var SPD_GHOST = 0.2;

var MIDI_NORMAL = [60, 64, 67, 72, 67, 64, 60, 55];
var MIDI_POWER  = [79, 78, 77, 76, 75, 74, 73, 72, 71, 70, 69, 68];
var MIDI_STEP_DT = 0.18;
var MIDI_POW_DT  = 0.09;

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

// ── Helpers ───────────────────────────────────────────────────────────────
function rep(s, n) { var r = ""; for (var i = 0; i < n; i++) r += s; return r; }
function wrapX(x) { return x < 0 ? MW - 1 : x >= MW ? 0 : x; }

function canPass(state, x, y, isGhost) {
    if (y < 0 || y >= MH) return false;
    if (x < 0 || x >= MW) return true;
    var c = state.maze[y * MW + x];
    return c !== WALL && (c !== DOOR || isGhost);
}

function d2(x1, y1, x2, y2) { return (x1-x2)*(x1-x2) + (y1-y2)*(y1-y2); }

function nearest(state, x, y) {
    var best = null, bd = 999999;
    for (var i = 0; i < state.plOrder.length; i++) {
        var p = state.pls[state.plOrder[i]];
        if (!p || p.dead) continue;
        var dd = d2(x, y, p.x, p.y);
        if (dd < bd) { bd = dd; best = p; }
    }
    return best;
}

function anyPowered(state) {
    for (var i = 0; i < state.plOrder.length; i++) {
        var p = state.pls[state.plOrder[i]];
        if (p && !p.dead && p.powerT > 0) return true;
    }
    return false;
}

// ── Maze init ─────────────────────────────────────────────────────────────
function parseMaze(state) {
    state.maze = new Array(MW * MH);
    state.dots = 0;
    for (var y = 0; y < MH; y++) {
        for (var x = 0; x < MW; x++) {
            var ch = MAZE_RAW[y].charAt(x);
            var v = EMPTY;
            if (ch === "#") v = WALL;
            else if (ch === ".") { v = DOT; state.dots++; }
            else if (ch === "o") { v = POWER; state.dots++; }
            else if (ch === "-") v = DOOR;
            state.maze[y * MW + x] = v;
        }
    }
    state.wallMask = new Array(MW * MH);
    for (var y = 0; y < MH; y++) {
        for (var x = 0; x < MW; x++) {
            if (state.maze[y * MW + x] !== WALL) { state.wallMask[y * MW + x] = 0; continue; }
            var m = 0;
            if (y > 0     && (state.maze[(y-1) * MW + x] === WALL || state.maze[(y-1) * MW + x] === DOOR)) m |= 1;
            if (y < MH-1  && (state.maze[(y+1) * MW + x] === WALL || state.maze[(y+1) * MW + x] === DOOR)) m |= 2;
            if (x > 0     && (state.maze[y * MW + (x-1)] === WALL || state.maze[y * MW + (x-1)] === DOOR)) m |= 4;
            if (x < MW-1  && (state.maze[y * MW + (x+1)] === WALL || state.maze[y * MW + (x+1)] === DOOR)) m |= 8;
            state.wallMask[y * MW + x] = m;
        }
    }
}

function resetGhosts(state) {
    state.ghosts = [];
    for (var i = 0; i < 4; i++) {
        state.ghosts.push({
            x: GSPAWN[i].x, y: GSPAWN[i].y,
            px: GSPAWN[i].x, py: GSPAWN[i].y,
            dir: UP, inHouse: i > 0,
            releaseTimer: i * 2.0, eaten: false, returning: false,
            color: GCOL_CANVAS[i]
        });
    }
}

// ── Ghost AI ──────────────────────────────────────────────────────────────
function gTarget(state, g, i) {
    var np = nearest(state, g.x, g.y);
    if (!np) return {x: 14, y: 12};
    if (anyPowered(state) && !g.returning) {
        var c = [{x:1,y:1},{x:26,y:1},{x:1,y:MH-2},{x:26,y:MH-2}];
        return c[i];
    }
    switch (i) {
        case 0: return {x: np.x, y: np.y};
        case 1: return {x: np.x + DX[np.dir]*4, y: np.y + DY[np.dir]*4};
        case 2:
            var b = state.ghosts[0];
            var ax = np.x + DX[np.dir]*2, ay = np.y + DY[np.dir]*2;
            return {x: 2*ax - b.x, y: 2*ay - b.y};
        case 3:
            return d2(g.x, g.y, np.x, np.y) > 64 ? {x:np.x, y:np.y} : {x:1, y:MH-2};
    }
    return {x: np.x, y: np.y};
}

function bfsDir(state, sx, sy, tx, ty) {
    if (sx === tx && sy === ty) return -1;
    var visited = {};
    visited[sx + "," + sy] = true;
    var queue = [];
    for (var d = 0; d < 4; d++) {
        var nx = wrapX(sx + DX[d]), ny = sy + DY[d];
        if (!canPass(state, nx, ny, true)) continue;
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
            if (!canPass(state, nx, ny, true)) continue;
            var key = nx + "," + ny;
            if (visited[key]) continue;
            if (nx === tx && ny === ty) return cur.fd;
            visited[key] = true;
            queue.push({x: nx, y: ny, fd: cur.fd});
        }
    }
    return -1;
}

function ghostAt(state, x, y, ei) {
    for (var j = 0; j < state.ghosts.length; j++) {
        if (j === ei) continue;
        var o = state.ghosts[j];
        if (o.eaten) continue;
        if (o.x === x && o.y === y) return true;
    }
    return false;
}

function moveGhost(state, g, i) {
    if (g.returning) {
        if (g.x === 14 && g.y === 12) {
            g.returning = false; g.eaten = false;
            g.inHouse = true; g.releaseTimer = 3.0;
            return;
        }
        var d = bfsDir(state, g.x, g.y, 14, 12);
        if (d >= 0) {
            var nx = wrapX(g.x + DX[d]), ny = g.y + DY[d];
            if (!ghostAt(state, nx, ny, i)) {
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
    var t = gTarget(state, g, i);
    var opp = OPPOSITE[g.dir];
    var prio = [UP, LEFT, DOWN, RIGHT];
    var ranked = [];
    for (var p = 0; p < 4; p++) {
        var d = prio[p];
        if (d === opp) continue;
        var nx = wrapX(g.x + DX[d]), ny = g.y + DY[d];
        if (!canPass(state, nx, ny, false)) continue;
        ranked.push({d: d, dist: d2(nx, ny, t.x, t.y)});
    }
    ranked.sort(function(a, b) { return a.dist - b.dist; });
    for (var r = 0; r < ranked.length; r++) {
        var d = ranked[r].d;
        var nx = wrapX(g.x + DX[d]), ny = g.y + DY[d];
        if (!ghostAt(state, nx, ny, i)) {
            g.x = nx; g.y = ny; g.dir = d;
            return;
        }
    }
    g.dir = opp;
}

// ── Player ────────────────────────────────────────────────────────────────
function newPlayer(state, id, name) {
    var idx = state.plOrder.length;
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

function movePac(state, ctx, p) {
    var nx = wrapX(p.x + DX[p.nextDir]), ny = p.y + DY[p.nextDir];
    if (canPass(state, nx, ny, false)) {
        p.x = nx; p.y = ny; p.dir = p.nextDir;
    } else {
        nx = wrapX(p.x + DX[p.dir]); ny = p.y + DY[p.dir];
        if (canPass(state, nx, ny, false)) { p.x = nx; p.y = ny; }
    }
    var cell = state.maze[p.y * MW + p.x];
    if (cell === DOT) {
        state.maze[p.y * MW + p.x] = EMPTY;
        p.score += PTS_DOT; state.dots--;
        ctx.midiNote(0, state.midiWakaAlt ? 72 : 69, 110, 60);
        state.midiWakaAlt = 1 - state.midiWakaAlt;
    } else if (cell === POWER) {
        state.maze[p.y * MW + p.x] = EMPTY;
        p.score += PTS_POW; state.dots--;
        p.powerT = POWER_DUR; p.geaten = 0;
        ctx.midiNote(0, 60, 127, 60);
        ctx.midiNote(0, 64, 127, 80);
        ctx.midiNote(0, 67, 127, 100);
        ctx.midiNote(0, 72, 127, 250);
    }
}

// ── MIDI sequencer ────────────────────────────────────────────────────────
function tickMidi(state, ctx, dt) {
    state.midiTimer += dt;
    var isPowered = anyPowered(state);
    var stepDt = isPowered ? MIDI_POW_DT : MIDI_STEP_DT;
    if (state.midiTimer >= stepDt) {
        state.midiTimer -= stepDt;
        if (isPowered) {
            var note = MIDI_POWER[state.midiPowStep % MIDI_POWER.length];
            state.midiPowStep++;
            ctx.midiNote(1, note, 95, Math.floor(stepDt * 950));
        } else {
            state.midiPowStep = 0;
            var note = MIDI_NORMAL[state.midiStep % MIDI_NORMAL.length];
            state.midiStep++;
            ctx.midiNote(1, note, 70, Math.floor(stepDt * 950));
        }
    }
}

// ── Update ────────────────────────────────────────────────────────────────
function tick(state, ctx, dt) {
    state.animTimer += dt;

    for (var i = 0; i < state.plOrder.length; i++) {
        var p = state.pls[state.plOrder[i]];
        if (!p) continue;
        if (p.respawnTimer > 0) p.respawnTimer -= dt;
        if (p.invulnTimer > 0) p.invulnTimer -= dt;
        if (p.powerT > 0) {
            p.powerT -= dt;
            if (p.powerT <= 0) { p.powerT = 0; p.geaten = 0; }
        }
    }
    for (var i = 0; i < state.ghosts.length; i++) {
        if (state.ghosts[i].inHouse && state.ghosts[i].releaseTimer > 0) {
            state.ghosts[i].releaseTimer -= dt;
        }
    }

    for (var i = 0; i < state.plOrder.length; i++) {
        var p = state.pls[state.plOrder[i]];
        if (p) { p.px = p.x; p.py = p.y; }
    }
    for (var i = 0; i < state.ghosts.length; i++) {
        state.ghosts[i].px = state.ghosts[i].x; state.ghosts[i].py = state.ghosts[i].y;
    }

    state.pacMoveTimer += dt;
    state.ghostMoveTimer += dt;

    var pacShouldMove = state.pacMoveTimer >= SPD_PAC;
    for (var i = 0; i < state.plOrder.length; i++) {
        var p = state.pls[state.plOrder[i]];
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
            movePac(state, ctx, p);
        }
    }
    if (pacShouldMove) state.pacMoveTimer -= SPD_PAC;

    for (var i = 0; i < state.ghosts.length; i++) {
        if (state.ghosts[i].returning) moveGhost(state, state.ghosts[i], i);
    }
    if (state.ghostMoveTimer >= SPD_GHOST) {
        for (var i = 0; i < state.ghosts.length; i++) {
            if (!state.ghosts[i].returning) moveGhost(state, state.ghosts[i], i);
        }
        state.ghostMoveTimer -= SPD_GHOST;
    }

    // Collisions
    for (var i = 0; i < state.plOrder.length; i++) {
        var p = state.pls[state.plOrder[i]];
        if (!p || p.dead) continue;
        for (var g = 0; g < state.ghosts.length; g++) {
            var gh = state.ghosts[g];
            if (gh.inHouse || gh.eaten || gh.returning) continue;
            var sameCell = (p.x === gh.x && p.y === gh.y);
            var swapped = (p.x === gh.px && p.y === gh.py &&
                           gh.x === p.px && gh.y === p.py);
            if (!sameCell && !swapped) continue;
            if (p.powerT > 0) {
                gh.eaten = true; gh.returning = true;
                var b = PTS_GHOST[Math.min(p.geaten, 3)];
                p.score += b; p.geaten++;
                ctx.chat(p.name + " ate " + GNAME[g] + "! +" + b);
                ctx.midiNote(0, 84, 127, 300);
            } else if (p.invulnTimer <= 0) {
                p.lives--;
                p.dead = true;
                if (p.lives <= 0) {
                    p.respawnTimer = RESPAWN_L;
                    p.lives = 3; p.score = Math.max(0, p.score - 500);
                    ctx.chat(p.name + " was caught! Respawning with penalty...");
                } else {
                    p.respawnTimer = RESPAWN_S;
                    ctx.chat(p.name + " was caught! " + p.lives + " lives left");
                }
                ctx.midiNote(0, 36, 127, 500);
            }
        }
    }

    if (state.dots <= 0) {
        state.round++;
        ctx.chat("Round " + state.round + "! Maze reset!");
        parseMaze(state); resetGhosts(state);
        for (var i = 0; i < state.plOrder.length; i++) {
            var p = state.pls[state.plOrder[i]];
            if (!p) continue;
            p.x = SPAWN.x; p.y = SPAWN.y;
            p.px = SPAWN.x; p.py = SPAWN.y;
            p.dir = LEFT; p.nextDir = LEFT;
            p.dead = false; p.invulnTimer = INVULN;
            p.powerT = 0; p.geaten = 0;
        }
    }

    tickMidi(state, ctx, dt);
}

// ── Canvas helpers ────────────────────────────────────────────────────────
function interpPos(ex, ey, epx, epy, frac) {
    var dx = ex - epx;
    if (dx >  MW / 2) dx -= MW;
    if (dx < -MW / 2) dx += MW;
    return { wx: epx + dx * frac, wy: epy + (ey - epy) * frac };
}

function drawPacman(canvas, cx, cy, r, color, dir, mouthAngle, dead, blinkOn) {
    if (dead) {
        canvas.setFillStyle("#555555");
        canvas.fillCircle(cx, cy, r * 0.3);
        return;
    }
    var bodyColor = blinkOn ? "#FFFFFF" : color;
    canvas.setFillStyle(bodyColor);
    canvas.beginPath();
    if (mouthAngle > 0.02) {
        var a = DIR_ANGLE[dir];
        canvas.arc(cx, cy, r, a + mouthAngle, a - mouthAngle + TAU);
        canvas.lineTo(cx, cy);
        canvas.closePath();
    } else {
        canvas.arc(cx, cy, r, 0, TAU);
    }
    canvas.fill();
    if (!dead) {
        var eyeA = DIR_ANGLE[dir] - Math.PI / 2;
        canvas.setFillStyle("#000000");
        canvas.fillCircle(cx + Math.cos(eyeA) * r * 0.45, cy + Math.sin(eyeA) * r * 0.45, r * 0.13);
    }
}

function drawGhost(canvas, cx, cy, r, color, scared, flashWhite, dir) {
    var bodyColor = scared ? (flashWhite ? SCARED_FLASH_COL : SCARED_COL) : color;
    canvas.setFillStyle(bodyColor);
    canvas.beginPath();
    canvas.arc(cx, cy, r, Math.PI, TAU);
    canvas.lineTo(cx + r, cy + r * 0.95);
    var bw = (r * 2) / 3;
    for (var b = 2; b >= 0; b--) {
        var tipX = cx - r + b * bw;
        canvas.quadraticCurveTo(tipX + bw * 0.5, cy + r * 0.45, tipX, cy + r * 0.95);
    }
    canvas.lineTo(cx - r, cy);
    canvas.closePath();
    canvas.fill();
    if (scared) {
        canvas.setFillStyle(flashWhite ? "#0000AA" : "#FFFFFF");
        canvas.fillCircle(cx - r * 0.35, cy - r * 0.05, r * 0.13);
        canvas.fillCircle(cx + r * 0.35, cy - r * 0.05, r * 0.13);
        canvas.fillCircle(cx - r * 0.4,  cy + r * 0.22, r * 0.09);
        canvas.fillCircle(cx - r * 0.15, cy + r * 0.32, r * 0.09);
        canvas.fillCircle(cx + r * 0.15, cy + r * 0.22, r * 0.09);
        canvas.fillCircle(cx + r * 0.4,  cy + r * 0.32, r * 0.09);
    } else {
        canvas.setFillStyle("#FFFFFF");
        canvas.fillCircle(cx - r * 0.35, cy - r * 0.08, r * 0.23);
        canvas.fillCircle(cx + r * 0.35, cy - r * 0.08, r * 0.23);
        canvas.setFillStyle("#0000CC");
        var pdx = DX[dir] * r * 0.10;
        var pdy = DY[dir] * r * 0.10;
        canvas.fillCircle(cx - r * 0.35 + pdx, cy - r * 0.08 + pdy, r * 0.13);
        canvas.fillCircle(cx + r * 0.35 + pdx, cy - r * 0.08 + pdy, r * 0.13);
    }
}

function drawEyes(canvas, cx, cy, r) {
    canvas.setFillStyle("#FFFFFF");
    canvas.fillCircle(cx - r * 0.35, cy - r * 0.15, r * 0.28);
    canvas.fillCircle(cx + r * 0.35, cy - r * 0.15, r * 0.28);
    canvas.setFillStyle("#0055FF");
    canvas.fillCircle(cx - r * 0.35, cy - r * 0.15, r * 0.16);
    canvas.fillCircle(cx + r * 0.35, cy - r * 0.15, r * 0.16);
}

var Game = {
    gameName: "Pac-Man",
    teamRange: { min: 1, max: 6 },

    init: function(ctx) {
        ctx.registerCommand({
            name: "score",
            description: "Show the Pac-Man scoreboard",
            handler: function(pid, isAdmin, args) {
                var state = Game.state;
                var sorted = state.plOrder.slice().sort(function(a, b) {
                    return (state.pls[b] ? state.pls[b].score : 0) - (state.pls[a] ? state.pls[a].score : 0);
                });
                var lines = ["--- PAC-MAN SCOREBOARD ---"];
                for (var i = 0; i < sorted.length; i++) {
                    var p = state.pls[sorted[i]];
                    if (!p) continue;
                    lines.push((i+1) + ". " + p.name + ": " + p.score + " pts (" + rep("♥", p.lives) + ")");
                }
                if (sorted.length === 0) lines.push("No players yet!");
                for (var i = 0; i < lines.length; i++) ctx.chatPlayer(pid, lines[i]);
            }
        });
        ctx.registerCommand({
            name: "reset",
            description: "Reset the Pac-Man game",
            adminOnly: true,
            handler: function(pid, isAdmin, args) {
                var state = Game.state;
                state.round = 1;
                state.pacMoveTimer = 0; state.ghostMoveTimer = 0; state.animTimer = 0;
                state.midiTimer = 0; state.midiStep = 0; state.midiPowStep = 0; state.midiWakaAlt = 0;
                parseMaze(state); resetGhosts(state);
                for (var i = 0; i < state.plOrder.length; i++) {
                    var p = state.pls[state.plOrder[i]];
                    if (!p) continue;
                    p.score = 0; p.lives = 3;
                    p.x = SPAWN.x; p.y = SPAWN.y;
                    p.px = SPAWN.x; p.py = SPAWN.y;
                    p.dir = LEFT; p.nextDir = LEFT;
                    p.dead = false; p.invulnTimer = INVULN;
                    p.respawnTimer = 0;
                    p.powerT = 0; p.geaten = 0;
                }
                ctx.chat("Game reset by admin!");
            }
        });

        var state = {
            maze: [], wallMask: [], dots: 0,
            pls: {}, plOrder: [],
            ghosts: [],
            round: 1,
            pacMoveTimer: 0, ghostMoveTimer: 0, animTimer: 0,
            midiTimer: 0, midiStep: 0, midiPowStep: 0, midiWakaAlt: 0
        };
        parseMaze(state);
        resetGhosts(state);
        ctx.midiProgram(0, 80);
        ctx.midiProgram(1, 80);
        ctx.midiCC(0, 7, 110);
        ctx.midiCC(1, 7, 65);
        return state;
    },

    update: function(state, dt, events, ctx) {
        for (var i = 0; i < events.length; i++) {
            var e = events[i];
            if (e.type === "join") {
                state.pls[e.playerID] = newPlayer(state, e.playerID, e.playerName);
                state.plOrder.push(e.playerID);
                ctx.chat(e.playerName + " joined Pac-Man!");
            } else if (e.type === "leave") {
                var idx = state.plOrder.indexOf(e.playerID);
                if (idx >= 0) state.plOrder.splice(idx, 1);
                delete state.pls[e.playerID];
            } else if (e.type === "input") {
                var p = state.pls[e.playerID];
                if (!p || p.dead) continue;
                if (e.key === "up") p.nextDir = UP;
                else if (e.key === "down") p.nextDir = DOWN;
                else if (e.key === "left") p.nextDir = LEFT;
                else if (e.key === "right") p.nextDir = RIGHT;
            }
        }
        tick(state, ctx, dt);
    },

    renderCanvas: function(state, me, canvas) {
        var w = canvas.width, h = canvas.height;
        var pacFrac   = Math.min(state.pacMoveTimer   / SPD_PAC,   1.0);
        var ghostFrac = Math.min(state.ghostMoveTimer / SPD_GHOST, 1.0);
        var t         = state.animTimer;

        var CELL  = Math.max(4, Math.floor(Math.min(w / MW, h / MH)));
        var halfC = CELL * 0.5;
        var viewW = Math.min(MW, Math.floor(w / CELL));
        var viewH = Math.min(MH, Math.floor(h / CELL));

        var meP = state.pls[me.id];
        var camWX, camWY;
        if (meP && !meP.dead) {
            var ip = interpPos(meP.x, meP.y, meP.px, meP.py, pacFrac);
            camWX = ip.wx; camWY = ip.wy;
        } else if (meP && meP.dead) {
            camWX = SPAWN.x; camWY = SPAWN.y;
        } else {
            camWX = MW / 2; camWY = MH / 2;
        }

        var startWX, startWY;
        if (viewW >= MW) startWX = 0;
        else {
            startWX = camWX - viewW * 0.5;
            if (startWX < 0) startWX = 0;
            if (startWX + viewW > MW) startWX = MW - viewW;
        }
        if (viewH >= MH) startWY = 0;
        else {
            startWY = camWY - viewH * 0.5;
            if (startWY < 0) startWY = 0;
            if (startWY + viewH > MH) startWY = MH - viewH;
        }

        var offX = (w - viewW * CELL) * 0.5;
        var offY = (h - viewH * CELL) * 0.5;

        function toSX(wx) { return offX + (wx - startWX) * CELL; }
        function toSY(wy) { return offY + (wy - startWY) * CELL; }

        canvas.setFillStyle("#000000");
        canvas.fillRect(0, 0, w, h);

        var mx0 = Math.floor(startWX) - 1;
        var my0 = Math.floor(startWY) - 1;
        var mx1 = mx0 + viewW + 3;
        var my1 = my0 + viewH + 3;

        for (var my = my0; my <= my1; my++) {
            if (my < 0 || my >= MH) continue;
            for (var mx = mx0; mx <= mx1; mx++) {
                var wmx  = ((mx % MW) + MW) % MW;
                var cell = state.maze[my * MW + wmx];
                var sx   = toSX(mx);
                var sy   = toSY(my);

                if (cell === WALL) {
                    canvas.setFillStyle("#0033BB");
                    canvas.fillRect(sx, sy, CELL + 1, CELL + 1);
                    canvas.setFillStyle("#0022AA");
                    canvas.fillRect(sx + 1, sy + 1, CELL - 1, CELL - 1);
                } else if (cell === DOT) {
                    var r = CELL * 0.11;
                    canvas.setFillStyle("#FFFFFF");
                    canvas.fillCircle(sx + halfC, sy + halfC, r);
                } else if (cell === POWER) {
                    var pulse = 0.82 + Math.sin(t * 5.0) * 0.18;
                    var r2 = CELL * 0.30 * pulse;
                    canvas.setFillStyle("#CC0000");
                    canvas.fillCircle(sx + halfC, sy + halfC, r2);
                    canvas.setFillStyle("#FF6666");
                    canvas.fillCircle(sx + halfC - r2 * 0.28, sy + halfC - r2 * 0.28, r2 * 0.32);
                } else if (cell === DOOR) {
                    canvas.setFillStyle("#FF99FF");
                    canvas.fillRect(sx + 1, sy + CELL * 0.42, CELL - 2, CELL * 0.16);
                }
            }
        }

        var isPowered = anyPowered(state);
        for (var i = 0; i < state.ghosts.length; i++) {
            var g  = state.ghosts[i];
            if (g.eaten && !g.returning) continue;
            var gfrac = g.returning ? 1.0 : ghostFrac;
            var ip = interpPos(g.x, g.y, g.px, g.py, gfrac);
            var sx = toSX(ip.wx + 0.5);
            var sy = toSY(ip.wy + 0.5);
            var r  = CELL * 0.44;
            if (g.returning) {
                drawEyes(canvas, sx, sy, r);
            } else if (isPowered) {
                var flash = (meP && meP.powerT > 0 && meP.powerT < 2.0)
                    ? (Math.floor(t * 6) % 2 === 1) : false;
                drawGhost(canvas, sx, sy, r, g.color, true, flash, g.dir);
            } else {
                drawGhost(canvas, sx, sy, r, g.color, false, false, g.dir);
            }
        }

        for (var pid in state.pls) {
            var p  = state.pls[pid];
            var ip = interpPos(p.x, p.y, p.px, p.py, pacFrac);
            var sx = toSX(ip.wx + 0.5);
            var sy = toSY(ip.wy + 0.5);
            var r  = CELL * 0.44;
            var mouthAngle = p.dead ? 0 : Math.abs(Math.sin(t * MOUTH_FREQ)) * MOUTH_MAX;
            var blinkOn = (p.invulnTimer > 0) && (Math.floor(t * 8) % 2 === 0);
            drawPacman(canvas, sx, sy, r, p.color, p.dir, mouthAngle, p.dead, blinkOn);
        }
    },

    renderAscii: function(state, me, cells) {
        var width = cells.width, height = cells.height;
        var ATTR_BOLD = cells.ATTR_BOLD;
        var pid = me ? me.id : "";
        var cw = (width >= 60) ? 2 : 1;
        var viewCols = Math.floor(width / cw);
        var viewRows = height;

        var meP = state.pls[pid];
        var cx = meP && !meP.dead ? meP.x : Math.floor(MW / 2);
        var cy = meP && !meP.dead ? meP.y : Math.floor(MH / 2);

        var startX = cx - Math.floor(viewCols / 2);
        var startY = cy - Math.floor(viewRows / 2);
        if (viewCols >= MW) startX = -Math.floor((viewCols - MW) / 2);
        else {
            if (startX < 0) startX = 0;
            if (startX + viewCols > MW) startX = MW - viewCols;
        }
        if (viewRows >= MH) startY = -Math.floor((viewRows - MH) / 2);
        else {
            if (startY < 0) startY = 0;
            if (startY + viewRows > MH) startY = MH - viewRows;
        }

        var ents = {};
        for (var g = 0; g < state.ghosts.length; g++) {
            var gh = state.ghosts[g];
            if (gh.eaten && !gh.returning) continue;
            var k = gh.x + "," + gh.y;
            if (gh.returning) {
                ents[k] = (cw === 2)
                    ? {ch: E_EYES, fg: CEYES, bg: null, emoji: true}
                    : {ch: ".", fg: CEYES, bg: null};
            } else {
                ents[k] = (cw === 2)
                    ? {ch: E_GHOST, fg: GCOL[g], bg: null, emoji: true}
                    : {ch: "M", fg: GCOL[g], bg: null};
            }
        }
        for (var i = 0; i < state.plOrder.length; i++) {
            var p = state.pls[state.plOrder[i]];
            if (!p) continue;
            var k = p.x + "," + p.y;
            var set = E_SETS[p.ci % E_SETS.length];
            if (cw === 2) {
                var emoji;
                if (p.dead) emoji = set[E_DEAD];
                else if (p.invulnTimer > 0) emoji = set[E_INVULN];
                else if (p.powerT > 0) emoji = set[E_POWERED];
                else emoji = set[E_NORMAL];
                ents[k] = {ch: emoji, fg: null, bg: null, emoji: true};
            } else {
                if (p.dead) ents[k] = {ch: "X", fg: "#555555", bg: null};
                else ents[k] = {ch: "@", fg: PCOL[p.ci % PCOL.length], bg: null, bold: (state.plOrder[i] === pid)};
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
                        cells.writeString(screenCol, row, e.ch, e.fg, e.bg || null);
                    } else if (e.bold) {
                        cells.setChar(screenCol, row, e.ch, e.fg, e.bg || null, ATTR_BOLD);
                        if (cw === 2) cells.setChar(screenCol + 1, row, " ", e.fg, e.bg || null);
                    } else {
                        cells.setChar(screenCol, row, e.ch, e.fg, e.bg || null);
                        if (cw === 2) cells.setChar(screenCol + 1, row, " ", e.fg, e.bg || null);
                    }
                    continue;
                }
                var c = state.maze[my * MW + mx];
                if (cw === 2) {
                    if (c === WALL) {
                        cells.setChar(screenCol, row, "█", CWALL, null);
                        cells.setChar(screenCol + 1, row, "█", CWALL, null);
                    } else if (c === DOT) {
                        cells.setChar(screenCol, row, "⠐", CDOT, null);
                        cells.setChar(screenCol + 1, row, "⠂", CDOT, null);
                    } else if (c === POWER) {
                        cells.writeString(screenCol, row, E_CHERRY, null, null);
                    } else if (c === DOOR) {
                        cells.setChar(screenCol, row, "─", CDOOR, null);
                        cells.setChar(screenCol + 1, row, "─", CDOOR, null);
                    }
                } else {
                    if (c === WALL) cells.setChar(screenCol, row, BOX[state.wallMask[my * MW + mx]], CWALL, null);
                    else if (c === DOT) cells.setChar(screenCol, row, "•", CDOT, null);
                    else if (c === POWER) cells.setChar(screenCol, row, "●", CPOW, null);
                    else if (c === DOOR) cells.setChar(screenCol, row, "─", CDOOR, null);
                }
            }
        }
    },

    statusBar: function(state, me) {
        var p = state.pls[me.id];
        if (!p) return "PAC-MAN";
        var h = rep("♥", p.lives);
        var pw = p.powerT > 0 ? "  POWER!" : "";
        return "PAC-MAN | Score: " + p.score + " | " + h + " | Rnd " + state.round + pw;
    },

    commandBar: function(state, me) {
        var p = state.pls[me.id];
        if (p && p.dead) return "Respawning...  /score for scoreboard";
        return "[↑↓←→] Move  [Enter] Chat  /score Scoreboard";
    }
};
