// wolf3d.js — Wolfenstein-3D-style team deathmatch on a 64x64 grid
// Load with: /game load wolf3d
//
// All render-relevant state lives in Game.state so that local canvas rendering
// (Pixels / Blocks-local) on the GUI client sees the same world as the server.
// The client's goja VM receives Game.state via OSC every frame; module-level
// variables would never be synced. Time uses Game.state._t (auto-injected by
// the engine after each update) because the client stubs now() to 0.

// ── Constants ────────────────────────────────────────────────────────────────

var MAP_W = 64, MAP_D = 64;
var WALL_H = 2.0;               // wall height in world units (eye stays at 0.5)
var PLAYER_HP = 3;
var KILL_LIMIT = 10;
var RESPAWN_SEC = 5.0;
var MOVE_INTERVAL = 0.12;       // seconds between grid steps while keys held / repeated
var FIRE_COOLDOWN = 0.35;
var LASER_TTL = 0.28;           // must outlive one server tick + network jitter so clients see the beam
var MAX_RAY_DIST = 40;
var PLANE_LEN = 0.66;           // ~66° FOV
var FOCAL_Y = 1.0;
var SCOREBOARD_TIMEOUT = 0.85;  // keep scoreboard visible after a tab press

var PLAYER_BOX_HALF = 0.25;     // cube half-size horizontally (0.5 total)
var PLAYER_BOX_HEIGHT = 0.8;    // world units (same as before — walls now tower over players)

var FOOTSTEP_INTERVAL = 0.35;

var TEAM_COLORS = ["#FF4444", "#44AAFF", "#44FF44", "#FFAA44", "#FF44FF", "#44FFFF"];

// 8 facing directions (45° increments). Unit vectors in world space.
// Index 0 = East (+X), rotating clockwise (increasing angle).
var FACING_ANGLE = [];
var FACING_DX = [];
var FACING_DZ = [];
(function buildFacings() {
    for (var i = 0; i < 8; i++) {
        var a = i * Math.PI / 4;
        FACING_ANGLE.push(a);
        FACING_DX.push(Math.cos(a));
        FACING_DZ.push(Math.sin(a));
    }
})();

// Integer step per facing direction (for grid movement).
var STEP_DX = [ 1,  1,  0, -1, -1, -1,  0,  1];
var STEP_DZ = [ 0,  1,  1,  1,  0, -1, -1, -1];

// ── Server-only state (not needed for rendering) ─────────────────────────────

var openCells = [];             // list of {x, z} for respawn picks
var winChecked = false;

// ── Shared state (Game.state) ────────────────────────────────────────────────

function ensureState() {
    if (!Game.state) Game.state = {};
    var s = Game.state;
    if (!s.players) s.players = {};
    if (!s.lasers) s.lasers = [];
    if (!s.teamData) s.teamData = [];
    if (!s.teamKills) s.teamKills = [];
    if (typeof s.mapGenerated !== "boolean") s.mapGenerated = false;
    if (!s.mapData) s.mapData = null;
    return s;
}

function gameTime() { return (Game.state && Game.state._t) || 0; }

// ── Map ──────────────────────────────────────────────────────────────────────

function mapIdx(x, z) { return z * MAP_W + x; }

function isSolid(x, z) {
    if (x < 0 || x >= MAP_W || z < 0 || z >= MAP_D) return true;
    var md = Game.state && Game.state.mapData;
    if (!md) return true;
    return md[mapIdx(x, z)] === 1;
}

function carveRect(md, x1, z1, x2, z2) {
    for (var z = z1; z <= z2; z++)
        for (var x = x1; x <= x2; x++)
            md[mapIdx(x, z)] = 0;
}

function generateMap() {
    var s = ensureState();
    var md = new Array(MAP_W * MAP_D);
    for (var i = 0; i < md.length; i++) md[i] = 1; // all solid

    // Carve rectangular rooms, avoiding overlap; keep 1-cell wall between rooms.
    var rooms = [];
    var attempts = 0;
    while (rooms.length < 16 && attempts < 300) {
        attempts++;
        var rw = 5 + Math.floor(Math.random() * 10);   // 5..14 wide
        var rd = 5 + Math.floor(Math.random() * 10);   // 5..14 deep
        var rx = 2 + Math.floor(Math.random() * (MAP_W - rw - 4));
        var rz = 2 + Math.floor(Math.random() * (MAP_D - rd - 4));
        var overlap = false;
        for (var k = 0; k < rooms.length; k++) {
            var r = rooms[k];
            if (rx <= r.x2 + 1 && rx + rw - 1 >= r.x1 - 1 &&
                rz <= r.z2 + 1 && rz + rd - 1 >= r.z1 - 1) {
                overlap = true; break;
            }
        }
        if (overlap) continue;
        var x2 = rx + rw - 1, z2 = rz + rd - 1;
        carveRect(md, rx, rz, x2, z2);
        rooms.push({ x1: rx, z1: rz, x2: x2, z2: z2,
                     cx: Math.floor((rx + x2) / 2), cz: Math.floor((rz + z2) / 2) });
    }

    // Connect every room to the next with L-shaped corridors (2 cells wide).
    for (var i = 1; i < rooms.length; i++) {
        var a = rooms[i - 1], b = rooms[i];
        var x1c = Math.min(a.cx, b.cx), x2c = Math.max(a.cx, b.cx);
        var z1c = Math.min(a.cz, b.cz), z2c = Math.max(a.cz, b.cz);
        carveRect(md, x1c, a.cz, x2c, a.cz + 1);
        carveRect(md, b.cx, z1c, b.cx + 1, z2c);
    }

    // Sprinkle a few extra cross-links so the layout isn't a single thread.
    for (var i = 0; i < 4; i++) {
        var a = rooms[Math.floor(Math.random() * rooms.length)];
        var b = rooms[Math.floor(Math.random() * rooms.length)];
        if (!a || !b || a === b) continue;
        var x1c = Math.min(a.cx, b.cx), x2c = Math.max(a.cx, b.cx);
        var z1c = Math.min(a.cz, b.cz), z2c = Math.max(a.cz, b.cz);
        carveRect(md, x1c, b.cz, x2c, b.cz + 1);
        carveRect(md, a.cx, z1c, a.cx + 1, z2c);
    }

    s.mapData = md;
    s.mapGenerated = true;

    // Collect every open cell for respawn picks (server-only).
    openCells = [];
    for (var z = 0; z < MAP_D; z++) {
        for (var x = 0; x < MAP_W; x++) {
            if (!isSolid(x, z)) openCells.push({ x: x, z: z });
        }
    }
    if (openCells.length === 0) {
        // Fallback: blast a small arena so the game is still playable.
        carveRect(md, 2, 2, MAP_W - 3, MAP_D - 3);
        for (var z2 = 2; z2 < MAP_D - 2; z2++)
            for (var x2 = 2; x2 < MAP_W - 2; x2++) openCells.push({ x: x2, z: z2 });
    }
}

