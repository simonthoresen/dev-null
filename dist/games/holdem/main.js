// main.js — Texas Hold'em for dev-null (NC-panel UI, team-based)
// Each team shares one hand. First team member to act controls the decision.

include("poker");
include("ui");

// ─── Game Constants ────────────────────────────────────────────────────
var STARTING_CHIPS = 1000;
var SMALL_BLIND = 10;
var BIG_BLIND = 20;
var BLIND_INCREASE_HANDS = 10;
var ACTION_TIMEOUT = 30; // seconds
var MIN_SEATS = 4;
var AI_NAMES = ['Ace', 'Bluff', 'Cash', 'Dice', 'Edge', 'Flint', 'Grit', 'Hawk', 'Iron', 'Jinx'];
var AI_PREFIX = 'ai_';
var AI_THINK_MIN = 1.0; // seconds
var AI_THINK_MAX = 3.0; // seconds

// ─── Game State ────────────────────────────────────────────────────────

// ─── Seat Management ───────────────────────────────────────────────────

function activePlayers() {
    return Game.state.seatOrder.filter(function(id) {
        var s = Game.state.seats[id];
        return s && !s.bustedOut;
    });
}

function playersInHand() {
    return Game.state.seatOrder.filter(function(id) {
        var s = Game.state.seats[id];
        return s && !s.folded && !s.bustedOut;
    });
}

function playersCanAct() {
    return Game.state.seatOrder.filter(function(id) {
        var s = Game.state.seats[id];
        return s && !s.folded && !s.bustedOut && !s.allIn;
    });
}

function seatIndexOf(seatID) {
    return Game.state.seatOrder.indexOf(seatID);
}

function nextActiveSeat(fromIdx) {
    var active = activePlayers();
    for (var i = 1; i <= Game.state.seatOrder.length; i++) {
        var idx = (fromIdx + i) % Game.state.seatOrder.length;
        if (active.indexOf(Game.state.seatOrder[idx]) >= 0) return idx;
    }
    return fromIdx;
}

function nextCanActSeat(fromIdx) {
    var canAct = playersCanAct();
    for (var i = 1; i <= Game.state.seatOrder.length; i++) {
        var idx = (fromIdx + i) % Game.state.seatOrder.length;
        if (canAct.indexOf(Game.state.seatOrder[idx]) >= 0) return idx;
    }
    return -1;
}

function nextCanActSeatExcluding(fromIdx, excludeID) {
    var canAct = playersCanAct();
    for (var i = 1; i <= Game.state.seatOrder.length; i++) {
        var idx = (fromIdx + i) % Game.state.seatOrder.length;
        var id = Game.state.seatOrder[idx];
        if (id !== excludeID && canAct.indexOf(id) >= 0) return idx;
    }
    return -1;
}

function isAI(seatID) {
    return seatID.indexOf(AI_PREFIX) === 0;
}

// Resolve a playerID to their team's seatID
function playerSeat(playerID) {
    var teamName = Game.state.playerToTeam[playerID];
    if (teamName) return Game.state.teamToSeat[teamName];
    return null;
}

// ─── Deal a New Hand ───────────────────────────────────────────────────

