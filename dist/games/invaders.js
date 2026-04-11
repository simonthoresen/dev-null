// invaders.js — Multiplayer Space Invaders for dev-null
// Load with: /load invaders

// ============================================================
// Constants
// ============================================================
var CRED  = "#AA0000";
var CGRN  = "#00AA00";
var CYEL  = "#AA5500";
var CBLU  = "#0000AA";
var CMAG  = "#AA00AA";
var CCYN  = "#00AAAA";
var CWHT  = "#AAAAAA";
var CDIM  = "#666666";

// Fixed world size in grid cells
// Walls at x=0, x=MAP_W-1. Ceiling at y=0, ground at y=MAP_H-1.
// Playable area is x:[1..MAP_W-2], y:[1..MAP_H-2].
var MAP_W = 60;
var MAP_H = 35;

// Player ship colors per slot (foreground for narrow, bg+fg for wide)
var SHIP_COLORS = [CGRN, CCYN, CMAG, CYEL, CWHT, CRED];
var SHIP_BG = [
    "#005F00",   // dark green
    "#005F87",   // dark cyan
    "#5F005F",   // dark magenta
    "#5F5F00",   // dark olive
    "#303030",   // dark gray
    "#5F0000"    // dark red
];
// Ship glyphs: 2-char ASCII art (reliable bg coverage) + 1-char narrow
var E_SHIP_WIDE = "/\\";    // two regular chars — bg fills both columns
var E_SHIP_NARROW = "^";

// Enemy emojis by tier (top rows = more points)
var E_ALIEN = [
    "\uD83D\uDC7E",  // 👾 — basic (10 pts)
    "\uD83D\uDC7D",  // 👽 — mid (20 pts)
    "\uD83E\uDD16",  // 🤖 — tough (30 pts)
    "\uD83D\uDC79"   // 👹 — boss row (50 pts)
];
var ALIEN_PTS = [10, 20, 30, 50];
var ALIEN_COLORS_1CH = [CGRN, CCYN, CMAG, CRED];

// Ground row alternating colors
var GROUND_FG_A = "#444444";
var GROUND_FG_B = "#666666";

// Effect emojis
var E_BOOM   = "\uD83D\uDCA5";  // 💥
var E_SHIELD = "\uD83D\uDEE1\uFE0F"; // 🛡️
var E_ZAP    = "\u26A1";        // ⚡
var E_HEART  = "\u2764\uFE0F";  // ❤️
var E_UFO    = "\uD83D\uDEF8";  // 🛸
var E_FIRE   = "\uD83D\uDD25";  // 🔥
// Bunker block chars: full → medium → light shade, same visual family

// Timing (all durations in seconds)
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
var GAME_WAVES = 5;
var WAVE_PAUSE = 3.0;
var BOOM_TTL = 0.5;

// Derived constants
var PLAYER_Y = MAP_H - 2;   // player row (above ground)
var GROUND_Y = MAP_H - 1;   // ground row

// Bunker shape (relative coords). 5 wide × 3 tall with arch.
var BUNKER_SHAPE = [
    [0,0],[1,0],[2,0],[3,0],[4,0],
    [0,1],[1,1],[2,1],[3,1],[4,1],
    [0,2],[1,2],      [3,2],[4,2]
];
var BUNKER_COUNT = 4;

// ============================================================
// State
// ============================================================
var pls = {}, plOrder = [];
var aliens = [];
var alienDX = 1;
var alienMoveTimer = 0;
var alienBullets = [];
var playerBullets = [];
var booms = [];
var powerups = [];
var ufo = null;
var ufoTimer = 0;
var ufoMoveTimer = 0;
var powerupMoveTimer = 0;
var elapsed = 0;
var wave = 1;
var waveAlienCount = 0;
var wavePause = 0;
var gameOver = false;
var gameOverAt = 0;
var bunkers = {};   // key "x,y" -> hp (1-3)
var inited = false;

// ============================================================
// Helpers
// ============================================================
function rep(s, n) { var r = ""; for (var i = 0; i < n; i++) r += s; return r; }
function clamp(v, lo, hi) { return v < lo ? lo : v > hi ? hi : v; }
function rng() { return Math.random(); }
function rngInt(lo, hi) { return lo + Math.floor(rng() * (hi - lo + 1)); }

