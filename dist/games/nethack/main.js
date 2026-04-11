// main.js — NetHack multiplayer roguelike for dev-null
// All players share the same dungeon and can cooperate or compete.

include("data");
include("dungeon");
include("fov");
include("entities");
include("ui");

// ─── Game State ────────────────────────────────────────────────────────────

var MAX_DEPTH = 10;
var LEVEL_WIDTH = 60;
var LEVEL_HEIGHT = 30;
var MONSTER_INTERVAL = 0.3; // seconds between monster updates
var HUNGER_INTERVAL = 5.0;  // seconds between hunger ticks


// ─── Level Management ──────────────────────────────────────────────────────

function getOrCreateLevel(depth) {
    if (!Game.state.levels[depth]) {
        Game.state.levels[depth] = generateLevel(LEVEL_WIDTH, LEVEL_HEIGHT, depth);
    }
    return Game.state.levels[depth];
}

function movePlayerToDepth(player, depth) {
    var level = getOrCreateLevel(depth);
    player.depth = depth;

    // Place at stairs (up if descending, down if ascending)
    if (depth > player.depth) {
        player.x = level.stairsUp.x;
        player.y = level.stairsUp.y;
    } else {
        player.x = level.stairsDown.x;
        player.y = level.stairsDown.y;
    }

    // Reset explored map for new level
    player.explored = createExploredMap(level.width, level.height);
    addMessage(player, 'You enter depth ' + depth + '.');
}

// ─── The Game Object ───────────────────────────────────────────────────────

var Game = {
    gameName: "NetHack",

    state: {
        players: {},        // playerID -> player object
        levels: {},         // depth -> level object
        monsterTimer: 0,    // accumulator for monster update interval
        hungerTimer: 0,     // accumulator for hunger interval
        monsterStep: 0,     // counts monster update steps for per-monster speed
        inventoryOpen: {},  // playerID -> boolean
        highScores: []      // persistent high scores
    },

    splashScreen: "",

    load: function(savedState) {
        if (savedState && savedState.highScores) {
            Game.state.highScores = savedState.highScores;
        }

        var t = teams();
        var playerCount = 0;
        for (var i = 0; i < t.length; i++) {
            playerCount += t[i].players.length;
        }

        var splash = figlet("NetHack", "standard");
        splash += "\n\nA multiplayer roguelike dungeon crawl";
        splash += "\nReach depth " + MAX_DEPTH + " — or die trying.";
        splash += "\n\nPlayers: " + playerCount;
        if (Game.state.highScores.length > 0) {
            splash += "\n\n--- Hall of Fame ---";
            for (var i = 0; i < Math.min(5, Game.state.highScores.length); i++) {
                var hs = Game.state.highScores[i];
                splash += "\n  " + hs.name + " - Depth " + hs.depth + " - " + hs.gold + " gold - " + hs.kills + " kills";
            }
        }
        Game.splashScreen = splash;
    },

    begin: function() {
        // Generate first level
        getOrCreateLevel(1);

        // Create players from teams
        var t = teams();
        for (var i = 0; i < t.length; i++) {
            for (var j = 0; j < t[i].players.length; j++) {
                var p = t[i].players[j];
                spawnPlayer(p.id, p.name);
            }
        }

        log("NetHack started with " + Object.keys(Game.state.players).length + " players");
    },

    onPlayerLeave: function(playerID) {
        // Keep player in game state for potential reconnection
        var player = Game.state.players[playerID];
        if (player) {
            addMessage(player, 'You feel disconnected from reality...');
        }
    },

    onInput: function(playerID, key) {
        var player = Game.state.players[playerID];
        if (!player) return;

        // Dead player input
        if (player.dead) {
            if (key === 'r') {
                respawnPlayer(player);
            }
            return;
        }

        // Inventory mode
        if (Game.state.inventoryOpen[playerID]) {
            handleInventoryInput(player, key);
            return;
        }

        // Normal input
        var dx = 0, dy = 0;
        switch (key) {
            case 'up':    dy = -1; break;
            case 'down':  dy = 1;  break;
            case 'left':  dx = -1; break;
            case 'right': dx = 1;  break;
            case 'g':     handlePickup(player); return;
            case ',':     handlePickup(player); return;
            case 'i':     Game.state.inventoryOpen[playerID] = true; return;
            case '>':     handleDescend(player); return;
            case '<':     handleAscend(player); return;
            case '.':     player.turnCount++; return; // wait
            default:      return;
        }

        if (dx !== 0 || dy !== 0) {
            handleMove(player, dx, dy);
        }
    },

    update: function(dt) {
        // Update monsters on a fixed interval
        Game.state.monsterTimer += dt;
        while (Game.state.monsterTimer >= MONSTER_INTERVAL) {
            Game.state.monsterTimer -= MONSTER_INTERVAL;
            Game.state.monsterStep++;
            for (var d in Game.state.levels) {
                updateMonsters(Game.state.levels[d], Game.state.players, Game.state.monsterStep);
            }
        }

        // Hunger system
        Game.state.hungerTimer += dt;
        while (Game.state.hungerTimer >= HUNGER_INTERVAL) {
            Game.state.hungerTimer -= HUNGER_INTERVAL;
            for (var pid in Game.state.players) {
                var p = Game.state.players[pid];
                if (!p.dead) {
                    p.hunger--;
                    if (p.hunger <= 0) {
                        p.hp -= 1;
                        if (p.hunger % 5 === 0 || p.hp <= 5) {
                            addMessage(p, 'You are starving!');
                        }
                        if (p.hp <= 0) {
                            p.dead = true;
                            addMessage(p, 'You starved to death!');
                            recordDeath(p);
                        }
                    }
                }
            }
        }
    },

    renderAscii: function(buf, playerID, ox, oy, width, height) {
        var player = Game.state.players[playerID];
        if (!player) {
            renderSpectatorView(buf, width, height);
        } else if (Game.state.inventoryOpen[playerID]) {
            renderInventory(buf, player, width, height);
        } else {
            var level = getOrCreateLevel(player.depth);
            renderView(buf, player, level, Game.state.players, width, height);
        }
    },

    statusBar: function(playerID) {
        var player = Game.state.players[playerID];
        if (!player) return "NetHack - Spectating";
        return renderStatusBar(player);
    },

    commandBar: function(playerID) {
        var player = Game.state.players[playerID];
        if (!player) return "[Enter] Chat";
        return renderCommandBar(player, Game.state.inventoryOpen[playerID]);
    }
};

