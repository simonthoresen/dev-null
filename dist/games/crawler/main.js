// main.js — First-person dungeon crawler for null-space
// 3D ASCII raycasting with NC inventory panel

include("maze");
include("render3d");
include("items");

// ─── Game State ────────────────────────────────────────────────────────────

var MAZE_W = 20;
var MAZE_H = 20;
var TURN_SPEED = Math.PI / 4; // 45 degrees per key press
var MOVE_SPEED = 1.0;


function createPlayer(id, name) {
    var spawn = findSpawn(Game.state.maze);
    return {
        id: id,
        name: name,
        x: spawn.x + 0.5,
        y: spawn.y + 0.5,
        angle: 0,
        hp: 100,
        maxHp: 100,
        atk: 5,
        def: 2,
        xp: 0,
        level: 1,
        gold: 0,
        dead: false,
        // Equipment slots
        slots: {
            head: null,
            chest: null,
            legs: null,
            feet: null,
            mainHand: null,
            offHand: null,
            ring: null,
            amulet: null
        },
        backpack: [], // unequipped items
        kills: 0
    };
}

function addMsg(playerID, text) {
    if (!Game.state.messages[playerID]) Game.state.messages[playerID] = [];
    Game.state.messages[playerID].push(text);
    if (Game.state.messages[playerID].length > 50) {
        Game.state.messages[playerID] = Game.state.messages[playerID].slice(-50);
    }
}

function playerTotalAtk(p) {
    var total = p.atk;
    if (p.slots.mainHand) total += p.slots.mainHand.atk || 0;
    if (p.slots.ring) total += p.slots.ring.atk || 0;
    return total;
}

function playerTotalDef(p) {
    var total = p.def;
    for (var slot in p.slots) {
        if (p.slots[slot] && p.slots[slot].def) {
            total += p.slots[slot].def;
        }
    }
    return total;
}

// ─── Game Object ───────────────────────────────────────────────────────────

