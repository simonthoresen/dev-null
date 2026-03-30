// invaders.js — Multiplayer Space Invaders for null-space
// Load with: /load invaders

// ============================================================
// Constants
// ============================================================
var RST   = "\x1b[0m";
var CBOLD = "\x1b[1m";
var CDIM  = "\x1b[2m";
var CRED  = "\x1b[31m";
var CGRN  = "\x1b[32m";
var CYEL  = "\x1b[33m";
var CBLU  = "\x1b[34m";
var CMAG  = "\x1b[35m";
var CCYN  = "\x1b[36m";
var CWHT  = "\x1b[37m";
var CBGR  = "\x1b[41m";
var CBGG  = "\x1b[42m";
var CBGY  = "\x1b[43m";

// Player ship emojis (one set per player slot)
var E_SHIPS = [
    "\uD83D\uDE80",  // 🚀
    "\uD83D\uDEF8",  // 🛸
    "\uD83D\uDE81",  // 🚁 — not great, let's use shuttle
    "\uD83C\uDFA0",  // 🎠 — nah
    "\uD83D\uDE82",  // 🚂 — nah
    "\uD83C\uDFAF"   // 🎯
];
// Better ship set — distinct colored unicode arrows + emoji mix
var SHIP_COLORS = [CGRN, CCYN, CMAG, CYEL, CWHT, CRED];
var E_SHIP_EMOJI = [
    "\uD83D\uDE80",  // 🚀
    "\uD83D\uDEF8",  // 🛸
    "\uD83D\uDE80",  // 🚀 (reuse with different color)
    "\uD83D\uDEF8",  // 🛸
    "\uD83D\uDE80",  // 🚀
    "\uD83D\uDEF8"   // 🛸
];

// Enemy emojis by tier (top rows = more points)
var E_ALIEN = [
    "\uD83D\uDC7E",  // 👾 — basic alien (10 pts)
    "\uD83D\uDC7D",  // 👽 — mid alien (20 pts)
    "\uD83E\uDD16",  // 🤖 — tough alien (30 pts)
    "\uD83D\uDC79"   // 👹 — boss row (50 pts)
];
var ALIEN_PTS = [10, 20, 30, 50];
var ALIEN_COLORS_1CH = [CGRN, CCYN, CMAG, CRED];

// Effect emojis
var E_BOOM  = "\uD83D\uDCA5";  // 💥
var E_STAR  = "\u2B50";         // ⭐  (bonus UFO)
var E_SKULL = "\uD83D\uDC80";  // 💀 (player dead)
var E_SHIELD = "\uD83D\uDEE1\uFE0F"; // 🛡️ (shield powerup)
var E_ZAP   = "\u26A1";        // ⚡ (rapid fire powerup)
var E_HEART = "\u2764\uFE0F";  // ❤️ (extra life)
var E_UFO   = "\uD83D\uDEF8";  // 🛸 (bonus UFO)
var E_FIRE  = "\uD83D\uDD25";  // 🔥 (explosion particle)

// Timing (ticks are ~100ms from framework)
var ALIEN_MOVE_INTERVAL = 8;    // ticks between alien formation shifts
var ALIEN_SHOOT_CHANCE = 0.015; // per alien per tick
var BULLET_SPEED = 1;           // cells per tick (player bullets go up)
var ALIEN_BULLET_SPEED = 1;     // cells per tick (alien bullets go down)
var PLAYER_SPEED = 1;           // cells per input
var RESPAWN_TICKS = 30;         // 3 seconds
var INVULN_TICKS = 20;          // 2 seconds after respawn
var UFO_INTERVAL = 300;         // ticks between UFO spawns
var UFO_SPEED = 3;              // moves every N ticks
var POWERUP_CHANCE = 0.15;      // chance to drop powerup on alien kill
var RAPID_FIRE_DUR = 80;        // ticks of rapid fire
var SHIELD_DUR = 60;            // ticks of shield
var FIRE_COOLDOWN = 4;          // ticks between shots
var RAPID_COOLDOWN = 2;         // ticks between shots with rapid fire
var GAME_WAVES = 5;             // waves before game ends
var WAVE_PAUSE = 30;            // ticks between waves