function startHand() {
    var active = activePlayers();
    if (active.length < 2) {
        Game.state.phase = 'waiting';
        if (active.length === 1) {
            var winner = Game.state.seats[active[0]];
            Game.state.lastWinMsg = winner.name + ' wins the tournament!';
            chat(winner.name + ' wins the tournament!');
        }
        return;
    }

    Game.state.handNum++;
    var level = Math.floor((Game.state.handNum - 1) / BLIND_INCREASE_HANDS);
    SMALL_BLIND = 10 * Math.pow(2, level);
    BIG_BLIND = SMALL_BLIND * 2;

    Game.state.deck = shuffle(makeDeck());
    Game.state.community = [];
    Game.state.pot = 0;
    Game.state.currentBet = 0;
    Game.state.lastAction = '';
    Game.state.showdownResults = null;
    Game.state.minRaise = BIG_BLIND;
    Game.state.raiseAmount = BIG_BLIND;
    Game.state.eliminated = [];

    for (var i = 0; i < Game.state.seatOrder.length; i++) {
        var s = Game.state.seats[Game.state.seatOrder[i]];
        if (!s) continue;
        s.hand = [];
        s.folded = s.bustedOut;
        s.bet = 0;
        s.allIn = false;
    }

    Game.state.dealerIdx = nextActiveSeat(Game.state.dealerIdx);

    var sbIdx = active.length === 2 ? Game.state.dealerIdx : nextActiveSeat(Game.state.dealerIdx);
    var bbIdx = nextActiveSeat(sbIdx);
    postBlind(sbIdx, SMALL_BLIND);
    postBlind(bbIdx, BIG_BLIND);
    Game.state.currentBet = BIG_BLIND;

    for (var d = 0; d < 2; d++) {
        for (var j = 0; j < active.length; j++) {
            var seat = Game.state.seats[active[j]];
            if (!seat.folded) seat.hand.push(Game.state.deck.pop());
        }
    }

    Game.state.actionOn = nextCanActSeat(bbIdx);
    Game.state.phase = 'preflop';
    Game.state.actionTimer = ACTION_TIMEOUT;

    var dealerName = Game.state.seats[Game.state.seatOrder[Game.state.dealerIdx]].name;
    log('Hand #' + Game.state.handNum + ' -- Dealer: ' + dealerName + ' -- Blinds: ' + SMALL_BLIND + '/' + BIG_BLIND);
}

function postBlind(seatIdx, amount) {
    var id = Game.state.seatOrder[seatIdx];
    var s = Game.state.seats[id];
    var actual = Math.min(amount, s.chips);
    s.chips -= actual;
    s.bet = actual;
    Game.state.pot += actual;
    if (s.chips === 0) s.allIn = true;
}

// ─── Betting Actions ───────────────────────────────────────────────────

function doFold(seatID) {
    var s = Game.state.seats[seatID];
    if (!s) return;
    s.folded = true;
    Game.state.lastAction = s.name + ' folds';
    log(Game.state.lastAction);
    advanceAction();
}

function doCheck(seatID) {
    var s = Game.state.seats[seatID];
    if (!s) return;
    Game.state.lastAction = s.name + ' checks';
    log(Game.state.lastAction);
    advanceAction();
}

function doCall(seatID) {
    var s = Game.state.seats[seatID];
    if (!s) return;
    var toCall = Game.state.currentBet - s.bet;
    var actual = Math.min(toCall, s.chips);
    s.chips -= actual;
    s.bet += actual;
    Game.state.pot += actual;
    if (s.chips === 0) s.allIn = true;
    Game.state.lastAction = s.name + ' calls' + (s.allIn ? ' (all-in)' : '');
    log(Game.state.lastAction);
    advanceAction();
}

function doRaise(seatID, amount) {
    var s = Game.state.seats[seatID];
    if (!s) return;
    var totalBet = Game.state.currentBet + amount;
    var totalCost = totalBet - s.bet;
    var actual = Math.min(totalCost, s.chips);
    s.chips -= actual;
    s.bet += actual;
    Game.state.pot += actual;
    if (s.chips === 0) s.allIn = true;
    if (s.bet > Game.state.currentBet) {
        Game.state.minRaise = s.bet - Game.state.currentBet;
        Game.state.currentBet = s.bet;
    }
    Game.state.lastAction = s.name + ' raises to ' + Game.state.currentBet + (s.allIn ? ' (all-in)' : '');
    log(Game.state.lastAction);
    Game.state.actionOn = nextCanActSeatExcluding(seatIndexOf(seatID), seatID);
    if (Game.state.actionOn === -1) {
        finishBettingRound();
    } else {
        Game.state.actionTimer = ACTION_TIMEOUT;
    }
}

