// main.js — Texas Hold'em for null-space (NC-panel UI, team-based)
// Each team shares one hand. First team member to act controls the decision.

include("poker");
include("ui");

// ─── Game Constants ────────────────────────────────────────────────────
var STARTING_CHIPS = 1000;
var SMALL_BLIND = 10;
var BIG_BLIND = 20;
var BLIND_INCREASE_HANDS = 10;
var ACTION_TIMEOUT = 300; // ticks (30 seconds at 10fps)
var MIN_SEATS = 4;
var AI_NAMES = ['Ace', 'Bluff', 'Cash', 'Dice', 'Edge', 'Flint', 'Grit', 'Hawk', 'Iron', 'Jinx'];
var AI_PREFIX = 'ai_';
var AI_THINK_MIN = 10;
var AI_THINK_MAX = 30;

// ─── Game State ────────────────────────────────────────────────────────
var state = {
    seats: {},        // seatID -> { name, chips, hand, folded, bet, allIn, bustedOut, isAI, aiThinkTimer }
    seatOrder: [],    // seat IDs in order
    teamToSeat: {},   // teamName -> seatID (for team->seat mapping)
    playerToTeam: {}, // playerID -> teamName
    phase: 'waiting', // waiting, preflop, flop, turn, river, showdown, between
    deck: [],
    community: [],
    pot: 0,
    currentBet: 0,
    actionOn: -1,     // index into seatOrder
    dealerIdx: 0,
    handNum: 0,
    lastAction: '',
    showdownTimer: 0,
    showdownResults: null,
    actionTimer: 0,
    minRaise: BIG_BLIND,
    raiseAmount: BIG_BLIND,
    lastWinMsg: '',
    eliminated: []
};

// ─── Seat Management ───────────────────────────────────────────────────

function activePlayers() {
    return state.seatOrder.filter(function(id) {
        var s = state.seats[id];
        return s && !s.bustedOut;
    });
}

function playersInHand() {
    return state.seatOrder.filter(function(id) {
        var s = state.seats[id];
        return s && !s.folded && !s.bustedOut;
    });
}

function playersCanAct() {
    return state.seatOrder.filter(function(id) {
        var s = state.seats[id];
        return s && !s.folded && !s.bustedOut && !s.allIn;
    });
}