function randomSpawnCell(awayFrom) {
    // Try up to 30 random picks for a cell not occupied by a live player.
    for (var tries = 0; tries < 30; tries++) {
        var c = openCells[Math.floor(Math.random() * openCells.length)];
        if (cellOccupied(c.x, c.z)) continue;
        if (awayFrom) {
            var dx = c.x - awayFrom.x, dz = c.z - awayFrom.z;
            if (dx * dx + dz * dz < 64) continue; // keep 8+ cells from hazard
        }
        return c;
    }
    return openCells[Math.floor(Math.random() * openCells.length)];
}

function cellOccupied(x, z) {
    var players = Game.state.players;
    for (var id in players) {
        var q = players[id];
        if (q.alive && q.gx === x && q.gz === z) return true;
    }
    return false;
}

// ── Players ──────────────────────────────────────────────────────────────────

function teamOf(p) { return Game.state.teamData[p.teamIdx]; }

function teamColor(idx) {
    var td = Game.state && Game.state.teamData;
    if (td && td[idx] && td[idx].color) return td[idx].color;
    return TEAM_COLORS[idx % TEAM_COLORS.length];
}

function refreshTeamData() {
    var s = ensureState();
    var t = teams();
    if (t && t.length) s.teamData = t;
    while (s.teamKills.length < s.teamData.length) s.teamKills.push(0);
}

function findPlayerTeam(pid) {
    var td = Game.state.teamData;
    for (var i = 0; i < td.length; i++) {
        var tp = td[i].players;
        for (var j = 0; j < tp.length; j++) {
            if (tp[j].id === pid) return { idx: i, name: tp[j].name };
        }
    }
    return null;
}

// Ensure a connected player has a live player record. Server-only — must not
// be called from the render path because it generates world state and calls
// teams(). If the player isn't on any team (single-team sandbox) they spawn
// on a synthetic team so they can still walk the map.
function ensureSpawned(pid) {
    var s = ensureState();
    if (s.players[pid]) return s.players[pid];
    refreshTeamData();
    var info = findPlayerTeam(pid);
    if (info) {
        spawnPlayer(pid, info.name, info.idx, true);
    } else {
        if (s.teamData.length === 0) {
            s.teamData = [{ name: "Solo", color: TEAM_COLORS[0], players: [] }];
            s.teamKills = [0];
        }
        spawnPlayer(pid, pid, 0, true);
    }
    return s.players[pid] || null;
}

function spawnPlayer(pid, pname, teamIdx, firstSpawn) {
    var s = ensureState();
    if (!s.mapGenerated || openCells.length === 0) {
        generateMap();
    }
    var cell = randomSpawnCell(null) || { x: Math.floor(MAP_W / 2), z: Math.floor(MAP_D / 2) };
    var facing = Math.floor(Math.random() * 8);
    if (!s.players[pid]) {
        s.players[pid] = {
            id: pid, name: pname, teamIdx: teamIdx,
            gx: cell.x, gz: cell.z, facing: facing,
            hp: PLAYER_HP, alive: true,
            fireCooldown: 0, moveCooldown: 0,
            deadUntil: 0, stepTimer: 0,
            scoreboardUntil: 0,
            kills: 0, deaths: 0
        };
    } else {
        var p = s.players[pid];
        p.gx = cell.x; p.gz = cell.z; p.facing = facing;
        p.hp = PLAYER_HP; p.alive = true;
        p.fireCooldown = 0; p.moveCooldown = 0;
        p.deadUntil = 0; p.stepTimer = 0;
    }
    if (!firstSpawn) {
        playPositionalSound(s.players[pid], 9, 60, 100, 500); // shimmer
    }
}

function respawnIfReady(p, nowSec) {
    if (p.alive) return;
    if (nowSec >= p.deadUntil) {
        var cell = randomSpawnCell(null);
        p.gx = cell.x; p.gz = cell.z;
        p.facing = Math.floor(Math.random() * 8);
        p.hp = PLAYER_HP; p.alive = true;
        p.fireCooldown = 0;
        playPositionalSound(p, 9, 72, 110, 400);
    }
}

function tryStep(p, dirIdx) {
    // dirIdx is an index into STEP_DX/STEP_DZ (0..7). Diagonal steps also
    // require at least one of the two adjacent cardinal cells to be open
    // so players can't clip through wall corners.
    var dx = STEP_DX[dirIdx], dz = STEP_DZ[dirIdx];
    var nx = p.gx + dx, nz = p.gz + dz;
    if (isSolid(nx, nz)) return false;
    if (dx !== 0 && dz !== 0) {
        if (isSolid(p.gx + dx, p.gz) && isSolid(p.gx, p.gz + dz)) return false;
    }
    if (cellOccupied(nx, nz)) return false;
    p.gx = nx; p.gz = nz;
    return true;
}

