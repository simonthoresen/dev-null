// quake.js — Neon Grid-Stalker: team-elimination FPS on a 64×64×8 voxel grid
// Load with: /game load quake

// ── Constants ────────────────────────────────────────────────────────────────

var MAP_W = 64, MAP_H = 8, MAP_D = 64;
var PLAYER_HP      = 5;
var MAX_RENDER_DIST = 20;
var PLANE_LEN      = 0.66;   // camera plane length → ~66° horizontal FOV
var FOCAL_Y        = 1.0;    // vertical projection: h*FOCAL_Y/dist per world unit
var LASER_TTL      = 0.15;
var FIRE_COOLDOWN  = 0.25;
var GRAVITY_INTERVAL = 0.22;
var MAX_PARTICLES  = 600;
var PARTICLE_GRAVITY = 14.0;

var TEAM_COLORS  = ["#FF4444","#44FF44","#4488FF","#FFFF44","#FF44FF","#44FFFF"];
var NEON_COLORS  = ["#FF4444","#44FF88","#44FFFF","#FFFF44","#FF44FF","#FF8800","#00FFFF","#FF00AA"];

// Facing direction → world delta (X=east, Z=south)
var FACING_DX = [1,  0, -1,  0];
var FACING_DZ = [0,  1,  0, -1];

// ── Map ───────────────────────────────────────────────────────────────────────

var mapData = null;
var mapGenerated = false;
var spawnPoints = [];

function mapIdx(x, y, z) { return y * MAP_W * MAP_D + z * MAP_W + x; }

function isSolid(x, y, z) {
    if (x < 0 || x >= MAP_W || y < 0 || y >= MAP_H || z < 0 || z >= MAP_D) return true;
    return mapData[mapIdx(x, y, z)] === 1;
}

function carve(x1, y1, z1, x2, y2, z2) {
    for (var y = y1; y <= y2; y++)
    for (var z = z1; z <= z2; z++)
    for (var x = x1; x <= x2; x++)
        mapData[mapIdx(x, y, z)] = 0;
}

function generateMap() {
    mapData = new Uint8Array(MAP_W * MAP_H * MAP_D);
    // Fill solid
    for (var i = 0; i < mapData.length; i++) mapData[i] = 1;

    var rooms = [];
    var attempts = 0;
    while (rooms.length < 14 && attempts < 200) {
        attempts++;
        var rw = 6 + Math.floor(Math.random() * 9);   // 6–14 wide
        var rd = 6 + Math.floor(Math.random() * 9);   // 6–14 deep
        var rh = 2 + Math.floor(Math.random() * 2);   // 2–3 tall
        var rx = 2 + Math.floor(Math.random() * (MAP_W - rw - 4));
        var rz = 2 + Math.floor(Math.random() * (MAP_D - rd - 4));
        var ry = 1;

        // Check overlap (leave 2-cell gap between rooms)
        var overlap = false;
        for (var k = 0; k < rooms.length; k++) {
            var r = rooms[k];
            if (rx <= r.x2 + 2 && rx + rw >= r.x1 - 2 &&
                rz <= r.z2 + 2 && rz + rd >= r.z1 - 2) { overlap = true; break; }
        }
        if (overlap) continue;

        var x2 = rx + rw - 1, z2 = rz + rd - 1, y2 = ry + rh - 1;
        carve(rx, ry, rz, x2, y2, z2);
        rooms.push({x1:rx, y1:ry, z1:rz, x2:x2, y2:y2, z2:z2,
                    cx: Math.floor((rx + x2)/2), cz: Math.floor((rz + z2)/2)});
    }

    // Connect consecutive rooms with L-shaped corridors (2 cells tall)
    for (var i = 1; i < rooms.length; i++) {
        var a = rooms[i-1], b = rooms[i];
        // Horizontal segment first
        var x1c = Math.min(a.cx, b.cx), x2c = Math.max(a.cx, b.cx);
        carve(x1c, 1, a.cz, x2c, 2, a.cz);
        // Vertical segment
        var z1c = Math.min(a.cz, b.cz), z2c = Math.max(a.cz, b.cz);
        carve(b.cx, 1, z1c, b.cx, 2, z2c);
    }

    // A few elevated platforms connected by 1-cell ramps
    for (var i = 0; i < 4 && i < rooms.length; i++) {
        var r = rooms[Math.floor(i * rooms.length / 4)];
        var px = r.cx, pz = r.cz;
        if (!isSolid(px, 3, pz)) continue;
        carve(px-1, 3, pz-1, px+1, 4, pz+1);
        // Ramp: clear cell at y=2 adjacent to platform so player can step up
        if (!isSolid(px+2, 1, pz)) carve(px+2, 2, pz, px+2, 2, pz);
    }

    // Spawn points: one per room corner area, spread across map
    var spawns = [];
    var stride = Math.max(1, Math.floor(rooms.length / 8));
    for (var i = 0; i < rooms.length; i += stride) {
        var r = rooms[i];
        // Place spawn 1 cell inside room at floor level
        spawns.push({x: r.x1 + 1, y: r.y1, z: r.z1 + 1});
        if (spawns.length >= 8) break;
    }
    // Fallback if map generation produced too few rooms
    if (spawns.length === 0) spawns.push({x:2, y:1, z:2});
    return spawns;
}

