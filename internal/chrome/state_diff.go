package chrome

import (
	"bytes"
	"encoding/json"

	"dev-null/internal/domain"
	"dev-null/internal/render"
)

// encodeStateBroadcast returns the OSC string(s) to push this player's view of
// Game.state to their local renderer. The snapshot is produced once per tick
// by the server; this function diffs it against the player's last-sent set.
//
// On the first broadcast of a session, Full is sent as a baseline. Each
// subsequent broadcast emits only the top-level keys whose marshaled bytes
// differ from the last sent value (plus null entries for removed keys).
//
// As a special case, a patch whose only changed key is `_gameTime` (the
// framework's monotonic game-clock) is suppressed — the client extrapolates
// `_gameTime` locally between snapshots from its own wall clock, so
// tick-rate clock-only patches would just burn bandwidth. We deliberately
// do NOT update lastSentKeys[_gameTime] when suppressing, so the next real
// state change re-includes the current `_gameTime` and the client can
// re-snap and detect drift.
//
// Empty return means "nothing changed since last frame" — no bytes are sent.
func (m *Model) encodeStateBroadcast(snap *domain.StateSnapshot) string {
	if snap == nil {
		return ""
	}

	if !m.sentBaseline {
		osc := render.EncodeStateOSC(snap.Full)
		if osc == "" {
			return ""
		}
		m.lastSentKeys = snap.Keys
		m.sentBaseline = true
		return osc
	}

	// Build a merge patch of changed/removed keys.
	patch := map[string]json.RawMessage{}
	for k, v := range snap.Keys {
		prev, ok := m.lastSentKeys[k]
		if !ok || !bytes.Equal(prev, v) {
			patch[k] = json.RawMessage(v)
		}
	}
	for k := range m.lastSentKeys {
		if _, ok := snap.Keys[k]; !ok {
			patch[k] = json.RawMessage("null")
		}
	}

	if len(patch) == 0 {
		return ""
	}

	// Suppress clock-only patches — client extrapolates _gameTime between
	// snapshots.
	if len(patch) == 1 {
		if _, only := patch["_gameTime"]; only {
			return ""
		}
	}

	data, err := json.Marshal(patch)
	if err != nil {
		return ""
	}
	osc := render.EncodeStatePatchOSC(data)
	if osc == "" {
		return ""
	}
	// Keep lastSentKeys[_gameTime] in sync with what we actually transmitted,
	// so the next non-clock change starts from the freshly-acked baseline.
	m.lastSentKeys = snap.Keys
	return osc
}