// ── Combat ───────────────────────────────────────────────────────────────────

function updateLasers(dt) {
    var lasers = Game.state.lasers;
    for (var i = lasers.length - 1; i >= 0; i--) {
        lasers[i].ttl -= dt;
        if (lasers[i].ttl <= 0) lasers.splice(i, 1);
    }
}

function fireLaser(shooter) {
    if (!shooter.alive || shooter.fireCooldown > 0) return;
    shooter.fireCooldown = FIRE_COOLDOWN;

    var eyeX = shooter.gx + 0.5, eyeZ = shooter.gz + 0.5;
    var angle = FACING_ANGLE[shooter.facing];
    var dx = Math.cos(angle), dz = Math.sin(angle);

    // DDA ray march over grid cells. Walk until a wall — or a foe whose cell
    // the ray enters — is hit.
    var mx = Math.floor(eyeX), mz = Math.floor(eyeZ);
    var stepX = dx >= 0 ? 1 : -1;
    var stepZ = dz >= 0 ? 1 : -1;
    var absDX = Math.abs(dx) < 1e-9 ? 1e9 : Math.abs(1 / dx);
    var absDZ = Math.abs(dz) < 1e-9 ? 1e9 : Math.abs(1 / dz);
    var tMaxX = dx >= 0 ? (mx + 1 - eyeX) * absDX : (eyeX - mx) * absDX;
    var tMaxZ = dz >= 0 ? (mz + 1 - eyeZ) * absDZ : (eyeZ - mz) * absDZ;

    var hitDist = MAX_RAY_DIST;
    var hitPlayer = null;
    var players = Game.state.players;

    for (var step = 0; step < MAX_RAY_DIST * 3; step++) {
        var t;
        if (tMaxX < tMaxZ) { t = tMaxX; tMaxX += absDX; mx += stepX; }
        else               { t = tMaxZ; tMaxZ += absDZ; mz += stepZ; }
        if (t > MAX_RAY_DIST) break;
        if (isSolid(mx, mz)) { hitDist = t; break; }
        for (var id in players) {
            var q = players[id];
            if (!q.alive || q === shooter) continue;
            if (q.teamIdx === shooter.teamIdx) continue; // no friendly fire
            if (q.gx === mx && q.gz === mz) {
                hitPlayer = q;
                hitDist = t;
                break;
            }
        }
        if (hitPlayer) break;
    }

    var hitX = eyeX + dx * hitDist;
    var hitZ = eyeZ + dz * hitDist;
    var color = teamColor(shooter.teamIdx);
    Game.state.lasers.push({ x1: eyeX, z1: eyeZ, x2: hitX, z2: hitZ, color: color, ttl: LASER_TTL });

    broadcastPositionalSound({ x: eyeX, z: eyeZ }, 9, 40, 120, 180);

    if (hitPlayer) {
        hitPlayer.hp--;
        broadcastPositionalSound({ x: hitX, z: hitZ }, 9, 48, 115, 200);
        if (hitPlayer.hp <= 0) {
            killPlayer(hitPlayer, shooter);
        }
    }
}

function killPlayer(victim, killer) {
    victim.alive = false;
    victim.deaths++;
    victim.deadUntil = gameTime() + RESPAWN_SEC;
    if (killer) {
        killer.kills++;
        var tk = Game.state.teamKills;
        tk[killer.teamIdx] = (tk[killer.teamIdx] || 0) + 1;
    }
    var vTeam = teamOf(victim);
    var kTeam = killer ? teamOf(killer) : null;
    var msg = victim.name + " [" + (vTeam ? vTeam.name : "?") + "]";
    if (killer && killer !== victim) {
        msg = killer.name + " [" + (kTeam ? kTeam.name : "?") + "] fragged " + msg;
    } else {
        msg = msg + " died";
    }
    chat(msg);
    broadcastPositionalSound({ x: victim.gx + 0.5, z: victim.gz + 0.5 }, 9, 28, 127, 600);
    checkWinCondition();
}

function checkWinCondition() {
    if (winChecked) return;
    var tk = Game.state.teamKills;
    var td = Game.state.teamData;
    var winner = -1;
    for (var i = 0; i < tk.length; i++) {
        if ((tk[i] || 0) >= KILL_LIMIT) { winner = i; break; }
    }
    if (winner < 0) return;
    winChecked = true;

    var results = [];
    for (var i = 0; i < td.length; i++) {
        var kills = tk[i] || 0;
        var suffix = (i === winner) ? " — WINNER" : "";
        results.push({ name: td[i].name, result: kills + " kills" + suffix,
                       _k: kills, _w: i === winner ? 1 : 0 });
    }
    results.sort(function (a, b) { return b._w - a._w || b._k - a._k; });
    gameOver(results);
}

// ── Audio (3D positional via per-player velocity) ────────────────────────────

function velocityFromDistance(d2) {
    // d2 = squared distance in cells. Falloff to 0 at ~20 cells.
    var d = Math.sqrt(d2);
    var v = Math.max(0, 1 - d / 20);
    return Math.max(1, Math.floor(v * v * 120));
}

function playPositionalSound(listener, ch, note, baseVel, durMs) {
    midiNotePlayer(listener.id, ch, note, baseVel, durMs);
}

