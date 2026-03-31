// holdem.js — Multiplayer Texas Hold'em for null-space
// Load with: /load holdem

// ============================================================
// ANSI colors
// ============================================================
var RST   = "\x1b[0m";
var BOLD  = "\x1b[1m";
var DIM   = "\x1b[2m";
var RED   = "\x1b[31m";
var GRN   = "\x1b[32m";
var YEL   = "\x1b[33m";
var BLU   = "\x1b[34m";
var MAG   = "\x1b[35m";
var CYN   = "\x1b[36m";
var WHT   = "\x1b[37m";
var BGGRN = "\x1b[42m";
var BGRED = "\x1b[41m";
var BGBLU = "\x1b[44m";
var BGDGRN = "\x1b[48;5;22m";
var BGDRED = "\x1b[48;5;52m";
var BGGOLD = "\x1b[48;5;136m";
var FGGOLD = "\x1b[38;5;136m";
var FGWHT = "\x1b[38;5;255m";
var FGBLK = "\x1b[30m";

// ============================================================
// Card suits and rendering
// ============================================================
var SUITS = ["s", "h", "d", "c"];  // spades, hearts, diamonds, clubs
var SUIT_SYMBOLS = { s: "\u2660", h: "\u2665", d: "\u2666", c: "\u2663" };
var SUIT_COLORS  = { s: WHT, h: RED, d: RED, c: WHT };
var RANKS = ["2","3","4","5","6","7","8","9","T","J","Q","K","A"];

function cardStr(card) {
    if (!card) return BGDGRN + "  " + RST;
    var r = card.rank === "T" ? "10" : card.rank;
    var sym = SUIT_SYMBOLS[card.suit];
    var col = SUIT_COLORS[card.suit];
    return col + BOLD + r + sym + RST;
}

function cardBack() {
    return BLU + BOLD + "\u2588\u2588" + RST;
}

// Card display width (visible chars)
function cardWidth(card) {
    if (!card) return 2;
    return card.rank === "T" ? 3 : 2;
}

// ============================================================
// Deck
// ============================================================
function makeDeck() {
    var deck = [];
    for (var si = 0; si < SUITS.length; si++) {
        for (var ri = 0; ri < RANKS.length; ri++) {
            deck.push({ rank: RANKS[ri], suit: SUITS[si] });
        }
    }
    return deck;
}

function shuffle(arr) {
    for (var i = arr.length - 1; i > 0; i--) {
        var j = Math.floor(Math.random() * (i + 1));
        var tmp = arr[i]; arr[i] = arr[j]; arr[j] = tmp;
    }
    return arr;
}

// ============================================================
// Hand evaluation
// ============================================================
var RANK_VAL = {};
for (var i = 0; i < RANKS.length; i++) RANK_VAL[RANKS[i]] = i + 2;

var HAND_NAMES = [
    "High Card", "Pair", "Two Pair", "Three of a Kind",
    "Straight", "Flush", "Full House", "Four of a Kind",
    "Straight Flush", "Royal Flush"
];

// Returns { rank: 0-9, kickers: [...], name: string }
function evaluateHand(cards) {
    // Generate all 5-card combos from 7 cards
    var best = null;
    var combos = combinations(cards, 5);
    for (var ci = 0; ci < combos.length; ci++) {
        var h = evalFive(combos[ci]);
        if (!best || compareHands(h, best) > 0) best = h;
    }
    return best;
}

function combinations(arr, k) {
    var result = [];
    function helper(start, combo) {
        if (combo.length === k) { result.push(combo.slice()); return; }
        for (var i = start; i < arr.length; i++) {
            combo.push(arr[i]);
            helper(i + 1, combo);
            combo.pop();
        }
    }
    helper(0, []);
    return result;
}

function uniqueCount(arr) {
    var seen = {};
    var count = 0;
    for (var i = 0; i < arr.length; i++) {
        if (!seen[arr[i]]) { seen[arr[i]] = true; count++; }
    }
    return count;
}

