// ui.js — NC widget tree rendering for Texas Hold'em
// Returns declarative widget trees that the framework renders using real NC controls.

// ─── Main View (returns widget tree for viewNC) ────────────────────────

function buildViewNC(teamID, width, height) {
    var seat = state.seats[teamID];

    var playersW = Math.min(30, Math.floor(width * 0.35));

    return {
        type: 'hsplit',
        children: [
            {
                type: 'vsplit', weight: 1,
                children: [
                    buildTablePanel(teamID),
                    buildHandPanel(teamID)
                ]
            },
            buildPlayersPanel(teamID, playersW)
        ]
    };
}

// ─── Table Panel (community cards + pot) ───────────────────────────────

function buildTablePanel(teamID) {
    var children = [];

    // Phase
    var phaseStr = '';
    if (state.phase === 'waiting') phaseStr = DIM + 'Waiting for players...' + RST;
    else if (state.phase === 'preflop') phaseStr = 'Pre-Flop';
    else if (state.phase === 'flop') phaseStr = 'Flop';
    else if (state.phase === 'turn') phaseStr = 'Turn';
    else if (state.phase === 'river') phaseStr = 'River';
    else if (state.phase === 'showdown') phaseStr = YEL + BOLD + 'Showdown!' + RST;
    else if (state.phase === 'between') phaseStr = DIM + (state.lastWinMsg || 'Next hand...') + RST;

    children.push({ type: 'label', text: phaseStr, align: 'center', height: 1 });
    children.push({ type: 'label', text: '', height: 1 });

    // Community cards
    var commStr = '';
    if (state.phase === 'waiting') {
        commStr = DIM + '[  ] [  ] [  ]  [  ]  [  ]' + RST;
    } else {
        for (var ci = 0; ci < 5; ci++) {
            if (ci > 0) commStr += ' ';
            if (ci < state.community.length) {
                commStr += cardBox(state.community[ci]);
            } else {
                commStr += DIM + '[  ]' + RST;
            }
        }
    }
    children.push({ type: 'label', text: commStr, align: 'center', height: 1 });
    children.push({ type: 'label', text: '', height: 1 });

    // Pot
    if (state.phase !== 'waiting') {
        children.push({ type: 'label', text: FGGOLD + BOLD + 'Pot: ' + state.pot + RST, align: 'center', height: 1 });
    }

    // Last action
    if (state.lastAction) {
        children.push({ type: 'label', text: DIM + state.lastAction + RST, align: 'center', height: 1 });
    }

    // Showdown results
    if (state.phase === 'showdown' && state.showdownResults) {
        children.push({ type: 'label', text: '', height: 1 });
        for (var ri = 0; ri < state.showdownResults.length; ri++) {
            var sr = state.showdownResults[ri];
            var srLine = sr.name + ': ' + cardBox(sr.cards[0]) + cardBox(sr.cards[1]);
            srLine += ' ' + GRN + sr.hand.name + RST;
            children.push({ type: 'label', text: srLine, align: 'center', height: 1 });
        }
    }

    var title = 'Hand #' + state.handNum + '  Blinds ' + SMALL_BLIND + '/' + BIG_BLIND;
    if (state.phase === 'waiting') title = "Texas Hold'em";

    return {
        type: 'panel', title: title, weight: 1,
        children: children
    };
}

// ─── Hand Panel (your team's cards) ────────────────────────────────────

function buildHandPanel(teamID) {
    var seat = state.seats[teamID];
    var children = [];

    if (!seat) {
        children.push({ type: 'label', text: DIM + 'Spectating' + RST, align: 'center' });
    } else if (seat.bustedOut) {
        children.push({ type: 'label', text: DIM + 'Eliminated' + RST, align: 'center' });
    } else if (seat.hand.length === 2) {
        children.push({ type: 'label', text: '  ' + cardBox(seat.hand[0]) + '  ' + cardBox(seat.hand[1]) + '  ', align: 'center' });

        if (state.community.length >= 3) {
            var allCards = seat.hand.concat(state.community);
            var eval_ = evaluateHand(allCards);
            children.push({ type: 'label', text: GRN + eval_.name + RST, align: 'center' });
        }

        var chipLine = FGGOLD + '$' + seat.chips + RST;
        if (seat.bet > 0) chipLine += '  ' + RED + 'Bet: ' + seat.bet + RST;
        if (seat.allIn) chipLine += '  ' + YEL + BOLD + 'ALL-IN' + RST;
        children.push({ type: 'label', text: chipLine, align: 'center' });
    } else if (seat.folded) {
        children.push({ type: 'label', text: DIM + 'Folded' + RST, align: 'center' });
    } else {
        children.push({ type: 'label', text: DIM + 'Waiting for deal...' + RST, align: 'center' });
    }

    return {
        type: 'panel', title: 'Your Hand', height: 7,
        children: children
    };
}