function broadcastPositionalSound(source, ch, note, baseVel, durMs) {
    var players = Game.state.players;
    for (var id in players) {
        var q = players[id];
        var dx = (q.gx + 0.5) - source.x;
        var dz = (q.gz + 0.5) - source.z;
        var d2 = dx * dx + dz * dz;
        var vel = Math.min(127, Math.floor(baseVel * velocityFromDistance(d2) / 120));
        if (vel < 3) continue;
        midiNotePlayer(id, ch, note, vel, durMs);
    }
}

// ── Update loop (server-only) ────────────────────────────────────────────────

var Game;

function updatePlayers(dt) {
    var nowSec = gameTime();
    var players = Game.state.players;
    for (var id in players) {
        var p = players[id];
        if (p.fireCooldown > 0) p.fireCooldown -= dt;
        if (p.moveCooldown > 0) p.moveCooldown -= dt;
        if (p.stepTimer > 0) p.stepTimer -= dt;
        if (!p.alive) { respawnIfReady(p, nowSec); continue; }
    }
}

// ── Input (server-only) ──────────────────────────────────────────────────────

function handleInput(p, key) {
    if (key === "tab") {
        p.scoreboardUntil = gameTime() + SCOREBOARD_TIMEOUT;
        return;
    }
    if (!p.alive) return;

    // Turning is always allowed, even on move cooldown.
    if (key === "q") { p.facing = (p.facing + 7) % 8; return; }
    if (key === "e") { p.facing = (p.facing + 1) % 8; return; }

    if (key === " " || key === "space") { fireLaser(p); return; }

    if (p.moveCooldown > 0) return;

    // WASD relative to facing. W/S cardinal, A/D 90° from facing.
    // forward = facing; back = facing + 4; left = facing + 6; right = facing + 2.
    var moveIdx = -1;
    if (key === "w") moveIdx = p.facing;
    else if (key === "s") moveIdx = (p.facing + 4) % 8;
    else if (key === "a") moveIdx = (p.facing + 6) % 8;
    else if (key === "d") moveIdx = (p.facing + 2) % 8;
    if (moveIdx < 0) return;

    if (tryStep(p, moveIdx)) {
        p.moveCooldown = MOVE_INTERVAL;
        if (p.stepTimer <= 0) {
            broadcastPositionalSound({ x: p.gx + 0.5, z: p.gz + 0.5 }, 9, 36, 90, 90);
            p.stepTimer = FOOTSTEP_INTERVAL;
        }
    }
}

// ── Renderer ─────────────────────────────────────────────────────────────────

function hexByte(v) {
    var s = Math.max(0, Math.min(255, Math.round(v))).toString(16);
    return s.length < 2 ? "0" + s : s;
}

function rgb(r, g, b) { return "#" + hexByte(r) + hexByte(g) + hexByte(b); }

function renderScene(ctx, playerID, w, h) {
    var s = Game.state;
    // Render paths must never mutate state or call server primitives (teams,
    // now, midiNote, etc). If the player hasn't been spawned yet — either the
    // local client's state hasn't arrived, or the player has no team — show
    // the spectator screen instead of trying to conjure one up.
    if (!s || !s.mapGenerated) { renderSpectator(ctx, w, h); return; }
    var p = s.players[playerID];
    if (!p) { renderSpectator(ctx, w, h); return; }

    var eyeX = p.gx + 0.5, eyeZ = p.gz + 0.5, eyeY = 0.5;
    var yaw = FACING_ANGLE[p.facing];
    var dirX = Math.cos(yaw), dirZ = Math.sin(yaw);
    var planeX = -dirZ * PLANE_LEN, planeZ = dirX * PLANE_LEN;

    // Sky and floor.
    ctx.setFillStyle("#1A1030");
    ctx.fillRect(0, 0, w, Math.floor(h / 2));
    ctx.setFillStyle("#20181A");
    ctx.fillRect(0, Math.floor(h / 2), w, h - Math.floor(h / 2));

    // Ground gradient bands.
    var half = Math.floor(h / 2);
    for (var y = 0; y < half; y += 2) {
        var t = y / half;
        var r = 12 + Math.round(t * 28);
        var g = 10 + Math.round(t * 18);
        var b = 28 + Math.round(t * 14);
        ctx.setFillStyle(rgb(r, g, b));
        ctx.fillRect(0, y, w, 2);
        var gy = h - y - 2;
        ctx.setFillStyle(rgb(28 + Math.round(t * 12), 20 + Math.round(t * 12), 22 + Math.round(t * 8)));
        ctx.fillRect(0, gy, w, 2);
    }

    // DDA raycaster for walls; fill one vertical strip per screen column.
    var zBuffer = new Array(w);
    for (var sx = 0; sx < w; sx++) {
        var camX = 2 * sx / w - 1;
        var rayX = dirX + planeX * camX;
        var rayZ = dirZ + planeZ * camX;

        var mx = Math.floor(eyeX), mz = Math.floor(eyeZ);
        var stepX = rayX >= 0 ? 1 : -1;
        var stepZ = rayZ >= 0 ? 1 : -1;
        var absDX = Math.abs(rayX) < 1e-9 ? 1e9 : Math.abs(1 / rayX);
        var absDZ = Math.abs(rayZ) < 1e-9 ? 1e9 : Math.abs(1 / rayZ);
        var tMaxX = rayX >= 0 ? (mx + 1 - eyeX) * absDX : (eyeX - mx) * absDX;
        var tMaxZ = rayZ >= 0 ? (mz + 1 - eyeZ) * absDZ : (eyeZ - mz) * absDZ;

        var side = 0;
        var perpDist = MAX_RAY_DIST;
        for (var iter = 0; iter < MAX_RAY_DIST * 3; iter++) {
            if (tMaxX < tMaxZ) { tMaxX += absDX; mx += stepX; side = 0; }
            else               { tMaxZ += absDZ; mz += stepZ; side = 1; }
            if (isSolid(mx, mz)) {
                perpDist = side === 0
                    ? (mx - eyeX + (1 - stepX) / 2) / rayX
                    : (mz - eyeZ + (1 - stepZ) / 2) / rayZ;
                if (perpDist < 0.05) perpDist = 0.05;
                break;
            }
            if ((side === 0 ? tMaxX : tMaxZ) > MAX_RAY_DIST) break;
        }
        zBuffer[sx] = perpDist;
        if (perpDist >= MAX_RAY_DIST) continue;

        // Eye at 0.5, walls span 0..WALL_H: (WALL_H - 0.5) above the horizon,
        // 0.5 below.
        var lineScale = h * FOCAL_Y / perpDist;
        var drawStart = Math.floor(h / 2 - (WALL_H - 0.5) * lineScale);
        var drawEnd = Math.floor(h / 2 + 0.5 * lineScale);
        if (drawStart < 0) drawStart = 0;
        if (drawEnd > h) drawEnd = h;

        var shade = Math.max(0.15, 1 - perpDist / MAX_RAY_DIST);
        // Checker-tinted brick: deeper red for N/S walls, warmer sand for E/W.
        var r, g, b;
        if (side === 0) {
            r = 120 * shade; g = 60 * shade; b = 60 * shade;
        } else {
            r = 150 * shade; g = 100 * shade; b = 60 * shade;
        }
        ctx.setFillStyle(rgb(r, g, b));
        ctx.fillRect(sx, drawStart, 1, drawEnd - drawStart);
    }

    // Collect and draw player cubes back-to-front, with z-buffer clipping.
    var cubes = [];
    var players = s.players;
    for (var id in players) {
        var q = players[id];
        if (!q.alive || id === playerID) continue;
        var dxq = (q.gx + 0.5) - eyeX, dzq = (q.gz + 0.5) - eyeZ;
        cubes.push({ p: q, d2: dxq * dxq + dzq * dzq });
    }
    cubes.sort(function (a, b) { return b.d2 - a.d2; });
    for (var i = 0; i < cubes.length; i++) {
        drawPlayerCube(ctx, cubes[i].p, eyeX, eyeY, eyeZ, dirX, dirZ, planeX, planeZ, w, h, zBuffer);
    }

    // Lasers.
    var lasers = s.lasers;
    for (var li = 0; li < lasers.length; li++) {
        drawLaser(ctx, lasers[li], eyeX, eyeY, eyeZ, dirX, dirZ, planeX, planeZ, w, h);
    }

    // HUD.
    renderHUD(ctx, p, w, h);

    // Minimap in top-right.
    renderMinimap(ctx, p, w, h);

    // Scoreboard (if tab recently pressed).
    if (p.scoreboardUntil > gameTime()) {
        renderScoreboard(ctx, w, h);
    }
}

