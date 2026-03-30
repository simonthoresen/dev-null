// example.js — minimal null-space app
// Load with: /load example

var state = {
    players: {},
    tick: 0
};

var Game = {
    onPlayerJoin: function(playerID, playerName) {
        state.players[playerID] = { name: playerName, x: 10, y: 5 };
        chat("** " + playerName + " entered the arena **");
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
    },

    view: function(playerID, width, height) {
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
        if (!p) return "null-space example";
        var count = Object.keys(state.players).length;
        return "Example App  |  pos: (" + p.x + "," + p.y + ")  |  players: " + count;
    },

    commandBar: function(playerID) {
        return "[↑↓←→] Move  [Enter] Chat";
    }
};