var Game = {
    gameName: "Crawler",

    state: {
        players: {},   // playerID -> player object
        maze: null,
        items: [],     // items on the ground: { x, y, item }
        monsters: [],  // { x, y, hp, maxHp, name, atk, ch, color, loot }
        tick: 0,
        floor: 1,
        messages: {}   // playerID -> [strings]
    },

    init: function(savedState) {
        Game.state.maze = generateMaze(MAZE_W, MAZE_H);
        Game.state.items = scatterItems(Game.state.maze, Game.state.floor);
        Game.state.monsters = spawnMonsters(Game.state.maze, Game.state.floor);
    },

    start: function() {
        var t = teams();
        for (var i = 0; i < t.length; i++) {
            for (var j = 0; j < t[i].players.length; j++) {
                var p = t[i].players[j];
                Game.state.players[p.id] = createPlayer(p.id, p.name);
                addMsg(p.id, "Welcome to the dungeon, " + p.name + "!");
                addMsg(p.id, "WASD to move, Q/E to turn.");
                addMsg(p.id, "Walk into monsters to attack.");
            }
        }
    },

    onPlayerLeave: function(playerID) {},

    onInput: function(playerID, key) {
        var p = Game.state.players[playerID];
        if (!p || p.dead) {
            if (p && p.dead && key === 'r') {
                // Respawn
                var spawn = findSpawn(Game.state.maze);
                p.x = spawn.x + 0.5;
                p.y = spawn.y + 0.5;
                p.angle = 0;
                p.hp = p.maxHp;
                p.dead = false;
                p.gold = Math.floor(p.gold / 2);
                addMsg(playerID, "You respawn... half your gold is lost.");
            }
            return;
        }

        var dx = 0, dy = 0;
        var cos = Math.cos(p.angle);
        var sin = Math.sin(p.angle);

        switch (key) {
            case 'w': case 'W':
                dx = cos * MOVE_SPEED;
                dy = sin * MOVE_SPEED;
                break;
            case 's': case 'S':
                dx = -cos * MOVE_SPEED;
                dy = -sin * MOVE_SPEED;
                break;
            case 'a': case 'A':
                dx = sin * MOVE_SPEED;
                dy = -cos * MOVE_SPEED;
                break;
            case 'd': case 'D':
                dx = -sin * MOVE_SPEED;
                dy = cos * MOVE_SPEED;
                break;
            case 'q': case 'Q':
                p.angle -= TURN_SPEED;
                return;
            case 'e': case 'E':
                p.angle += TURN_SPEED;
                return;
            case 'g': case 'G':
                pickupItem(p);
                return;
            case '1': case '2': case '3': case '4':
            case '5': case '6': case '7': case '8':
                equipFromBackpack(p, parseInt(key) - 1);
                return;
            case 'u': case 'U':
                useItem(p);
                return;
            case 'x': case 'X':
                dropItem(p);
                return;
            default:
                return;
        }

        if (dx === 0 && dy === 0) return;

        var newX = p.x + dx;
        var newY = p.y + dy;
        var cellX = Math.floor(newX);
        var cellY = Math.floor(newY);

        // Check for monster collision (attack)
        for (var m = 0; m < Game.state.monsters.length; m++) {
            var mon = Game.state.monsters[m];
            if (mon.hp > 0 && mon.x === cellX && mon.y === cellY) {
                attackMonster(p, mon, m);
                return;
            }
        }

        // Wall collision with margin
        var margin = 0.2;
        if (canWalk(Game.state.maze, newX, newY, margin)) {
            p.x = newX;
            p.y = newY;

            // Check stairs
            var cx = Math.floor(p.x);
            var cy = Math.floor(p.y);
            if (Game.state.maze.grid[cy][cx] === TILE_STAIRS) {
                descendFloor();
            }
        }
    },

    update: function(dt) {
        Game.state.tick++;

        // Monster AI - move toward nearby players every 5 ticks
        if (Game.state.tick % 5 === 0) {
            for (var m = 0; m < Game.state.monsters.length; m++) {
                var mon = Game.state.monsters[m];
                if (mon.hp <= 0) continue;
                monsterAI(mon);
            }
        }
    },

    render: function(buf, playerID, ox, oy, w, h) {
        var p = Game.state.players[playerID];
        if (!p) return;
        if (p.dead) {
            renderDeathScreen(buf, p, ox, oy, w, h);
            return;
        }
        render3D(buf, p, Game.state.maze, Game.state.monsters, Game.state.items, ox, oy, w, h);
    },

    layout: function(playerID, width, height) {
        var p = Game.state.players[playerID];
        if (!p) return null;

        var invChildren = [];
        var slotNames = [
            { key: 'head', label: 'Head' },
            { key: 'chest', label: 'Chest' },
            { key: 'legs', label: 'Legs' },
            { key: 'feet', label: 'Feet' },
            { key: 'mainHand', label: 'Weapon' },
            { key: 'offHand', label: 'Off Hand' },
            { key: 'ring', label: 'Ring' },
            { key: 'amulet', label: 'Amulet' }
        ];

        for (var i = 0; i < slotNames.length; i++) {
            var s = slotNames[i];
            var item = p.slots[s.key];
            var text = s.label + ': ';
            if (item) {
                text += item.name;
            } else {
                text += '(empty)';
            }
            invChildren.push({ type: 'label', text: text, height: 1 });
        }

        invChildren.push({ type: 'divider' });

        // Backpack header
        invChildren.push({ type: 'label', text: 'Backpack (' + p.backpack.length + ')', height: 1 });
        invChildren.push({ type: 'divider' });

        // Backpack items as textview
        var bpLines = [];
        if (p.backpack.length === 0) {
            bpLines.push('  (empty)');
        } else {
            for (var i = 0; i < p.backpack.length; i++) {
                var it = p.backpack[i];
                bpLines.push(' ' + (i + 1) + ') ' + it.name);
            }
        }
        invChildren.push({ type: 'textview', lines: bpLines, weight: 1 });

        invChildren.push({ type: 'divider' });

        // Stats
        invChildren.push({ type: 'label', text: 'ATK: ' + playerTotalAtk(p) + '  DEF: ' + playerTotalDef(p), height: 1 });
        invChildren.push({ type: 'label', text: 'Gold: ' + p.gold + '  Lv: ' + p.level, height: 1 });

        // Messages as textview at bottom of game panel
        var msgs = Game.state.messages[playerID] || [];
        var recentMsgs = msgs.slice(-20);

        return {
            type: 'hsplit',
            children: [
                {
                    type: 'vsplit', weight: 1,
                    children: [
                        { type: 'gameview', weight: 1, IsFocusable: true },
                        {
                            type: 'panel', title: 'Log', height: 6,
                            children: [
                                { type: 'textview', lines: recentMsgs, weight: 1 }
                            ]
                        }
                    ]
                },
                {
                    type: 'panel', title: 'Inventory', width: 28,
                    children: invChildren
                }
            ]
        };
    },

    statusBar: function(playerID) {
        var p = Game.state.players[playerID];
        if (!p) return "Crawler - Spectating";
        if (p.dead) return "DEAD - Press [r] to respawn";
        return 'HP:' + p.hp + '/' + p.maxHp +
               '  Floor:' + Game.state.floor +
               '  Kills:' + p.kills +
               '  XP:' + p.xp;
    },

    commandBar: function(playerID) {
        var p = Game.state.players[playerID];
        if (!p) return '';
        if (p.dead) return '[r] Respawn';
        return '[WASD] Move  [QE] Turn  [G] Grab  [1-8] Equip  [U] Use  [X] Drop';
    }
};

