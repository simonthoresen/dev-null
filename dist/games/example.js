// example.js — demonstrates the full null-space game lifecycle
// Load with: /game load example

var Game = {
    gameName: "Example Arena",

    // Game.state holds all mutable game data. The framework reads this for
    // suspend/resume and client-side state replication.
    state: {
        players: {},
        tick: 0,
        maxTicks: 300,  // game lasts 30 seconds (300 ticks at 100ms)
        highScore: 0
    },

    // Require 1-4 teams.
    teamRange: { min: 1, max: 4 },

    // Called before splash. Restore state, set up splash screen.
    // players() and teams() are available.
    init: function(savedState) {
        if (savedState && savedState.highScore) {
            Game.state.highScore = savedState.highScore;
        }
        // Build dynamic splash screen with team and player info.
        var t = teams();
        var playerCount = 0;
        var teamNames = [];
        for (var i = 0; i < t.length; i++) {
            teamNames.push(t[i].name + " (" + t[i].players.length + ")");
            playerCount += t[i].players.length;
        }
        Game.splashScreen = "=== EXAMPLE ARENA ===\n"
            + "Move with arrows, press Space to score\n"
            + "Game lasts 30 seconds\n"
            + "\nHigh score: " + Game.state.highScore
            + "\nTeams: " + teamNames.join(" vs ")
            + "\nPlayers: " + playerCount;
    },

    // Called at splash→playing transition. Set up game Game.state.
    start: function() {
        var t = teams();
        for (var i = 0; i < t.length; i++) {
            for (var j = 0; j < t[i].players.length; j++) {
                var p = t[i].players[j];
                Game.state.players[p.id] = {
                    name: p.name,
                    team: t[i].name,
                    x: 5 + Math.floor(Math.random() * 20),
                    y: 2 + Math.floor(Math.random() * 8),
                    score: 0
                };
            }
        }
        log("Example start: " + t.length + " teams");
    },

    onPlayerLeave: function(playerID) {
        delete Game.state.players[playerID];
    },

    onInput: function(playerID, key) {
        var p = Game.state.players[playerID];
        if (!p) return;
        if (key === "up")    p.y = Math.max(0, p.y - 1);
        if (key === "down")  p.y++;
        if (key === "left")  p.x = Math.max(0, p.x - 1);
        if (key === "right") p.x++;
        // Collect dots for score.
        if (key === " ") p.score += 10;
    },

    update: function(dt) {
        Game.state.tick++;

        // Check if game is over.
        if (Game.state.tick >= Game.state.maxTicks) {
            // Update high score.
            var best = 0;
            for (var id in Game.state.players) {
                if (Game.state.players[id].score > best) best = Game.state.players[id].score;
            }
            if (best > Game.state.highScore) Game.state.highScore = best;

            // Build ranked results and end the game.
            var results = [];
            for (var id in Game.state.players) {
                var p = Game.state.players[id];
                results.push({ name: p.name + " (" + p.team + ")", result: p.score + " pts" });
            }
            results.sort(function(a, b) {
                return parseInt(b.result) - parseInt(a.result);
            });
            gameOver(results, { highScore: Game.state.highScore });
        }
    },

    render: function(buf, playerID, ox, oy, width, height) {
        for (var y = 0; y < height; y++) {
            for (var x = 0; x < width; x++) {
                var ch = ".";
                for (var id in Game.state.players) {
                    var p = Game.state.players[id];
                    if (p.x === x && p.y === y) {
                        ch = (id === playerID) ? "@" : "O";
                        break;
                    }
                }
                buf.setChar(x, y, ch, null, null);
            }
        }
    },

    statusBar: function(playerID) {
        var p = Game.state.players[playerID];
        if (!p) return "Example Arena";
        var remaining = Math.max(0, Math.ceil((Game.state.maxTicks - Game.state.tick) / 10));
        var count = Object.keys(Game.state.players).length;
        return "Example Arena  |  " + p.team + "  |  score: " + p.score + "  |  " + remaining + "s  |  players: " + count;
    },

    commandBar: function(playerID) {
        return "[arrow] Move  [Space] Score  [Enter] Chat";
    }
};
