// main.js — Texas Hold'em for dev-null (NC-panel UI, team-based)
// Each team shares one hand. First team member to act controls the decision.

include("poker");
include("ui");

// ─── Game Constants ────────────────────────────────────────────────────
var STARTING_CHIPS = 1000;
var INITIAL_SMALL_BLIND = 10;
var INITIAL_BIG_BLIND = 20;
var BLIND_INCREASE_HANDS = 10;
var ACTION_TIMEOUT = 30; // seconds
var MIN_SEATS = 4;

// All mutable game state lives on Game.state; _s is rebound at each hook
// entry so existing helpers reference _s.* (including the blind-level
// scalars, which raise as play progresses).
var _s = null;

function initialState() {
    return {
        seats: {}, seatOrder: [], teamToSeat: {}, playerToTeam: {},
        phase: 'waiting',
        deck: [], community: [],
        pot: 0, currentBet: 0,
        actionOn: -1, dealerIdx: 0, handNum: 0,
        lastAction: '', showdownTimer: 0, showdownResults: null,
        actionTimer: 0,
        minRaise: INITIAL_BIG_BLIND, raiseAmount: INITIAL_BIG_BLIND,
        lastWinMsg: '', eliminated: [],
        SMALL_BLIND: INITIAL_SMALL_BLIND,
        BIG_BLIND: INITIAL_BIG_BLIND
    };
}
var AI_NAMES = ['Ace', 'Bluff', 'Cash', 'Dice', 'Edge', 'Flint', 'Grit', 'Hawk', 'Iron', 'Jinx'];
var AI_PREFIX = 'ai_';
var AI_THINK_MIN = 1.0; // seconds
var AI_THINK_MAX = 3.0; // seconds

// ─── Game State ────────────────────────────────────────────────────────

// ─── Seat Management ───────────────────────────────────────────────────

function activePlayers() {
    return _s.seatOrder.filter(function(id) {
        var s = _s.seats[id];
        return s && !s.bustedOut;
    });
}

function playersInHand() {
    return _s.seatOrder.filter(function(id) {
        var s = _s.seats[id];
        return s && !s.folded && !s.bustedOut;
    });
}

function playersCanAct() {
    return _s.seatOrder.filter(function(id) {
        var s = _s.seats[id];
        return s && !s.folded && !s.bustedOut && !s.allIn;
    });
}

function seatIndexOf(seatID) {
    return _s.seatOrder.indexOf(seatID);
}

function nextActiveSeat(fromIdx) {
    var active = activePlayers();
    for (var i = 1; i <= _s.seatOrder.length; i++) {
        var idx = (fromIdx + i) % _s.seatOrder.length;
        if (active.indexOf(_s.seatOrder[idx]) >= 0) return idx;
    }
    return fromIdx;
}

function nextCanActSeat(fromIdx) {
    var canAct = playersCanAct();
    for (var i = 1; i <= _s.seatOrder.length; i++) {
        var idx = (fromIdx + i) % _s.seatOrder.length;
        if (canAct.indexOf(_s.seatOrder[idx]) >= 0) return idx;
    }
    return -1;
}

function nextCanActSeatExcluding(fromIdx, excludeID) {
    var canAct = playersCanAct();
    for (var i = 1; i <= _s.seatOrder.length; i++) {
        var idx = (fromIdx + i) % _s.seatOrder.length;
        var id = _s.seatOrder[idx];
        if (id !== excludeID && canAct.indexOf(id) >= 0) return idx;
    }
    return -1;
}

function isAI(seatID) {
    return seatID.indexOf(AI_PREFIX) === 0;
}

// Resolve a playerID to their team's seatID
function playerSeat(playerID) {
    var teamName = _s.playerToTeam[playerID];
    if (teamName) return _s.teamToSeat[teamName];
    return null;
}

// ─── Deal a New Hand ───────────────────────────────────────────────────