// Minimap: full top-down view of the map in the top-right corner.
// Walls are gray tiles; each player is a team-colored tile with a white "nose"
// pixel one square ahead so facing direction is legible at a glance.
var MINIMAP_FACING_OFFSET = [
    [ 1,  0],   // 0 E
    [ 1,  1],   // 1 SE
    [ 0,  1],   // 2 S
    [-1,  1],   // 3 SW
    [-1,  0],   // 4 W
    [-1, -1],   // 5 NW
    [ 0, -1],   // 6 N
    [ 1, -1]    // 7 NE
];

function renderMinimap(ctx, me, w, h) {
    var s = Game.state;
    if (!s || !s.mapData) return;

    var cellPx = 2;
    var mmW = MAP_W * cellPx;
    var mmH = MAP_D * cellPx;
    var pad = 8;
    var ox = w - mmW - pad;
    var oy = pad;

    // Background panel.
    ctx.setFillStyle("#0A0810CC");
    ctx.fillRect(ox - 2, oy - 2, mmW + 4, mmH + 4);
    ctx.setStrokeStyle("#FFFFFF66");
    ctx.setLineWidth(1);
    ctx.strokeRect(ox - 2, oy - 2, mmW + 4, mmH + 4);

    // Walls.
    ctx.setFillStyle("#6B553A");
    for (var z = 0; z < MAP_D; z++) {
        for (var x = 0; x < MAP_W; x++) {
            if (isSolid(x, z)) {
                ctx.fillRect(ox + x * cellPx, oy + z * cellPx, cellPx, cellPx);
            }
        }
    }

    // Players.
    var players = s.players;
    for (var id in players) {
        var p = players[id];
        if (!p.alive) continue;
        var px = ox + p.gx * cellPx;
        var py = oy + p.gz * cellPx;
        ctx.setFillStyle(teamColor(p.teamIdx));
        ctx.fillRect(px, py, cellPx, cellPx);

        // Nose pixel one cell ahead in the facing direction.
        var off = MINIMAP_FACING_OFFSET[p.facing];
        if (off) {
            ctx.setFillStyle(id === me.id ? "#FFFFFF" : "#FFEEAA");
            ctx.fillRect(px + off[0] * cellPx, py + off[1] * cellPx, cellPx, cellPx);
        }

        // Bright outline on self.
        if (id === me.id) {
            ctx.setStrokeStyle("#FFFFFF");
            ctx.setLineWidth(1);
            ctx.strokeRect(px - 1, py - 1, cellPx + 2, cellPx + 2);
        }
    }
}

