// profanity-filter.js — example null-space plugin
// Load with: /plugin load profanity-filter
//
// Intercepts all chat messages and replaces banned words with asterisks.
// Demonstrates the onChatMessage pipeline hook.

var BANNED = ["badword", "anotherbadword"]; // extend as needed

function censor(text) {
    BANNED.forEach(function(word) {
        var re = new RegExp(word, "gi");
        var stars = "*".repeat(word.length);
        text = text.replace(re, stars);
    });
    return text;
}

var Plugin = {
    onChatMessage: function(msg) {
        // Return null to drop the message entirely, or return (modified) msg to allow it.
        msg.text = censor(msg.text);
        return msg;
    },

    onPlayerJoin: function(playerID, playerName) {
        log("profanity-filter: player joined " + playerName);
    }
};