// ============================================================
// Bunkers
// ============================================================
function spawnBunkers() {
    bunkers = {};
    var bunkerY = PLAYER_Y - 4;
    var playW = MAP_W - 2;
    var spacing = Math.floor(playW / (BUNKER_COUNT + 1));
    for (var b = 0; b < BUNKER_COUNT; b++) {
        var bx = 1 + spacing * (b + 1) - 2;
        for (var s = 0; s < BUNKER_SHAPE.length; s++) {
            var cx = bx + BUNKER_SHAPE[s][0];
            var cy = bunkerY + BUNKER_SHAPE[s][1];
            if (cx >= 1 && cx < MAP_W - 1 && cy >= 1 && cy < GROUND_Y) {
                bunkers[cx + "," + cy] = 3;
            }
        }
    }
}

function hitBunker(x, y) {
    var k = x + "," + y;
    if (bunkers[k]) {
        bunkers[k]--;
        if (bunkers[k] <= 0) delete bunkers[k];
        return true;
    }
    return false;
}

// ============================================================
// Alien formation
// ============================================================
function spawnWave() {
    aliens = [];
    var rows = Math.min(3 + Math.floor(wave / 2), 5);
    var cols = Math.min(8 + wave, 12);
    var playW = MAP_W - 2;
    var startX = 1 + Math.floor((playW - cols * 3) / 2);
    var startY = 2;
    log("Spawning wave " + wave + ": " + rows + "x" + cols + " aliens");

    for (var r = 0; r < rows; r++) {
        var tier = Math.min(rows - 1 - r, E_ALIEN.length - 1);
        for (var c = 0; c < cols; c++) {
            aliens.push({
                x: startX + c * 3,
                y: startY + r * 2,
                tier: tier,
                alive: true
            });
        }
    }
    alienDX = 1;
    alienMoveTimer = 0;
    waveAlienCount = aliens.length;
    alienBullets = [];
    spawnBunkers();
}

function aliveAliens() {
    var count = 0;
    for (var i = 0; i < aliens.length; i++) {
        if (aliens[i].alive) count++;
    }
    return count;
}

function alienBounds() {
    var minX = 9999, maxX = -1, maxY = -1;
    for (var i = 0; i < aliens.length; i++) {
        var a = aliens[i];
        if (!a.alive) continue;
        if (a.x < minX) minX = a.x;
        if (a.x > maxX) maxX = a.x;
        if (a.y > maxY) maxY = a.y;
    }
    return {minX: minX, maxX: maxX, maxY: maxY};
}

// ============================================================
// Player
// ============================================================
function newPlayer(id, name) {
    return {
        id: id, name: name,
        x: Math.floor(MAP_W / 2), y: PLAYER_Y,
        score: 0, lives: 3,
        dead: false, respawnTimer: 0, invulnTimer: 0,
        cooldown: 0,
        rapidFire: 0,
        shield: 0,
        ci: plOrder.length % SHIP_BG.length
    };
}