// ─── Players Panel (all seats) ─────────────────────────────────────────

function buildPlayersPanel(teamID, panelWidth) {
    var rows = [];

    for (var si = 0; si < state.seatOrder.length; si++) {
        var sid = state.seatOrder[si];
        var seat = state.seats[sid];
        if (!seat) continue;

        var isMe = sid === teamID;
        var isDealer = state.dealerIdx === si;
        var isAction = state.actionOn === si;

        // Name with indicators
        var nameStr = '';
        if (isDealer) nameStr += YEL + 'D ' + RST;
        if (isAction && state.phase !== 'waiting' && state.phase !== 'showdown' && state.phase !== 'between') {
            nameStr += WHT + BOLD + '\u25b6 ' + RST;
        }
        var displayName = seat.name;
        if (seat.isAI) displayName += DIM + ' bot' + RST;
        nameStr += (isMe ? CYN + BOLD : WHT) + displayName + RST;

        if (seat.bustedOut) nameStr = DIM + '\u2620 ' + seat.name + RST;
        else if (seat.folded && state.phase !== 'waiting' && state.phase !== 'between') {
            nameStr = DIM + seat.name + ' (fold)' + RST;
        }

        // Chips + bet
        var chipStr = FGGOLD + '$' + seat.chips + RST;
        if (seat.bet > 0) chipStr += ' ' + RED + '+' + seat.bet + RST;
        if (seat.allIn) chipStr += ' ' + YEL + 'AI' + RST;

        // Cards
        var cardLine = '';
        if (state.phase === 'showdown' && !seat.folded && seat.hand.length === 2) {
            cardLine = cardBox(seat.hand[0]) + cardBox(seat.hand[1]);
        } else if (isMe && seat.hand.length === 2) {
            cardLine = cardBox(seat.hand[0]) + cardBox(seat.hand[1]);
        } else if (seat.hand.length === 2 && !seat.folded && !seat.bustedOut) {
            cardLine = cardBackBox() + cardBackBox();
        }

        rows.push([nameStr, chipStr, cardLine]);
    }

    return {
        type: 'panel', title: 'Players', width: panelWidth,
        children: [{
            type: 'table',
            rows: rows
        }]
    };
}

// ─── Status Bar ────────────────────────────────────────────────────────

function renderStatusBar(teamID) {
    var seat = state.seats[teamID];
    var chips = seat ? '$' + seat.chips : '';
    var phase = state.phase === 'waiting' ? 'Waiting' :
                state.phase.charAt(0).toUpperCase() + state.phase.slice(1);
    var nPlayers = activePlayers().length;
    return "Hold'em  |  " + phase + '  |  Blinds ' + SMALL_BLIND + '/' + BIG_BLIND +
           '  |  ' + chips + '  |  ' + nPlayers + ' seats';
}

// ─── Command Bar ───────────────────────────────────────────────────────

function renderCommandBar(teamID) {
    var seat = state.seats[teamID];
    if (!seat) return '[Enter] Chat';

    if (state.phase === 'waiting') {
        var need = 2 - activePlayers().length;
        if (need > 0) return 'Waiting for ' + need + ' more team(s)...  [Enter] Chat';
        return 'Starting soon...  [Enter] Chat';
    }
    if (state.phase === 'showdown' || state.phase === 'between') return '[Enter] Chat';
    if (seat.bustedOut) return 'Eliminated  [Enter] Chat';
    if (seat.folded) return 'Folded  [Enter] Chat';

    if (state.actionOn >= 0 && state.seatOrder[state.actionOn] === teamID) {
        var toCall = state.currentBet - seat.bet;
        var actions = '';
        if (toCall > 0) {
            actions = '[Space/C] Call ' + toCall + '  [F] Fold';
        } else {
            actions = '[Space/C] Check';
        }
        if (seat.chips > toCall) {
            actions += '  [R] Raise ' + state.raiseAmount + '  [\u2191\u2193] Adjust';
        }
        actions += '  [A] All-in ' + seat.chips;
        return actions;
    }

    var waitName = '...';
    if (state.actionOn >= 0 && state.seats[state.seatOrder[state.actionOn]]) {
        waitName = state.seats[state.seatOrder[state.actionOn]].name;
    }
    return 'Waiting for ' + waitName + '  [Enter] Chat';
}