function startHand() {
    var active = activePlayers();
    if (active.length < 2) {
        _s.phase = 'waiting';
        if (active.length === 1) {
            var winner = _s.seats[active[0]];
            _s.lastWinMsg = winner.name + ' wins the tournament!';
            Game._ctx.chat(winner.name + ' wins the tournament!');
        }
        return;
    }

    _s.handNum++;
    var level = Math.floor((_s.handNum - 1) / BLIND_INCREASE_HANDS);
    _s.SMALL_BLIND = 10 * Math.pow(2, level);
    _s.BIG_BLIND = _s.SMALL_BLIND * 2;

    _s.deck = shuffle(makeDeck());
    _s.community = [];
    _s.pot = 0;
    _s.currentBet = 0;
    _s.lastAction = '';
    _s.showdownResults = null;
    _s.minRaise = _s.BIG_BLIND;
    _s.raiseAmount = _s.BIG_BLIND;
    _s.eliminated = [];

    for (var i = 0; i < _s.seatOrder.length; i++) {
        var s = _s.seats[_s.seatOrder[i]];
        if (!s) continue;
        s.hand = [];
        s.folded = s.bustedOut;
        s.bet = 0;
        s.allIn = false;
    }

    _s.dealerIdx = nextActiveSeat(_s.dealerIdx);

    var sbIdx = active.length === 2 ? _s.dealerIdx : nextActiveSeat(_s.dealerIdx);
    var bbIdx = nextActiveSeat(sbIdx);
    postBlind(sbIdx, _s.SMALL_BLIND);
    postBlind(bbIdx, _s.BIG_BLIND);
    _s.currentBet = _s.BIG_BLIND;

    for (var d = 0; d < 2; d++) {
        for (var j = 0; j < active.length; j++) {
            var seat = _s.seats[active[j]];
            if (!seat.folded) seat.hand.push(_s.deck.pop());
        }
    }

    _s.actionOn = nextCanActSeat(bbIdx);
    _s.phase = 'preflop';
    _s.actionTimer = ACTION_TIMEOUT;

    var dealerName = _s.seats[_s.seatOrder[_s.dealerIdx]].name;
    Game._ctx.log('Hand #' + _s.handNum + ' -- Dealer: ' + dealerName + ' -- Blinds: ' + _s.SMALL_BLIND + '/' + _s.BIG_BLIND);
}

function postBlind(seatIdx, amount) {
    var id = _s.seatOrder[seatIdx];
    var s = _s.seats[id];
    var actual = Math.min(amount, s.chips);
    s.chips -= actual;
    s.bet = actual;
    _s.pot += actual;
    if (s.chips === 0) s.allIn = true;
}

// ─── Betting Actions ───────────────────────────────────────────────────

function doFold(seatID) {
    var s = _s.seats[seatID];
    if (!s) return;
    s.folded = true;
    _s.lastAction = s.name + ' folds';
    Game._ctx.log(_s.lastAction);
    advanceAction();
}

function doCheck(seatID) {
    var s = _s.seats[seatID];
    if (!s) return;
    _s.lastAction = s.name + ' checks';
    Game._ctx.log(_s.lastAction);
    advanceAction();
}

function doCall(seatID) {
    var s = _s.seats[seatID];
    if (!s) return;
    var toCall = _s.currentBet - s.bet;
    var actual = Math.min(toCall, s.chips);
    s.chips -= actual;
    s.bet += actual;
    _s.pot += actual;
    if (s.chips === 0) s.allIn = true;
    _s.lastAction = s.name + ' calls' + (s.allIn ? ' (all-in)' : '');
    Game._ctx.log(_s.lastAction);
    advanceAction();
}

function doRaise(seatID, amount) {
    var s = _s.seats[seatID];
    if (!s) return;
    var totalBet = _s.currentBet + amount;
    var totalCost = totalBet - s.bet;
    var actual = Math.min(totalCost, s.chips);
    s.chips -= actual;
    s.bet += actual;
    _s.pot += actual;
    if (s.chips === 0) s.allIn = true;
    if (s.bet > _s.currentBet) {
        _s.minRaise = s.bet - _s.currentBet;
        _s.currentBet = s.bet;
    }
    _s.lastAction = s.name + ' raises to ' + _s.currentBet + (s.allIn ? ' (all-in)' : '');
    Game._ctx.log(_s.lastAction);
    _s.actionOn = nextCanActSeatExcluding(seatIndexOf(seatID), seatID);
    if (_s.actionOn === -1) {
        finishBettingRound();
    } else {
        _s.actionTimer = ACTION_TIMEOUT;
    }
}

