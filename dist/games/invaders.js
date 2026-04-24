// invaders.js — Multiplayer Space Invaders for dev-null
// Load with: /game load invaders

// ── Constants ──────────────────────────────────────────────────────────────
var CRED  = "#AA0000";
var CGRN  = "#00AA00";
var CYEL  = "#AA5500";
var CBLU  = "#0000AA";
var CMAG  = "#AA00AA";
var CCYN  = "#00AAAA";
var CWHT  = "#AAAAAA";
var CDIM  = "#666666";

var MAP_W = 60;
var MAP_H = 35;

var SHIP_COLORS = [CGRN, CCYN, CMAG, CYEL, CWHT, CRED];
var SHIP_BG = ["#005F00","#005F87","#5F005F","#5F5F00","#303030","#5F0000"];
var E_SHIP_NARROW = "^";

var E_ALIEN = [
    "👾",  // 👾 — basic (10 pts)
    "👽",  // 👽 — mid (20 pts)
    "🤖",  // 🤖 — tough (30 pts)
    "👹"   // 👹 — boss row (50 pts)
];
var ALIEN_PTS = [10, 20, 30, 50];
var ALIEN_COLORS_1CH = [CGRN, CCYN, CMAG, CRED];

var GROUND_FG_A = "#444444";
var GROUND_FG_B = "#666666";

var E_BOOM   = "💥";
var E_SHIELD = "🛡️";
var E_ZAP    = "⚡";
var E_HEART  = "❤️";
var E_UFO    = "🛸";
var E_FIRE   = "🔥";

var ALIEN_MOVE_INTERVAL = 0.8;
var ALIEN_SHOOT_CHANCE = 0.015;
var BULLET_SPEED = 1;
var ALIEN_BULLET_SPEED = 1;
var PLAYER_SPEED = 1;
var RESPAWN_TIME = 3.0;
var INVULN_TIME = 2.0;
var UFO_INTERVAL = 30.0;
var UFO_MOVE_INTERVAL = 0.3;
var POWERUP_CHANCE = 0.15;
var POWERUP_MOVE_INTERVAL = 0.3;
var RAPID_FIRE_DUR = 8.0;
var SHIELD_DUR = 6.0;
var FIRE_COOLDOWN = 0.4;
var RAPID_COOLDOWN = 0.2;
var WAVE_PAUSE = 3.0;
var BOOM_TTL = 0.5;

var PLAYER_Y = MAP_H - 2;
var GROUND_Y = MAP_H - 1;

var BUNKER_SHAPE = [
    [0,0],[1,0],[2,0],[3,0],[4,0],
    [0,1],[1,1],[2,1],[3,1],[4,1],
    [0,2],[1,2],      [3,2],[4,2]
];
var BUNKER_COUNT = 4;

// ── Helpers ───────────────────────────────────────────────────────────────
function rep(s, n) { var r = ""; for (var i = 0; i < n; i++) r += s; return r; }
function clamp(v, lo, hi) { return v < lo ? lo : v > hi ? hi : v; }
function rng() { return Math.random(); }
function rngInt(lo, hi) { return lo + Math.floor(rng() * (hi - lo + 1)); }

// ── Bunkers ───────────────────────────────────────────────────────────────
function spawnBunkers(state) {
    state.bunkers = {};
    var bunkerY = PLAYER_Y - 4;
    var playW = MAP_W - 2;
    var spacing = Math.floor(playW / (BUNKER_COUNT + 1));
    for (var b = 0; b < BUNKER_COUNT; b++) {
        var bx = 1 + spacing * (b + 1) - 2;
        for (var s = 0; s < BUNKER_SHAPE.length; s++) {
            var cx = bx + BUNKER_SHAPE[s][0];
            var cy = bunkerY + BUNKER_SHAPE[s][1];
            if (cx >= 1 && cx < MAP_W - 1 && cy >= 1 && cy < GROUND_Y) {
                state.bunkers[cx + "," + cy] = 3;
            }
        }
    }
}

function hitBunker(state, x, y) {
    var k = x + "," + y;
    if (state.bunkers[k]) {
        state.bunkers[k]--;
        if (state.bunkers[k] <= 0) delete state.bunkers[k];
        return true;
    }
    return false;
}