function doAllIn(seatID) {
    var s = Game.state.seats[seatID];
    if (!s) return;
    var amount = s.chips;
    s.chips = 0;
    s.bet += amount;
    Game.state.pot += amount;
    s.allIn = true;
    if (s.bet > Game.state.currentBet) {
        Game.state.minRaise = Math.max(Game.state.minRaise, s.bet - Game.state.currentBet);
        Game.state.currentBet = s.bet;
        Game.state.lastAction = s.name + ' all-in for ' + s.bet;
        Game.state.actionOn = nextCanActSeatExcluding(seatIndexOf(seatID), seatID);
        if (Game.state.actionOn === -1) { finishBettingRound(); return; }
        Game.state.actionTimer = ACTION_TIMEOUT;
    } else {
        Game.state.lastAction = s.name + ' all-in for ' + s.bet;
        advanceAction();
    }
    log(Game.state.lastAction);
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

    var nextIdx = nextCanActSeat(Game.state.actionOn);
    if (nextIdx === -1) { finishBettingRound(); return; }

    var nextID = Game.state.seatOrder[nextIdx];
    var nextS = Game.state.seats[nextID];

    var allMatched = true;
    for (var i = 0; i < canAct.length; i++) {
        if (Game.state.seats[canAct[i]].bet < Game.state.currentBet) { allMatched = false; break; }
    }
    if (allMatched && Game.state.currentBet > 0 && nextS.bet === Game.state.currentBet) {
        finishBettingRound();
        return;
    }

    Game.state.actionOn = nextIdx;
    Game.state.actionTimer = ACTION_TIMEOUT;
}

function finishBettingRound() {
    for (var i = 0; i < Game.state.seatOrder.length; i++) {
        var s = Game.state.seats[Game.state.seatOrder[i]];
        if (s) s.bet = 0;
    }
    Game.state.currentBet = 0;
    Game.state.minRaise = BIG_BLIND;
    Game.state.raiseAmount = BIG_BLIND;

    var inHand = playersInHand();
    if (inHand.length === 1) { awardPot(inHand[0]); endHand(); return; }

    if (Game.state.phase === 'preflop') {
        Game.state.phase = 'flop';
        Game.state.deck.pop();
        Game.state.community.push(Game.state.deck.pop(), Game.state.deck.pop(), Game.state.deck.pop());
    } else if (Game.state.phase === 'flop') {
        Game.state.phase = 'turn';
        Game.state.deck.pop();
        Game.state.community.push(Game.state.deck.pop());
    } else if (Game.state.phase === 'turn') {
        Game.state.phase = 'river';
        Game.state.deck.pop();
        Game.state.community.push(Game.state.deck.pop());
    } else if (Game.state.phase === 'river') {
        doShowdown();
        return;
    }

    var canAct = playersCanAct();
    if (canAct.length <= 1) { finishBettingRound(); return; }
    Game.state.actionOn = nextCanActSeat(Game.state.dealerIdx);
    Game.state.actionTimer = ACTION_TIMEOUT;
}

// ─── Showdown ──────────────────────────────────────────────────────────

function doShowdown() {
    Game.state.phase = 'showdown';
    var inHand = playersInHand();
    var results = [];
    for (var i = 0; i < inHand.length; i++) {
        var id = inHand[i];
        var s = Game.state.seats[id];
        var allCards = s.hand.concat(Game.state.community);
        var eval_ = evaluateHand(allCards);
        results.push({ id: id, name: s.name, hand: eval_, cards: s.hand });
    }
    results.sort(function(a, b) { return compareHands(b.hand, a.hand); });

    var winners = [results[0]];
    for (var j = 1; j < results.length; j++) {
        if (compareHands(results[j].hand, results[0].hand) === 0) winners.push(results[j]);
        else break;
    }

    var share = Math.floor(Game.state.pot / winners.length);
    var remainder = Game.state.pot - share * winners.length;
    var winMsg = '';
    for (var w = 0; w < winners.length; w++) {
        var ws = Game.state.seats[winners[w].id];
        ws.chips += share + (w === 0 ? remainder : 0);
        if (winMsg) winMsg += ', ';
        winMsg += ws.name;
    }
    winMsg += ' wins ' + Game.state.pot + ' with ' + results[0].hand.name;
    Game.state.lastWinMsg = winMsg;
    chat(winMsg);
    log(winMsg);
    Game.state.showdownResults = results;
    Game.state.showdownTimer = 5;
}