// ─── Combat ────────────────────────────────────────────────────────────────

function attackMonster(p, mon, idx) {
    var atk = playerTotalAtk(p);
    var dmg = Math.max(1, atk - Math.floor(mon.def || 0));
    dmg += Math.floor(Math.random() * 3);
    mon.hp -= dmg;
    addMsg(p.id, "You hit " + mon.name + " for " + dmg + " damage!");

    if (mon.hp <= 0) {
        addMsg(p.id, mon.name + " is defeated!");
        p.kills++;
        p.xp += mon.xpReward || 5;

        // Drop loot
        if (mon.loot) {
            var drop = rollLoot(mon.loot, Game.state.floor);
            if (drop) {
                Game.state.items.push({ x: mon.x, y: mon.y, item: drop });
                addMsg(p.id, mon.name + " dropped " + drop.name + "!");
            }
        }

        // Gold drop
        var goldDrop = Math.floor(Math.random() * 10 * Game.state.floor) + 1;
        p.gold += goldDrop;
        addMsg(p.id, "You find " + goldDrop + " gold.");

        // Level up check
        var xpNeeded = p.level * 20;
        if (p.xp >= xpNeeded) {
            p.level++;
            p.xp -= xpNeeded;
            p.maxHp += 10;
            p.hp = p.maxHp;
            p.atk += 1;
            p.def += 1;
            addMsg(p.id, "LEVEL UP! You are now level " + p.level + "!");
            chat(p.name + " reached level " + p.level + "!");
        }

        Game.state.monsters.splice(idx, 1);
    }
}

function monsterAI(mon) {
    // Find nearest player
    var nearest = null;
    var nearDist = 999;
    for (var pid in Game.state.players) {
        var p = Game.state.players[pid];
        if (p.dead) continue;
        var dx = p.x - (mon.x + 0.5);
        var dy = p.y - (mon.y + 0.5);
        var dist = Math.sqrt(dx * dx + dy * dy);
        if (dist < nearDist && dist < 6) {
            nearDist = dist;
            nearest = p;
        }
    }
    if (!nearest) return;

    // If adjacent, attack
    var px = Math.floor(nearest.x);
    var py = Math.floor(nearest.y);
    if (Math.abs(mon.x - px) + Math.abs(mon.y - py) <= 1) {
        var def = playerTotalDef(nearest);
        var dmg = Math.max(1, (mon.atk || 3) - Math.floor(def / 2));
        dmg += Math.floor(Math.random() * 2);
        nearest.hp -= dmg;
        addMsg(nearest.id, mon.name + " hits you for " + dmg + "!");
        if (nearest.hp <= 0) {
            nearest.hp = 0;
            nearest.dead = true;
            addMsg(nearest.id, "You have been slain by " + mon.name + "!");
            chat(nearest.name + " was slain by " + mon.name + "!");
        }
        return;
    }

    // Move toward player
    var dx = 0, dy = 0;
    if (Math.abs(nearest.x - (mon.x + 0.5)) > Math.abs(nearest.y - (mon.y + 0.5))) {
        dx = nearest.x > mon.x + 0.5 ? 1 : -1;
    } else {
        dy = nearest.y > mon.y + 0.5 ? 1 : -1;
    }

    var nx = mon.x + dx;
    var ny = mon.y + dy;
    if (nx >= 0 && nx < Game.state.maze.w && ny >= 0 && ny < Game.state.maze.h &&
        Game.state.maze.grid[ny][nx] !== TILE_WALL) {
        // Check no other monster there
        var blocked = false;
        for (var m = 0; m < Game.state.monsters.length; m++) {
            if (Game.state.monsters[m] !== mon && Game.state.monsters[m].hp > 0 &&
                Game.state.monsters[m].x === nx && Game.state.monsters[m].y === ny) {
                blocked = true;
                break;
            }
        }
        if (!blocked) {
            mon.x = nx;
            mon.y = ny;
        }
    }
}