// ── Players ──────────────────────────────────────────────────────────────────

var players = {};   // id → player
var teamData = [];  // [{name, color, players:[{id,name},...]}]

function spawnPlayer(pid, pname, teamIdx) {
    var sp = spawnPoints[teamIdx % spawnPoints.length] || {x:2,y:1,z:2};
    // Jitter spawn so teammates don't stack
    var jx = sp.x + Math.floor(Math.random()*3);
    var jz = sp.z + Math.floor(Math.random()*3);
    if (isSolid(jx, sp.y, jz)) { jx = sp.x; jz = sp.z; }
    players[pid] = {
        id: pid, name: pname,
        gx: jx, gy: sp.y, gz: jz,
        facing: teamIdx % 4,
        hp: PLAYER_HP, teamIdx: teamIdx,
        alive: true,
        fireCooldown: 0, gravTimer: 0,
        deathParticleSpawned: false,
        kills: 0, deaths: 0
    };
}

function tryMove(p, dx, dy, dz) {
    var nx = p.gx + dx, ny = p.gy + dy, nz = p.gz + dz;
    if (!isSolid(nx, ny, nz)) {
        p.gx = nx; p.gy = ny; p.gz = nz;
    }
}

function tryJump(p) {
    if (isSolid(p.gx, p.gy - 1, p.gz) && !isSolid(p.gx, p.gy + 1, p.gz)) {
        p.gy++;
        p.gravTimer = -0.15; // small upward delay before gravity kicks back in
    }
}

function updatePlayers(dt) {
    for (var id in players) {
        var p = players[id];
        if (!p.alive) continue;
        if (p.fireCooldown > 0) p.fireCooldown -= dt;
        p.gravTimer += dt;
        if (p.gravTimer >= GRAVITY_INTERVAL) {
            p.gravTimer = 0;
            if (!isSolid(p.gx, p.gy - 1, p.gz)) p.gy--;
        }
    }
}

// ── Particles ─────────────────────────────────────────────────────────────────

var particles = [];

function addParticles(count, wx, wy, wz, speed, colors, upward) {
    for (var i = 0; i < count; i++) {
        if (particles.length >= MAX_PARTICLES) break;
        var vx = (Math.random() - 0.5) * speed;
        var vy = upward ? Math.random() * speed : (Math.random() - 0.5) * speed;
        var vz = (Math.random() - 0.5) * speed;
        particles.push({
            wx: wx, wy: wy, wz: wz,
            vx: vx, vy: vy, vz: vz,
            color: colors[Math.floor(Math.random() * colors.length)],
            life: 0.4 + Math.random() * 0.8
        });
    }
}

function updateParticles(dt) {
    for (var i = particles.length - 1; i >= 0; i--) {
        var p = particles[i];
        p.life -= dt;
        if (p.life <= 0) { particles.splice(i, 1); continue; }
        p.vy -= PARTICLE_GRAVITY * dt;
        p.wx += p.vx * dt;
        p.wy += p.vy * dt;
        p.wz += p.vz * dt;
        if (isSolid(Math.floor(p.wx), Math.floor(p.wy), Math.floor(p.wz))) {
            particles.splice(i, 1);
        }
    }
}

// ── Combat & Lasers ───────────────────────────────────────────────────────────

var lasers = [];
var winChecked = false;

function updateLasers(dt) {
    for (var i = lasers.length - 1; i >= 0; i--) {
        lasers[i].ttl -= dt;
        if (lasers[i].ttl <= 0) lasers.splice(i, 1);
    }
}