function awardPot(winnerID) {
    var s = Game.state.seats[winnerID];
    s.chips += Game.state.pot;
    Game.state.lastWinMsg = s.name + ' wins ' + Game.state.pot;
    chat(Game.state.lastWinMsg);
    log(Game.state.lastWinMsg);
    Game.state.pot = 0;
}

function endHand() {
    Game.state.phase = 'between';
    Game.state.showdownTimer = 3;
    Game.state.eliminated = [];
    for (var i = 0; i < Game.state.seatOrder.length; i++) {
        var s = Game.state.seats[Game.state.seatOrder[i]];
        if (s && s.chips <= 0 && !s.bustedOut) {
            s.bustedOut = true;
            Game.state.eliminated.push(s.name);
            chat(s.name + ' is eliminated!');
        }
    }
}

// ─── AI ────────────────────────────────────────────────────────────────

function addAIPlayer() {
    var usedNames = {};
    for (var i = 0; i < Game.state.seatOrder.length; i++) {
        var s = Game.state.seats[Game.state.seatOrder[i]];
        if (s) usedNames[s.name] = true;
    }
    var name = null;
    for (var n = 0; n < AI_NAMES.length; n++) {
        if (!usedNames[AI_NAMES[n]]) { name = AI_NAMES[n]; break; }
    }
    if (!name) name = 'Bot' + Math.floor(Math.random() * 1000);
    var id = AI_PREFIX + name.toLowerCase();
    Game.state.seats[id] = {
        name: name, chips: STARTING_CHIPS, hand: [], folded: false,
        bet: 0, allIn: false, bustedOut: false, isAI: true, aiThinkTimer: 0
    };
    Game.state.seatOrder.push(id);
    chat(name + ' (bot) sits down');
}

function fillAIPlayers() {
    while (activePlayers().length < MIN_SEATS) addAIPlayer();
}