// ── Alien formation ───────────────────────────────────────────────────────
function spawnWave(state, ctx) {
    state.aliens = [];
    var rows = Math.min(3 + Math.floor(state.wave / 2), 5);
    var cols = Math.min(8 + state.wave, 12);
    var playW = MAP_W - 2;
    var startX = 1 + Math.floor((playW - cols * 3) / 2);
    var startY = 2;
    if (ctx) ctx.log("Spawning wave " + state.wave + ": " + rows + "x" + cols + " aliens");

    for (var r = 0; r < rows; r++) {
        var tier = Math.min(rows - 1 - r, E_ALIEN.length - 1);
        for (var c = 0; c < cols; c++) {
            state.aliens.push({
                x: startX + c * 3,
                y: startY + r * 2,
                tier: tier,
                alive: true
            });
        }
    }
    state.alienDX = 1;
    state.alienMoveTimer = 0;
    state.waveAlienCount = state.aliens.length;
    state.alienBullets = [];
    spawnBunkers(state);
}

function aliveAliens(state) {
    var count = 0;
    for (var i = 0; i < state.aliens.length; i++) {
        if (state.aliens[i].alive) count++;
    }
    return count;
}

function alienBounds(state) {
    var minX = 9999, maxX = -1, maxY = -1;
    for (var i = 0; i < state.aliens.length; i++) {
        var a = state.aliens[i];
        if (!a.alive) continue;
        if (a.x < minX) minX = a.x;
        if (a.x > maxX) maxX = a.x;
        if (a.y > maxY) maxY = a.y;
    }
    return {minX: minX, maxX: maxX, maxY: maxY};
}

// ── Player ────────────────────────────────────────────────────────────────
function newPlayer(state, id, name) {
    return {
        id: id, name: name,
        x: Math.floor(MAP_W / 2), y: PLAYER_Y,
        score: 0, lives: 3,
        dead: false, respawnTimer: 0, invulnTimer: 0,
        cooldown: 0,
        rapidFire: 0,
        shield: 0,
        ci: state.plOrder.length % SHIP_BG.length
    };
}

// ── Event handlers ─────────────────────────────────────────────────────────
function onInputEvent(state, playerID, key) {
    var p = state.pls[playerID];
    if (!p || p.dead || state.isGameOver) return;
    if (key === "left") {
        p.x = Math.max(1, p.x - PLAYER_SPEED);
    } else if (key === "right") {
        p.x = Math.min(MAP_W - 2, p.x + PLAYER_SPEED);
    } else if (key === " " || key === "space" || key === "up") {
        var cd = p.rapidFire > 0 ? RAPID_COOLDOWN : FIRE_COOLDOWN;
        if (p.cooldown <= 0) {
            state.playerBullets.push({x: p.x, y: p.y - 1, owner: playerID});
            p.cooldown = cd;
        }
    }
}

function onJoinEvent(state, ctx, playerID, playerName) {
    state.pls[playerID] = newPlayer(state, playerID, playerName);
    state.plOrder.push(playerID);
    ctx.chat(playerName + " joined Space Invaders!");
}

function onLeaveEvent(state, playerID) {
    var idx = state.plOrder.indexOf(playerID);
    if (idx >= 0) state.plOrder.splice(idx, 1);
    for (var i = state.playerBullets.length - 1; i >= 0; i--) {
        if (state.playerBullets[i].owner === playerID) state.playerBullets.splice(i, 1);
    }
    delete state.pls[playerID];
}

// ── Tick (simulation step) ─────────────────────────────────────────────────
function killPlayer(state, ctx, p) {
    p.lives--;
    p.dead = true;
    state.booms.push({x: p.x, y: p.y, ttl: BOOM_TTL});
    if (p.lives <= 0) {
        p.respawnTimer = RESPAWN_TIME * 2;
        p.lives = 3;
        p.score = Math.max(0, p.score - 200);
        ctx.chat(p.name + " destroyed! Respawning with penalty...");
    } else {
        p.respawnTimer = RESPAWN_TIME;
        ctx.chat(p.name + " hit! " + p.lives + " lives left");
    }
}

