// ui.js — NC widget tree rendering for Texas Hold'em
// Returns declarative widget trees that the framework renders using real NC controls.

// ─── Main View (returns widget tree for viewNC) ────────────────────────

function buildViewNC(teamID, width, height) {
    var seat = Game.state.seats[teamID];

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
    if (Game.state.phase === 'waiting') phaseStr = DIM + 'Waiting for players...' + RST;
    else if (Game.state.phase === 'preflop') phaseStr = 'Pre-Flop';
    else if (Game.state.phase === 'flop') phaseStr = 'Flop';
    else if (Game.state.phase === 'turn') phaseStr = 'Turn';
    else if (Game.state.phase === 'river') phaseStr = 'River';
    else if (Game.state.phase === 'showdown') phaseStr = YEL + BOLD + 'Showdown!' + RST;
    else if (Game.state.phase === 'between') phaseStr = DIM + (Game.state.lastWinMsg || 'Next hand...') + RST;

    children.push({ type: 'label', text: phaseStr, align: 'center', height: 1 });
    children.push({ type: 'label', text: '', height: 1 });

    // Community cards
    var commStr = '';
    if (Game.state.phase === 'waiting') {
        commStr = DIM + '[  ] [  ] [  ]  [  ]  [  ]' + RST;
    } else {
        for (var ci = 0; ci < 5; ci++) {
            if (ci > 0) commStr += ' ';
            if (ci < Game.state.community.length) {
                commStr += cardBox(Game.state.community[ci]);
            } else {
                commStr += DIM + '[  ]' + RST;
            }
        }
    }
    children.push({ type: 'label', text: commStr, align: 'center', height: 1 });
    children.push({ type: 'label', text: '', height: 1 });

    // Pot
    if (Game.state.phase !== 'waiting') {
        children.push({ type: 'label', text: FGGOLD + BOLD + 'Pot: ' + Game.state.pot + RST, align: 'center', height: 1 });
    }

    // Last action
    if (Game.state.lastAction) {
        children.push({ type: 'label', text: DIM + Game.state.lastAction + RST, align: 'center', height: 1 });
    }

    // Showdown results
    if (Game.state.phase === 'showdown' && Game.state.showdownResults) {
        children.push({ type: 'label', text: '', height: 1 });
        for (var ri = 0; ri < Game.state.showdownResults.length; ri++) {
            var sr = Game.state.showdownResults[ri];
            var srLine = sr.name + ': ' + cardBox(sr.cards[0]) + cardBox(sr.cards[1]);
            srLine += ' ' + GRN + sr.hand.name + RST;
            children.push({ type: 'label', text: srLine, align: 'center', height: 1 });
        }
    }

    var title = 'Hand #' + Game.state.handNum + '  Blinds ' + SMALL_BLIND + '/' + BIG_BLIND;
    if (Game.state.phase === 'waiting') title = "Texas Hold'em";

    return {
        type: 'panel', title: title, weight: 1,
        children: children
    };
}

// ─── Hand Panel (your team's cards) ────────────────────────────────────

function buildHandPanel(teamID) {
    var seat = Game.state.seats[teamID];
    var children = [];

    if (!seat) {
        children.push({ type: 'label', text: DIM + 'Spectating' + RST, align: 'center' });
    } else if (seat.bustedOut) {
        children.push({ type: 'label', text: DIM + 'Eliminated' + RST, align: 'center' });
    } else if (seat.hand.length === 2) {
        children.push({ type: 'label', text: '  ' + cardBox(seat.hand[0]) + '  ' + cardBox(seat.hand[1]) + '  ', align: 'center' });

        if (Game.state.community.length >= 3) {
            var allCards = seat.hand.concat(Game.state.community);
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

    for (var si = 0; si < Game.state.seatOrder.length; si++) {
        var sid = Game.state.seatOrder[si];
        var seat = Game.state.seats[sid];
        if (!seat) continue;

        var isMe = sid === teamID;
        var isDealer = Game.state.dealerIdx === si;
        var isAction = Game.state.actionOn === si;

        // Name with indicators
        var nameStr = '';
        if (isDealer) nameStr += YEL + 'D ' + RST;
        if (isAction && Game.state.phase !== 'waiting' && Game.state.phase !== 'showdown' && Game.state.phase !== 'between') {
            nameStr += WHT + BOLD + '\u25b6 ' + RST;
        }
        var displayName = seat.name;
        if (seat.isAI) displayName += DIM + ' bot' + RST;
        nameStr += (isMe ? CYN + BOLD : WHT) + displayName + RST;

        if (seat.bustedOut) nameStr = DIM + '\u2620 ' + seat.name + RST;
        else if (seat.folded && Game.state.phase !== 'waiting' && Game.state.phase !== 'between') {
            nameStr = DIM + seat.name + ' (fold)' + RST;
        }

        // Chips + bet
        var chipStr = FGGOLD + '$' + seat.chips + RST;
        if (seat.bet > 0) chipStr += ' ' + RED + '+' + seat.bet + RST;
        if (seat.allIn) chipStr += ' ' + YEL + 'AI' + RST;

        // Cards
        var cardLine = '';
        if (Game.state.phase === 'showdown' && !seat.folded && seat.hand.length === 2) {
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
    var seat = Game.state.seats[teamID];
    var chips = seat ? '$' + seat.chips : '';
    var phase = Game.state.phase === 'waiting' ? 'Waiting' :
                Game.state.phase.charAt(0).toUpperCase() + Game.state.phase.slice(1);
    var nPlayers = activePlayers().length;
    return "Hold'em  |  " + phase + '  |  Blinds ' + SMALL_BLIND + '/' + BIG_BLIND +
           '  |  ' + chips + '  |  ' + nPlayers + ' seats';
}

// ─── Command Bar ───────────────────────────────────────────────────────

function renderCommandBar(teamID) {
    var seat = Game.state.seats[teamID];
    if (!seat) return '[Enter] Chat';

    if (Game.state.phase === 'waiting') {
        var need = 2 - activePlayers().length;
        if (need > 0) return 'Waiting for ' + need + ' more team(s)...  [Enter] Chat';
        return 'Starting soon...  [Enter] Chat';
    }
    if (Game.state.phase === 'showdown' || Game.state.phase === 'between') return '[Enter] Chat';
    if (seat.bustedOut) return 'Eliminated  [Enter] Chat';
    if (seat.folded) return 'Folded  [Enter] Chat';

    if (Game.state.actionOn >= 0 && Game.state.seatOrder[Game.state.actionOn] === teamID) {
        var toCall = Game.state.currentBet - seat.bet;
        var actions = '';
        if (toCall > 0) {
            actions = '[Space/C] Call ' + toCall + '  [F] Fold';
        } else {
            actions = '[Space/C] Check';
        }
        if (seat.chips > toCall) {
            actions += '  [R] Raise ' + Game.state.raiseAmount + '  [\u2191\u2193] Adjust';
        }
        actions += '  [A] All-in ' + seat.chips;
        return actions;
    }

    var waitName = '...';
    if (Game.state.actionOn >= 0 && Game.state.seats[Game.state.seatOrder[Game.state.actionOn]]) {
        waitName = Game.state.seats[Game.state.seatOrder[Game.state.actionOn]].name;
    }
    return 'Waiting for ' + waitName + '  [Enter] Chat';
}