function evalFive(cards) {
    var vals = [], suits = [];
    for (var _i = 0; _i < cards.length; _i++) {
        vals.push(RANK_VAL[cards[_i].rank]);
        suits.push(cards[_i].suit);
    }
    vals.sort(function(a,b) { return b - a; });

    var isFlush = suits[0] === suits[1] && suits[1] === suits[2] && suits[2] === suits[3] && suits[3] === suits[4];

    // Check straight (including ace-low)
    var isStraight = false;
    var straightHigh = 0;
    if (vals[0] - vals[4] === 4 && uniqueCount(vals) === 5) {
        isStraight = true;
        straightHigh = vals[0];
    }
    // Ace-low straight: A-2-3-4-5
    if (vals[0] === 14 && vals[1] === 5 && vals[2] === 4 && vals[3] === 3 && vals[4] === 2) {
        isStraight = true;
        straightHigh = 5;
    }

    // Count ranks
    var counts = {};
    for (var i = 0; i < vals.length; i++) {
        counts[vals[i]] = (counts[vals[i]] || 0) + 1;
    }
    var groups = [];
    for (var v in counts) groups.push({ val: parseInt(v), count: counts[v] });
    groups.sort(function(a, b) { return b.count - a.count || b.val - a.val; });

    if (isStraight && isFlush) {
        if (straightHigh === 14) return { rank: 9, kickers: [14], name: "Royal Flush" };
        return { rank: 8, kickers: [straightHigh], name: "Straight Flush" };
    }
    if (groups[0].count === 4) {
        return { rank: 7, kickers: [groups[0].val, groups[1].val], name: "Four of a Kind" };
    }
    if (groups[0].count === 3 && groups[1].count === 2) {
        return { rank: 6, kickers: [groups[0].val, groups[1].val], name: "Full House" };
    }
    if (isFlush) {
        return { rank: 5, kickers: vals, name: "Flush" };
    }
    if (isStraight) {
        return { rank: 4, kickers: [straightHigh], name: "Straight" };
    }
    if (groups[0].count === 3) {
        return { rank: 3, kickers: [groups[0].val, groups[1].val, groups[2].val], name: "Three of a Kind" };
    }
    if (groups[0].count === 2 && groups[1].count === 2) {
        var hi = Math.max(groups[0].val, groups[1].val);
        var lo = Math.min(groups[0].val, groups[1].val);
        return { rank: 2, kickers: [hi, lo, groups[2].val], name: "Two Pair" };
    }
    if (groups[0].count === 2) {
        return { rank: 1, kickers: [groups[0].val, groups[1].val, groups[2].val, groups[3].val], name: "Pair" };
    }
    return { rank: 0, kickers: vals, name: "High Card" };
}

function compareHands(a, b) {
    if (a.rank !== b.rank) return a.rank - b.rank;
    for (var i = 0; i < Math.min(a.kickers.length, b.kickers.length); i++) {
        if (a.kickers[i] !== b.kickers[i]) return a.kickers[i] - b.kickers[i];
    }
    return 0;
}

// ============================================================
// Game State
// ============================================================
var STARTING_CHIPS = 1000;
var SMALL_BLIND = 10;
var BIG_BLIND = 20;
var BLIND_INCREASE_HANDS = 10;
var ACTION_TIMEOUT = 300; // ticks (30 seconds)
var MIN_PLAYERS = 5;
var AI_THINK_MIN = 10;  // ticks (1s)
var AI_THINK_MAX = 30;  // ticks (3s)
var AI_NAMES = ["Ace", "Bluff", "Cash", "Dice", "Edge", "Flint", "Grit", "Hawk", "Iron", "Jinx"];
var AI_PREFIX = "ai_";

var state = {
    players: {},     // id -> { name, chips, hand, folded, bet, allIn, bustedOut }
    seatOrder: [],   // player IDs in seat order
    phase: "waiting", // waiting, preflop, flop, turn, river, showdown
    deck: [],
    community: [],
    pot: 0,
    currentBet: 0,
    actionOn: -1,    // index into seatOrder
    dealerIdx: 0,
    handNum: 0,
    lastAction: "",
    showdownTimer: 0,
    showdownResults: null,
    actionTimer: 0,
    minRaise: BIG_BLIND,
    raiseAmount: BIG_BLIND,  // current raise selector value
    lastWinMsg: "",
    eliminated: []   // recently eliminated player names
};

// ============================================================
// Seat management
// ============================================================
function activePlayers() {
    return state.seatOrder.filter(function(id) {
        var p = state.players[id];
        return p && !p.bustedOut;
    });
}

function playersInHand() {
    return state.seatOrder.filter(function(id) {
        var p = state.players[id];
        return p && !p.folded && !p.bustedOut;
    });
}

function playersCanAct() {
    return state.seatOrder.filter(function(id) {
        var p = state.players[id];
        return p && !p.folded && !p.bustedOut && !p.allIn;
    });
}

// ============================================================
// Deal a new hand
// ============================================================
function startHand() {
    var active = activePlayers();
    if (active.length < 2) {
        state.phase = "waiting";
        if (active.length === 1) {
            var winner = state.players[active[0]];
            state.lastWinMsg = BOLD + GRN + winner.name + " wins the tournament!" + RST;
            chat(winner.name + " wins the tournament!");
        }
        return;
    }

    state.handNum++;
    // Increase blinds every N hands
    var level = Math.floor((state.handNum - 1) / BLIND_INCREASE_HANDS);
    SMALL_BLIND = 10 * Math.pow(2, level);
    BIG_BLIND = SMALL_BLIND * 2;

    state.deck = shuffle(makeDeck());
    state.community = [];
    state.pot = 0;
    state.currentBet = 0;
    state.lastAction = "";
    state.showdownResults = null;
    state.minRaise = BIG_BLIND;
    state.raiseAmount = BIG_BLIND;
    state.eliminated = [];

    // Reset player hand state
    for (var i = 0; i < state.seatOrder.length; i++) {
        var p = state.players[state.seatOrder[i]];
        if (!p) continue;
        p.hand = [];
        p.folded = p.bustedOut;
        p.bet = 0;
        p.allIn = false;
    }

    // Move dealer
    state.dealerIdx = nextActiveSeat(state.dealerIdx);

    // Post blinds
    var sbIdx = active.length === 2 ? state.dealerIdx : nextActiveSeat(state.dealerIdx);
    var bbIdx = nextActiveSeat(sbIdx);

    postBlind(sbIdx, SMALL_BLIND);
    postBlind(bbIdx, BIG_BLIND);
    state.currentBet = BIG_BLIND;

    // Deal hole cards
    for (var d = 0; d < 2; d++) {
        for (var j = 0; j < active.length; j++) {
            var pl = state.players[active[j]];
            if (!pl.folded) pl.hand.push(state.deck.pop());
        }
    }

    // Action starts left of big blind
    state.actionOn = nextCanActSeat(bbIdx);
    state.phase = "preflop";
    state.actionTimer = ACTION_TIMEOUT;

    var dealerName = state.players[state.seatOrder[state.dealerIdx]].name;
    log("Hand #" + state.handNum + " — Dealer: " + dealerName + " — Blinds: " + SMALL_BLIND + "/" + BIG_BLIND);
}