function endGame(state, ctx) {
    state.isGameOver = true;
    state.gameOverAt = state.elapsed;
    var sorted = state.plOrder.slice().sort(function(a, b) {
        return (state.pls[b] ? state.pls[b].score : 0) - (state.pls[a] ? state.pls[a].score : 0);
    });
    ctx.chat("=== GAME OVER ===");
    var results = [];
    for (var i = 0; i < sorted.length; i++) {
        var p = state.pls[sorted[i]];
        if (!p) continue;
        var medal = i === 0 ? "🥇" : i === 1 ? "🥈" : i === 2 ? "🥉" : "  ";
        ctx.chat(medal + " " + (i + 1) + ". " + p.name + ": " + p.score + " pts");
        results.push({ name: p.name, result: p.score + " pts" });
    }
    if (sorted.length > 0 && state.pls[sorted[0]]) {
        ctx.chat(state.pls[sorted[0]].name + " wins!");
    }
    ctx.gameOver(results);
}

function tick(state, ctx, dt) {
    if (state.isGameOver) return;
    state.elapsed += dt;

    if (!state.inited) {
        state.inited = true;
        spawnWave(state, ctx);
    }

    // Wave pause
    if (state.wavePause > 0) {
        state.wavePause -= dt;
        if (state.wavePause <= 0) {
            spawnWave(state, ctx);
            for (var i = 0; i < state.plOrder.length; i++) {
                var p = state.pls[state.plOrder[i]];
                if (p && !p.dead) p.y = PLAYER_Y;
            }
        }
        return;
    }

    // Player timers
    for (var i = 0; i < state.plOrder.length; i++) {
        var p = state.pls[state.plOrder[i]];
        if (!p) continue;
        if (p.cooldown > 0) p.cooldown -= dt;
        if (p.rapidFire > 0) p.rapidFire -= dt;
        if (p.shield > 0) p.shield -= dt;
        if (p.dead) {
            p.respawnTimer -= dt;
            if (p.respawnTimer <= 0) {
                p.dead = false;
                p.x = Math.floor(MAP_W / 2);
                p.y = PLAYER_Y;
                p.invulnTimer = INVULN_TIME;
            }
        }
        if (p.invulnTimer > 0) p.invulnTimer -= dt;
    }

    // Player bullets up
    for (var i = state.playerBullets.length - 1; i >= 0; i--) {
        state.playerBullets[i].y -= BULLET_SPEED;
        if (state.playerBullets[i].y <= 0) { state.playerBullets.splice(i, 1); continue; }
        if (hitBunker(state, state.playerBullets[i].x, state.playerBullets[i].y)) {
            state.playerBullets.splice(i, 1);
        }
    }

    // Alien bullets down
    for (var i = state.alienBullets.length - 1; i >= 0; i--) {
        state.alienBullets[i].y += ALIEN_BULLET_SPEED;
        if (state.alienBullets[i].y >= GROUND_Y) { state.alienBullets.splice(i, 1); continue; }
        if (hitBunker(state, state.alienBullets[i].x, state.alienBullets[i].y)) {
            state.alienBullets.splice(i, 1);
        }
    }

    // Move alien formation
    state.alienMoveTimer += dt;
    var alive = aliveAliens(state);
    var speedUp = Math.max(0.2, ALIEN_MOVE_INTERVAL - Math.floor((state.waveAlienCount - alive) / 4) * 0.1);
    if (state.alienMoveTimer >= speedUp) {
        state.alienMoveTimer = 0;
        var b = alienBounds(state);
        var hitEdge = false;
        if (state.alienDX > 0 && b.maxX >= MAP_W - 2) hitEdge = true;
        if (state.alienDX < 0 && b.minX <= 1) hitEdge = true;

        if (hitEdge) {
            state.alienDX = -state.alienDX;
            for (var i = 0; i < state.aliens.length; i++) {
                if (state.aliens[i].alive) state.aliens[i].y++;
            }
            for (var i = 0; i < state.aliens.length; i++) {
                var a = state.aliens[i];
                if (!a.alive) continue;
                var k = a.x + "," + a.y;
                if (state.bunkers[k]) delete state.bunkers[k];
            }
        } else {
            for (var i = 0; i < state.aliens.length; i++) {
                if (state.aliens[i].alive) state.aliens[i].x += state.alienDX;
            }
        }
    }

    // Alien shooting
    for (var i = 0; i < state.aliens.length; i++) {
        var a = state.aliens[i];
        if (!a.alive) continue;
        if (rng() < ALIEN_SHOOT_CHANCE) {
            var blocked = false;
            for (var j = 0; j < state.aliens.length; j++) {
                if (j === i || !state.aliens[j].alive) continue;
                if (state.aliens[j].x === a.x && state.aliens[j].y > a.y) {
                    blocked = true;
                    break;
                }
            }
            if (!blocked) state.alienBullets.push({x: a.x, y: a.y + 1});
        }
    }

    // UFO
    state.ufoTimer += dt;
    if (state.ufoTimer >= UFO_INTERVAL && !state.ufo) {
        state.ufoTimer = 0;
        var dir = rng() > 0.5 ? 1 : -1;
        state.ufo = {
            x: dir > 0 ? 1 : MAP_W - 2,
            y: 1, dir: dir,
            pts: [50, 100, 150, 300][rngInt(0, 3)]
        };
    }
    if (state.ufo) {
        state.ufoMoveTimer += dt;
        while (state.ufoMoveTimer >= UFO_MOVE_INTERVAL) {
            state.ufoMoveTimer -= UFO_MOVE_INTERVAL;
            state.ufo.x += state.ufo.dir;
            if (state.ufo.x < 1 || state.ufo.x >= MAP_W - 1) { state.ufo = null; break; }
        }
    }

    // Powerups
    state.powerupMoveTimer += dt;
    var powerupSteps = 0;
    while (state.powerupMoveTimer >= POWERUP_MOVE_INTERVAL) {
        state.powerupMoveTimer -= POWERUP_MOVE_INTERVAL;
        powerupSteps++;
    }
    for (var i = state.powerups.length - 1; i >= 0; i--) {
        state.powerups[i].y += powerupSteps;
        if (state.powerups[i].y >= GROUND_Y) state.powerups.splice(i, 1);
    }

    // Explosions decay
    for (var i = state.booms.length - 1; i >= 0; i--) {
        state.booms[i].ttl -= dt;
        if (state.booms[i].ttl <= 0) state.booms.splice(i, 1);
    }

    // Player bullets vs aliens / UFO
    for (var bi = state.playerBullets.length - 1; bi >= 0; bi--) {
        var bul = state.playerBullets[bi];
        var hit = false;
        for (var ai = 0; ai < state.aliens.length; ai++) {
            var a = state.aliens[ai];
            if (!a.alive) continue;
            if (Math.abs(bul.x - a.x) <= 1 && bul.y === a.y) {
                a.alive = false;
                hit = true;
                var shooter = state.pls[bul.owner];
                if (shooter) shooter.score += ALIEN_PTS[a.tier];
                state.booms.push({x: a.x, y: a.y, ttl: BOOM_TTL});
                if (rng() < POWERUP_CHANCE) {
                    var types = ["rapid", "shield", "life"];
                    state.powerups.push({x: a.x, y: a.y, type: types[rngInt(0, 2)]});
                }
                break;
            }
        }
        if (!hit && state.ufo) {
            if (Math.abs(bul.x - state.ufo.x) <= 1 && bul.y === state.ufo.y) {
                hit = true;
                var shooter2 = state.pls[bul.owner];
                if (shooter2) {
                    shooter2.score += state.ufo.pts;
                    ctx.chat(shooter2.name + " shot the UFO! +" + state.ufo.pts);
                }
                state.booms.push({x: state.ufo.x, y: state.ufo.y, ttl: BOOM_TTL});
                state.ufo = null;
            }
        }
        if (hit) state.playerBullets.splice(bi, 1);
    }

    // Alien bullets vs players
    for (var bi = state.alienBullets.length - 1; bi >= 0; bi--) {
        var bul = state.alienBullets[bi];
        for (var pi = 0; pi < state.plOrder.length; pi++) {
            var p = state.pls[state.plOrder[pi]];
            if (!p || p.dead) continue;
            if (Math.abs(bul.x - p.x) <= 1 && bul.y === p.y) {
                state.alienBullets.splice(bi, 1);
                if (p.invulnTimer > 0) break;
                if (p.shield > 0) {
                    p.shield = 0;
                    state.booms.push({x: p.x, y: p.y - 1, ttl: BOOM_TTL});
                    break;
                }
                killPlayer(state, ctx, p);
                break;
            }
        }
    }

    // Aliens reaching ground
    var ab = alienBounds(state);
    if (ab.maxY >= PLAYER_Y - 1) {
        var invaded = false;
        for (var ai = 0; ai < state.aliens.length; ai++) {
            var a = state.aliens[ai];
            if (!a.alive) continue;
            if (a.y >= GROUND_Y) { invaded = true; break; }
            if (a.y < PLAYER_Y - 1) continue;
            for (var pi = 0; pi < state.plOrder.length; pi++) {
                var p = state.pls[state.plOrder[pi]];
                if (!p || p.dead || p.invulnTimer > 0) continue;
                if (Math.abs(a.x - p.x) <= 1 && Math.abs(a.y - p.y) <= 1) {
                    if (p.shield > 0) p.shield = 0;
                    else killPlayer(state, ctx, p);
                }
            }
        }
        if (invaded) {
            ctx.chat("The aliens reached the ground! All players lose a life!");
            for (var ai = 0; ai < state.aliens.length; ai++) state.aliens[ai].alive = false;
            for (var pi = 0; pi < state.plOrder.length; pi++) {
                var p = state.pls[state.plOrder[pi]];
                if (!p || p.dead) continue;
                killPlayer(state, ctx, p);
            }
        }
    }

    // Powerup pickups
    for (var i = state.powerups.length - 1; i >= 0; i--) {
        var pw = state.powerups[i];
        for (var pi = 0; pi < state.plOrder.length; pi++) {
            var p = state.pls[state.plOrder[pi]];
            if (!p || p.dead) continue;
            if (Math.abs(pw.x - p.x) <= 1 && pw.y === p.y) {
                if (pw.type === "rapid") {
                    p.rapidFire = RAPID_FIRE_DUR;
                    ctx.chatPlayer(p.id, "⚡ Rapid fire!");
                } else if (pw.type === "shield") {
                    p.shield = SHIELD_DUR;
                    ctx.chatPlayer(p.id, "🛡️ Shield active!");
                } else if (pw.type === "life") {
                    p.lives = Math.min(p.lives + 1, 5);
                    ctx.chatPlayer(p.id, "❤️ Extra life!");
                }
                state.powerups.splice(i, 1);
                break;
            }
        }
    }

    // Wave cleared
    if (aliveAliens(state) === 0 && state.wavePause <= 0) {
        if (state.wave >= state.gameWaves) {
            endGame(state, ctx);
        } else {
            state.wave++;
            state.wavePause = WAVE_PAUSE;
            ctx.chat("Wave " + state.wave + " incoming!");
        }
    }
}