function seatIndexOf(seatID) {
    return state.seatOrder.indexOf(seatID);
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

function nextCanActSeatExcluding(fromIdx, excludeID) {
    var canAct = playersCanAct();
    for (var i = 1; i <= state.seatOrder.length; i++) {
        var idx = (fromIdx + i) % state.seatOrder.length;
        var id = state.seatOrder[idx];
        if (id !== excludeID && canAct.indexOf(id) >= 0) return idx;
    }
    return -1;
}

function isAI(seatID) {
    return seatID.indexOf(AI_PREFIX) === 0;
}

// Resolve a playerID to their team's seatID
function playerSeat(playerID) {
    var teamName = state.playerToTeam[playerID];
    if (teamName) return state.teamToSeat[teamName];
    return null;
}

// ─── Deal a New Hand ───────────────────────────────────────────────────

function startHand() {
    var active = activePlayers();
    if (active.length < 2) {
        state.phase = 'waiting';
        if (active.length === 1) {
            var winner = state.seats[active[0]];
            state.lastWinMsg = winner.name + ' wins the tournament!';
            chat(winner.name + ' wins the tournament!');
        }
        return;
    }

    state.handNum++;
    var level = Math.floor((state.handNum - 1) / BLIND_INCREASE_HANDS);
    SMALL_BLIND = 10 * Math.pow(2, level);
    BIG_BLIND = SMALL_BLIND * 2;

    state.deck = shuffle(makeDeck());
    state.community = [];
    state.pot = 0;
    state.currentBet = 0;
    state.lastAction = '';
    state.showdownResults = null;
    state.minRaise = BIG_BLIND;
    state.raiseAmount = BIG_BLIND;
    state.eliminated = [];

    for (var i = 0; i < state.seatOrder.length; i++) {
        var s = state.seats[state.seatOrder[i]];
        if (!s) continue;
        s.hand = [];
        s.folded = s.bustedOut;
        s.bet = 0;
        s.allIn = false;
    }

    state.dealerIdx = nextActiveSeat(state.dealerIdx);

    var sbIdx = active.length === 2 ? state.dealerIdx : nextActiveSeat(state.dealerIdx);
    var bbIdx = nextActiveSeat(sbIdx);
    postBlind(sbIdx, SMALL_BLIND);
    postBlind(bbIdx, BIG_BLIND);
    state.currentBet = BIG_BLIND;

    for (var d = 0; d < 2; d++) {
        for (var j = 0; j < active.length; j++) {
            var seat = state.seats[active[j]];
            if (!seat.folded) seat.hand.push(state.deck.pop());
        }
    }

    state.actionOn = nextCanActSeat(bbIdx);
    state.phase = 'preflop';
    state.actionTimer = ACTION_TIMEOUT;

    var dealerName = state.seats[state.seatOrder[state.dealerIdx]].name;
    log('Hand #' + state.handNum + ' -- Dealer: ' + dealerName + ' -- Blinds: ' + SMALL_BLIND + '/' + BIG_BLIND);
}

function postBlind(seatIdx, amount) {
    var id = state.seatOrder[seatIdx];
    var s = state.seats[id];
    var actual = Math.min(amount, s.chips);
    s.chips -= actual;
    s.bet = actual;
    state.pot += actual;
    if (s.chips === 0) s.allIn = true;
}

// ─── Betting Actions ───────────────────────────────────────────────────

function doFold(seatID) {
    var s = state.seats[seatID];
    if (!s) return;
    s.folded = true;
    state.lastAction = s.name + ' folds';
    log(state.lastAction);
    advanceAction();
}

function doCheck(seatID) {
    var s = state.seats[seatID];
    if (!s) return;
    state.lastAction = s.name + ' checks';
    log(state.lastAction);
    advanceAction();
}

function doCall(seatID) {
    var s = state.seats[seatID];
    if (!s) return;
    var toCall = state.currentBet - s.bet;
    var actual = Math.min(toCall, s.chips);
    s.chips -= actual;
    s.bet += actual;
    state.pot += actual;
    if (s.chips === 0) s.allIn = true;
    state.lastAction = s.name + ' calls' + (s.allIn ? ' (all-in)' : '');
    log(state.lastAction);
    advanceAction();
}

function doRaise(seatID, amount) {
    var s = state.seats[seatID];
    if (!s) return;
    var totalBet = state.currentBet + amount;
    var totalCost = totalBet - s.bet;
    var actual = Math.min(totalCost, s.chips);
    s.chips -= actual;
    s.bet += actual;
    state.pot += actual;
    if (s.chips === 0) s.allIn = true;
    if (s.bet > state.currentBet) {
        state.minRaise = s.bet - state.currentBet;
        state.currentBet = s.bet;
    }
    state.lastAction = s.name + ' raises to ' + state.currentBet + (s.allIn ? ' (all-in)' : '');
    log(state.lastAction);
    state.actionOn = nextCanActSeatExcluding(seatIndexOf(seatID), seatID);
    if (state.actionOn === -1) {
        finishBettingRound();
    } else {
        state.actionTimer = ACTION_TIMEOUT;
    }
}

function doAllIn(seatID) {
    var s = state.seats[seatID];
    if (!s) return;
    var amount = s.chips;
    s.chips = 0;
    s.bet += amount;
    state.pot += amount;
    s.allIn = true;
    if (s.bet > state.currentBet) {
        state.minRaise = Math.max(state.minRaise, s.bet - state.currentBet);
        state.currentBet = s.bet;
        state.lastAction = s.name + ' all-in for ' + s.bet;
        state.actionOn = nextCanActSeatExcluding(seatIndexOf(seatID), seatID);
        if (state.actionOn === -1) { finishBettingRound(); return; }
        state.actionTimer = ACTION_TIMEOUT;
    } else {
        state.lastAction = s.name + ' all-in for ' + s.bet;
        advanceAction();
    }
    log(state.lastAction);
}

function advanceAction() {
    var inHand = playersInHand();
    if (inHand.length === 1) {
        awardPot(inHand[0]);
        endHand();
        return;
    }
    var canAct = playersCanAct();
    if (canAct.length === 0) { finishBettingRound(); return; }

    var nextIdx = nextCanActSeat(state.actionOn);
    if (nextIdx === -1) { finishBettingRound(); return; }

    var nextID = state.seatOrder[nextIdx];
    var nextS = state.seats[nextID];

    var allMatched = true;
    for (var i = 0; i < canAct.length; i++) {
        if (state.seats[canAct[i]].bet < state.currentBet) { allMatched = false; break; }
    }
    if (allMatched && state.currentBet > 0 && nextS.bet === state.currentBet) {
        finishBettingRound();
        return;
    }

    state.actionOn = nextIdx;
    state.actionTimer = ACTION_TIMEOUT;
}

function finishBettingRound() {
    for (var i = 0; i < state.seatOrder.length; i++) {
        var s = state.seats[state.seatOrder[i]];
        if (s) s.bet = 0;
    }
    state.currentBet = 0;
    state.minRaise = BIG_BLIND;
    state.raiseAmount = BIG_BLIND;

    var inHand = playersInHand();
    if (inHand.length === 1) { awardPot(inHand[0]); endHand(); return; }

    if (state.phase === 'preflop') {
        state.phase = 'flop';
        state.deck.pop();
        state.community.push(state.deck.pop(), state.deck.pop(), state.deck.pop());
    } else if (state.phase === 'flop') {
        state.phase = 'turn';
        state.deck.pop();
        state.community.push(state.deck.pop());
    } else if (state.phase === 'turn') {
        state.phase = 'river';
        state.deck.pop();
        state.community.push(state.deck.pop());
    } else if (state.phase === 'river') {
        doShowdown();
        return;
    }

    var canAct = playersCanAct();
    if (canAct.length <= 1) { finishBettingRound(); return; }
    state.actionOn = nextCanActSeat(state.dealerIdx);
    state.actionTimer = ACTION_TIMEOUT;
}

// ─── Showdown ──────────────────────────────────────────────────────────

function doShowdown() {
    state.phase = 'showdown';
    var inHand = playersInHand();
    var results = [];
    for (var i = 0; i < inHand.length; i++) {
        var id = inHand[i];
        var s = state.seats[id];
        var allCards = s.hand.concat(state.community);
        var eval_ = evaluateHand(allCards);
        results.push({ id: id, name: s.name, hand: eval_, cards: s.hand });
    }
    results.sort(function(a, b) { return compareHands(b.hand, a.hand); });

    var winners = [results[0]];
    for (var j = 1; j < results.length; j++) {
        if (compareHands(results[j].hand, results[0].hand) === 0) winners.push(results[j]);
        else break;
    }

    var share = Math.floor(state.pot / winners.length);
    var remainder = state.pot - share * winners.length;
    var winMsg = '';
    for (var w = 0; w < winners.length; w++) {
        var ws = state.seats[winners[w].id];
        ws.chips += share + (w === 0 ? remainder : 0);
        if (winMsg) winMsg += ', ';
        winMsg += ws.name;
    }
    winMsg += ' wins ' + state.pot + ' with ' + results[0].hand.name;
    state.lastWinMsg = winMsg;
    chat(winMsg);
    log(winMsg);
    state.showdownResults = results;
    state.showdownTimer = 50;
}

function awardPot(winnerID) {
    var s = state.seats[winnerID];
    s.chips += state.pot;
    state.lastWinMsg = s.name + ' wins ' + state.pot;
    chat(state.lastWinMsg);
    log(state.lastWinMsg);
    state.pot = 0;
}

function endHand() {
    state.phase = 'between';
    state.showdownTimer = 30;
    state.eliminated = [];
    for (var i = 0; i < state.seatOrder.length; i++) {
        var s = state.seats[state.seatOrder[i]];
        if (s && s.chips <= 0 && !s.bustedOut) {
            s.bustedOut = true;
            state.eliminated.push(s.name);
            chat(s.name + ' is eliminated!');
        }
    }
}

// ─── AI ────────────────────────────────────────────────────────────────

function addAIPlayer() {
    var usedNames = {};
    for (var i = 0; i < state.seatOrder.length; i++) {
        var s = state.seats[state.seatOrder[i]];
        if (s) usedNames[s.name] = true;
    }
    var name = null;
    for (var n = 0; n < AI_NAMES.length; n++) {
        if (!usedNames[AI_NAMES[n]]) { name = AI_NAMES[n]; break; }
    }
    if (!name) name = 'Bot' + Math.floor(Math.random() * 1000);
    var id = AI_PREFIX + name.toLowerCase();
    state.seats[id] = {
        name: name, chips: STARTING_CHIPS, hand: [], folded: false,
        bet: 0, allIn: false, bustedOut: false, isAI: true, aiThinkTimer: 0
    };
    state.seatOrder.push(id);
    chat(name + ' (bot) sits down');
}

function fillAIPlayers() {
    while (activePlayers().length < MIN_SEATS) addAIPlayer();
}

function aiDecide(seatID) {
    var s = state.seats[seatID];
    if (!s || s.folded || s.allIn) return;
    var toCall = state.currentBet - s.bet;
    var strength = aiHandStrength(s.hand, state.community);
    var potOdds = toCall > 0 ? toCall / (state.pot + toCall) : 0;
    var rand = Math.random();
    var aggression = ((seatID.charCodeAt(seatID.length - 1) % 5) - 2) * 0.05;
    strength += aggression;

    if (toCall === 0) {
        if (strength > 0.65 && rand < 0.5) {
            var raiseAmt = Math.max(state.minRaise, Math.floor(state.pot * (0.3 + strength * 0.5)));
            raiseAmt = Math.min(raiseAmt, s.chips);
            if (strength > 0.85 && rand < 0.15) doAllIn(seatID);
            else doRaise(seatID, raiseAmt);
        } else {
            doCheck(seatID);
        }
    } else {
        var callRatio = toCall / s.chips;
        if (strength > 0.8 && rand < 0.3) {
            if (strength > 0.9 && rand < 0.1) doAllIn(seatID);
            else {
                var r2 = Math.max(state.minRaise, Math.floor(state.pot * 0.5));
                r2 = Math.min(r2, s.chips - toCall);
                if (r2 >= state.minRaise) doRaise(seatID, r2);
                else doCall(seatID);
            }
        } else if (strength > potOdds + 0.1 || (callRatio < 0.1 && strength > 0.25)) {
            doCall(seatID);
        } else if (strength > 0.4 && rand < 0.3) {
            doCall(seatID);
        } else {
            doFold(seatID);
        }
    }
}

// ─── Tick ──────────────────────────────────────────────────────────────

function tick() {
    if (state.phase === 'showdown' || state.phase === 'between') {
        state.showdownTimer--;
        if (state.showdownTimer <= 0) {
            endHand();
            if (state.phase === 'between') {
                state.showdownTimer = 0;
                startHand();
            }
        }
        return;
    }
    if (state.phase === 'waiting') return;

    if (state.actionOn >= 0 && state.actionOn < state.seatOrder.length) {
        var id = state.seatOrder[state.actionOn];
        var s = id ? state.seats[id] : null;

        if (s && isAI(id) && !s.folded && !s.allIn) {
            if (!s.aiThinkTimer) {
                s.aiThinkTimer = AI_THINK_MIN + Math.floor(Math.random() * (AI_THINK_MAX - AI_THINK_MIN));
            }
            s.aiThinkTimer--;
            if (s.aiThinkTimer <= 0) {
                s.aiThinkTimer = 0;
                aiDecide(id);
            }
        } else if (state.actionTimer > 0) {
            state.actionTimer--;
            if (state.actionTimer <= 0) {
                if (s) {
                    if (s.bet >= state.currentBet) doCheck(id);
                    else doFold(id);
                }
            }
        }
    }
}

// ─── Game Object ───────────────────────────────────────────────────────

var Game = {
    gameName: "Texas Hold'em",

    init: function(savedState) {
        var t = teams();
        var playerCount = 0;
        for (var i = 0; i < t.length; i++) playerCount += t[i].players.length;

        var splash = figlet("Hold'em", "standard");
        splash += '\n\nTeam-based Texas Hold\'em Poker';
        splash += '\nEach team shares one hand. First to act controls.';
        splash += '\n\nTeams: ' + t.length + '  Players: ' + playerCount;
        Game.splashScreen = splash;
    },

    start: function() {
        var t = teams();
        for (var i = 0; i < t.length; i++) {
            var team = t[i];
            var seatID = 'team_' + i;
            state.seats[seatID] = {
                name: team.name, chips: STARTING_CHIPS, hand: [], folded: false,
                bet: 0, allIn: false, bustedOut: false, isAI: false, aiThinkTimer: 0
            };
            state.seatOrder.push(seatID);
            state.teamToSeat[team.name] = seatID;
            for (var j = 0; j < team.players.length; j++) {
                state.playerToTeam[team.players[j].id] = team.name;
            }
        }

        fillAIPlayers();
        if (activePlayers().length >= 2) startHand();
        log("Hold'em started with " + state.seatOrder.length + ' seats (' + t.length + ' teams)');
    },

    onPlayerLeave: function(playerID) {
        // Teams persist; individual player leaving doesn't eliminate the team
        var seatID = playerSeat(playerID);
        if (seatID) {
            var seat = state.seats[seatID];
            if (seat) {
                // Check if all team members are gone (would need team tracking)
                // For now, team stays alive
            }
        }
    },

    onInput: function(playerID, key) {
        var seatID = playerSeat(playerID);
        if (!seatID) return;
        var seat = state.seats[seatID];
        if (!seat || seat.bustedOut) return;

        if (state.phase === 'showdown' || state.phase === 'between' || state.phase === 'waiting') return;
        if (state.actionOn < 0 || state.seatOrder[state.actionOn] !== seatID) return;
        if (seat.folded || seat.allIn) return;

        var toCall = state.currentBet - seat.bet;

        if (key === 'f' || key === 'F') {
            if (toCall > 0) doFold(seatID);
        } else if (key === 'c' || key === 'C' || key === ' ') {
            if (toCall > 0) doCall(seatID);
            else doCheck(seatID);
        } else if (key === 'r' || key === 'R') {
            if (seat.chips > toCall) doRaise(seatID, state.raiseAmount);
        } else if (key === 'a' || key === 'A') {
            doAllIn(seatID);
        } else if (key === 'up') {
            state.raiseAmount = Math.min(seat.chips - toCall, state.raiseAmount + BIG_BLIND);
        } else if (key === 'down') {
            state.raiseAmount = Math.max(state.minRaise, state.raiseAmount - BIG_BLIND);
        }
    },

    update: function(dt) {
        tick();
    },

    render: function(playerID, width, height) {
        return '';
    },

    renderNC: function(playerID, width, height) {
        var seatID = playerSeat(playerID);
        if (!seatID) {
            if (state.seatOrder.length > 0) seatID = state.seatOrder[0];
            else return { type: 'label', text: 'Waiting...', align: 'center' };
        }
        return buildViewNC(seatID, width, height);
    },

    statusBar: function(playerID) {
        var seatID = playerSeat(playerID);
        return renderStatusBar(seatID);
    },

    commandBar: function(playerID) {
        var seatID = playerSeat(playerID);
        return renderCommandBar(seatID);
    }
};

// ─── Commands ──────────────────────────────────────────────────────────

registerCommand({
    name: 'chips',
    description: 'Show chip counts for all seats',
    handler: function(playerID, isAdmin, args) {
        var msg = 'Chip counts:';
        for (var i = 0; i < state.seatOrder.length; i++) {
            var s = state.seats[state.seatOrder[i]];
            if (s) msg += '\n  ' + s.name + ': $' + s.chips + (s.bustedOut ? ' (out)' : '');
        }
        chatPlayer(playerID, msg);
    }
});

registerCommand({
    name: 'newgame',
    description: 'Reset and start a new tournament (admin only)',
    adminOnly: true,
    handler: function(playerID, isAdmin, args) {
        state.phase = 'waiting';
        state.handNum = 0;
        state.pot = 0;
        state.community = [];
        state.lastWinMsg = '';
        SMALL_BLIND = 10;
        BIG_BLIND = 20;
        for (var i = 0; i < state.seatOrder.length; i++) {
            var s = state.seats[state.seatOrder[i]];
            if (s) {
                s.chips = STARTING_CHIPS;
                s.bustedOut = false;
                s.hand = [];
                s.folded = false;
                s.bet = 0;
                s.allIn = false;
            }
        }
        chat('New tournament started! All seats reset to $' + STARTING_CHIPS);
        fillAIPlayers();
        if (activePlayers().length >= 2) startHand();
    }
});