function postBlind(seatIdx, amount) {
    var id = state.seatOrder[seatIdx];
    var p = state.players[id];
    var actual = Math.min(amount, p.chips);
    p.chips -= actual;
    p.bet = actual;
    state.pot += actual;
    if (p.chips === 0) p.allIn = true;
}

function nextActiveSeat(fromIdx) {
    var active = activePlayers();
    for (var i = 1; i <= state.seatOrder.length; i++) {
        var idx = (fromIdx + i) % state.seatOrder.length;
        if (active.indexOf(state.seatOrder[idx]) >= 0) return idx;
    }
    return fromIdx;
}

function nextCanActSeat(fromIdx) {
    var canAct = playersCanAct();
    for (var i = 1; i <= state.seatOrder.length; i++) {
        var idx = (fromIdx + i) % state.seatOrder.length;
        if (canAct.indexOf(state.seatOrder[idx]) >= 0) return idx;
    }
    return -1;
}

// ============================================================
// Betting actions
// ============================================================
function doFold(playerID) {
    var p = state.players[playerID];
    if (!p) return;
    p.folded = true;
    state.lastAction = p.name + " folds";
    log(state.lastAction);
    advanceAction();
}

function doCheck(playerID) {
    var p = state.players[playerID];
    if (!p) return;
    state.lastAction = p.name + " checks";
    log(state.lastAction);
    advanceAction();
}

function doCall(playerID) {
    var p = state.players[playerID];
    if (!p) return;
    var toCall = state.currentBet - p.bet;
    var actual = Math.min(toCall, p.chips);
    p.chips -= actual;
    p.bet += actual;
    state.pot += actual;
    if (p.chips === 0) p.allIn = true;
    state.lastAction = p.name + " calls" + (p.allIn ? " (all-in)" : "");
    log(state.lastAction);
    advanceAction();
}

function doRaise(playerID, amount) {
    var p = state.players[playerID];
    if (!p) return;
    var toCall = state.currentBet - p.bet;
    var totalBet = state.currentBet + amount;
    var totalCost = totalBet - p.bet;
    var actual = Math.min(totalCost, p.chips);
    p.chips -= actual;
    p.bet += actual;
    state.pot += actual;
    if (p.chips === 0) p.allIn = true;
    var newBet = p.bet;
    if (newBet > state.currentBet) {
        state.minRaise = newBet - state.currentBet;
        state.currentBet = newBet;
    }
    state.lastAction = p.name + " raises to " + state.currentBet + (p.allIn ? " (all-in)" : "");
    log(state.lastAction);
    // Reset action to go around again
    state.actionOn = nextCanActSeatExcluding(seatIndexOf(playerID), playerID);
    if (state.actionOn === -1) {
        finishBettingRound();
    } else {
        state.actionTimer = ACTION_TIMEOUT;
    }
}

function doAllIn(playerID) {
    var p = state.players[playerID];
    if (!p) return;
    var amount = p.chips;
    p.chips = 0;
    p.bet += amount;
    state.pot += amount;
    p.allIn = true;
    if (p.bet > state.currentBet) {
        state.minRaise = Math.max(state.minRaise, p.bet - state.currentBet);
        state.currentBet = p.bet;
        state.lastAction = p.name + " all-in for " + p.bet;
        // Reset action around
        state.actionOn = nextCanActSeatExcluding(seatIndexOf(playerID), playerID);
        if (state.actionOn === -1) {
            finishBettingRound();
            return;
        }
        state.actionTimer = ACTION_TIMEOUT;
    } else {
        state.lastAction = p.name + " all-in for " + p.bet;
        advanceAction();
    }
    log(state.lastAction);
}

function seatIndexOf(playerID) {
    return state.seatOrder.indexOf(playerID);
}

function nextCanActSeatExcluding(fromIdx, excludeID) {
    var canAct = playersCanAct();
    for (var i = 1; i <= state.seatOrder.length; i++) {
        var idx = (fromIdx + i) % state.seatOrder.length;
        var id = state.seatOrder[idx];
        if (id !== excludeID && canAct.indexOf(id) >= 0) return idx;
    }
    return -1;
}