function aiDecide(seatID) {
    var s = Game.state.seats[seatID];
    if (!s || s.folded || s.allIn) return;
    var toCall = Game.state.currentBet - s.bet;
    var strength = aiHandStrength(s.hand, Game.state.community);
    var potOdds = toCall > 0 ? toCall / (Game.state.pot + toCall) : 0;
    var rand = Math.random();
    var aggression = ((seatID.charCodeAt(seatID.length - 1) % 5) - 2) * 0.05;
    strength += aggression;

    if (toCall === 0) {
        if (strength > 0.65 && rand < 0.5) {
            var raiseAmt = Math.max(Game.state.minRaise, Math.floor(Game.state.pot * (0.3 + strength * 0.5)));
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
                var r2 = Math.max(Game.state.minRaise, Math.floor(Game.state.pot * 0.5));
                r2 = Math.min(r2, s.chips - toCall);
                if (r2 >= Game.state.minRaise) doRaise(seatID, r2);
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

function tick(dt) {
    if (Game.state.phase === 'showdown' || Game.state.phase === 'between') {
        Game.state.showdownTimer -= dt;
        if (Game.state.showdownTimer <= 0) {
            endHand();
            if (Game.state.phase === 'between') {
                Game.state.showdownTimer = 0;
                startHand();
            }
        }
        return;
    }
    if (Game.state.phase === 'waiting') return;

    if (Game.state.actionOn >= 0 && Game.state.actionOn < Game.state.seatOrder.length) {
        var id = Game.state.seatOrder[Game.state.actionOn];
        var s = id ? Game.state.seats[id] : null;

        if (s && isAI(id) && !s.folded && !s.allIn) {
            if (!s.aiThinkTimer) {
                s.aiThinkTimer = AI_THINK_MIN + Math.random() * (AI_THINK_MAX - AI_THINK_MIN);
            }
            s.aiThinkTimer -= dt;
            if (s.aiThinkTimer <= 0) {
                s.aiThinkTimer = 0;
                aiDecide(id);
            }
        } else if (Game.state.actionTimer > 0) {
            Game.state.actionTimer -= dt;
            if (Game.state.actionTimer <= 0) {
                if (s) {
                    if (s.bet >= Game.state.currentBet) doCheck(id);
                    else doFold(id);
                }
            }
        }
    }
}

// ─── Game Object ───────────────────────────────────────────────────────

var Game = {
    gameName: "Texas Hold'em",

    state: {
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
    },

    load: function(savedState) {
        var t = teams();
        var playerCount = 0;
        for (var i = 0; i < t.length; i++) playerCount += t[i].players.length;

        var splash = figlet("Hold'em", "standard");
        splash += '\n\nTeam-based Texas Hold\'em Poker';
        splash += '\nEach team shares one hand. First to act controls.';
        splash += '\n\nTeams: ' + t.length + '  Players: ' + playerCount;
        Game.splashScreen = splash;
    },

    begin: function() {
        var t = teams();
        for (var i = 0; i < t.length; i++) {
            var team = t[i];
            var seatID = 'team_' + i;
            Game.state.seats[seatID] = {
                name: team.name, chips: STARTING_CHIPS, hand: [], folded: false,
                bet: 0, allIn: false, bustedOut: false, isAI: false, aiThinkTimer: 0
            };
            Game.state.seatOrder.push(seatID);
            Game.state.teamToSeat[team.name] = seatID;
            for (var j = 0; j < team.players.length; j++) {
                Game.state.playerToTeam[team.players[j].id] = team.name;
            }
        }

        fillAIPlayers();
        if (activePlayers().length >= 2) startHand();
        log("Hold'em started with " + Game.state.seatOrder.length + ' seats (' + t.length + ' teams)');
    },

    onPlayerLeave: function(playerID) {
        // Teams persist; individual player leaving doesn't eliminate the team
        var seatID = playerSeat(playerID);
        if (seatID) {
            var seat = Game.state.seats[seatID];
            if (seat) {
                // Check if all team members are gone (would need team tracking)
                // For now, team stays alive
            }
        }
    },

    onInput: function(playerID, key) {
        var seatID = playerSeat(playerID);
        if (!seatID) return;
        var seat = Game.state.seats[seatID];
        if (!seat || seat.bustedOut) return;

        if (Game.state.phase === 'showdown' || Game.state.phase === 'between' || Game.state.phase === 'waiting') return;
        if (Game.state.actionOn < 0 || Game.state.seatOrder[Game.state.actionOn] !== seatID) return;
        if (seat.folded || seat.allIn) return;

        var toCall = Game.state.currentBet - seat.bet;

        if (key === 'f' || key === 'F') {
            if (toCall > 0) doFold(seatID);
        } else if (key === 'c' || key === 'C' || key === ' ') {
            if (toCall > 0) doCall(seatID);
            else doCheck(seatID);
        } else if (key === 'r' || key === 'R') {
            if (seat.chips > toCall) doRaise(seatID, Game.state.raiseAmount);
        } else if (key === 'a' || key === 'A') {
            doAllIn(seatID);
        } else if (key === 'up') {
            Game.state.raiseAmount = Math.min(seat.chips - toCall, Game.state.raiseAmount + BIG_BLIND);
        } else if (key === 'down') {
            Game.state.raiseAmount = Math.max(Game.state.minRaise, Game.state.raiseAmount - BIG_BLIND);
        }
    },

    update: function(dt) {
        tick(dt);
    },

    renderAscii: function(buf, playerID, ox, oy, width, height) {
        // holdem uses layout exclusively
    },

    layout: function(playerID, width, height) {
        var seatID = playerSeat(playerID);
        if (!seatID) {
            if (Game.state.seatOrder.length > 0) seatID = Game.state.seatOrder[0];
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
        for (var i = 0; i < Game.state.seatOrder.length; i++) {
            var s = Game.state.seats[Game.state.seatOrder[i]];
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
        Game.state.phase = 'waiting';
        Game.state.handNum = 0;
        Game.state.pot = 0;
        Game.state.community = [];
        Game.state.lastWinMsg = '';
        SMALL_BLIND = 10;
        BIG_BLIND = 20;
        for (var i = 0; i < Game.state.seatOrder.length; i++) {
            var s = Game.state.seats[Game.state.seatOrder[i]];
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
