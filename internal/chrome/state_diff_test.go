package chrome

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

// decodeOSC unpacks one of our ns;state / ns;state-patch OSC strings back
// into its JSON payload so tests can assert on what went on the wire.
func decodeOSC(t *testing.T, osc, expectedKind string) map[string]any {
	t.Helper()
	if !strings.HasPrefix(osc, "\x1b]ns;"+expectedKind+";") {
		t.Fatalf("expected ns;%s OSC, got %q", expectedKind, osc)
	}
	payload := strings.TrimPrefix(osc, "\x1b]ns;"+expectedKind+";")
	payload = strings.TrimSuffix(payload, "\x07")
	gzBytes, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("base64: %v", err)
	}
	gz, err := gzip.NewReader(bytes.NewReader(gzBytes))
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	defer gz.Close()
	raw, err := readAll(gz)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal %q: %v", raw, err)
	}
	return out
}

func readAll(r interface {
	Read(p []byte) (int, error)
}) ([]byte, error) {
	var buf bytes.Buffer
	tmp := make([]byte, 4096)
	for {
		n, err := r.Read(tmp)
		if n > 0 {
			buf.Write(tmp[:n])
		}
		if err != nil {
			if err.Error() == "EOF" {
				return buf.Bytes(), nil
			}
			return buf.Bytes(), err
		}
	}
}

func TestEncodeStateBroadcast_FirstSendIsBaseline(t *testing.T) {
	m := &Model{}
	state := map[string]any{
		"players": map[string]any{"p1": map[string]any{"x": 1}},
		"map":     []any{0.0, 1.0, 0.0},
	}

	osc := m.encodeStateBroadcast(state)
	if osc == "" {
		t.Fatal("expected non-empty broadcast on first send")
	}
	got := decodeOSC(t, osc, "state")
	if len(got) != 2 {
		t.Fatalf("baseline should carry all top-level keys, got %v", got)
	}
	if !m.sentBaseline {
		t.Error("sentBaseline should be true after first send")
	}
	if _, ok := m.lastSentKeys["map"]; !ok {
		t.Error("lastSentKeys should be populated")
	}
}

func TestEncodeStateBroadcast_NoChangeEmitsNothing(t *testing.T) {
	m := &Model{}
	state := map[string]any{"k": 1.0}

	_ = m.encodeStateBroadcast(state)          // baseline
	osc := m.encodeStateBroadcast(state)       // identical
	if osc != "" {
		t.Fatalf("unchanged state should not broadcast, got %q", osc)
	}
}

func TestEncodeStateBroadcast_OnlyChangedKeysInPatch(t *testing.T) {
	m := &Model{}
	state := map[string]any{
		"players": map[string]any{"p1": map[string]any{"x": 1.0}},
		"map":     []any{0.0, 1.0, 0.0},
		"time":    0.1,
	}
	_ = m.encodeStateBroadcast(state) // baseline

	// Mutate only "players" and "time"; "map" stays identical.
	state["players"] = map[string]any{"p1": map[string]any{"x": 2.0}}
	state["time"] = 0.2

	osc := m.encodeStateBroadcast(state)
	if osc == "" {
		t.Fatal("expected patch broadcast for changed keys")
	}
	patch := decodeOSC(t, osc, "state-patch")
	if _, ok := patch["map"]; ok {
		t.Errorf("unchanged key 'map' leaked into patch: %v", patch)
	}
	if _, ok := patch["players"]; !ok {
		t.Errorf("changed key 'players' missing from patch: %v", patch)
	}
	if _, ok := patch["time"]; !ok {
		t.Errorf("changed key 'time' missing from patch: %v", patch)
	}
}

func TestEncodeStateBroadcast_RemovedKeyBecomesNull(t *testing.T) {
	m := &Model{}
	state := map[string]any{"a": 1.0, "b": 2.0}
	_ = m.encodeStateBroadcast(state)

	state = map[string]any{"a": 1.0} // "b" removed
	osc := m.encodeStateBroadcast(state)
	patch := decodeOSC(t, osc, "state-patch")
	if v, ok := patch["b"]; !ok || v != nil {
		t.Errorf("expected b:null in patch, got %v", patch)
	}
	if _, ok := patch["a"]; ok {
		t.Errorf("unchanged a should not appear: %v", patch)
	}
}

func TestEncodeStateBroadcast_ClockOnlyPatchIsSuppressed(t *testing.T) {
	m := &Model{}
	state := map[string]any{
		"_gameTime":      0.0,
		"players": map[string]any{"p1": map[string]any{"x": 1.0}},
	}
	_ = m.encodeStateBroadcast(state) // baseline

	// Bump only the clock — should produce no broadcast.
	state["_gameTime"] = 0.1
	if osc := m.encodeStateBroadcast(state); osc != "" {
		t.Fatalf("clock-only change should be suppressed, got %q", osc)
	}
	state["_gameTime"] = 0.2
	if osc := m.encodeStateBroadcast(state); osc != "" {
		t.Fatalf("repeated clock-only change should still be suppressed, got %q", osc)
	}

	// Now change a real key. The patch must include both that key AND the
	// current _t (so the client can re-snap and detect drift).
	state["_gameTime"] = 0.3
	state["players"] = map[string]any{"p1": map[string]any{"x": 2.0}}
	osc := m.encodeStateBroadcast(state)
	if osc == "" {
		t.Fatal("expected patch when a non-clock key changes")
	}
	patch := decodeOSC(t, osc, "state-patch")
	if _, ok := patch["players"]; !ok {
		t.Errorf("changed key 'players' missing: %v", patch)
	}
	if _, ok := patch["_gameTime"]; !ok {
		t.Errorf("expected _t to ride along when other keys change: %v", patch)
	}
}

func TestEncodeStateBroadcast_NonMapStateRoundtrips(t *testing.T) {
	m := &Model{}
	// Some games might stash state as a scalar/array at the top level; the
	// transport shouldn't panic.
	osc := m.encodeStateBroadcast([]any{1.0, 2.0, 3.0})
	if osc == "" {
		t.Fatal("scalar state should still send baseline")
	}
	// Unchanged on second send.
	if again := m.encodeStateBroadcast([]any{1.0, 2.0, 3.0}); again != "" {
		t.Errorf("unchanged scalar state should not rebroadcast, got %q", again)
	}
	// Changed scalar produces a patch.
	osc = m.encodeStateBroadcast([]any{1.0, 2.0, 4.0})
	if osc == "" {
		t.Fatal("changed scalar state should produce a patch")
	}
	if !strings.Contains(osc, "ns;state-patch;") {
		t.Errorf("expected state-patch, got %q", osc)
	}
}