function doAllIn(seatID) {
    var s = _s.seats[seatID];
    if (!s) return;
    var amount = s.chips;
    s.chips = 0;
    s.bet += amount;
    _s.pot += amount;
    s.allIn = true;
    if (s.bet > _s.currentBet) {
        _s.minRaise = Math.max(_s.minRaise, s.bet - _s.currentBet);
        _s.currentBet = s.bet;
        _s.lastAction = s.name + ' all-in for ' + s.bet;
        _s.actionOn = nextCanActSeatExcluding(seatIndexOf(seatID), seatID);
        if (_s.actionOn === -1) { finishBettingRound(); return; }
        _s.actionTimer = ACTION_TIMEOUT;
    } else {
        _s.lastAction = s.name + ' all-in for ' + s.bet;
        advanceAction();
    }
    Game._ctx.log(_s.lastAction);
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

    var nextIdx = nextCanActSeat(_s.actionOn);
    if (nextIdx === -1) { finishBettingRound(); return; }

    var nextID = _s.seatOrder[nextIdx];
    var nextS = _s.seats[nextID];

    var allMatched = true;
    for (var i = 0; i < canAct.length; i++) {
        if (_s.seats[canAct[i]].bet < _s.currentBet) { allMatched = false; break; }
    }
    if (allMatched && _s.currentBet > 0 && nextS.bet === _s.currentBet) {
        finishBettingRound();
        return;
    }

    _s.actionOn = nextIdx;
    _s.actionTimer = ACTION_TIMEOUT;
}

function finishBettingRound() {
    for (var i = 0; i < _s.seatOrder.length; i++) {
        var s = _s.seats[_s.seatOrder[i]];
        if (s) s.bet = 0;
    }
    _s.currentBet = 0;
    _s.minRaise = _s.BIG_BLIND;
    _s.raiseAmount = _s.BIG_BLIND;

    var inHand = playersInHand();
    if (inHand.length === 1) { awardPot(inHand[0]); endHand(); return; }

    if (_s.phase === 'preflop') {
        _s.phase = 'flop';
        _s.deck.pop();
        _s.community.push(_s.deck.pop(), _s.deck.pop(), _s.deck.pop());
    } else if (_s.phase === 'flop') {
        _s.phase = 'turn';
        _s.deck.pop();
        _s.community.push(_s.deck.pop());
    } else if (_s.phase === 'turn') {
        _s.phase = 'river';
        _s.deck.pop();
        _s.community.push(_s.deck.pop());
    } else if (_s.phase === 'river') {
        doShowdown();
        return;
    }

    var canAct = playersCanAct();
    if (canAct.length <= 1) { finishBettingRound(); return; }
    _s.actionOn = nextCanActSeat(_s.dealerIdx);
    _s.actionTimer = ACTION_TIMEOUT;
}

// ─── Showdown ──────────────────────────────────────────────────────────

function doShowdown() {
    _s.phase = 'showdown';
    var inHand = playersInHand();
    var results = [];
    for (var i = 0; i < inHand.length; i++) {
        var id = inHand[i];
        var s = _s.seats[id];
        var allCards = s.hand.concat(_s.community);
        var eval_ = evaluateHand(allCards);
        results.push({ id: id, name: s.name, hand: eval_, cards: s.hand });
    }
    results.sort(function(a, b) { return compareHands(b.hand, a.hand); });

    var winners = [results[0]];
    for (var j = 1; j < results.length; j++) {
        if (compareHands(results[j].hand, results[0].hand) === 0) winners.push(results[j]);
        else break;
    }

    var share = Math.floor(_s.pot / winners.length);
    var remainder = _s.pot - share * winners.length;
    var winMsg = '';
    for (var w = 0; w < winners.length; w++) {
        var ws = _s.seats[winners[w].id];
        ws.chips += share + (w === 0 ? remainder : 0);
        if (winMsg) winMsg += ', ';
        winMsg += ws.name;
    }
    winMsg += ' wins ' + _s.pot + ' with ' + results[0].hand.name;
    _s.lastWinMsg = winMsg;
    Game._ctx.chat(winMsg);
    Game._ctx.log(winMsg);
    _s.showdownResults = results;
    _s.showdownTimer = 5;
}

function awardPot(winnerID) {
    var s = _s.seats[winnerID];
    s.chips += _s.pot;
    _s.lastWinMsg = s.name + ' wins ' + _s.pot;
    Game._ctx.chat(_s.lastWinMsg);
    Game._ctx.log(_s.lastWinMsg);
    _s.pot = 0;
}