// ============================================================
// State
// ============================================================
var pls = {}, plOrder = [];
var aliens = [];
var alienDX = 1;                // formation move direction
var alienMoveTimer = 0;
var alienBullets = [];
var playerBullets = [];
var booms = [];                 // explosion effects {x, y, ttl}
var powerups = [];              // {x, y, type, dy}
var ufo = null;                 // {x, y, dir, pts}
var ufoTimer = 0;
var frame = 0;
var lastTick = 0;
var wave = 1;
var waveAlienCount = 0;
var wavePause = 0;              // countdown between waves
var gameOver = false;
var gameOverAt = 0;
var fieldW = 0, fieldH = 0;    // set on first render

// ============================================================
// Helpers
// ============================================================
function rep(s, n) { var r = ""; for (var i = 0; i < n; i++) r += s; return r; }
function clamp(v, lo, hi) { return v < lo ? lo : v > hi ? hi : v; }
function rng() { return Math.random(); }
function rngInt(lo, hi) { return lo + Math.floor(rng() * (hi - lo + 1)); }

// ============================================================
// Alien formation
// ============================================================
function spawnWave(w, h) {
    aliens = [];
    // Rows increase with wave number (3-5 rows, 8-12 cols)
    var rows = Math.min(3 + Math.floor(wave / 2), 5);
    var cols = Math.min(8 + wave, 12);
    var startX = Math.floor((w - cols * 3) / 2);
    var startY = 1;

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
        x: 0, y: 0,  // set on first render
        score: 0, lives: 3,
        dead: false, respawnAt: 0, invuln: 0,
        cooldown: 0,
        rapidFire: 0,
        shield: 0,
        ci: plOrder.length % E_SHIP_EMOJI.length,
        placed: false
    };
}

function placePlayer(p, w, h) {
    if (p.placed) return;
    var slot = plOrder.indexOf(p.id);
    var count = plOrder.length;
    var spacing = Math.floor(w / (count + 1));
    p.x = spacing * (slot + 1);
    p.y = h - 1;
    p.placed = true;
}

