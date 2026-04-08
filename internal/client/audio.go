package client

import (
	"encoding/binary"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/hajimehoshi/ebiten/v2/audio"
	"github.com/sinshu/go-meltysynth/meltysynth"

	"dev-null/internal/domain"
)

const (
	midiSampleRate = 44100
	// renderSamples is the number of samples per Render call (~5.8ms at 44100 Hz).
	renderSamples = 256
)

// MidiSynth renders MIDI events to audio using a SoundFont via go-meltysynth.
type MidiSynth struct {
	synth  *meltysynth.Synthesizer
	player *audio.Player
	mu     sync.Mutex

	// needsPlayer is set when the synth is loaded but the audio player hasn't
	// been created yet. Player creation is deferred to the first Update() call
	// because Ebitengine's audio.Player.Play() blocks before ebiten.RunGame.
	needsPlayer bool

	// Scheduled NoteOff events.
	pendingOffs []scheduledOff

	// Current SoundFont name (for display/UI).
	fontName string
}

type scheduledOff struct {
	channel int
	note    int
	at      time.Time
}

// NewMidiSynth creates a synthesizer. If sf2Path is empty or loading fails,
// the synth starts silent (no crash) and can be loaded later via LoadSoundFont.
func NewMidiSynth(sf2Path string) *MidiSynth {
	ms := &MidiSynth{}
	if sf2Path != "" {
		if err := ms.LoadSoundFont(sf2Path); err != nil {
			slog.Warn("MIDI synth: failed to load SoundFont", "path", sf2Path, "err", err)
		}
	}
	return ms
}

// LoadSoundFont loads an SF2 file and (re)creates the synthesizer.
// Any currently playing notes are stopped.
func (ms *MidiSynth) LoadSoundFont(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	sf, err := meltysynth.NewSoundFont(f)
	if err != nil {
		return err
	}

	settings := meltysynth.NewSynthesizerSettings(midiSampleRate)
	synth, err := meltysynth.NewSynthesizer(sf, settings)
	if err != nil {
		return err
	}

	ms.mu.Lock()
	defer ms.mu.Unlock()

	// Stop existing player if any.
	if ms.player != nil {
		ms.player.Pause()
		ms.player.Close()
		ms.player = nil
	}

	ms.synth = synth
	ms.pendingOffs = nil

	// Defer audio player creation until the game loop is running.
	// Ebitengine's audio.Player.Play() blocks if called before ebiten.RunGame,
	// so we mark the synth as needing a player and create it on first Update().
	ms.needsPlayer = true
	return nil
}

// ensurePlayer creates the audio player if deferred from LoadSoundFont.
// Must be called from the game loop (after ebiten.RunGame has started).
func (ms *MidiSynth) ensurePlayer() {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	if !ms.needsPlayer || ms.synth == nil {
		return
	}
	ms.needsPlayer = false

	stream := &synthStream{synth: ms}
	ctx := getAudioCtx()
	player, err := ctx.NewPlayer(stream)
	if err != nil {
		slog.Warn("MIDI synth: failed to create audio player", "err", err)
		ms.synth = nil
		return
	}
	player.SetBufferSize(time.Millisecond * 50)
	player.Play()
	ms.player = player
}

// NoteOn triggers a note. If durationMs > 0, a NoteOff is scheduled automatically.
func (ms *MidiSynth) NoteOn(channel, note, velocity, durationMs int) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	if ms.synth == nil {
		return
	}
	ms.synth.NoteOn(int32(channel), int32(note), int32(velocity))
	if durationMs > 0 {
		ms.pendingOffs = append(ms.pendingOffs, scheduledOff{
			channel: channel,
			note:    note,
			at:      time.Now().Add(time.Duration(durationMs) * time.Millisecond),
		})
	}
}

// NoteOff stops a note.
func (ms *MidiSynth) NoteOff(channel, note int) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	if ms.synth == nil {
		return
	}
	ms.synth.NoteOff(int32(channel), int32(note))
}

// ProgramChange sets the instrument on a channel.
func (ms *MidiSynth) ProgramChange(channel, program int) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	if ms.synth == nil {
		return
	}
	// MIDI program change: command 0xC0
	ms.synth.ProcessMidiMessage(int32(channel), 0xC0, int32(program), 0)
}