function fireLaser(playerID) {
    var p = players[playerID];
    if (!p || !p.alive || p.fireCooldown > 0) return;
    p.fireCooldown = FIRE_COOLDOWN;

    var eyeX = p.gx + 0.5, eyeY = p.gy + 0.5, eyeZ = p.gz + 0.5;
    var dx = FACING_DX[p.facing], dz = FACING_DZ[p.facing];
    var tColor = (teamData[p.teamIdx] && teamData[p.teamIdx].color) || TEAM_COLORS[p.teamIdx % TEAM_COLORS.length];

    // March ray in XZ plane, check player hits along the way
    var cx = Math.floor(eyeX), cz = Math.floor(eyeZ);
    var stepX = dx > 0 ? 1 : (dx < 0 ? -1 : 0);
    var stepZ = dz > 0 ? 1 : (dz < 0 ? -1 : 0);
    var tDeltaX = stepX !== 0 ? Math.abs(1 / dx) : Infinity;
    var tDeltaZ = stepZ !== 0 ? Math.abs(1 / dz) : Infinity;
    var tMaxX = stepX > 0 ? (cx + 1 - eyeX) * tDeltaX : (eyeX - cx) * tDeltaX;
    var tMaxZ = stepZ > 0 ? (cz + 1 - eyeZ) * tDeltaZ : (eyeZ - cz) * tDeltaZ;

    var dist = 0;
    var hitX = eyeX + dx * MAX_RENDER_DIST, hitY = eyeY, hitZ = eyeZ + dz * MAX_RENDER_DIST;
    var hitEnemy = false;

    for (var step = 0; step < MAX_RENDER_DIST * 2; step++) {
        if (tMaxX < tMaxZ) {
            dist = tMaxX; tMaxX += tDeltaX; cx += stepX;
        } else {
            dist = tMaxZ; tMaxZ += tDeltaZ; cz += stepZ;
        }
        if (dist > MAX_RENDER_DIST) break;
        if (isSolid(cx, Math.floor(eyeY), cz)) {
            hitX = eyeX + dx * dist; hitZ = eyeZ + dz * dist;
            break;
        }
        // Check if any enemy player center is near this ray point
        for (var eid in players) {
            if (eid === playerID) continue;
            var ep = players[eid];
            if (!ep.alive) continue;
            if (ep.teamIdx === p.teamIdx) continue; // no friendly fire
            // Point-on-ray distance: project enemy onto ray, check lateral offset
            var relX = ep.gx + 0.5 - eyeX, relZ = ep.gz + 0.5 - eyeZ;
            var proj = relX * dx + relZ * dz; // dot product along ray
            if (proj < 0 || proj > MAX_RENDER_DIST) continue;
            var latX = relX - dx * proj, latZ = relZ - dz * proj;
            var lat2 = latX*latX + latZ*latZ;
            var dy = (ep.gy + 0.5) - eyeY;
            if (lat2 < 0.36 && dy*dy < 1.5) { // within 0.6 radius and ~1 cell tall
                hitX = eyeX + dx * proj; hitY = ep.gy + 0.5; hitZ = eyeZ + dz * proj;
                hitEnemy = true;
                ep.hp--;
                var hc = (teamData[ep.teamIdx] && teamData[ep.teamIdx].color) || TEAM_COLORS[ep.teamIdx % TEAM_COLORS.length];
                addParticles(15, hitX, hitY, hitZ, 6, NEON_COLORS, true);
                if (ep.hp <= 0) {
                    ep.alive = false;
                    ep.deaths++;
                    p.kills++;
                    addParticles(60, ep.gx+0.5, ep.gy+0.5, ep.gz+0.5, 10, NEON_COLORS, false);
                    chat(ep.name + " was fragged by " + p.name + "!");
                }
                break;
            }
        }
        if (hitEnemy) break;
    }

    lasers.push({x1:eyeX, y1:eyeY, z1:eyeZ, x2:hitX, y2:hitY, z2:hitZ, color:tColor, ttl:LASER_TTL});
    checkWinCondition();
}