// ============================================================
// Tick
// ============================================================
function tick(w, h) {
    var now = Date.now();
    if (now - lastTick < 90) return;
    lastTick = now;

    if (gameOver) return;

    frame++;

    // Init field on first tick
    if (fieldW !== w || fieldH !== h) {
        fieldW = w; fieldH = h;
        if (aliens.length === 0) spawnWave(w, h);
        // Re-place all players at bottom row
        for (var i = 0; i < plOrder.length; i++) {
            var p = pls[plOrder[i]];
            if (p) { p.y = h - 1; p.placed = true; }
        }
    }

    // Wave pause countdown
    if (wavePause > 0) {
        wavePause--;
        if (wavePause === 0) {
            spawnWave(w, h);
            // Reset player positions
            for (var i = 0; i < plOrder.length; i++) {
                var p = pls[plOrder[i]];
                if (p && !p.dead) p.y = h - 1;
            }
        }
        return;
    }

    // Decrement player timers
    for (var i = 0; i < plOrder.length; i++) {
        var p = pls[plOrder[i]];
        if (!p) continue;
        if (p.cooldown > 0) p.cooldown--;
        if (p.rapidFire > 0) p.rapidFire--;
        if (p.shield > 0) p.shield--;
        if (p.dead && frame >= p.respawnAt) {
            p.dead = false;
            p.x = Math.floor(w / 2);
            p.y = h - 1;
            p.invuln = frame + INVULN_TICKS;
        }
    }

    // Move player bullets (up)
    for (var i = playerBullets.length - 1; i >= 0; i--) {
        playerBullets[i].y -= BULLET_SPEED;
        if (playerBullets[i].y < 0) {
            playerBullets.splice(i, 1);
        }
    }

    // Move alien bullets (down)
    for (var i = alienBullets.length - 1; i >= 0; i--) {
        alienBullets[i].y += ALIEN_BULLET_SPEED;
        if (alienBullets[i].y >= h) {
            alienBullets.splice(i, 1);
        }
    }

    // Move alien formation
    alienMoveTimer++;
    // Speed up as aliens die
    var alive = aliveAliens();
    var speedUp = Math.max(2, ALIEN_MOVE_INTERVAL - Math.floor((waveAlienCount - alive) / 4));
    if (alienMoveTimer >= speedUp) {
        alienMoveTimer = 0;
        var b = alienBounds();
        var hitEdge = false;
        if (alienDX > 0 && b.maxX >= w - 3) hitEdge = true;
        if (alienDX < 0 && b.minX <= 1) hitEdge = true;

        if (hitEdge) {
            alienDX = -alienDX;
            // Move all down
            for (var i = 0; i < aliens.length; i++) {
                if (aliens[i].alive) aliens[i].y++;
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
            // Only bottom-most alien in each column shoots
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
    ufoTimer++;
    if (ufoTimer >= UFO_INTERVAL && !ufo) {
        ufoTimer = 0;
        var dir = rng() > 0.5 ? 1 : -1;
        ufo = {
            x: dir > 0 ? -2 : w + 1,
            y: 0,
            dir: dir,
            pts: [50, 100, 150, 300][rngInt(0, 3)]
        };
    }
    if (ufo && frame % UFO_SPEED === 0) {
        ufo.x += ufo.dir;
        if (ufo.x < -3 || ufo.x > w + 2) ufo = null;
    }

    // Move powerups
    for (var i = powerups.length - 1; i >= 0; i--) {
        if (frame % 3 === 0) powerups[i].y++;
        if (powerups[i].y >= h) {
            powerups.splice(i, 1);
        }
    }

    // Decay explosions
    for (var i = booms.length - 1; i >= 0; i--) {
        booms[i].ttl--;
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
            // Hit detection: bullet within 1 cell of alien
            if (Math.abs(bul.x - a.x) <= 1 && Math.abs(bul.y - a.y) <= 0) {
                a.alive = false;
                hit = true;
                var shooter = pls[bul.owner];
                if (shooter) {
                    shooter.score += ALIEN_PTS[a.tier];
                }
                booms.push({x: a.x, y: a.y, ttl: 4});
                // Maybe drop powerup
                if (rng() < POWERUP_CHANCE) {
                    var types = ["rapid", "shield", "life"];
                    powerups.push({x: a.x, y: a.y, type: types[rngInt(0, 2)]});
                }
                break;
            }
        }
        // Player bullets vs UFO
        if (!hit && ufo) {
            if (Math.abs(bul.x - ufo.x) <= 1 && bul.y === ufo.y) {
                hit = true;
                var shooter = pls[bul.owner];
                if (shooter) {
                    shooter.score += ufo.pts;
                    chat(shooter.name + " shot the UFO! +" + ufo.pts);
                }
                booms.push({x: ufo.x, y: ufo.y, ttl: 5});
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
                if (frame < p.invuln) break; // invulnerable
                if (p.shield > 0) {
                    p.shield = 0; // absorb hit
                    booms.push({x: p.x, y: p.y - 1, ttl: 3});
                    break;
                }
                killPlayer(p);
                break;
            }
        }
    }

    // Aliens reaching player row = death for nearby players
    var ab = alienBounds();
    if (ab.maxY >= h - 2) {
        for (var ai = 0; ai < aliens.length; ai++) {
            var a = aliens[ai];
            if (!a.alive || a.y < h - 2) continue;
            for (var pi = 0; pi < plOrder.length; pi++) {
                var p = pls[plOrder[pi]];
                if (!p || p.dead || frame < p.invuln) continue;
                if (Math.abs(a.x - p.x) <= 1 && Math.abs(a.y - p.y) <= 1) {
                    if (p.shield > 0) {
                        p.shield = 0;
                    } else {
                        killPlayer(p);
                    }
                }
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
    if (aliveAliens() === 0 && wavePause === 0) {
        if (wave >= GAME_WAVES) {
            endGame();
        } else {
            wave++;
            wavePause = WAVE_PAUSE;
            chat("Wave " + wave + " incoming!");
        }
    }
}

function killPlayer(p) {
    p.lives--;
    p.dead = true;
    booms.push({x: p.x, y: p.y, ttl: 5});
    if (p.lives <= 0) {
        p.respawnAt = frame + RESPAWN_TICKS * 2;
        p.lives = 3;
        p.score = Math.max(0, p.score - 200);
        chat(p.name + " destroyed! Respawning with penalty...");
    } else {
        p.respawnAt = frame + RESPAWN_TICKS;
        chat(p.name + " hit! " + p.lives + " lives left");
    }
}

function endGame() {
    gameOver = true;
    gameOverAt = frame;
    // Find winner
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
// Rendering
// ============================================================
function render(pid, width, height) {
    tick(width, height);

    var cw = (width >= 60) ? 2 : 1;
    var viewCols = Math.floor(width / cw);
    var viewRows = height;
    var emptyCell = rep(" ", cw);

    // Build entity map: key "x,y" -> display string
    var ents = {};

    // Explosions first (lowest priority, overwritten by living things)
    for (var i = 0; i < booms.length; i++) {
        var b = booms[i];
        if (cw === 2) {
            ents[b.x + "," + b.y] = b.ttl > 2 ? E_BOOM : E_FIRE;
        } else {
            ents[b.x + "," + b.y] = CRED + CBOLD + "*" + RST;
        }
    }

    // Powerups
    for (var i = 0; i < powerups.length; i++) {
        var pw = powerups[i];
        var k = pw.x + "," + pw.y;
        if (cw === 2) {
            if (pw.type === "rapid") ents[k] = E_ZAP;
            else if (pw.type === "shield") ents[k] = E_SHIELD;
            else ents[k] = E_HEART;
        } else {
            if (pw.type === "rapid") ents[k] = CYEL + "z" + RST;
            else if (pw.type === "shield") ents[k] = CBLU + "s" + RST;
            else ents[k] = CRED + "+" + RST;
        }
    }

    // Aliens
    for (var i = 0; i < aliens.length; i++) {
        var a = aliens[i];
        if (!a.alive) continue;
        var k = a.x + "," + a.y;
        if (cw === 2) {
            ents[k] = E_ALIEN[a.tier];
        } else {
            ents[k] = ALIEN_COLORS_1CH[a.tier] + "W" + RST;
        }
    }

    // UFO
    if (ufo) {
        var k = ufo.x + "," + ufo.y;
        if (cw === 2) {
            ents[k] = E_UFO;
        } else {
            ents[k] = CRED + CBOLD + "U" + RST;
        }
    }

    // Alien bullets
    for (var i = 0; i < alienBullets.length; i++) {
        var b = alienBullets[i];
        var k = b.x + "," + b.y;
        if (cw === 2) {
            ents[k] = CRED + "\u2593\u2593" + RST;
        } else {
            ents[k] = CRED + "!" + RST;
        }
    }

    // Player bullets
    for (var i = 0; i < playerBullets.length; i++) {
        var b = playerBullets[i];
        var k = b.x + "," + b.y;
        var shooter = pls[b.owner];
        var col = shooter ? SHIP_COLORS[shooter.ci] : CWHT;
        if (cw === 2) {
            ents[k] = col + "\u2502\u2502" + RST;
        } else {
            ents[k] = col + "|" + RST;
        }
    }

    // Players
    for (var i = 0; i < plOrder.length; i++) {
        var p = pls[plOrder[i]];
        if (!p || p.dead) continue;
        var k = p.x + "," + p.y;
        if (cw === 2) {
            if (frame < p.invuln && frame % 4 < 2) {
                // Blink when invulnerable
                ents[k] = emptyCell;
            } else if (p.shield > 0) {
                ents[k] = E_SHIELD;
            } else {
                ents[k] = E_SHIP_EMOJI[p.ci];
            }
        } else {
            var col = SHIP_COLORS[p.ci];
            if (frame < p.invuln && frame % 4 < 2) {
                ents[k] = " ";
            } else if (p.shield > 0) {
                ents[k] = col + CBOLD + "O" + RST;
            } else {
                ents[k] = (plOrder[i] === pid ? CBOLD : "") + col + "A" + RST;
            }
        }
    }

    // Game-over overlay
    if (gameOver) {
        var lines = [];
        for (var r = 0; r < viewRows; r++) lines.push(rep(" ", width));
        // Center "GAME OVER" text
        var goText = "=== GAME OVER ===";
        var cy = Math.floor(viewRows / 2) - 2;
        var cx = Math.floor((width - goText.length) / 2);
        if (cy >= 0 && cy < viewRows) {
            var line = rep(" ", cx) + CRED + CBOLD + goText + RST;
            line += rep(" ", Math.max(0, width - cx - goText.length));
            lines[cy] = line;
        }
        // Scoreboard
        var sorted = plOrder.slice().sort(function(a, b) {
            return (pls[b] ? pls[b].score : 0) - (pls[a] ? pls[a].score : 0);
        });
        for (var i = 0; i < sorted.length && cy + 2 + i < viewRows; i++) {
            var p = pls[sorted[i]];
            if (!p) continue;
            var medal = i === 0 ? "#1" : i === 1 ? "#2" : i === 2 ? "#3" : "#" + (i + 1);
            var entry = medal + " " + p.name + ": " + p.score + " pts";
            var ex = Math.floor((width - entry.length) / 2);
            var col = i === 0 ? CYEL + CBOLD : i === 1 ? CWHT : CDIM;
            lines[cy + 2 + i] = rep(" ", Math.max(0, ex)) + col + entry + RST + rep(" ", Math.max(0, width - ex - entry.length));
        }
        // Restart hint
        var hint = "Admin: /reset to play again";
        var hy = Math.min(cy + 2 + sorted.length + 2, viewRows - 1);
        if (hy >= 0 && hy < viewRows) {
            var hx = Math.floor((width - hint.length) / 2);
            lines[hy] = rep(" ", Math.max(0, hx)) + CDIM + hint + RST + rep(" ", Math.max(0, width - hx - hint.length));
        }
        return lines.join("\n");
    }

    // Wave pause overlay
    if (wavePause > 0) {
        var lines = [];
        for (var r = 0; r < viewRows; r++) lines.push(rep(" ", width));
        var waveText = "WAVE " + wave;
        var cy = Math.floor(viewRows / 2);
        var cx = Math.floor((width - waveText.length) / 2);
        if (cy >= 0 && cy < viewRows) {
            lines[cy] = rep(" ", cx) + CYEL + CBOLD + waveText + RST + rep(" ", Math.max(0, width - cx - waveText.length));
        }
        return lines.join("\n");
    }

    // Build lines
    var lines = [];
    for (var row = 0; row < viewRows; row++) {
        var parts = [];
        var visW = 0;
        for (var col = 0; col < viewCols; col++) {
            var k = col + "," + row;
            if (ents[k]) {
                parts.push(ents[k]);
                visW += cw;
            } else {
                parts.push(emptyCell);
                visW += cw;
            }
        }
        var rpad = width - visW;
        if (rpad > 0) parts.push(rep(" ", rpad));
        lines.push(parts.join(""));
    }

    return lines.join("\n");
}

// ============================================================
// Init
// ============================================================
lastTick = Date.now();

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
        wave = 1; frame = 0; gameOver = false;
        aliens = []; alienBullets = []; playerBullets = [];
        booms = []; powerups = []; ufo = null; ufoTimer = 0;
        wavePause = 0;
        for (var i = 0; i < plOrder.length; i++) {
            var p = pls[plOrder[i]];
            if (!p) continue;
            p.score = 0; p.lives = 3;
            p.dead = false; p.invuln = frame + INVULN_TICKS;
            p.rapidFire = 0; p.shield = 0; p.cooldown = 0;
            p.placed = false;
        }
        // Will respawn wave on next tick when fieldW is set
        fieldW = 0; fieldH = 0;
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
        // Remove their bullets
        for (var i = playerBullets.length - 1; i >= 0; i--) {
            if (playerBullets[i].owner === playerID) playerBullets.splice(i, 1);
        }
        delete pls[playerID];
    },

    onInput: function(playerID, key) {
        var p = pls[playerID];
        if (!p || p.dead || gameOver) return;

        if (key === "left") {
            p.x = Math.max(0, p.x - PLAYER_SPEED);
        } else if (key === "right") {
            p.x = Math.min(fieldW > 0 ? Math.floor(fieldW / (fieldW >= 60 ? 2 : 1)) - 1 : 40, p.x + PLAYER_SPEED);
        } else if (key === " " || key === "up") {
            // Shoot
            var cd = p.rapidFire > 0 ? RAPID_COOLDOWN : FIRE_COOLDOWN;
            if (p.cooldown <= 0) {
                playerBullets.push({x: p.x, y: p.y - 1, owner: playerID});
                p.cooldown = cd;
            }
        }
    },

    view: function(playerID, width, height) {
        // Place new players
        var p = pls[playerID];
        if (p && !p.placed && height > 0) {
            p.x = Math.floor(Math.floor(width / (width >= 60 ? 2 : 1)) / 2);
            p.y = height - 1;
            p.placed = true;
        }
        return render(playerID, width, height);
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