function advanceAction() {
    // Check if only one player remains
    var inHand = playersInHand();
    if (inHand.length === 1) {
        // Last player standing wins
        awardPot(inHand[0]);
        endHand();
        return;
    }

    var canAct = playersCanAct();
    if (canAct.length === 0) {
        // Everyone is all-in or folded
        finishBettingRound();
        return;
    }

    // Check if betting round is complete (everyone who can act has matched current bet)
    var nextIdx = nextCanActSeat(state.actionOn);
    if (nextIdx === -1) {
        finishBettingRound();
        return;
    }

    var nextID = state.seatOrder[nextIdx];
    var nextP = state.players[nextID];

    // If we've gone around and everyone has matched
    var allMatched = true;
    for (var i = 0; i < canAct.length; i++) {
        var cp = state.players[canAct[i]];
        if (cp.bet < state.currentBet) { allMatched = false; break; }
    }

    if (allMatched && state.currentBet > 0) {
        // Check the next player — if they've already acted and matched, round is done
        if (nextP.bet === state.currentBet) {
            finishBettingRound();
            return;
        }
    }

    state.actionOn = nextIdx;
    state.actionTimer = ACTION_TIMEOUT;
}

function finishBettingRound() {
    // Reset bets for next round
    for (var i = 0; i < state.seatOrder.length; i++) {
        var p = state.players[state.seatOrder[i]];
        if (p) p.bet = 0;
    }
    state.currentBet = 0;
    state.minRaise = BIG_BLIND;
    state.raiseAmount = BIG_BLIND;

    var inHand = playersInHand();
    if (inHand.length === 1) {
        awardPot(inHand[0]);
        endHand();
        return;
    }

    // Advance to next phase
    if (state.phase === "preflop") {
        state.phase = "flop";
        state.deck.pop(); // burn
        state.community.push(state.deck.pop());
        state.community.push(state.deck.pop());
        state.community.push(state.deck.pop());
    } else if (state.phase === "flop") {
        state.phase = "turn";
        state.deck.pop(); // burn
        state.community.push(state.deck.pop());
    } else if (state.phase === "turn") {
        state.phase = "river";
        state.deck.pop(); // burn
        state.community.push(state.deck.pop());
    } else if (state.phase === "river") {
        doShowdown();
        return;
    }

    // If only 0 or 1 players can still act, run through remaining streets
    var canAct = playersCanAct();
    if (canAct.length <= 1) {
        finishBettingRound();
        return;
    }

    // Action starts left of dealer
    state.actionOn = nextCanActSeat(state.dealerIdx);
    state.actionTimer = ACTION_TIMEOUT;
}

// ============================================================
// Showdown
// ============================================================
function doShowdown() {
    state.phase = "showdown";
    var inHand = playersInHand();

    // Evaluate all hands
    var results = [];
    for (var i = 0; i < inHand.length; i++) {
        var id = inHand[i];
        var p = state.players[id];
        var allCards = p.hand.concat(state.community);
        var eval_ = evaluateHand(allCards);
        results.push({ id: id, name: p.name, hand: eval_, cards: p.hand });
    }

    // Sort best to worst
    results.sort(function(a, b) { return compareHands(b.hand, a.hand); });

    // Find winners (could be ties)
    var winners = [results[0]];
    for (var j = 1; j < results.length; j++) {
        if (compareHands(results[j].hand, results[0].hand) === 0) {
            winners.push(results[j]);
        } else {
            break;
        }
    }

    // Split pot
    var share = Math.floor(state.pot / winners.length);
    var remainder = state.pot - share * winners.length;
    var winMsg = "";
    for (var w = 0; w < winners.length; w++) {
        var wp = state.players[winners[w].id];
        var award = share + (w === 0 ? remainder : 0);
        wp.chips += award;
        if (winMsg) winMsg += ", ";
        winMsg += wp.name;
    }
    winMsg += " wins " + state.pot + " with " + results[0].hand.name;
    state.lastWinMsg = winMsg;
    chat(winMsg);
    log(winMsg);

    state.showdownResults = results;
    state.showdownTimer = 50; // 5 seconds to view results
}

function awardPot(winnerID) {
    var p = state.players[winnerID];
    p.chips += state.pot;
    state.lastWinMsg = p.name + " wins " + state.pot;
    chat(state.lastWinMsg);
    log(state.lastWinMsg);
    state.pot = 0;
}

function endHand() {
    state.phase = "between";
    state.showdownTimer = 30; // 3 second pause between hands

    // Eliminate busted players
    state.eliminated = [];
    for (var i = 0; i < state.seatOrder.length; i++) {
        var p = state.players[state.seatOrder[i]];
        if (p && p.chips <= 0 && !p.bustedOut) {
            p.bustedOut = true;
            state.eliminated.push(p.name);
            chat(p.name + " is eliminated!");
        }
    }
}

// ============================================================
// Tick
// ============================================================
var tickCount = 0;

function tick() {
    tickCount++;

    if (state.phase === "showdown" || state.phase === "between") {
        state.showdownTimer--;
        if (state.showdownTimer <= 0) {
            endHand();
            if (state.phase === "between") {
                state.showdownTimer = 0;
                startHand();
            }
        }
        return;
    }

    if (state.phase === "waiting") return;

    // Action timer & AI logic
    if (state.actionOn >= 0 && state.actionOn < state.seatOrder.length) {
        var id = state.seatOrder[state.actionOn];
        var ap = id ? state.players[id] : null;

        if (ap && isAI(id) && !ap.folded && !ap.allIn) {
            // AI player: use think timer
            if (!ap.aiThinkTimer) {
                ap.aiThinkTimer = AI_THINK_MIN + Math.floor(Math.random() * (AI_THINK_MAX - AI_THINK_MIN));
            }
            ap.aiThinkTimer--;
            if (ap.aiThinkTimer <= 0) {
                ap.aiThinkTimer = 0;
                aiDecide(id);
            }
        } else if (state.actionTimer > 0) {
            state.actionTimer--;
            if (state.actionTimer <= 0) {
                // Auto-fold on timeout (human players only)
                if (ap) {
                    if (ap.bet >= state.currentBet) {
                        doCheck(id);
                    } else {
                        doFold(id);
                    }
                }
            }
        }
    }
}