// ControlChange sends a CC message.
func (ms *MidiSynth) ControlChange(channel, controller, value int) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	if ms.synth == nil {
		return
	}
	// MIDI CC: command 0xB0
	ms.synth.ProcessMidiMessage(int32(channel), 0xB0, int32(controller), int32(value))
}

// AllNotesOff silences everything and clears pending offs.
func (ms *MidiSynth) AllNotesOff() {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	if ms.synth == nil {
		return
	}
	ms.synth.NoteOffAll(false)
	ms.pendingOffs = nil
}

// DispatchEvent routes a domain.MidiEvent to the appropriate method.
func (ms *MidiSynth) DispatchEvent(ev domain.MidiEvent) {
	switch domain.MidiEventType(ev.Type) {
	case domain.MidiNoteOn:
		ms.NoteOn(ev.Channel, ev.Note, ev.Velocity, ev.DurationMs)
	case domain.MidiProgramChange:
		ms.ProgramChange(ev.Channel, ev.Program)
	case domain.MidiControlChange:
		ms.ControlChange(ev.Channel, ev.Controller, ev.Velocity)
	}
}

// FontName returns the name of the currently loaded SoundFont.
func (ms *MidiSynth) FontName() string {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	return ms.fontName
}

// Close stops the synth and releases resources.
func (ms *MidiSynth) Close() {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	if ms.player != nil {
		ms.player.Pause()
		ms.player.Close()
		ms.player = nil
	}
	ms.synth = nil
	ms.pendingOffs = nil
}

// processPendingOffs fires NoteOff for any scheduled events that have expired.
// Must be called with ms.mu held.
func (ms *MidiSynth) processPendingOffs() {
	if len(ms.pendingOffs) == 0 {
		return
	}
	now := time.Now()
	kept := ms.pendingOffs[:0]
	for _, off := range ms.pendingOffs {
		if now.After(off.at) {
			ms.synth.NoteOff(int32(off.channel), int32(off.note))
		} else {
			kept = append(kept, off)
		}
	}
	ms.pendingOffs = kept
}

// synthStream implements io.Reader for Ebitengine's audio player.
// It renders PCM from the synthesizer on demand.
type synthStream struct {
	synth *MidiSynth
	left  [renderSamples]float32
	right [renderSamples]float32
	// Residual buffer from partial reads.
	residual []byte
}

func (s *synthStream) Read(p []byte) (int, error) {
	// Drain any residual bytes from previous render.
	if len(s.residual) > 0 {
		n := copy(p, s.residual)
		s.residual = s.residual[n:]
		return n, nil
	}

	s.synth.mu.Lock()
	if s.synth.synth == nil {
		s.synth.mu.Unlock()
		// No synth loaded — produce silence.
		n := len(p)
		for i := range p[:n] {
			p[i] = 0
		}
		return n, nil
	}

	// Process pending NoteOffs.
	s.synth.processPendingOffs()

	// Render audio.
	s.synth.synth.Render(s.left[:], s.right[:])
	s.synth.mu.Unlock()

	// Convert float32 stereo to interleaved int16 PCM (little-endian).
	// Ebitengine expects stereo int16 at the context sample rate.
	buf := make([]byte, renderSamples*4) // 2 channels × 2 bytes per sample
	for i := 0; i < renderSamples; i++ {
		l := clampF32ToI16(s.left[i])
		r := clampF32ToI16(s.right[i])
		binary.LittleEndian.PutUint16(buf[i*4:], uint16(l))
		binary.LittleEndian.PutUint16(buf[i*4+2:], uint16(r))
	}

	n := copy(p, buf)
	if n < len(buf) {
		s.residual = buf[n:]
	}
	return n, nil
}

// clampF32ToI16 converts a float32 sample [-1, 1] to int16.
func clampF32ToI16(f float32) int16 {
	s := f * 32767
	if s > 32767 {
		return 32767
	}
	if s < -32768 {
		return -32768
	}
	return int16(s)
}

// Ensure synthStream always produces data (never EOF).
var _ io.Reader = (*synthStream)(nil)