function checkWinCondition() {
    if (winChecked) return;
    var aliveTeams = [], deadTeams = [];
    for (var ti = 0; ti < teamData.length; ti++) {
        var team = teamData[ti];
        var anyAlive = false;
        for (var pi = 0; pi < team.players.length; pi++) {
            var ep = players[team.players[pi].id];
            if (ep && ep.alive) { anyAlive = true; break; }
        }
        if (anyAlive) aliveTeams.push(ti);
        else deadTeams.push(ti);
    }
    if (aliveTeams.length <= 1 && teamData.length >= 2) {
        winChecked = true;
        var results = [];
        for (var ti = 0; ti < teamData.length; ti++) {
            var kills = 0;
            for (var pi = 0; pi < teamData[ti].players.length; pi++) {
                var ep = players[teamData[ti].players[pi].id];
                if (ep) kills += ep.kills;
            }
            var isWinner = aliveTeams.indexOf(ti) >= 0;
            results.push({name: teamData[ti].name, result: (isWinner ? "WINNER - " : "") + kills + " kills", _kills: kills, _w: isWinner ? 1 : 0});
        }
        results.sort(function(a,b){ return b._w - a._w || b._kills - a._kills; });
        gameOver(results);
    }
}

// ── Renderer ──────────────────────────────────────────────────────────────────

function hexByte(v) {
    var s = Math.max(0, Math.min(255, Math.round(v))).toString(16);
    return s.length < 2 ? "0" + s : s;
}

function drawGlow(ctx, x, y, radius, color) {
    for (var i = 5; i >= 1; i--) {
        var r = radius * (i / 5);
        var alpha = Math.round((1 - i / 5) * 80 + 8);
        ctx.setFillStyle(color + hexByte(alpha));
        ctx.fillCircle(x, y, r);
    }
}

function projectSprite(eyeX, eyeY, eyeZ, dirX, dirZ, planeX, planeZ, wx, wy, wz, w, h) {
    var sx = wx - eyeX, sz = wz - eyeZ;
    // inv det of camera matrix
    var det = dirX * planeZ - planeX * dirZ;
    if (Math.abs(det) < 0.0001) return null;
    var invDet = 1.0 / det;
    var camX = invDet * (planeZ * sx - planeX * sz);
    var camZ = invDet * (-dirZ * sx + dirX * sz); // true depth
    if (camZ <= 0.1) return null;
    var screenX = Math.floor(w / 2 * (1 + camX / camZ));
    var dy = eyeY - wy;
    var screenY = Math.floor(h / 2 + dy * h * FOCAL_Y / camZ);
    return {screenX: screenX, screenY: screenY, camZ: camZ};
}

function renderWalls(ctx, eyeX, eyeY, eyeZ, dirX, dirZ, planeX, planeZ, w, h, zBuffer) {
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

        var nearestDist = MAX_RENDER_DIST;
        var filledRows = 0; // bitmask not feasible for large h; use count heuristic

        for (var iter = 0; iter < MAX_RENDER_DIST * 3 && nearestDist >= MAX_RENDER_DIST; iter++) {
            var side;
            if (tMaxX < tMaxZ) {
                tMaxX += absDX; mx += stepX; side = 0;
            } else {
                tMaxZ += absDZ; mz += stepZ; side = 1;
            }

            var perpDist = side === 0
                ? (mx - eyeX + (1 - stepX) / 2) / rayX
                : (mz - eyeZ + (1 - stepZ) / 2) / rayZ;
            if (perpDist < 0.05) perpDist = 0.05;
            if (perpDist > MAX_RENDER_DIST) break;

            // Draw all solid voxels in this XZ column
            var hadSolid = false;
            for (var vy = 0; vy < MAP_H; vy++) {
                if (!isSolid(mx, vy, mz)) continue;
                hadSolid = true;
                var worldBot = vy, worldTop = vy + 1;
                var sTop = Math.floor(h / 2 - (worldTop - eyeY) * h * FOCAL_Y / perpDist);
                var sBot = Math.floor(h / 2 - (worldBot - eyeY) * h * FOCAL_Y / perpDist);
                if (sTop >= h || sBot <= 0) continue;
                sTop = sTop < 0 ? 0 : sTop;
                sBot = sBot >= h ? h - 1 : sBot;
                if (sBot < sTop) continue;

                // Shading: deep blue-grey, distance falloff, side tint
                var shade = Math.max(0, 1 - perpDist / MAX_RENDER_DIST);
                var base = 20 + Math.round(shade * 60);
                var rf = side === 0 ? Math.round(base * 0.6) : Math.round(base * 0.45);
                var gf = side === 0 ? Math.round(base * 0.8) : Math.round(base * 0.65);
                var bf = side === 0 ? Math.round(base * 1.5) : Math.round(base * 1.2);

                // Top face of voxel (floor of next cell) slightly brighter
                if (vy === 0 || !isSolid(mx, vy - 1, mz)) {
                    // This voxel's top face is a floor/platform — tint it slightly cyan
                    rf = Math.round(rf * 0.8);
                    gf = Math.round(gf * 1.0);
                    bf = Math.round(bf * 1.2);
                }

                ctx.setFillStyle("#" + hexByte(rf) + hexByte(gf) + hexByte(bf));
                ctx.fillRect(sx, sTop, 1, sBot - sTop + 1);
            }
            if (hadSolid && nearestDist >= MAX_RENDER_DIST) {
                nearestDist = perpDist;
            }
        }
        zBuffer[sx] = nearestDist < MAX_RENDER_DIST ? nearestDist : MAX_RENDER_DIST;
    }
}