function worldToCamera(wx, wz, eyeX, eyeZ, dirX, dirZ, planeX, planeZ) {
    // World point offset in (dir, plane) basis: [sx; sz] = [[dirX, planeX]; [dirZ, planeZ]] * [camZ; camX].
    // Inverting that matrix gives camZ (forward) and camX (right-offset).
    var sx = wx - eyeX, sz = wz - eyeZ;
    var det = dirX * planeZ - planeX * dirZ;
    if (Math.abs(det) < 1e-6) return null;
    var inv = 1 / det;
    return {
        camX: inv * (-dirZ * sx + dirX * sz),       // right
        camZ: inv * (planeZ * sx - planeX * sz)     // forward
    };
}

function projectPoint(wx, wy, wz, eyeX, eyeY, eyeZ, dirX, dirZ, planeX, planeZ, w, h) {
    var c = worldToCamera(wx, wz, eyeX, eyeZ, dirX, dirZ, planeX, planeZ);
    if (!c || c.camZ <= 0.05) return null;
    var sx = Math.floor(w / 2 * (1 + c.camX / c.camZ));
    var dy = eyeY - wy;
    var sy = Math.floor(h / 2 + dy * h * FOCAL_Y / c.camZ);
    return { sx: sx, sy: sy, camZ: c.camZ };
}

function drawPlayerCube(ctx, q, eyeX, eyeY, eyeZ, dirX, dirZ, planeX, planeZ, w, h, zBuffer) {
    var cx = q.gx + 0.5, cz = q.gz + 0.5;
    var hs = PLAYER_BOX_HALF;
    var yBot = 0, yTop = PLAYER_BOX_HEIGHT;
    var color = teamColor(q.teamIdx);

    var dxq = cx - eyeX, dzq = cz - eyeZ;
    var dist = Math.sqrt(dxq * dxq + dzq * dzq);

    // Each side face: corners (min/max in x/z) and normal.
    var faces = [
        { nx:  1, nz:  0, x0: cx + hs, z0: cz - hs, x1: cx + hs, z1: cz + hs }, // +X
        { nx: -1, nz:  0, x0: cx - hs, z0: cz + hs, x1: cx - hs, z1: cz - hs }, // -X
        { nx:  0, nz:  1, x0: cx + hs, z0: cz + hs, x1: cx - hs, z1: cz + hs }, // +Z
        { nx:  0, nz: -1, x0: cx - hs, z0: cz - hs, x1: cx + hs, z1: cz - hs }  // -Z
    ];

    for (var fi = 0; fi < faces.length; fi++) {
        var f = faces[fi];
        var fcx = (f.x0 + f.x1) / 2, fcz = (f.z0 + f.z1) / 2;
        var toEyeX = eyeX - fcx, toEyeZ = eyeZ - fcz;
        if (f.nx * toEyeX + f.nz * toEyeZ <= 0) continue;

        var c0 = projectPoint(f.x0, yBot, f.z0, eyeX, eyeY, eyeZ, dirX, dirZ, planeX, planeZ, w, h);
        var c1 = projectPoint(f.x1, yBot, f.z1, eyeX, eyeY, eyeZ, dirX, dirZ, planeX, planeZ, w, h);
        var c2 = projectPoint(f.x1, yTop, f.z1, eyeX, eyeY, eyeZ, dirX, dirZ, planeX, planeZ, w, h);
        var c3 = projectPoint(f.x0, yTop, f.z0, eyeX, eyeY, eyeZ, dirX, dirZ, planeX, planeZ, w, h);
        if (!c0 || !c1 || !c2 || !c3) continue;

        var faceCam = worldToCamera(fcx, fcz, eyeX, eyeZ, dirX, dirZ, planeX, planeZ);
        if (!faceCam) continue;
        var faceDepth = faceCam.camZ;
        if (faceDepth <= 0.05) continue;

        var xMin = Math.min(c0.sx, c1.sx, c2.sx, c3.sx);
        var xMax = Math.max(c0.sx, c1.sx, c2.sx, c3.sx);
        xMin = Math.max(0, xMin); xMax = Math.min(w - 1, xMax);
        if (xMax < xMin) continue;

        var leftSx = c0.sx, rightSx = c1.sx;
        if (leftSx === rightSx) continue;
        var leftYTop = c3.sy, rightYTop = c2.sy;
        var leftYBot = c0.sy, rightYBot = c1.sy;
        if (leftSx > rightSx) {
            var t; t = leftSx; leftSx = rightSx; rightSx = t;
            t = leftYTop; leftYTop = rightYTop; rightYTop = t;
            t = leftYBot; leftYBot = rightYBot; rightYBot = t;
        }

        var lightDot = -(f.nx * dirX + f.nz * dirZ);
        var shade = 0.55 + 0.4 * Math.max(0, lightDot);
        shade *= Math.max(0.2, 1 - dist / 25);
        var rC = parseInt(color.substr(1, 2), 16) * shade;
        var gC = parseInt(color.substr(3, 2), 16) * shade;
        var bC = parseInt(color.substr(5, 2), 16) * shade;
        var faceColor = rgb(rC, gC, bC);
        var outlineColor = rgb(rC * 0.55, gC * 0.55, bC * 0.55);

        for (var sx = xMin; sx <= xMax; sx++) {
            if (sx < 0 || sx >= w) continue;
            if (zBuffer[sx] !== undefined && zBuffer[sx] < faceDepth) continue;
            var tx = (sx - leftSx) / (rightSx - leftSx);
            var yTopS = leftYTop + (rightYTop - leftYTop) * tx;
            var yBotS = leftYBot + (rightYBot - leftYBot) * tx;
            if (yTopS > yBotS) { var tmp = yTopS; yTopS = yBotS; yBotS = tmp; }
            var py = Math.floor(yTopS);
            var hx = Math.floor(yBotS) - py;
            if (py < 0) { hx += py; py = 0; }
            if (py + hx > h) hx = h - py;
            if (hx <= 0) continue;
            ctx.setFillStyle(faceColor);
            ctx.fillRect(sx, py, 1, hx);
            if (sx === xMin || sx === xMax) {
                ctx.setFillStyle(outlineColor);
                ctx.fillRect(sx, py, 1, hx);
            }
        }
    }

    // HP pip trio floating above cube.
    var head = projectPoint(cx, yTop + 0.15, cz, eyeX, eyeY, eyeZ, dirX, dirZ, planeX, planeZ, w, h);
    if (head && head.camZ < 20) {
        var pipSize = Math.max(2, Math.floor(6 / head.camZ));
        for (var i = 0; i < PLAYER_HP; i++) {
            var col = i < q.hp ? "#FF4444" : "#333344";
            ctx.setFillStyle(col);
            ctx.fillRect(head.sx - pipSize * 2 + i * (pipSize + 1), head.sy, pipSize, pipSize);
        }
    }
}