// ============================================================
// Tick — all coordinates in fixed world space
// ============================================================
function tick(dt) {
    if (gameOver) return;

    elapsed += dt;

    if (!inited) {
        inited = true;
        spawnWave();
    }

    // Wave pause
    if (wavePause > 0) {
        wavePause -= dt;
        if (wavePause <= 0) {
            spawnWave();
            for (var i = 0; i < plOrder.length; i++) {
                var p = pls[plOrder[i]];
                if (p && !p.dead) p.y = PLAYER_Y;
            }
        }
        return;
    }

    // Player timers
    for (var i = 0; i < plOrder.length; i++) {
        var p = pls[plOrder[i]];
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

    // Move player bullets (up)
    for (var i = playerBullets.length - 1; i >= 0; i--) {
        playerBullets[i].y -= BULLET_SPEED;
        if (playerBullets[i].y <= 0) { // hit ceiling
            playerBullets.splice(i, 1);
            continue;
        }
        if (hitBunker(playerBullets[i].x, playerBullets[i].y)) {
            playerBullets.splice(i, 1);
        }
    }

    // Move alien bullets (down)
    for (var i = alienBullets.length - 1; i >= 0; i--) {
        alienBullets[i].y += ALIEN_BULLET_SPEED;
        if (alienBullets[i].y >= GROUND_Y) { // hit ground
            alienBullets.splice(i, 1);
            continue;
        }
        if (hitBunker(alienBullets[i].x, alienBullets[i].y)) {
            alienBullets.splice(i, 1);
        }
    }

    // Move alien formation
    alienMoveTimer += dt;
    var alive = aliveAliens();
    var speedUp = Math.max(0.2, ALIEN_MOVE_INTERVAL - Math.floor((waveAlienCount - alive) / 4) * 0.1);
    if (alienMoveTimer >= speedUp) {
        alienMoveTimer = 0;
        var b = alienBounds();
        var hitEdge = false;
        if (alienDX > 0 && b.maxX >= MAP_W - 2) hitEdge = true;
        if (alienDX < 0 && b.minX <= 1) hitEdge = true;

        if (hitEdge) {
            alienDX = -alienDX;
            for (var i = 0; i < aliens.length; i++) {
                if (aliens[i].alive) aliens[i].y++;
            }
            var ab2 = alienBounds();
            log("Aliens bounced, maxY=" + ab2.maxY + " ground=" + GROUND_Y + " alive=" + alive);
            // Aliens crush bunkers
            for (var i = 0; i < aliens.length; i++) {
                var a = aliens[i];
                if (!a.alive) continue;
                var k = a.x + "," + a.y;
                if (bunkers[k]) delete bunkers[k];
            }
        } else {
            for (var i = 0; i < aliens.length; i++) {
                if (aliens[i].alive) aliens[i].x += alienDX;
            }
        }
    }

    // Alien shooting
    for (var i = 0; i < aliens.length; i++) {
        var a = aliens[i];
        if (!a.alive) continue;
        if (rng() < ALIEN_SHOOT_CHANCE) {
            var blocked = false;
            for (var j = 0; j < aliens.length; j++) {
                if (j === i || !aliens[j].alive) continue;
                if (aliens[j].x === a.x && aliens[j].y > a.y) {
                    blocked = true;
                    break;
                }
            }
            if (!blocked) {
                alienBullets.push({x: a.x, y: a.y + 1});
            }
        }
    }

    // UFO
    ufoTimer += dt;
    if (ufoTimer >= UFO_INTERVAL && !ufo) {
        ufoTimer = 0;
        var dir = rng() > 0.5 ? 1 : -1;
        ufo = {
            x: dir > 0 ? 1 : MAP_W - 2,
            y: 1,
            dir: dir,
            pts: [50, 100, 150, 300][rngInt(0, 3)]
        };
    }
    if (ufo) {
        ufoMoveTimer += dt;
        while (ufoMoveTimer >= UFO_MOVE_INTERVAL) {
            ufoMoveTimer -= UFO_MOVE_INTERVAL;
            ufo.x += ufo.dir;
            if (ufo.x < 1 || ufo.x >= MAP_W - 1) { ufo = null; break; }
        }
    }

    // Move powerups
    powerupMoveTimer += dt;
    var powerupSteps = 0;
    while (powerupMoveTimer >= POWERUP_MOVE_INTERVAL) {
        powerupMoveTimer -= POWERUP_MOVE_INTERVAL;
        powerupSteps++;
    }
    for (var i = powerups.length - 1; i >= 0; i--) {
        powerups[i].y += powerupSteps;
        if (powerups[i].y >= GROUND_Y) {
            powerups.splice(i, 1);
        }
    }

    // Decay explosions
    for (var i = booms.length - 1; i >= 0; i--) {
        booms[i].ttl -= dt;
        if (booms[i].ttl <= 0) booms.splice(i, 1);
    }

    // --- Collision detection ---

    // Player bullets vs aliens
    for (var bi = playerBullets.length - 1; bi >= 0; bi--) {
        var bul = playerBullets[bi];
        var hit = false;
        for (var ai = 0; ai < aliens.length; ai++) {
            var a = aliens[ai];
            if (!a.alive) continue;
            if (Math.abs(bul.x - a.x) <= 1 && bul.y === a.y) {
                a.alive = false;
                hit = true;
                var shooter = pls[bul.owner];
                if (shooter) shooter.score += ALIEN_PTS[a.tier];
                booms.push({x: a.x, y: a.y, ttl: BOOM_TTL});
                if (rng() < POWERUP_CHANCE) {
                    var types = ["rapid", "shield", "life"];
                    powerups.push({x: a.x, y: a.y, type: types[rngInt(0, 2)]});
                }
                break;
            }
        }
        if (!hit && ufo) {
            if (Math.abs(bul.x - ufo.x) <= 1 && bul.y === ufo.y) {
                hit = true;
                var shooter = pls[bul.owner];
                if (shooter) {
                    shooter.score += ufo.pts;
                    chat(shooter.name + " shot the UFO! +" + ufo.pts);
                }
                booms.push({x: ufo.x, y: ufo.y, ttl: BOOM_TTL});
                ufo = null;
            }
        }
        if (hit) playerBullets.splice(bi, 1);
    }

    // Alien bullets vs players
    for (var bi = alienBullets.length - 1; bi >= 0; bi--) {
        var bul = alienBullets[bi];
        for (var pi = 0; pi < plOrder.length; pi++) {
            var p = pls[plOrder[pi]];
            if (!p || p.dead) continue;
            if (Math.abs(bul.x - p.x) <= 1 && bul.y === p.y) {
                alienBullets.splice(bi, 1);
                if (p.invulnTimer > 0) break;
                if (p.shield > 0) {
                    p.shield = 0;
                    booms.push({x: p.x, y: p.y - 1, ttl: BOOM_TTL});
                    break;
                }
                killPlayer(p);
                break;
            }
        }
    }

    // Aliens reaching ground — wipe wave, penalise players
    var ab = alienBounds();
    if (ab.maxY >= PLAYER_Y - 1) {
        var invaded = false;
        for (var ai = 0; ai < aliens.length; ai++) {
            var a = aliens[ai];
            if (!a.alive) continue;
            if (a.y >= GROUND_Y) { invaded = true; break; }
            // Kill players that overlap with low aliens
            if (a.y < PLAYER_Y - 1) continue;
            for (var pi = 0; pi < plOrder.length; pi++) {
                var p = pls[plOrder[pi]];
                if (!p || p.dead || p.invulnTimer > 0) continue;
                if (Math.abs(a.x - p.x) <= 1 && Math.abs(a.y - p.y) <= 1) {
                    if (p.shield > 0) { p.shield = 0; }
                    else { killPlayer(p); }
                }
            }
        }
        if (invaded) {
            log("Aliens reached ground at " + elapsed.toFixed(1) + "s, wiping wave");
            chat("The aliens reached the ground! All players lose a life!");
            for (var ai = 0; ai < aliens.length; ai++) {
                aliens[ai].alive = false;
            }
            for (var pi = 0; pi < plOrder.length; pi++) {
                var p = pls[plOrder[pi]];
                if (!p || p.dead) continue;
                killPlayer(p);
            }
        }
    }

    // Players vs powerups
    for (var i = powerups.length - 1; i >= 0; i--) {
        var pw = powerups[i];
        for (var pi = 0; pi < plOrder.length; pi++) {
            var p = pls[plOrder[pi]];
            if (!p || p.dead) continue;
            if (Math.abs(pw.x - p.x) <= 1 && pw.y === p.y) {
                if (pw.type === "rapid") {
                    p.rapidFire = RAPID_FIRE_DUR;
                    chatPlayer(p.id, "\u26A1 Rapid fire!");
                } else if (pw.type === "shield") {
                    p.shield = SHIELD_DUR;
                    chatPlayer(p.id, "\uD83D\uDEE1\uFE0F Shield active!");
                } else if (pw.type === "life") {
                    p.lives = Math.min(p.lives + 1, 5);
                    chatPlayer(p.id, "\u2764\uFE0F Extra life!");
                }
                powerups.splice(i, 1);
                break;
            }
        }
    }

    // Wave cleared?
    if (aliveAliens() === 0 && wavePause <= 0) {
        if (wave >= GAME_WAVES) {
            log("All waves complete, ending game at " + elapsed.toFixed(1) + "s");
            endGame();
        } else {
            wave++;
            wavePause = WAVE_PAUSE;
            log("Wave " + wave + " starting in " + WAVE_PAUSE + "s (elapsed " + elapsed.toFixed(1) + "s)");
            chat("Wave " + wave + " incoming!");
        }
    }
}

function killPlayer(p) {
    p.lives--;
    p.dead = true;
    booms.push({x: p.x, y: p.y, ttl: BOOM_TTL});
    if (p.lives <= 0) {
        p.respawnTimer = RESPAWN_TIME * 2;
        p.lives = 3;
        p.score = Math.max(0, p.score - 200);
        chat(p.name + " destroyed! Respawning with penalty...");
    } else {
        p.respawnTimer = RESPAWN_TIME;
        chat(p.name + " hit! " + p.lives + " lives left");
    }
}

function endGame() {
    gameOver = true;
    gameOverAt = elapsed;
    var sorted = plOrder.slice().sort(function(a, b) {
        return (pls[b] ? pls[b].score : 0) - (pls[a] ? pls[a].score : 0);
    });
    chat("=== GAME OVER ===");
    for (var i = 0; i < sorted.length; i++) {
        var p = pls[sorted[i]];
        if (!p) continue;
        var medal = i === 0 ? "\uD83E\uDD47" : i === 1 ? "\uD83E\uDD48" : i === 2 ? "\uD83E\uDD49" : "  ";
        chat(medal + " " + (i + 1) + ". " + p.name + ": " + p.score + " pts");
    }
    if (sorted.length > 0 && pls[sorted[0]]) {
        chat(pls[sorted[0]].name + " wins!");
    }
}

// ============================================================
// Rendering — viewport is a window into the fixed world
// ============================================================
function render(buf, pid, width, height) {
    var cw = (width >= 60) ? 2 : 1;
    var viewCols = Math.floor(width / cw);
    var viewRows = height;

    // Camera: center on player, clamp to world
    var me = pls[pid];
    var cx = me && !me.dead ? me.x : Math.floor(MAP_W / 2);
    var cy = me && !me.dead ? me.y : Math.floor(MAP_H / 2);

    var camX, camY;
    if (viewCols >= MAP_W) {
        camX = -Math.floor((viewCols - MAP_W) / 2);
    } else {
        camX = clamp(cx - Math.floor(viewCols / 2), 0, MAP_W - viewCols);
    }
    if (viewRows >= MAP_H) {
        camY = -Math.floor((viewRows - MAP_H) / 2);
    } else {
        camY = clamp(cy - Math.floor(viewRows / 2), 0, MAP_H - viewRows);
    }

    // Game-over overlay (screen coords, no world rendering)
    if (gameOver) {
        var goText = "=== GAME OVER ===";
        var oy = Math.floor(viewRows / 2) - 2;
        var ox = Math.floor((width - goText.length) / 2);
        if (oy >= 0 && oy < viewRows) {
            buf.writeString(ox, oy, goText, CRED, null, ATTR_BOLD);
        }
        var sorted = plOrder.slice().sort(function(a, b) {
            return (pls[b] ? pls[b].score : 0) - (pls[a] ? pls[a].score : 0);
        });
        for (var i = 0; i < sorted.length && oy + 2 + i < viewRows; i++) {
            var p = pls[sorted[i]];
            if (!p) continue;
            var medal = i === 0 ? "#1" : i === 1 ? "#2" : i === 2 ? "#3" : "#" + (i + 1);
            var entry = medal + " " + p.name + ": " + p.score + " pts";
            var ex = Math.floor((width - entry.length) / 2);
            var col = i === 0 ? CYEL : i === 1 ? CWHT : CDIM;
            var attr = i === 0 ? ATTR_BOLD : 0;
            buf.writeString(ex, oy + 2 + i, entry, col, null, attr);
        }
        var hint = "Admin: /reset to play again";
        var hy = Math.min(oy + 2 + sorted.length + 2, viewRows - 1);
        if (hy >= 0 && hy < viewRows) {
            var hx = Math.floor((width - hint.length) / 2);
            buf.writeString(hx, hy, hint, CDIM, null, 0);
        }
        return;
    }

    // Wave pause overlay (screen coords, no world rendering)
    if (wavePause > 0) {
        var waveText = "WAVE " + wave;
        var oy = Math.floor(viewRows / 2);
        var ox = Math.floor((width - waveText.length) / 2);
        if (oy >= 0 && oy < viewRows) {
            buf.writeString(ox, oy, waveText, CYEL, null, ATTR_BOLD);
        }
        return;
    }

    // Build entity map in world coords
    // Each entry: {ch: "X", fg: color, bg: color, attr: 0, ch2: "Y"} for 2-wide
    // or {ch: "X", fg: color, bg: color, attr: 0} for 1-wide
    // For emoji (cw===2): {emoji: "\uD83D\uDC7E"}
    var ents = {};

    // Bunkers
    for (var k in bunkers) {
        var hp = bunkers[k];
        if (hp >= 3) ents[k] = {ch: "\u2588", ch2: "\u2588", fg: CGRN, bg: null, attr: 0};
        else if (hp === 2) ents[k] = {ch: "\u2593", ch2: "\u2593", fg: CYEL, bg: null, attr: 0};
        else ents[k] = {ch: "\u2591", ch2: "\u2591", fg: CRED, bg: null, attr: 0};
    }

    // Explosions
    for (var i = 0; i < booms.length; i++) {
        var b = booms[i];
        if (cw === 2) {
            ents[b.x + "," + b.y] = {emoji: b.ttl > BOOM_TTL * 0.4 ? E_BOOM : E_FIRE};
        } else {
            ents[b.x + "," + b.y] = {ch: "*", fg: CRED, bg: null, attr: ATTR_BOLD};
        }
    }

    // Powerups
    for (var i = 0; i < powerups.length; i++) {
        var pw = powerups[i];
        var k = pw.x + "," + pw.y;
        if (cw === 2) {
            if (pw.type === "rapid") ents[k] = {emoji: E_ZAP};
            else if (pw.type === "shield") ents[k] = {emoji: E_SHIELD};
            else ents[k] = {emoji: E_HEART};
        } else {
            if (pw.type === "rapid") ents[k] = {ch: "z", fg: CYEL, bg: null, attr: 0};
            else if (pw.type === "shield") ents[k] = {ch: "s", fg: CBLU, bg: null, attr: 0};
            else ents[k] = {ch: "+", fg: CRED, bg: null, attr: 0};
        }
    }

    // Aliens
    for (var i = 0; i < aliens.length; i++) {
        var a = aliens[i];
        if (!a.alive) continue;
        if (cw === 2) {
            ents[a.x + "," + a.y] = {emoji: E_ALIEN[a.tier]};
        } else {
            ents[a.x + "," + a.y] = {ch: "W", fg: ALIEN_COLORS_1CH[a.tier], bg: null, attr: 0};
        }
    }

    // UFO
    if (ufo) {
        if (cw === 2) {
            ents[ufo.x + "," + ufo.y] = {emoji: E_UFO};
        } else {
            ents[ufo.x + "," + ufo.y] = {ch: "U", fg: CRED, bg: null, attr: ATTR_BOLD};
        }
    }

    // Alien bullets
    for (var i = 0; i < alienBullets.length; i++) {
        var b = alienBullets[i];
        ents[b.x + "," + b.y] = {ch: "\u2593", ch2: "\u2593", fg: CRED, bg: null, attr: 0};
    }

    // Player bullets
    for (var i = 0; i < playerBullets.length; i++) {
        var b = playerBullets[i];
        var shooter = pls[b.owner];
        var col = shooter ? SHIP_COLORS[shooter.ci] : CWHT;
        if (cw === 2) {
            ents[b.x + "," + b.y] = {ch: "\u2502", ch2: "\u2502", fg: col, bg: null, attr: 0};
        } else {
            ents[b.x + "," + b.y] = {ch: "|", fg: col, bg: null, attr: 0};
        }
    }

    // Players
    for (var i = 0; i < plOrder.length; i++) {
        var p = pls[plOrder[i]];
        if (!p || p.dead) continue;
        var k = p.x + "," + p.y;
        var bg = SHIP_BG[p.ci];
        var col = SHIP_COLORS[p.ci];
        var blinkOn = Math.floor(elapsed / 0.2) % 2 === 0;
        if (cw === 2) {
            if (p.invulnTimer > 0 && blinkOn) {
                ents[k] = {ch: " ", ch2: " ", fg: null, bg: null, attr: 0};
            } else if (p.shield > 0) {
                ents[k] = {ch: "{", ch2: "}", fg: CWHT, bg: bg, attr: ATTR_BOLD};
            } else {
                ents[k] = {ch: "/", ch2: "\\", fg: CWHT, bg: bg, attr: ATTR_BOLD};
            }
        } else {
            if (p.invulnTimer > 0 && blinkOn) {
                ents[k] = {ch: " ", fg: null, bg: null, attr: 0};
            } else if (p.shield > 0) {
                ents[k] = {ch: "O", fg: col, bg: null, attr: ATTR_BOLD};
            } else {
                var attr = plOrder[i] === pid ? ATTR_BOLD : 0;
                ents[k] = {ch: E_SHIP_NARROW, fg: col, bg: null, attr: attr};
            }
        }
    }

    // Render viewport
    for (var row = 0; row < viewRows; row++) {
        for (var col = 0; col < viewCols; col++) {
            var wx = camX + col;
            var wy = camY + row;
            var sx = col * cw;   // screen x

            // Outside world = empty
            if (wx < 0 || wx >= MAP_W || wy < 0 || wy >= MAP_H) {
                continue;
            }

            // Ceiling row — full wall across
            if (wy === 0) {
                buf.setChar(sx, row, "\u2588", CBLU, null, 0);
                if (cw === 2) buf.setChar(sx + 1, row, "\u2588", CBLU, null, 0);
                continue;
            }

            // Ground row
            if (wy === GROUND_Y) {
                var gfg = (cw === 2)
                    ? (Math.floor(wx / 2) % 2 === 0 ? GROUND_FG_A : GROUND_FG_B)
                    : (wx % 2 === 0 ? GROUND_FG_A : GROUND_FG_B);
                buf.setChar(sx, row, "\u2584", gfg, null, 0);
                if (cw === 2) buf.setChar(sx + 1, row, "\u2584", gfg, null, 0);
                continue;
            }

            // Walls (left, right)
            if (wx === 0 || wx === MAP_W - 1) {
                buf.setChar(sx, row, "\u2588", CBLU, null, 0);
                if (cw === 2) buf.setChar(sx + 1, row, "\u2588", CBLU, null, 0);
                continue;
            }

            // Entity?
            var k = wx + "," + wy;
            var e = ents[k];
            if (e) {
                if (e.emoji) {
                    // Emoji: write using writeString (takes 2 columns)
                    buf.writeString(sx, row, e.emoji, null, null, 0);
                } else {
                    buf.setChar(sx, row, e.ch, e.fg, e.bg, e.attr);
                    if (cw === 2) {
                        var c2 = e.ch2 ? e.ch2 : e.ch;
                        buf.setChar(sx + 1, row, c2, e.fg, e.bg, e.attr);
                    }
                }
            }
        }
    }
}

// ============================================================
// Init
// ============================================================

registerCommand({
    name: "score",
    description: "Show the Space Invaders scoreboard",
    handler: function(pid, isAdmin, args) {
        var sorted = plOrder.slice().sort(function(a, b) {
            return (pls[b] ? pls[b].score : 0) - (pls[a] ? pls[a].score : 0);
        });
        var lines = ["--- SPACE INVADERS SCOREBOARD ---"];
        for (var i = 0; i < sorted.length; i++) {
            var p = pls[sorted[i]];
            if (!p) continue;
            lines.push((i + 1) + ". " + p.name + ": " + p.score + " pts (" + rep("\u2665", p.lives) + ")");
        }
        if (sorted.length === 0) lines.push("No players yet!");
        for (var i = 0; i < lines.length; i++) chatPlayer(pid, lines[i]);
    }
});

registerCommand({
    name: "reset",
    description: "Reset the Space Invaders game",
    adminOnly: true,
    handler: function(pid, isAdmin, args) {
        wave = 1; elapsed = 0; gameOver = false;
        aliens = []; alienBullets = []; playerBullets = [];
        booms = []; powerups = []; ufo = null; ufoTimer = 0;
        ufoMoveTimer = 0; powerupMoveTimer = 0;
        wavePause = 0; bunkers = {}; inited = false;
        for (var i = 0; i < plOrder.length; i++) {
            var p = pls[plOrder[i]];
            if (!p) continue;
            p.score = 0; p.lives = 3;
            p.dead = false; p.invulnTimer = INVULN_TIME;
            p.rapidFire = 0; p.shield = 0; p.cooldown = 0;
            p.x = Math.floor(MAP_W / 2); p.y = PLAYER_Y;
        }
        chat("Game reset by admin!");
    }
});

registerCommand({
    name: "waves",
    description: "Set number of waves (admin only)",
    adminOnly: true,
    handler: function(pid, isAdmin, args) {
        if (args.length < 1 || isNaN(parseInt(args[0]))) {
            chatPlayer(pid, "Usage: /waves <number> (currently " + GAME_WAVES + ")");
            return;
        }
        GAME_WAVES = Math.max(1, parseInt(args[0]));
        chat("Game set to " + GAME_WAVES + " waves!");
    }
});

// ============================================================
// Game API
// ============================================================
var Game = {
    onPlayerJoin: function(playerID, playerName) {
        pls[playerID] = newPlayer(playerID, playerName);
        plOrder.push(playerID);
        chat(playerName + " joined Space Invaders!");
    },

    onPlayerLeave: function(playerID) {
        var idx = plOrder.indexOf(playerID);
        if (idx >= 0) plOrder.splice(idx, 1);
        for (var i = playerBullets.length - 1; i >= 0; i--) {
            if (playerBullets[i].owner === playerID) playerBullets.splice(i, 1);
        }
        delete pls[playerID];
    },

    onInput: function(playerID, key) {
        var p = pls[playerID];
        if (!p || p.dead || gameOver) return;

        if (key === "left") {
            p.x = Math.max(1, p.x - PLAYER_SPEED);
        } else if (key === "right") {
            p.x = Math.min(MAP_W - 2, p.x + PLAYER_SPEED);
        } else if (key === " " || key === "up") {
            var cd = p.rapidFire > 0 ? RAPID_COOLDOWN : FIRE_COOLDOWN;
            if (p.cooldown <= 0) {
                playerBullets.push({x: p.x, y: p.y - 1, owner: playerID});
                p.cooldown = cd;
            }
        }
    },

    update: function(dt) {
        tick(dt);
    },

    renderAscii: function(buf, playerID, ox, oy, width, height) {
        render(buf, playerID, width, height);
    },

    statusBar: function(playerID) {
        var p = pls[playerID];
        if (!p) return "SPACE INVADERS";
        var h = rep("\u2665", p.lives);
        var extras = "";
        if (p.rapidFire > 0) extras += " \u26A1";
        if (p.shield > 0) extras += " \uD83D\uDEE1\uFE0F";
        if (gameOver) return "SPACE INVADERS | GAME OVER | Score: " + p.score;
        return "SPACE INVADERS | Score: " + p.score + " | " + h + " | Wave " + wave + "/" + GAME_WAVES + extras;
    },

    commandBar: function(playerID) {
        var p = pls[playerID];
        if (gameOver) return "/score Scoreboard  /reset to play again";
        if (p && p.dead) return "Respawning...  /score for scoreboard";
        return "[\u2190\u2192] Move  [Space/\u2191] Shoot  [Enter] Chat  /score Scoreboard";
    }
};