function renderFrame(ctx, playerID, w, h) {
    var p = players[playerID];
    if (!p) { renderSpectator(ctx, w, h); return; }

    var eyeX = p.gx + 0.5, eyeY = p.gy + 0.5, eyeZ = p.gz + 0.5;
    var yaw  = p.facing * (Math.PI / 2);
    var dirX = Math.cos(yaw), dirZ = Math.sin(yaw);
    var planeX = -dirZ * PLANE_LEN, planeZ = dirX * PLANE_LEN;

    // Background: upper half = void blue, lower half = floor dark
    ctx.setFillStyle("#020208");
    ctx.fillRect(0, 0, w, Math.ceil(h / 2));
    ctx.setFillStyle("#05050F");
    ctx.fillRect(0, Math.floor(h / 2), w, h - Math.floor(h / 2));

    // Z-buffer per column
    var zBuffer = [];
    for (var i = 0; i < w; i++) zBuffer.push(MAX_RENDER_DIST);

    renderWalls(ctx, eyeX, eyeY, eyeZ, dirX, dirZ, planeX, planeZ, w, h, zBuffer);

    // Collect sprites (other players + particles) sorted back-to-front
    var sprites = [];
    for (var sid in players) {
        if (sid === playerID) continue;
        var sp = players[sid];
        if (!sp.alive) continue;
        var tColor = (teamData[sp.teamIdx] && teamData[sp.teamIdx].color) || TEAM_COLORS[sp.teamIdx % TEAM_COLORS.length];
        sprites.push({type:"player", wx:sp.gx+0.5, wy:sp.gy+0.5, wz:sp.gz+0.5, color:tColor});
    }
    for (var pi = 0; pi < particles.length; pi++) {
        var pt = particles[pi];
        sprites.push({type:"particle", wx:pt.wx, wy:pt.wy, wz:pt.wz, color:pt.color});
    }

    // Sort sprites back-to-front
    for (var si = 0; si < sprites.length; si++) {
        var s = sprites[si];
        s._d2 = (s.wx-eyeX)*(s.wx-eyeX) + (s.wz-eyeZ)*(s.wz-eyeZ);
    }
    sprites.sort(function(a, b){ return b._d2 - a._d2; });

    for (var si = 0; si < sprites.length; si++) {
        var s = sprites[si];
        var proj = projectSprite(eyeX, eyeY, eyeZ, dirX, dirZ, planeX, planeZ, s.wx, s.wy, s.wz, w, h);
        if (!proj) continue;
        var sx = proj.screenX, sy = proj.screenY, camZ = proj.camZ;
        if (sx < -w || sx > 2*w) continue;
        if (camZ > zBuffer[Math.max(0, Math.min(w-1, sx))]) continue; // behind wall

        if (s.type === "player") {
            var radius = Math.max(3, Math.floor(h * FOCAL_Y / camZ / 2));
            drawGlow(ctx, sx, sy, radius * 1.8, s.color);
            ctx.setFillStyle(s.color);
            ctx.fillCircle(sx, sy, radius);
            // Slight highlight
            ctx.setFillStyle(s.color + "88");
            ctx.fillCircle(sx - radius*0.25, sy - radius*0.25, radius * 0.45);
        } else {
            // Particle
            var pr = Math.max(1, Math.floor(2 / camZ));
            ctx.setFillStyle(s.color);
            ctx.fillRect(sx - pr, sy - pr, pr*2, pr*2);
        }
    }

    // Lasers
    for (var li = 0; li < lasers.length; li++) {
        var l = lasers[li];
        var p1 = projectSprite(eyeX, eyeY, eyeZ, dirX, dirZ, planeX, planeZ, l.x1, l.y1, l.z1, w, h);
        var p2 = projectSprite(eyeX, eyeY, eyeZ, dirX, dirZ, planeX, planeZ, l.x2, l.y2, l.z2, w, h);
        // If both behind, skip; if only one behind, extend to screen edge
        if (!p1 && !p2) continue;
        // Compute screen-space endpoints (approximation: project valid ones only)
        var sx1, sy1, sx2, sy2;
        if (p1) { sx1 = p1.screenX; sy1 = p1.screenY; }
        else { sx1 = p2 ? p2.screenX : w/2; sy1 = h/2; }
        if (p2) { sx2 = p2.screenX; sy2 = p2.screenY; }
        else { sx2 = sx1; sy2 = sy1; }

        // Glow bloom: 3 passes
        var passes = [{w:5, a:"22"}, {w:2, a:"66"}, {w:1, a:"CC"}];
        for (var pp = 0; pp < passes.length; pp++) {
            ctx.setStrokeStyle(pp === 2 ? "#FFFFFF" + passes[pp].a : l.color + passes[pp].a);
            ctx.setLineWidth(passes[pp].w);
            ctx.beginPath();
            ctx.moveTo(sx1, sy1);
            ctx.lineTo(sx2, sy2);
            ctx.stroke();
        }
    }

    // HUD
    renderHUD(ctx, p, w, h);
}

