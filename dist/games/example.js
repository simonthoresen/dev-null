// example.js — demonstrates the full null-space game lifecycle
// Load with: /game load example

var state = {
    players: {},
    teams: [],
    tick: 0,
    maxTicks: 300,  // game lasts 30 seconds (300 ticks at 100ms)
    highScore: 0
};

var Game = {
    gameName: "Example Arena",

    // Require 1-4 teams.
    teamRange: { min: 1, max: 4 },

    // Called once after loading — receives teams, saved state, and player list.
    init: function(config) {
        state.teams = config.teams || [];
        if (config.savedState && config.savedState.highScore) {
            state.highScore = config.savedState.highScore;
        }
        log("Example init: " + state.teams.length + " teams, high score: " + state.highScore);
    },

    onPlayerJoin: function(playerID, playerName) {
        // Find which team this player belongs to.
        var teamName = "none";
        for (var i = 0; i < state.teams.length; i++) {
            var t = state.teams[i];
            for (var j = 0; j < t.players.length; j++) {
                if (t.players[j] === playerID) {
                    teamName = t.name;
                }
            }
        }
        state.players[playerID] = {
            name: playerName,
            team: teamName,
            x: 5 + Math.floor(Math.random() * 20),
            y: 2 + Math.floor(Math.random() * 8),
            score: 0
        };
        chat("** " + playerName + " (" + teamName + ") entered the arena **");
    },

    onPlayerLeave: function(playerID) {
        delete state.players[playerID];
    },

    onInput: function(playerID, key) {
        var p = state.players[playerID];
        if (!p) return;
        if (key === "up")    p.y = Math.max(0, p.y - 1);
        if (key === "down")  p.y++;
        if (key === "left")  p.x = Math.max(0, p.x - 1);
        if (key === "right") p.x++;
        // Collect dots for score.
        if (key === " ") p.score += 10;
    },

    view: function(playerID, width, height) {
        state.tick++;

        // Check if game is over.
        if (state.tick >= state.maxTicks) {
            // Update high score.
            var best = 0;
            for (var id in state.players) {
                if (state.players[id].score > best) best = state.players[id].score;
            }
            if (best > state.highScore) state.highScore = best;

            // Build ranked results and end the game.
            var results = [];
            for (var id in state.players) {
                var p = state.players[id];
                results.push({ name: p.name + " (" + p.team + ")", result: p.score + " pts" });
            }
            results.sort(function(a, b) {
                return parseInt(b.result) - parseInt(a.result);
            });
            gameOver(results, { highScore: state.highScore });
        }

        var me = state.players[playerID];
        var lines = [];
        for (var y = 0; y < height; y++) {
            var line = "";
            for (var x = 0; x < width; x++) {
                var ch = ".";
                for (var id in state.players) {
                    var p = state.players[id];
                    if (p.x === x && p.y === y) {
                        ch = (id === playerID) ? "@" : "O";
                        break;
                    }
                }
                line += ch;
            }
            lines.push(line);
        }
        return lines.join("\n");
    },

    statusBar: function(playerID) {
        var p = state.players[playerID];
        if (!p) return "Example Arena";
        var remaining = Math.max(0, Math.ceil((state.maxTicks - state.tick) / 10));
        var count = Object.keys(state.players).length;
        return "Example Arena  |  " + p.team + "  |  score: " + p.score + "  |  " + remaining + "s  |  players: " + count;
    },

    commandBar: function(playerID) {
        return "[arrow] Move  [Space] Score  [Enter] Chat";
    },

    // Custom splash screen (optional — remove to see the default).
    splashScreen: function(width, height) {
        var lines = [];
        var title = "=== EXAMPLE ARENA ===";
        var sub = "Move with arrows, press Space to score";
        var info = "Game lasts 30 seconds";
        var hs = "High score: " + state.highScore;

        var midY = Math.floor(height / 2) - 2;
        for (var y = 0; y < height; y++) {
            if (y === midY) {
                lines.push(center(title, width));
            } else if (y === midY + 1) {
                lines.push(center(sub, width));
            } else if (y === midY + 2) {
                lines.push(center(info, width));
            } else if (y === midY + 4) {
                lines.push(center(hs, width));
            } else if (y === midY + 6 && state.teams.length > 0) {
                var teamNames = [];
                for (var i = 0; i < state.teams.length; i++) {
                    teamNames.push(state.teams[i].name + " (" + state.teams[i].players.length + ")");
                }
                lines.push(center("Teams: " + teamNames.join(" vs "), width));
            } else {
                lines.push("");
            }
        }
        return lines.join("\n");
    }
};

function center(text, width) {
    var pad = Math.max(0, Math.floor((width - text.length) / 2));
    return Array(pad + 1).join(" ") + text;
}