// ─── Game Logic Helpers ────────────────────────────────────────────────────

function spawnPlayer(id, name) {
    var level = getOrCreateLevel(1);
    var spawn = findSpawnPoint(level);
    var player = createPlayer(id, name, spawn.x, spawn.y);
    player.explored = createExploredMap(level.width, level.height);
    Game.state.players[id] = player;
    addMessage(player, 'Welcome to the dungeon, ' + name + '!');
    chat(name + ' enters the dungeon.');
}

function respawnPlayer(player) {
    recordDeath(player);

    // Reset player stats
    var level = getOrCreateLevel(1);
    var spawn = findSpawnPoint(level);
    player.x = spawn.x;
    player.y = spawn.y;
    player.hp = 20;
    player.maxHp = 20;
    player.atk = 3;
    player.def = 1;
    player.level = 1;
    player.xp = 0;
    player.xpToLevel = 20;
    player.gold = 0;
    player.hunger = 500;
    player.depth = 1;
    player.weapon = null;
    player.armor = null;
    player.inventory = [];
    player.dead = false;
    player.messages = [];
    player.kills = 0;
    player.turnCount = 0;
    player.explored = createExploredMap(level.width, level.height);
    addMessage(player, 'You return to the dungeon...');
    chat(player.name + ' respawns at depth 1.');
}

function recordDeath(player) {
    var score = {
        name: player.name,
        depth: player.depth,
        gold: player.gold,
        kills: player.kills,
        level: player.level
    };

    Game.state.highScores.push(score);
    Game.state.highScores.sort(function(a, b) {
        if (b.depth !== a.depth) return b.depth - a.depth;
        if (b.gold !== a.gold) return b.gold - a.gold;
        return b.kills - a.kills;
    });
    if (Game.state.highScores.length > 20) {
        Game.state.highScores = Game.state.highScores.slice(0, 20);
    }
}

function handleMove(player, dx, dy) {
    var level = getOrCreateLevel(player.depth);
    var nx = player.x + dx;
    var ny = player.y + dy;

    if (nx < 0 || nx >= level.width || ny < 0 || ny >= level.height) return;

    // Check for monster at target
    var mon = monsterAt(level, nx, ny);
    if (mon) {
        playerAttackMonster(player, mon);
        player.turnCount++;
        return;
    }

    // Check walkability
    var tile = level.grid[ny][nx];
    if (!isWalkable(tile) && tile !== TILES.DOOR_CLOSED) return;

    // Open closed doors
    if (tile === TILES.DOOR_CLOSED) {
        level.grid[ny][nx] = TILES.DOOR_OPEN;
        addMessage(player, 'You open the door.');
        player.turnCount++;
        return;
    }

    player.x = nx;
    player.y = ny;
    player.turnCount++;

    // Check traps
    checkTraps(player, level);

    // Auto-pickup gold
    for (var i = level.items.length - 1; i >= 0; i--) {
        var item = level.items[i];
        if (item.x === player.x && item.y === player.y && item.category === 'gold') {
            player.gold += item.def.value;
            addMessage(player, 'You pick up ' + item.def.value + ' gold.');
            level.items.splice(i, 1);
        }
    }
}