function renderHUD(ctx, p, w, h) {
    // Crosshair
    var cx = Math.floor(w/2), cy = Math.floor(h/2);
    ctx.setFillStyle("#FFFFFF99");
    ctx.fillRect(cx - 5, cy - 1, 4, 2);
    ctx.fillRect(cx + 2, cy - 1, 4, 2);
    ctx.fillRect(cx - 1, cy - 5, 2, 4);
    ctx.fillRect(cx - 1, cy + 2, 2, 4);

    // Health pips bottom-left
    for (var i = 0; i < PLAYER_HP; i++) {
        var filled = i < p.hp;
        var hx = 6 + i * 12, hy = h - 16;
        var pipColor = filled ? (p.hp <= 2 ? "#FF4444" : p.hp <= 4 ? "#FFFF44" : "#44FF44") : "#333355";
        ctx.setFillStyle(pipColor);
        ctx.fillCircle(hx, hy, 4);
    }

    // Team/kill counter top-left
    var tColor = (teamData[p.teamIdx] && teamData[p.teamIdx].color) || "#FFFFFF";
    ctx.setFillStyle(tColor + "CC");
    ctx.fillRect(4, 4, 6, 6);
    ctx.setFillStyle("#AAAACC");
    ctx.fillText("K:" + p.kills, 14, 14);

    // Mini-map top-right corner (32×32 px)
    var mmSize = 32;
    var mmX = w - mmSize - 4, mmY = 4;
    ctx.setFillStyle("#00001188");
    ctx.fillRect(mmX, mmY, mmSize, mmSize);
    // Draw visible map cells as dots at 0.5px/cell scale
    var mmScale = mmSize / MAP_W;
    // Just show player positions and walls near player
    ctx.setFillStyle("#222244");
    ctx.fillRect(mmX, mmY, mmSize, mmSize);

    // All players
    for (var id in players) {
        var mp = players[id];
        if (!mp.alive) continue;
        var mpc = (teamData[mp.teamIdx] && teamData[mp.teamIdx].color) || TEAM_COLORS[mp.teamIdx % TEAM_COLORS.length];
        var mpx = mmX + Math.floor(mp.gx * mmScale);
        var mpy = mmY + Math.floor(mp.gz * mmScale);
        ctx.setFillStyle(mpc);
        ctx.fillRect(mpx - 1, mpy - 1, 2 + (id === p.id ? 1 : 0), 2 + (id === p.id ? 1 : 0));
    }
}

function renderSpectator(ctx, w, h) {
    ctx.setFillStyle("#020208");
    ctx.fillRect(0, 0, w, h);
    ctx.setFillStyle("#FF4444");
    ctx.fillText("DEAD", w/2 - 12, h/2);
}

// ── Game Object ───────────────────────────────────────────────────────────────