function drawLaser(ctx, l, eyeX, eyeY, eyeZ, dirX, dirZ, planeX, planeZ, w, h) {
    var a = projectPoint(l.x1, eyeY, l.z1, eyeX, eyeY, eyeZ, dirX, dirZ, planeX, planeZ, w, h);
    var b = projectPoint(l.x2, eyeY, l.z2, eyeX, eyeY, eyeZ, dirX, dirZ, planeX, planeZ, w, h);
    if (!a && !b) return;
    var ax = a ? a.sx : w / 2, ay = a ? a.sy : h - 10;
    var bx = b ? b.sx : w / 2, by = b ? b.sy : h / 2;
    var tFade = Math.max(0, Math.min(1, l.ttl / LASER_TTL));
    var pulse = 0.4 + 0.6 * tFade;
    var passes = [
        { w: 7, a: Math.round(36 * pulse) },
        { w: 3, a: Math.round(120 * pulse) },
        { w: 1, a: Math.round(255 * pulse) }
    ];
    for (var i = 0; i < passes.length; i++) {
        var alpha = hexByte(passes[i].a);
        var stroke = (i === 2 ? "#FFFFFF" : l.color) + alpha;
        ctx.setStrokeStyle(stroke);
        ctx.setLineWidth(passes[i].w);
        ctx.beginPath();
        ctx.moveTo(ax, ay);
        ctx.lineTo(bx, by);
        ctx.stroke();
    }
}

function renderHUD(ctx, p, w, h) {
    var cx = Math.floor(w / 2), cy = Math.floor(h / 2);
    ctx.setFillStyle("#FFFFFFBB");
    ctx.fillRect(cx - 5, cy, 4, 1);
    ctx.fillRect(cx + 2, cy, 4, 1);
    ctx.fillRect(cx, cy - 5, 1, 4);
    ctx.fillRect(cx, cy + 2, 1, 4);

    for (var i = 0; i < PLAYER_HP; i++) {
        var hx = 10 + i * 16, hy = h - 18;
        var col = i < p.hp ? "#FF4455" : "#332233";
        ctx.setFillStyle(col);
        ctx.fillCircle(hx, hy, 5);
        ctx.fillCircle(hx + 4, hy, 5);
        ctx.setFillStyle(col);
        ctx.beginPath();
        ctx.moveTo(hx - 5, hy + 1);
        ctx.lineTo(hx + 9, hy + 1);
        ctx.lineTo(hx + 2, hy + 9);
        ctx.closePath();
        ctx.fill();
    }

    var tc = teamColor(p.teamIdx);
    ctx.setFillStyle(tc);
    ctx.fillRect(w - 80, h - 20, 10, 10);
    ctx.setFillStyle("#FFFFFFDD");
    ctx.fillText("K:" + p.kills + " D:" + p.deaths, w - 65, h - 10);

    if (!p.alive) {
        ctx.setFillStyle("#330000A0");
        ctx.fillRect(0, 0, w, h);
        ctx.setFillStyle("#FF4444");
        var sec = Math.max(0, Math.ceil(p.deadUntil - gameTime()));
        ctx.fillText("YOU DIED — respawning in " + sec + "s", w / 2 - 90, h / 2);
    }
}

function renderScoreboard(ctx, w, h) {
    var td = Game.state.teamData;
    var rowH = 18;
    var rows = td.length;
    var boxW = Math.min(420, w - 40);
    var boxH = rowH * (rows + 2) + 20;
    var bx = Math.floor((w - boxW) / 2), by = 40;
    ctx.setFillStyle("#000000D0");
    ctx.fillRect(bx, by, boxW, boxH);
    ctx.setStrokeStyle("#FFFFFF80");
    ctx.setLineWidth(1);
    ctx.strokeRect(bx, by, boxW, boxH);
    ctx.setFillStyle("#FFFFFF");
    ctx.fillText("SCOREBOARD  (first to " + KILL_LIMIT + " kills wins)", bx + 14, by + 18);
    ctx.fillText("Team", bx + 14, by + 18 + rowH);
    ctx.fillText("Kills", bx + 200, by + 18 + rowH);
    ctx.fillText("Deaths", bx + 260, by + 18 + rowH);
    ctx.fillText("K/D", bx + 340, by + 18 + rowH);
    var players = Game.state.players;
    for (var i = 0; i < rows; i++) {
        var t = td[i];
        var k = 0, d = 0;
        for (var j = 0; j < t.players.length; j++) {
            var q = players[t.players[j].id];
            if (q) { k += q.kills; d += q.deaths; }
        }
        var kd = d === 0 ? (k === 0 ? "0.00" : "∞") : (k / d).toFixed(2);
        var y = by + 18 + rowH * (2 + i);
        ctx.setFillStyle(teamColor(i));
        ctx.fillRect(bx + 14, y - 10, 10, 10);
        ctx.setFillStyle("#FFFFFF");
        ctx.fillText(t.name, bx + 30, y);
        ctx.fillText("" + k, bx + 200, y);
        ctx.fillText("" + d, bx + 260, y);
        ctx.fillText(kd, bx + 340, y);
    }
}