// ── Render ────────────────────────────────────────────────────────────────
function render(state, me, cells) {
    var width = cells.width;
    var height = cells.height;
    var ATTR_BOLD = cells.ATTR_BOLD;
    var pid = me ? me.id : "";
    var cw = (width >= 60) ? 2 : 1;
    var viewCols = Math.floor(width / cw);
    var viewRows = height;

    var mePl = state.pls[pid];
    var cx = mePl && !mePl.dead ? mePl.x : Math.floor(MAP_W / 2);
    var cy = mePl && !mePl.dead ? mePl.y : Math.floor(MAP_H / 2);

    var camX, camY;
    if (viewCols >= MAP_W) camX = -Math.floor((viewCols - MAP_W) / 2);
    else                   camX = clamp(cx - Math.floor(viewCols / 2), 0, MAP_W - viewCols);
    if (viewRows >= MAP_H) camY = -Math.floor((viewRows - MAP_H) / 2);
    else                   camY = clamp(cy - Math.floor(viewRows / 2), 0, MAP_H - viewRows);

    if (state.isGameOver) {
        var goText = "=== GAME OVER ===";
        var oy = Math.floor(viewRows / 2) - 2;
        var ox = Math.floor((width - goText.length) / 2);
        if (oy >= 0 && oy < viewRows) cells.writeString(ox, oy, goText, CRED, null, ATTR_BOLD);
        var sorted = state.plOrder.slice().sort(function(a, b) {
            return (state.pls[b] ? state.pls[b].score : 0) - (state.pls[a] ? state.pls[a].score : 0);
        });
        for (var i = 0; i < sorted.length && oy + 2 + i < viewRows; i++) {
            var p = state.pls[sorted[i]];
            if (!p) continue;
            var medal = i === 0 ? "#1" : i === 1 ? "#2" : i === 2 ? "#3" : "#" + (i + 1);
            var entry = medal + " " + p.name + ": " + p.score + " pts";
            var ex = Math.floor((width - entry.length) / 2);
            var col = i === 0 ? CYEL : i === 1 ? CWHT : CDIM;
            var attr = i === 0 ? ATTR_BOLD : 0;
            cells.writeString(ex, oy + 2 + i, entry, col, null, attr);
        }
        return;
    }

    if (state.wavePause > 0) {
        var waveText = "WAVE " + state.wave;
        var oy = Math.floor(viewRows / 2);
        var ox = Math.floor((width - waveText.length) / 2);
        if (oy >= 0 && oy < viewRows) cells.writeString(ox, oy, waveText, CYEL, null, ATTR_BOLD);
        return;
    }

    var ents = {};

    for (var k in state.bunkers) {
        var hp = state.bunkers[k];
        if (hp >= 3) ents[k] = {ch: "█", ch2: "█", fg: CGRN, bg: null, attr: 0};
        else if (hp === 2) ents[k] = {ch: "▓", ch2: "▓", fg: CYEL, bg: null, attr: 0};
        else ents[k] = {ch: "░", ch2: "░", fg: CRED, bg: null, attr: 0};
    }
    for (var i = 0; i < state.booms.length; i++) {
        var b = state.booms[i];
        ents[b.x + "," + b.y] = (cw === 2)
            ? {emoji: b.ttl > BOOM_TTL * 0.4 ? E_BOOM : E_FIRE}
            : {ch: "*", fg: CRED, bg: null, attr: ATTR_BOLD};
    }
    for (var i = 0; i < state.powerups.length; i++) {
        var pw = state.powerups[i];
        var k2 = pw.x + "," + pw.y;
        if (cw === 2) {
            if (pw.type === "rapid") ents[k2] = {emoji: E_ZAP};
            else if (pw.type === "shield") ents[k2] = {emoji: E_SHIELD};
            else ents[k2] = {emoji: E_HEART};
        } else {
            if (pw.type === "rapid") ents[k2] = {ch: "z", fg: CYEL, bg: null, attr: 0};
            else if (pw.type === "shield") ents[k2] = {ch: "s", fg: CBLU, bg: null, attr: 0};
            else ents[k2] = {ch: "+", fg: CRED, bg: null, attr: 0};
        }
    }
    for (var i = 0; i < state.aliens.length; i++) {
        var a = state.aliens[i];
        if (!a.alive) continue;
        ents[a.x + "," + a.y] = (cw === 2)
            ? {emoji: E_ALIEN[a.tier]}
            : {ch: "W", fg: ALIEN_COLORS_1CH[a.tier], bg: null, attr: 0};
    }
    if (state.ufo) {
        ents[state.ufo.x + "," + state.ufo.y] = (cw === 2)
            ? {emoji: E_UFO}
            : {ch: "U", fg: CRED, bg: null, attr: ATTR_BOLD};
    }
    for (var i = 0; i < state.alienBullets.length; i++) {
        var bu = state.alienBullets[i];
        ents[bu.x + "," + bu.y] = {ch: "▓", ch2: "▓", fg: CRED, bg: null, attr: 0};
    }
    for (var i = 0; i < state.playerBullets.length; i++) {
        var bu = state.playerBullets[i];
        var shooter = state.pls[bu.owner];
        var col = shooter ? SHIP_COLORS[shooter.ci] : CWHT;
        ents[bu.x + "," + bu.y] = (cw === 2)
            ? {ch: "│", ch2: "│", fg: col, bg: null, attr: 0}
            : {ch: "|", fg: col, bg: null, attr: 0};
    }
    for (var i = 0; i < state.plOrder.length; i++) {
        var p = state.pls[state.plOrder[i]];
        if (!p || p.dead) continue;
        var k3 = p.x + "," + p.y;
        var bg = SHIP_BG[p.ci];
        var col = SHIP_COLORS[p.ci];
        var blinkOn = Math.floor((state.elapsed || 0) / 0.2) % 2 === 0;
        if (cw === 2) {
            if (p.invulnTimer > 0 && blinkOn) ents[k3] = {ch: " ", ch2: " ", fg: null, bg: null, attr: 0};
            else if (p.shield > 0)            ents[k3] = {ch: "{", ch2: "}", fg: CWHT, bg: bg, attr: ATTR_BOLD};
            else                              ents[k3] = {ch: "/", ch2: "\\", fg: CWHT, bg: bg, attr: ATTR_BOLD};
        } else {
            if (p.invulnTimer > 0 && blinkOn) ents[k3] = {ch: " ", fg: null, bg: null, attr: 0};
            else if (p.shield > 0)            ents[k3] = {ch: "O", fg: col, bg: null, attr: ATTR_BOLD};
            else {
                var attrP = state.plOrder[i] === pid ? ATTR_BOLD : 0;
                ents[k3] = {ch: E_SHIP_NARROW, fg: col, bg: null, attr: attrP};
            }
        }
    }

    for (var row = 0; row < viewRows; row++) {
        for (var col = 0; col < viewCols; col++) {
            var wx = camX + col;
            var wy = camY + row;
            var sx = col * cw;
            if (wx < 0 || wx >= MAP_W || wy < 0 || wy >= MAP_H) continue;
            if (wy === 0) {
                cells.setChar(sx, row, "█", CBLU, null, 0);
                if (cw === 2) cells.setChar(sx + 1, row, "█", CBLU, null, 0);
                continue;
            }
            if (wy === GROUND_Y) {
                var gfg = (cw === 2)
                    ? (Math.floor(wx / 2) % 2 === 0 ? GROUND_FG_A : GROUND_FG_B)
                    : (wx % 2 === 0 ? GROUND_FG_A : GROUND_FG_B);
                cells.setChar(sx, row, "▄", gfg, null, 0);
                if (cw === 2) cells.setChar(sx + 1, row, "▄", gfg, null, 0);
                continue;
            }
            if (wx === 0 || wx === MAP_W - 1) {
                cells.setChar(sx, row, "█", CBLU, null, 0);
                if (cw === 2) cells.setChar(sx + 1, row, "█", CBLU, null, 0);
                continue;
            }
            var k = wx + "," + wy;
            var e = ents[k];
            if (e) {
                if (e.emoji) {
                    cells.writeString(sx, row, e.emoji, null, null, 0);
                } else {
                    cells.setChar(sx, row, e.ch, e.fg, e.bg, e.attr);
                    if (cw === 2) {
                        var c2 = e.ch2 ? e.ch2 : e.ch;
                        cells.setChar(sx + 1, row, c2, e.fg, e.bg, e.attr);
                    }
                }
            }
        }
    }
}