var Game = {
    gameName: "Neon Grid-Stalker",
    teamRange: { min: 2, max: 6 },

    state: {},

    load: function(savedState) {
        if (!mapGenerated) {
            spawnPoints = generateMap();
            mapGenerated = true;
        }
        players = {};
        lasers = [];
        particles = [];
        winChecked = false;
    },

    begin: function() {
        players = {};
        lasers = [];
        particles = [];
        winChecked = false;
        teamData = teams();
        for (var i = 0; i < teamData.length; i++) {
            for (var j = 0; j < teamData[i].players.length; j++) {
                var tp = teamData[i].players[j];
                spawnPlayer(tp.id, tp.name, i);
            }
        }
    },

    update: function(dt) {
        updatePlayers(dt);
        updateParticles(dt);
        updateLasers(dt);
    },

    onInput: function(playerID, key) {
        var p = players[playerID];
        if (!p || !p.alive) return;
        var dx = FACING_DX[p.facing], dz = FACING_DZ[p.facing];
        switch (key) {
            case "w": case "up":    tryMove(p,  dx, 0,  dz); break;
            case "s": case "down":  tryMove(p, -dx, 0, -dz); break;
            case "a":               tryMove(p,  dz, 0, -dx); break;  // strafe left
            case "d":               tryMove(p, -dz, 0,  dx); break;  // strafe right
            case "q": case "left":  p.facing = (p.facing + 3) % 4; break;
            case "e": case "right": p.facing = (p.facing + 1) % 4; break;
            case " ":               tryJump(p); break;
            case "f":               fireLaser(playerID); break;
        }
    },

    onPlayerLeave: function(playerID) {
        delete players[playerID];
        checkWinCondition();
    },

    renderCanvas: function(ctx, playerID, w, h) {
        if (!mapGenerated) {
            ctx.setFillStyle("#020208");
            ctx.fillRect(0, 0, w, h);
            return;
        }
        if (!players[playerID] || !players[playerID].alive) {
            renderSpectator(ctx, w, h);
        } else {
            renderFrame(ctx, playerID, w, h);
        }
    },

    renderAscii: function(buf, playerID, ox, oy, w, h) {
        buf.fill(ox, oy, w, h, " ", "#333355", "#000011");
        // Top-down minimap: scale map to viewport
        var scaleX = w / MAP_W, scaleZ = h / MAP_D;
        for (var z = 0; z < MAP_D; z += 2) {
            for (var x = 0; x < MAP_W; x += 2) {
                var bx = ox + Math.floor(x * scaleX);
                var by = oy + Math.floor(z * scaleZ);
                if (bx >= ox + w || by >= oy + h) continue;
                // Check floor level (y=1)
                if (isSolid(x, 1, z)) {
                    buf.setChar(bx, by, "\u2588", "#224466", "#000011");
                } else {
                    buf.setChar(bx, by, ".", "#333355", "#000011");
                }
            }
        }
        // Player positions
        for (var id in players) {
            var p = players[id];
            if (!p.alive) continue;
            var px = ox + Math.floor(p.gx * scaleX);
            var pz = oy + Math.floor(p.gz * scaleZ);
            if (px >= ox && px < ox+w && pz >= oy && pz < oy+h) {
                var tColor = (teamData[p.teamIdx] && teamData[p.teamIdx].color) || TEAM_COLORS[p.teamIdx % TEAM_COLORS.length];
                var dirs = [">","v","<","^"];
                var isLocal = (id === playerID);
                buf.setChar(px, pz, isLocal ? "@" : dirs[p.facing], tColor, null);
            }
        }
        buf.writeString(ox, oy, "Neon Grid-Stalker [SSH mode - canvas recommended]", "#666688", null);
    },

    statusBar: function(playerID) {
        var p = players[playerID];
        if (!p) return "Neon Grid-Stalker";
        if (!p.alive) return "Neon Grid-Stalker | DEAD";
        var tname = (teamData[p.teamIdx] && teamData[p.teamIdx].name) || ("Team " + (p.teamIdx+1));
        var dirs = ["E","S","W","N"];
        return "Neon Grid-Stalker | " + tname + " | HP: " + p.hp + "/" + PLAYER_HP + " | K/D: " + p.kills + "/" + p.deaths + " | Facing: " + dirs[p.facing];
    },

    commandBar: function(playerID) {
        return "[WASD] Move  [Q/E] Turn  [Space] Jump  [F] Fire";
    }
};