// ─── Inventory Actions ─────────────────────────────────────────────────────

function pickupItem(p) {
    var cx = Math.floor(p.x);
    var cy = Math.floor(p.y);
    for (var i = Game.state.items.length - 1; i >= 0; i--) {
        if (Game.state.items[i].x === cx && Game.state.items[i].y === cy) {
            var it = Game.state.items[i].item;
            p.backpack.push(it);
            Game.state.items.splice(i, 1);
            addMsg(p.id, "Picked up " + it.name + ".");
            return;
        }
    }
    addMsg(p.id, "Nothing to pick up here.");
}

function equipFromBackpack(p, idx) {
    if (idx < 0 || idx >= p.backpack.length) return;
    var it = p.backpack[idx];
    if (!it.slot) {
        addMsg(p.id, it.name + " cannot be equipped.");
        return;
    }
    // Swap with current
    var old = p.slots[it.slot];
    p.slots[it.slot] = it;
    p.backpack.splice(idx, 1);
    if (old) {
        p.backpack.push(old);
        addMsg(p.id, "Swapped " + old.name + " for " + it.name + ".");
    } else {
        addMsg(p.id, "Equipped " + it.name + ".");
    }
}

function useItem(p) {
    // Use first consumable in backpack
    for (var i = 0; i < p.backpack.length; i++) {
        var it = p.backpack[i];
        if (it.type === 'consumable') {
            if (it.heal) {
                p.hp = Math.min(p.maxHp, p.hp + it.heal);
                addMsg(p.id, "Used " + it.name + ". Healed " + it.heal + " HP.");
            }
            p.backpack.splice(i, 1);
            return;
        }
    }
    addMsg(p.id, "No consumables in backpack.");
}

function dropItem(p) {
    if (p.backpack.length === 0) {
        addMsg(p.id, "Backpack is empty.");
        return;
    }
    var it = p.backpack.pop();
    Game.state.items.push({ x: Math.floor(p.x), y: Math.floor(p.y), item: it });
    addMsg(p.id, "Dropped " + it.name + ".");
}

// ─── Floor Transition ──────────────────────────────────────────────────────

function descendFloor() {
    Game.state.floor++;
    Game.state.maze = generateMaze(MAZE_W, MAZE_H);
    Game.state.items = scatterItems(Game.state.maze, Game.state.floor);
    Game.state.monsters = spawnMonsters(Game.state.maze, Game.state.floor);

    for (var pid in Game.state.players) {
        var p = Game.state.players[pid];
        if (!p.dead) {
            var spawn = findSpawn(Game.state.maze);
            p.x = spawn.x + 0.5;
            p.y = spawn.y + 0.5;
            addMsg(p.id, "You descend to floor " + Game.state.floor + ".");
        }
    }
    chat("The party descends to floor " + Game.state.floor + "!");
}

function renderDeathScreen(buf, p, ox, oy, w, h) {
    var lines = [
        "YOU DIED",
        "",
        p.name + " - Level " + p.level,
        "Floor " + Game.state.floor,
        "Kills: " + p.kills,
        "Gold: " + p.gold,
        "",
        "Press [r] to respawn"
    ];
    for (var i = 0; i < lines.length; i++) {
        var row = Math.floor(h / 2) - Math.floor(lines.length / 2) + i;
        if (row >= 0 && row < h) {
            var col = Math.floor((w - lines[i].length) / 2);
            if (col < 0) col = 0;
            var color = (i === 0) ? "#ff4444" : "#aaaaaa";
            buf.writeString(ox + col, oy + row, lines[i], color, null);
        }
    }
}