function renderSpectator(ctx, w, h) {
    ctx.setFillStyle("#000000");
    ctx.fillRect(0, 0, w, h);
    ctx.setFillStyle("#888888");
    ctx.fillText("Spectating", w / 2 - 25, h / 2);
}

// ── ASCII fallback: top-down mini-map ────────────────────────────────────────

function renderAsciiMap(buf, playerID, ox, oy, width, height) {
    var s = Game.state;
    buf.fill(ox, oy, width, height, " ", null, "#08080F");
    if (!s || !s.mapGenerated) return;
    var scaleX = width / MAP_W, scaleZ = height / MAP_D;
    for (var z = 0; z < MAP_D; z++) {
        for (var x = 0; x < MAP_W; x++) {
            var bx = ox + Math.floor(x * scaleX);
            var by = oy + Math.floor(z * scaleZ);
            if (bx >= ox + width || by >= oy + height) continue;
            if (isSolid(x, z)) buf.setChar(bx, by, "█", "#443322", "#08080F");
        }
    }
    var players = s.players;
    var me = players[playerID];
    for (var id in players) {
        var p = players[id];
        if (!p.alive) continue;
        var px = ox + Math.floor(p.gx * scaleX);
        var pz = oy + Math.floor(p.gz * scaleZ);
        if (px < ox || px >= ox + width || pz < oy || pz >= oy + height) continue;
        var tc = teamColor(p.teamIdx);
        var ch = id === playerID ? "@" : "o";
        if (id !== playerID) {
            var arrows = [">", "↘", "v", "↙", "<", "↖", "^", "↗"];
            ch = arrows[p.facing];
        }
        buf.setChar(px, pz, ch, tc, null);
    }
    buf.writeString(ox + 1, oy, "wolf3d (ASCII fallback — use canvas/GUI client)", "#888899", null);
    if (me) {
        buf.writeString(ox + 1, oy + height - 1,
            "HP " + me.hp + "/" + PLAYER_HP + "  K " + me.kills + "  D " + me.deaths,
            "#DDDDEE", null);
    }
}

// ── Game object ──────────────────────────────────────────────────────────────

Game = {
    gameName: "wolf3d",
    teamRange: { min: 1, max: 6 },

    // load() runs on both server and client. On the client, Game.state will
    // be replaced by a SetState call immediately after; we only need to make
    // sure our module-local helpers won't touch state before then. The server
    // gets its real map in begin().
    load: function (savedState) {
        ensureState();
        openCells = [];
        winChecked = false;
    },

    begin: function () {
        var s = ensureState();
        s.players = {};
        s.lasers = [];
        s.teamData = teams() || [];
        s.teamKills = [];
        for (var i = 0; i < s.teamData.length; i++) s.teamKills.push(0);
        winChecked = false;
        generateMap();
        var spawned = 0;
        for (var ti = 0; ti < s.teamData.length; ti++) {
            var tps = s.teamData[ti].players || [];
            for (var j = 0; j < tps.length; j++) {
                var tp = tps[j];
                spawnPlayer(tp.id, tp.name, ti, true);
                spawned++;
            }
        }
        log("wolf3d begin: teams=" + s.teamData.length + " players_spawned=" + spawned);
        chat("[wolf3d] ready — teams: " + s.teamData.length + ", players spawned: " + spawned);
        // MIDI setup: channel 9 is the GM drum channel on most SoundFonts.
        midiProgram(9, 0);
    },

    onPlayerJoin: function (playerID, playerName) {
        refreshTeamData();
        if (!ensureSpawned(playerID)) {
            log("wolf3d: onPlayerJoin " + playerID + " has no team; skipping spawn");
        }
    },

    update: function (dt) {
        updatePlayers(dt);
        updateLasers(dt);
    },

    onInput: function (playerID, key) {
        var p = Game.state.players[playerID] || ensureSpawned(playerID);
        if (!p) return;
        handleInput(p, key);
    },

    onPlayerLeave: function (playerID) {
        delete Game.state.players[playerID];
        checkWinCondition();
    },

    renderCanvas: function (ctx, playerID, w, h) {
        var s = Game.state;
        if (!s || !s.mapGenerated) {
            ctx.setFillStyle("#000");
            ctx.fillRect(0, 0, w, h);
            return;
        }
        renderScene(ctx, playerID, w, h);
    },

    renderAscii: function (buf, playerID, ox, oy, width, height) {
        renderAsciiMap(buf, playerID, ox, oy, width, height);
    },

    statusBar: function (playerID) {
        var s = Game.state;
        if (!s) return "wolf3d";
        var p = s.players[playerID];
        if (!p) return "wolf3d";
        var td = s.teamData;
        var tk = s.teamKills;
        var tn = (td[p.teamIdx] && td[p.teamIdx].name) || "?";
        var scores = [];
        for (var i = 0; i < td.length; i++) {
            scores.push(td[i].name + ":" + (tk[i] || 0));
        }
        var state = p.alive ? ("HP " + p.hp + "/" + PLAYER_HP) : "DEAD";
        return "wolf3d | " + tn + " | " + state + " | K/D " + p.kills + "/" + p.deaths +
               " | " + scores.join("  ") + " | first to " + KILL_LIMIT;
    },

    commandBar: function (playerID) {
        return "[WASD] Move  [Q/E] Turn 45°  [Space] Fire  [Tab] Scores";
    }
};

// Initialize Game.state early so module-level helpers can touch it safely.
ensureState();