function handlePickup(player) {
    var level = getOrCreateLevel(player.depth);
    pickupItem(player, level);
    player.turnCount++;
}

function handleDescend(player) {
    var level = getOrCreateLevel(player.depth);
    if (level.grid[player.y][player.x] !== TILES.STAIRS_DOWN) {
        addMessage(player, 'There are no stairs going down here.');
        return;
    }
    var newDepth = player.depth + 1;
    if (newDepth > MAX_DEPTH) {
        // Victory!
        addMessage(player, 'You have conquered the dungeon!');
        recordDeath(player); // record as a high score
        chat(player.name + ' has conquered all ' + MAX_DEPTH + ' depths!');

        // Build results
        var results = [];
        for (var pid in Game.state.players) {
            var p = Game.state.players[pid];
            results.push({
                name: p.name,
                result: 'Depth ' + p.depth + ' | ' + p.gold + ' gold | ' + p.kills + ' kills'
            });
        }
        results.sort(function(a, b) { return 0; }); // keep order
        gameOver(results, { highScores: Game.state.highScores });
        return;
    }

    var newLevel = getOrCreateLevel(newDepth);
    player.depth = newDepth;
    player.x = newLevel.stairsUp.x;
    player.y = newLevel.stairsUp.y;
    player.explored = createExploredMap(newLevel.width, newLevel.height);
    addMessage(player, 'You descend to depth ' + newDepth + '.');
    chat(player.name + ' descends to depth ' + newDepth + '.');
}

function handleAscend(player) {
    var level = getOrCreateLevel(player.depth);
    if (level.grid[player.y][player.x] !== TILES.STAIRS_UP) {
        addMessage(player, 'There are no stairs going up here.');
        return;
    }
    if (player.depth <= 1) {
        addMessage(player, 'You cannot leave the dungeon!');
        return;
    }
    var newDepth = player.depth - 1;
    var newLevel = getOrCreateLevel(newDepth);
    player.depth = newDepth;
    player.x = newLevel.stairsDown.x;
    player.y = newLevel.stairsDown.y;
    player.explored = createExploredMap(newLevel.width, newLevel.height);
    addMessage(player, 'You ascend to depth ' + newDepth + '.');
}

function handleInventoryInput(player, key) {
    if (key === 'esc') {
        Game.state.inventoryOpen[player.id] = false;
        return;
    }

    // a-o = use item 0-14
    var code = key.charCodeAt(0);
    if (key.length === 1 && code >= 97 && code <= 111) { // a-o
        var index = code - 97;
        if (index < player.inventory.length) {
            var item = player.inventory[index];
            // Handle scrolls specially (need level access)
            if (item.category === 'scrolls') {
                handleScroll(player, item, index);
            } else {
                useItem(player, index);
            }
            Game.state.inventoryOpen[player.id] = false;
        }
    }
}

function handleScroll(player, item, index) {
    var level = getOrCreateLevel(player.depth);
    switch (item.def.effect) {
        case 'teleport':
            var spawn = findSpawnPoint(level);
            player.x = spawn.x;
            player.y = spawn.y;
            addMessage(player, 'You read the scroll. The world shifts around you!');
            break;
        case 'map':
            // Reveal entire level
            for (var y = 0; y < level.height; y++) {
                for (var x = 0; x < level.width; x++) {
                    if (level.grid[y][x] !== TILES.VOID) {
                        player.explored[y][x] = true;
                    }
                }
            }
            addMessage(player, 'You read the scroll. The level is revealed!');
            break;
        case 'identify':
            addMessage(player, 'You feel more knowledgeable.');
            break;
    }
    player.inventory.splice(index, 1);
}

function renderSpectatorView(buf, width, height) {
    var center = Math.floor(height / 2);
    var text = '--- Spectating ---';
    var pad = Math.floor((width - text.length) / 2);
    if (pad < 0) pad = 0;
    if (center >= 0 && center < height) {
        buf.writeString(pad, center, text, null, null);
    }
}

// ─── Register Commands ─────────────────────────────────────────────────────

registerCommand({
    name: "scores",
    description: "Show high scores",
    handler: function(playerID, isAdmin, args) {
        if (Game.state.highScores.length === 0) {
            chatPlayer(playerID, "No high scores yet.");
            return;
        }
        var lines = ["--- Hall of Fame ---"];
        for (var i = 0; i < Math.min(10, Game.state.highScores.length); i++) {
            var hs = Game.state.highScores[i];
            lines.push((i + 1) + ". " + hs.name + " - Depth " + hs.depth + " - " + hs.gold + " gold - " + hs.kills + " kills");
        }
        chatPlayer(playerID, lines.join("\n"));
    }
});