function endHand() {
    _s.phase = 'between';
    _s.showdownTimer = 3;
    _s.eliminated = [];
    for (var i = 0; i < _s.seatOrder.length; i++) {
        var s = _s.seats[_s.seatOrder[i]];
        if (s && s.chips <= 0 && !s.bustedOut) {
            s.bustedOut = true;
            _s.eliminated.push(s.name);
            Game._ctx.chat(s.name + ' is eliminated!');
        }
    }
}

// ─── AI ────────────────────────────────────────────────────────────────

function addAIPlayer() {
    var usedNames = {};
    for (var i = 0; i < _s.seatOrder.length; i++) {
        var s = _s.seats[_s.seatOrder[i]];
        if (s) usedNames[s.name] = true;
    }
    var name = null;
    for (var n = 0; n < AI_NAMES.length; n++) {
        if (!usedNames[AI_NAMES[n]]) { name = AI_NAMES[n]; break; }
    }
    if (!name) name = 'Bot' + Math.floor(Math.random() * 1000);
    var id = AI_PREFIX + name.toLowerCase();
    _s.seats[id] = {
        name: name, chips: STARTING_CHIPS, hand: [], folded: false,
        bet: 0, allIn: false, bustedOut: false, isAI: true, aiThinkTimer: 0
    };
    _s.seatOrder.push(id);
    Game._ctx.chat(name + ' (bot) sits down');
}

function fillAIPlayers() {
    while (activePlayers().length < MIN_SEATS) addAIPlayer();
}

