package chrome

import (
	"bytes"
	"encoding/json"
	"sort"

	"dev-null/internal/render"
)

// encodeStateBroadcast returns the OSC string(s) to push this player's view of
// Game.state to their local renderer. It performs depth-1 merge-patch diffing:
// the first broadcast of a session sends the full state as a baseline, and
// each subsequent broadcast sends only the top-level keys whose marshaled
// bytes differ from the last sent value (plus null entries for removed keys).
//
// As a special case, a patch whose only changed key is `_gameTime` (the
// framework's monotonic game-clock) is suppressed. The client extrapolates
// `_gameTime` locally between snapshots from its own wall clock, so a
// tick-rate stream of clock-only patches would just burn bandwidth. We
// deliberately do NOT update lastSentKeys[_gameTime] when suppressing —
// that way the next real state change re-includes the current `_gameTime`
// so the client can re-snap and detect drift.
//
// Empty return means "nothing changed since last frame" — no bytes are sent.
func (m *Model) encodeStateBroadcast(stateObj any) string {
	// Marshal each top-level key independently so we can diff them in isolation.
	// A stateObj that isn't a map (nil, scalar) degrades to a single-key
	// "_root" entry so the machinery still functions end-to-end.
	currKeys := marshalTopLevelKeys(stateObj)

	if !m.sentBaseline {
		// First broadcast this session: send the whole state via the existing
		// ns;state path so the client has a baseline to patch against.
		data, err := json.Marshal(stateObj)
		if err != nil {
			return ""
		}
		osc := render.EncodeStateOSC(data)
		if osc == "" {
			return ""
		}
		m.lastSentKeys = currKeys
		m.sentBaseline = true
		return osc
	}

	// Subsequent broadcasts: build a merge patch of changed/removed keys.
	patch := map[string]json.RawMessage{}
	for k, v := range currKeys {
		prev, ok := m.lastSentKeys[k]
		if !ok || !bytes.Equal(prev, v) {
			patch[k] = json.RawMessage(v)
		}
	}
	for k := range m.lastSentKeys {
		if _, ok := currKeys[k]; !ok {
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
	m.lastSentKeys = currKeys
	return osc
}

// marshalTopLevelKeys returns one marshaled JSON blob per top-level key of obj.
// Non-object inputs map to a single "_root" entry, which keeps the diff path
// consistent for games that stash their state under a scalar or array.
func marshalTopLevelKeys(obj any) map[string][]byte {
	m, ok := obj.(map[string]any)
	if !ok {
		data, err := json.Marshal(obj)
		if err != nil {
			return nil
		}
		return map[string][]byte{"_root": data}
	}
	out := make(map[string][]byte, len(m))
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys) // stable iteration for easier debugging
	for _, k := range keys {
		data, err := json.Marshal(m[k])
		if err != nil {
			continue
		}
		out[k] = data
	}
	return out
}