// ============================================================
// Rendering helpers
// ============================================================
function pad(str, len) {
    while (str.length < len) str += " ";
    return str;
}

function padLeft(str, len) {
    while (str.length < len) str = " " + str;
    return str;
}

function center(str, len) {
    var rawLen = stripAnsi(str).length;
    if (rawLen >= len) return str;
    var left = Math.floor((len - rawLen) / 2);
    var right = len - rawLen - left;
    return spaces(left) + str + spaces(right);
}

function spaces(n) {
    var s = "";
    for (var i = 0; i < n; i++) s += " ";
    return s;
}

function stripAnsi(str) {
    return str.replace(/\x1b\[[0-9;]*m/g, "");
}

function repeat(ch, n) {
    var s = "";
    for (var i = 0; i < n; i++) s += ch;
    return s;
}

function truncate(str, maxLen) {
    var raw = stripAnsi(str);
    if (raw.length <= maxLen) return str;
    // Simple truncation for plain strings
    return str.substring(0, maxLen - 1) + "\u2026";
}

// ============================================================
// View rendering
// ============================================================
function renderTable(playerID, width, height) {
    var me = state.players[playerID];
    var lines = [];
    var active = activePlayers();
    var numPlayers = active.length;

    // Calculate layout
    var tableTop = 1;
    var tableH = Math.max(12, height - 4);
    var tableMidY = Math.floor(tableH / 2) + tableTop;

    // Background: fill with green felt
    for (var y = 0; y < height; y++) {
        lines.push(BGDGRN + spaces(width) + RST);
    }

    // == Community cards ==
    var communityY = tableMidY - 2;
    var commStr = "";
    if (state.phase === "waiting") {
        commStr = DIM + "Waiting for players..." + RST;
    } else if (state.community.length === 0 && state.phase === "preflop") {
        commStr = DIM + "[  ] [  ] [  ]  [  ]  [  ]" + RST;
    } else {
        for (var ci = 0; ci < 5; ci++) {
            if (ci > 0) commStr += " ";
            if (ci < state.community.length) {
                commStr += "[" + cardStr(state.community[ci]) + BGDGRN + "]";
            } else {
                commStr += DIM + "[  ]" + RST;
            }
        }
    }
    setLine(lines, communityY, center(commStr + BGDGRN, width), width);

    // == Pot ==
    var potY = communityY + 1;
    var potStr = "";
    if (state.phase !== "waiting") {
        potStr = FGGOLD + BOLD + "Pot: " + state.pot + RST;
        if (state.lastAction) {
            potStr += BGDGRN + "  " + DIM + state.lastAction + RST;
        }
    }
    setLine(lines, potY, center(potStr + BGDGRN, width), width);

    // == Phase indicator ==
    var phaseY = communityY - 1;
    var phaseStr = "";
    if (state.phase === "preflop") phaseStr = "Pre-Flop";
    else if (state.phase === "flop") phaseStr = "Flop";
    else if (state.phase === "turn") phaseStr = "Turn";
    else if (state.phase === "river") phaseStr = "River";
    else if (state.phase === "showdown") phaseStr = YEL + BOLD + "Showdown!" + RST;
    else if (state.phase === "between") phaseStr = DIM + state.lastWinMsg + RST;
    if (phaseStr) setLine(lines, phaseY, center(phaseStr + BGDGRN, width), width);

    // == Player seats arranged around the table ==
    if (numPlayers > 0) {
        var seats = arrangeSeatPositions(playerID, numPlayers, width, height, tableTop, tableH);
        for (var si = 0; si < seats.length; si++) {
            var seat = seats[si];
            var pid = seat.id;
            var p = state.players[pid];
            if (!p) continue;

            var isMe = pid === playerID;
            var isDealer = state.seatOrder[state.dealerIdx] === pid;
            var isAction = state.actionOn >= 0 && state.seatOrder[state.actionOn] === pid;

            // Name line
            var nameStr = "";
            if (isDealer) nameStr += YEL + "D " + RST;
            if (isAction && state.phase !== "waiting" && state.phase !== "showdown" && state.phase !== "between") {
                nameStr += BOLD + WHT + "\u25b6 " + RST;
            }
            var displayName = p.name + (p.isAI ? DIM + " bot" + RST : "");
            nameStr += (isMe ? CYN + BOLD : WHT) + displayName + RST;
            if (p.bustedOut) nameStr = DIM + "\u2620 " + p.name + RST;
            if (p.folded && !p.bustedOut) nameStr = DIM + p.name + " (fold)" + RST;

            // Chips line
            var chipStr = FGGOLD + "$" + p.chips + RST;
            if (p.bet > 0) chipStr += RED + " +" + p.bet + RST;

            // Cards line
            var cardLine = "";
            if (p.bustedOut) {
                cardLine = "";
            } else if (state.phase === "showdown" && !p.folded && p.hand.length === 2) {
                cardLine = "[" + cardStr(p.hand[0]) + BGDGRN + "][" + cardStr(p.hand[1]) + BGDGRN + "]";
            } else if (isMe && p.hand.length === 2) {
                cardLine = "[" + cardStr(p.hand[0]) + BGDGRN + "][" + cardStr(p.hand[1]) + BGDGRN + "]";
            } else if (p.hand.length === 2 && !p.folded) {
                cardLine = "[" + cardBack() + BGDGRN + "][" + cardBack() + BGDGRN + "]";
            }

            // Showdown hand name
            if (state.phase === "showdown" && state.showdownResults && !p.folded) {
                for (var ri = 0; ri < state.showdownResults.length; ri++) {
                    if (state.showdownResults[ri].id === pid) {
                        cardLine += " " + GRN + state.showdownResults[ri].hand.name + RST;
                        break;
                    }
                }
            }

            setLine(lines, seat.y, placeCentered(lines[seat.y] || "", seat.x, nameStr + BGDGRN, width), width);
            setLine(lines, seat.y + 1, placeCentered(lines[seat.y + 1] || "", seat.x, chipStr + BGDGRN, width), width);
            if (cardLine) {
                setLine(lines, seat.y + 2, placeCentered(lines[seat.y + 2] || "", seat.x, cardLine + BGDGRN, width), width);
            }
        }
    }

    // == Timer bar for action player ==
    if (me && !me.folded && !me.bustedOut && state.actionOn >= 0 && state.seatOrder[state.actionOn] === playerID) {
        var timerY = height - 1;
        var pct = state.actionTimer / ACTION_TIMEOUT;
        var barLen = Math.max(1, Math.floor(width * 0.4));
        var filled = Math.floor(barLen * pct);
        var timerColor = pct > 0.5 ? GRN : pct > 0.2 ? YEL : RED;
        var timerStr = timerColor + repeat("\u2588", filled) + DIM + repeat("\u2591", barLen - filled) + RST;
        var seconds = Math.ceil(state.actionTimer / 10);
        timerStr = BGDGRN + "  " + timerStr + BGDGRN + " " + timerColor + seconds + "s" + RST;
        setLine(lines, timerY, timerStr + BGDGRN + spaces(Math.max(0, width - stripAnsi(timerStr).length)) + RST, width);
    }

    return lines.join("\n");
}

function setLine(lines, y, content, width) {
    if (y >= 0 && y < lines.length) {
        lines[y] = content;
    }
}

function placeCentered(line, centerX, content, width) {
    var rawLen = stripAnsi(content).length;
    var startX = Math.max(0, centerX - Math.floor(rawLen / 2));
    // Build a new line with content placed at startX
    var bg = BGDGRN;
    return bg + spaces(startX) + content + bg + spaces(Math.max(0, width - startX - rawLen)) + RST;
}

function arrangeSeatPositions(myID, numPlayers, width, height, tableTop, tableH) {
    var active = activePlayers();
    var seats = [];

    // Reorder so current player is at bottom center
    var myIdx = active.indexOf(myID);
    var ordered = [];
    if (myIdx >= 0) {
        for (var i = 0; i < active.length; i++) {
            ordered.push(active[(myIdx + i) % active.length]);
        }
    } else {
        ordered = active.slice();
    }

    // Fixed positions based on player count (max ~10)
    // Position 0 = bottom center (me), then clockwise
    var positions = getSeatLayout(ordered.length, width, height, tableTop, tableH);

    for (var s = 0; s < ordered.length; s++) {
        seats.push({
            id: ordered[s],
            x: positions[s].x,
            y: positions[s].y
        });
    }
    return seats;
}

function getSeatLayout(count, width, height, tableTop, tableH) {
    var cx = Math.floor(width / 2);
    var cy = Math.floor(tableH / 2) + tableTop;
    var rx = Math.floor(width * 0.38);
    var ry = Math.floor(tableH * 0.38);
    var positions = [];

    // Place seats around an ellipse, starting from bottom (pi/2) going clockwise
    for (var i = 0; i < count; i++) {
        var angle = (Math.PI / 2) + (2 * Math.PI * i / count);
        var x = Math.floor(cx + rx * Math.cos(angle));
        var y = Math.floor(cy - ry * Math.sin(angle));
        // Clamp
        x = Math.max(2, Math.min(width - 16, x));
        y = Math.max(tableTop, Math.min(height - 4, y));
        positions.push({ x: x, y: y });
    }
    return positions;
}

// ============================================================
// AI Players
// ============================================================
function isAI(playerID) {
    return playerID.indexOf(AI_PREFIX) === 0;
}

function addAIPlayer() {
    // Pick an unused AI name
    var usedNames = {};
    for (var i = 0; i < state.seatOrder.length; i++) {
        var p = state.players[state.seatOrder[i]];
        if (p) usedNames[p.name] = true;
    }
    var name = null;
    for (var n = 0; n < AI_NAMES.length; n++) {
        if (!usedNames[AI_NAMES[n]]) { name = AI_NAMES[n]; break; }
    }
    if (!name) name = "Bot" + Math.floor(Math.random() * 1000);
    var id = AI_PREFIX + name.toLowerCase();
    state.players[id] = {
        name: name,
        chips: STARTING_CHIPS,
        hand: [],
        folded: false,
        bet: 0,
        allIn: false,
        bustedOut: false,
        isAI: true,
        aiThinkTimer: 0
    };
    state.seatOrder.push(id);
    chat(name + " (bot) sits down");
}

function fillAIPlayers() {
    var active = activePlayers();
    while (active.length < MIN_PLAYERS) {
        addAIPlayer();
        active = activePlayers();
    }
}

function humanCount() {
    var count = 0;
    for (var i = 0; i < state.seatOrder.length; i++) {
        var p = state.players[state.seatOrder[i]];
        if (p && !p.bustedOut && !isAI(state.seatOrder[i])) count++;
    }
    return count;
}

// AI hand strength: simple heuristic (0.0 - 1.0)
function aiHandStrength(playerID) {
    var p = state.players[playerID];
    if (!p || p.hand.length < 2) return 0.3;

    var c1 = RANK_VAL[p.hand[0].rank];
    var c2 = RANK_VAL[p.hand[1].rank];
    var hi = Math.max(c1, c2);
    var lo = Math.min(c1, c2);
    var suited = p.hand[0].suit === p.hand[1].suit;
    var pair = c1 === c2;

    // Base strength from card ranks (0-1 scale)
    var strength = (hi + lo - 4) / 24; // normalized: worst=0, best=1

    if (pair) strength += 0.25 + (hi - 2) * 0.02;
    if (suited) strength += 0.06;
    if (hi - lo <= 4 && !pair) strength += 0.04; // connected
    if (hi >= 12) strength += 0.08; // face cards

    // Post-flop: use actual hand evaluation
    if (state.community.length >= 3) {
        var allCards = p.hand.concat(state.community);
        var eval_ = evaluateHand(allCards);
        // rank 0-9, normalize
        strength = 0.2 + eval_.rank * 0.09;
        if (eval_.rank >= 2) strength += 0.1; // two pair+
        if (eval_.rank >= 4) strength += 0.15; // straight+
    }

    return Math.min(1.0, Math.max(0.0, strength));
}

function aiDecide(playerID) {
    var p = state.players[playerID];
    if (!p || p.folded || p.allIn) return;

    var toCall = state.currentBet - p.bet;
    var strength = aiHandStrength(playerID);
    var potOdds = toCall > 0 ? toCall / (state.pot + toCall) : 0;
    var rand = Math.random();

    // Add some personality variance per AI
    var aggression = ((playerID.charCodeAt(playerID.length - 1) % 5) - 2) * 0.05;
    strength += aggression;

    if (toCall === 0) {
        // Can check or raise
        if (strength > 0.65 && rand < 0.5) {
            // Raise with good hands
            var raiseAmt = Math.max(state.minRaise, Math.floor(state.pot * (0.3 + strength * 0.5)));
            raiseAmt = Math.min(raiseAmt, p.chips);
            raiseAmt = Math.max(raiseAmt, state.minRaise);
            if (strength > 0.85 && rand < 0.15) {
                doAllIn(playerID);
            } else {
                doRaise(playerID, raiseAmt);
            }
        } else {
            doCheck(playerID);
        }
    } else {
        // Must call, raise, or fold
        var callRatio = toCall / p.chips;

        if (strength > 0.8 && rand < 0.3) {
            // Strong hand: raise or all-in
            if (strength > 0.9 && rand < 0.1) {
                doAllIn(playerID);
            } else {
                var raiseAmt2 = Math.max(state.minRaise, Math.floor(state.pot * 0.5));
                raiseAmt2 = Math.min(raiseAmt2, p.chips - toCall);
                if (raiseAmt2 >= state.minRaise) {
                    doRaise(playerID, raiseAmt2);
                } else {
                    doCall(playerID);
                }
            }
        } else if (strength > potOdds + 0.1 || (callRatio < 0.1 && strength > 0.25)) {
            // Decent odds or cheap call
            doCall(playerID);
        } else if (strength > 0.4 && rand < 0.3) {
            // Marginal hand, sometimes call anyway
            doCall(playerID);
        } else {
            doFold(playerID);
        }
    }
}

// ============================================================
// Game object
// ============================================================
var Game = {
    onPlayerJoin: function(playerID, playerName) {
        if (state.players[playerID]) {
            // Reconnecting
            state.players[playerID].name = playerName;
            return;
        }
        state.players[playerID] = {
            name: playerName,
            chips: STARTING_CHIPS,
            hand: [],
            folded: false,
            bet: 0,
            allIn: false,
            bustedOut: false,
            isAI: false,
            aiThinkTimer: 0
        };
        state.seatOrder.push(playerID);
        chat(playerName + " sits down ($" + STARTING_CHIPS + ")");

        // Fill with AI players to reach MIN_PLAYERS
        fillAIPlayers();

        // Auto-start when we have 2+ players and waiting
        if (state.phase === "waiting" && activePlayers().length >= 2) {
            startHand();
        }
    },

    onPlayerLeave: function(playerID) {
        var p = state.players[playerID];
        if (!p) return;
        chat(p.name + " leaves the table");

        // Fold if in a hand
        if (!p.folded && !p.bustedOut && state.phase !== "waiting") {
            p.folded = true;
            if (state.actionOn >= 0 && state.seatOrder[state.actionOn] === playerID) {
                advanceAction();
            }
        }

        // Remove from seat order
        var idx = state.seatOrder.indexOf(playerID);
        if (idx >= 0) {
            state.seatOrder.splice(idx, 1);
            if (state.dealerIdx >= state.seatOrder.length) state.dealerIdx = 0;
            if (state.actionOn >= state.seatOrder.length) state.actionOn = 0;
        }
        delete state.players[playerID];

        // Check if hand should end
        var inHand = playersInHand();
        if (inHand.length === 1 && state.phase !== "waiting" && state.phase !== "showdown" && state.phase !== "between") {
            awardPot(inHand[0]);
            endHand();
        }
        if (activePlayers().length < 2 && state.phase !== "waiting") {
            state.phase = "waiting";
        }
    },

    onInput: function(playerID, key) {
        var p = state.players[playerID];
        if (!p || p.bustedOut) return;

        // During showdown/between, no actions
        if (state.phase === "showdown" || state.phase === "between" || state.phase === "waiting") return;

        // Only act if it's this player's turn
        if (state.actionOn < 0 || state.seatOrder[state.actionOn] !== playerID) return;
        if (p.folded || p.allIn) return;

        var toCall = state.currentBet - p.bet;

        if (key === "f" || key === "F") {
            if (toCall > 0) doFold(playerID);
        } else if (key === "c" || key === "C") {
            if (toCall > 0) {
                doCall(playerID);
            } else {
                doCheck(playerID);
            }
        } else if (key === "r" || key === "R") {
            if (p.chips > toCall) {
                doRaise(playerID, state.raiseAmount);
            }
        } else if (key === "a" || key === "A") {
            doAllIn(playerID);
        } else if (key === "up") {
            // Increase raise amount
            state.raiseAmount = Math.min(p.chips - toCall, state.raiseAmount + BIG_BLIND);
        } else if (key === "down") {
            // Decrease raise amount
            state.raiseAmount = Math.max(state.minRaise, state.raiseAmount - BIG_BLIND);
        } else if (key === " ") {
            // Space = check/call (most common action)
            if (toCall > 0) {
                doCall(playerID);
            } else {
                doCheck(playerID);
            }
        }
    },

    view: function(playerID, width, height) {
        tick();
        return renderTable(playerID, width, height);
    },

    statusBar: function(playerID) {
        var p = state.players[playerID];
        var chips = p ? "$" + p.chips : "";
        var phase = state.phase === "waiting" ? "Waiting" : state.phase.charAt(0).toUpperCase() + state.phase.slice(1);
        var blinds = "Blinds " + SMALL_BLIND + "/" + BIG_BLIND;
        var nPlayers = activePlayers().length;
        return "Hold'em  |  " + phase + "  |  " + blinds + "  |  " + chips + "  |  " + nPlayers + " players";
    },

    commandBar: function(playerID) {
        var p = state.players[playerID];
        if (!p) return "[Enter] Chat";

        if (state.phase === "waiting") return "Waiting for " + (2 - activePlayers().length) + " more player(s)...  [Enter] Chat";
        if (state.phase === "showdown" || state.phase === "between") return "  [Enter] Chat";
        if (p.bustedOut) return "Eliminated  [Enter] Chat";
        if (p.folded) return "Folded  [Enter] Chat";

        if (state.actionOn >= 0 && state.seatOrder[state.actionOn] === playerID) {
            var toCall = state.currentBet - p.bet;
            var actions = "";
            if (toCall > 0) {
                actions = "[Space/C] Call " + toCall + "  [F] Fold";
            } else {
                actions = "[Space/C] Check";
            }
            if (p.chips > toCall) {
                actions += "  [R] Raise " + state.raiseAmount + "  [\u2191\u2193] Adjust";
            }
            actions += "  [A] All-in " + p.chips;
            return actions;
        }

        return "Waiting for " + (state.actionOn >= 0 ? state.players[state.seatOrder[state.actionOn]].name : "...") + "  [Enter] Chat";
    }
};

// ============================================================
// Commands
// ============================================================
registerCommand({
    name: "chips",
    description: "Show chip counts for all players",
    handler: function(playerID, isAdmin, args) {
        var msg = "Chip counts:";
        for (var i = 0; i < state.seatOrder.length; i++) {
            var p = state.players[state.seatOrder[i]];
            if (p) {
                msg += "\n  " + p.name + ": $" + p.chips + (p.bustedOut ? " (out)" : "");
            }
        }
        chatPlayer(playerID, msg);
    }
});

registerCommand({
    name: "newgame",
    description: "Reset and start a new tournament (admin only)",
    adminOnly: true,
    handler: function(playerID, isAdmin, args) {
        state.phase = "waiting";
        state.handNum = 0;
        state.pot = 0;
        state.community = [];
        state.lastWinMsg = "";
        SMALL_BLIND = 10;
        BIG_BLIND = 20;
        for (var i = 0; i < state.seatOrder.length; i++) {
            var p = state.players[state.seatOrder[i]];
            if (p) {
                p.chips = STARTING_CHIPS;
                p.bustedOut = false;
                p.hand = [];
                p.folded = false;
                p.bet = 0;
                p.allIn = false;
            }
        }
        chat("New tournament started! All players reset to $" + STARTING_CHIPS);
        fillAIPlayers();
        if (activePlayers().length >= 2) startHand();
    }
});