function aiDecide(seatID) {
    var s = _s.seats[seatID];
    if (!s || s.folded || s.allIn) return;
    var toCall = _s.currentBet - s.bet;
    var strength = aiHandStrength(s.hand, _s.community);
    var potOdds = toCall > 0 ? toCall / (_s.pot + toCall) : 0;
    var rand = Math.random();
    var aggression = ((seatID.charCodeAt(seatID.length - 1) % 5) - 2) * 0.05;
    strength += aggression;

    if (toCall === 0) {
        if (strength > 0.65 && rand < 0.5) {
            var raiseAmt = Math.max(_s.minRaise, Math.floor(_s.pot * (0.3 + strength * 0.5)));
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
                var r2 = Math.max(_s.minRaise, Math.floor(_s.pot * 0.5));
                r2 = Math.min(r2, s.chips - toCall);
                if (r2 >= _s.minRaise) doRaise(seatID, r2);
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
    if (_s.phase === 'showdown' || _s.phase === 'between') {
        _s.showdownTimer -= dt;
        if (_s.showdownTimer <= 0) {
            endHand();
            if (_s.phase === 'between') {
                _s.showdownTimer = 0;
                startHand();
            }
        }
        return;
    }
    if (_s.phase === 'waiting') return;

    if (_s.actionOn >= 0 && _s.actionOn < _s.seatOrder.length) {
        var id = _s.seatOrder[_s.actionOn];
        var s = id ? _s.seats[id] : null;

        if (s && isAI(id) && !s.folded && !s.allIn) {
            if (!s.aiThinkTimer) {
                s.aiThinkTimer = AI_THINK_MIN + Math.random() * (AI_THINK_MAX - AI_THINK_MIN);
            }
            s.aiThinkTimer -= dt;
            if (s.aiThinkTimer <= 0) {
                s.aiThinkTimer = 0;
                aiDecide(id);
            }
        } else if (_s.actionTimer > 0) {
            _s.actionTimer -= dt;
            if (_s.actionTimer <= 0) {
                if (s) {
                    if (s.bet >= _s.currentBet) doCheck(id);
                    else doFold(id);
                }
            }
        }
    }
}

// ─── Game Object ───────────────────────────────────────────────────────

var Game = {
    gameName: "Texas Hold'em",

    _ctx: null,  // shared ctx pointer for helpers that need chat/log/midi

    init: function(ctx) {
        Game._ctx = ctx;
        ctx.registerCommand({
            name: 'chips',
            description: 'Show chip counts for all seats',
            handler: function(playerID, isAdmin, args) {
                var msg = 'Chip counts:';
                for (var i = 0; i < _s.seatOrder.length; i++) {
                    var s = _s.seats[_s.seatOrder[i]];
                    if (s) msg += '\n  ' + s.name + ': $' + s.chips + (s.bustedOut ? ' (out)' : '');
                }
                ctx.chatPlayer(playerID, msg);
            }
        });
        ctx.registerCommand({
            name: 'newgame',
            description: 'Reset and start a new tournament (admin only)',
            adminOnly: true,
            handler: function(playerID, isAdmin, args) {
                _s.phase = 'waiting';
                _s.handNum = 0;
                _s.pot = 0;
                _s.community = [];
                _s.lastWinMsg = '';
                _s.SMALL_BLIND = INITIAL_SMALL_BLIND;
                _s.BIG_BLIND = INITIAL_BIG_BLIND;
                for (var i = 0; i < _s.seatOrder.length; i++) {
                    var s = _s.seats[_s.seatOrder[i]];
                    if (s) {
                        s.chips = STARTING_CHIPS;
                        s.bustedOut = false;
                        s.hand = [];
                        s.folded = false;
                        s.bet = 0;
                        s.allIn = false;
                    }
                }
                ctx.chat('New tournament started! All seats reset to $' + STARTING_CHIPS);
                fillAIPlayers();
                if (activePlayers().length >= 2) startHand();
            }
        });
        return initialState();
    },

    begin: function(state, ctx) {
        _s = state;
        Game._ctx = ctx;
        var t = state.teams || [];
        for (var i = 0; i < t.length; i++) {
            var team = t[i];
            var seatID = 'team_' + i;
            _s.seats[seatID] = {
                name: team.name, chips: STARTING_CHIPS, hand: [], folded: false,
                bet: 0, allIn: false, bustedOut: false, isAI: false, aiThinkTimer: 0
            };
            _s.seatOrder.push(seatID);
            _s.teamToSeat[team.name] = seatID;
            for (var j = 0; j < team.players.length; j++) {
                _s.playerToTeam[team.players[j].id] = team.name;
            }
        }
        fillAIPlayers();
        if (activePlayers().length >= 2) startHand();
        ctx.log("Hold'em started with " + _s.seatOrder.length + ' seats (' + t.length + ' teams)');
    },

    update: function(state, dt, events, ctx) {
        _s = state;
        Game._ctx = ctx;
        for (var i = 0; i < events.length; i++) {
            var e = events[i];
            if (e.type !== 'input') continue;
            var seatID = playerSeat(e.playerID);
            if (!seatID) continue;
            var seat = _s.seats[seatID];
            if (!seat || seat.bustedOut) continue;
            if (_s.phase === 'showdown' || _s.phase === 'between' || _s.phase === 'waiting') continue;
            if (_s.actionOn < 0 || _s.seatOrder[_s.actionOn] !== seatID) continue;
            if (seat.folded || seat.allIn) continue;
            var toCall = _s.currentBet - seat.bet;
            var key = e.key;
            if (key === 'f' || key === 'F') {
                if (toCall > 0) doFold(seatID);
            } else if (key === 'c' || key === 'C' || key === ' ') {
                if (toCall > 0) doCall(seatID);
                else doCheck(seatID);
            } else if (key === 'r' || key === 'R') {
                if (seat.chips > toCall) doRaise(seatID, _s.raiseAmount);
            } else if (key === 'a' || key === 'A') {
                doAllIn(seatID);
            } else if (key === 'up') {
                _s.raiseAmount = Math.min(seat.chips - toCall, _s.raiseAmount + _s.BIG_BLIND);
            } else if (key === 'down') {
                _s.raiseAmount = Math.max(_s.minRaise, _s.raiseAmount - _s.BIG_BLIND);
            }
        }
        tick(dt);
    },

    renderAscii: function(state, me, cells) {
        _s = state;
        // holdem uses layout exclusively
    },

    layout: function(state, me) {
        _s = state;
        var seatID = playerSeat(me.id);
        if (!seatID) {
            if (_s.seatOrder.length > 0) seatID = _s.seatOrder[0];
            else return { type: 'label', text: 'Waiting...', align: 'center' };
        }
        return buildViewNC(seatID, 0, 0);
    },

    statusBar: function(state, me) {
        _s = state;
        var seatID = playerSeat(me.id);
        return renderStatusBar(seatID);
    },

    commandBar: function(state, me) {
        _s = state;
        var seatID = playerSeat(me.id);
        return renderCommandBar(seatID);
    }
};