var Game = {
    gameName: "Space Invaders",
    teamRange: { min: 1, max: 6 },

    init: function(ctx) {
        ctx.registerCommand({
            name: "score",
            description: "Show the Space Invaders scoreboard",
            handler: function(pid, isAdmin, args) {
                // Command runs with the runtime mu held; it's safe to read the
                // live state directly through Game.
                var state = Game.state;
                var sorted = state.plOrder.slice().sort(function(a, b) {
                    return (state.pls[b] ? state.pls[b].score : 0) - (state.pls[a] ? state.pls[a].score : 0);
                });
                var lines = ["--- SPACE INVADERS SCOREBOARD ---"];
                for (var i = 0; i < sorted.length; i++) {
                    var p = state.pls[sorted[i]];
                    if (!p) continue;
                    lines.push((i + 1) + ". " + p.name + ": " + p.score + " pts (" + rep("♥", p.lives) + ")");
                }
                if (sorted.length === 0) lines.push("No players yet!");
                for (var i = 0; i < lines.length; i++) ctx.chatPlayer(pid, lines[i]);
            }
        });
        ctx.registerCommand({
            name: "reset",
            description: "Reset the Space Invaders game",
            adminOnly: true,
            handler: function(pid, isAdmin, args) {
                var state = Game.state;
                state.wave = 1; state.elapsed = 0; state.isGameOver = false;
                state.aliens = []; state.alienBullets = []; state.playerBullets = [];
                state.booms = []; state.powerups = []; state.ufo = null; state.ufoTimer = 0;
                state.ufoMoveTimer = 0; state.powerupMoveTimer = 0;
                state.wavePause = 0; state.bunkers = {}; state.inited = false;
                for (var i = 0; i < state.plOrder.length; i++) {
                    var p = state.pls[state.plOrder[i]];
                    if (!p) continue;
                    p.score = 0; p.lives = 3;
                    p.dead = false; p.invulnTimer = INVULN_TIME;
                    p.rapidFire = 0; p.shield = 0; p.cooldown = 0;
                    p.x = Math.floor(MAP_W / 2); p.y = PLAYER_Y;
                }
                ctx.chat("Game reset by admin!");
            }
        });
        ctx.registerCommand({
            name: "waves",
            description: "Set number of waves (admin only)",
            adminOnly: true,
            handler: function(pid, isAdmin, args) {
                if (args.length < 1 || isNaN(parseInt(args[0]))) {
                    ctx.chatPlayer(pid, "Usage: /waves <number> (currently " + Game.state.gameWaves + ")");
                    return;
                }
                Game.state.gameWaves = Math.max(1, parseInt(args[0]));
                ctx.chat("Game set to " + Game.state.gameWaves + " waves!");
            }
        });

        return {
            pls: {}, plOrder: [],
            aliens: [], alienDX: 1, alienMoveTimer: 0,
            alienBullets: [], playerBullets: [],
            booms: [], powerups: [],
            ufo: null, ufoTimer: 0, ufoMoveTimer: 0,
            powerupMoveTimer: 0,
            elapsed: 0, wave: 1, waveAlienCount: 0, wavePause: 0,
            isGameOver: false, gameOverAt: 0,
            bunkers: {}, inited: false,
            gameWaves: 5
        };
    },

    begin: function(state, ctx) {
        // Nothing to do — init laid out everything. Join events bring
        // players in during update.
    },

    update: function(state, dt, events, ctx) {
        for (var i = 0; i < events.length; i++) {
            var e = events[i];
            if (e.type === "input") onInputEvent(state, e.playerID, e.key);
            else if (e.type === "join") onJoinEvent(state, ctx, e.playerID, e.playerName);
            else if (e.type === "leave") onLeaveEvent(state, e.playerID);
        }
        tick(state, ctx, dt);
    },

    renderAscii: function(state, me, cells) {
        render(state, me, cells);
    },

    statusBar: function(state, me) {
        var p = state.pls[me.id];
        if (!p) return "SPACE INVADERS";
        var h = rep("♥", p.lives);
        var extras = "";
        if (p.rapidFire > 0) extras += " ⚡";
        if (p.shield > 0) extras += " 🛡️";
        if (state.isGameOver) return "SPACE INVADERS | GAME OVER | Score: " + p.score;
        return "SPACE INVADERS | Score: " + p.score + " | " + h + " | Wave " + state.wave + "/" + state.gameWaves + extras;
    },

    commandBar: function(state, me) {
        var p = state.pls[me.id];
        if (state.isGameOver) return "/score Scoreboard  /reset to play again";
        if (p && p.dead) return "Respawning...  /score for scoreboard";
        return "[←→] Move  [Space/↑] Shoot  [Enter] Chat  /score Scoreboard";
    }
};
