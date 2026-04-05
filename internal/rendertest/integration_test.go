package rendertest

import (
	"bytes"
	"context"
	"net"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"null-space/internal/server"
)

// ─── Integration test helpers ────────────────────────────────────────────────

// testServer starts a real SSH server on a random port using the given
// scenario's state and returns the address and a cancel function.
// The host key is written to a temp dir so parallel tests don't collide.
func startTestServer(t *testing.T, sc renderScenario) (addr string, cancel context.CancelFunc) {
	t.Helper()

	// Pre-bind to :0 to get a free OS-assigned port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	addr = ln.Addr().String()

	// Each test gets its own temp dir for the host key and data files.
	// The host key is stored inside dataDir (not CWD) so no chdir is needed.
	dir := t.TempDir()

	srv, err := server.New(addr, "", dir, 50*time.Millisecond)
	if err != nil {
		ln.Close()
		t.Fatalf("server.New: %v", err)
	}

	// Apply the scenario's state setup.
	sc.setup(srv.State())

	ctx, cancelFn := context.WithTimeout(context.Background(), 10*time.Second)

	ready := make(chan struct{})
	go func() {
		if err := srv.ServeListener(ctx, ln, ready); err != nil {
			// Expected on context cancel; ignore.
		}
	}()

	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		cancelFn()
		t.Fatal("server did not become ready within 5s")
	}

	return addr, cancelFn
}

// sshCapture connects to the SSH server at addr using SSH env vars, requests
// an 80×24 PTY, reads output for captureMs milliseconds, then returns the
// reconstructed screen state via the mini VT100 emulator.
func sshCapture(t *testing.T, addr, playerName string, envVars []string, captureMs int) string {
	t.Helper()

	cfg := &gossh.ClientConfig{
		User:            playerName,
		Auth:            []gossh.AuthMethod{gossh.Password("")},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(), //nolint:gosec // test only
		Timeout:         5 * time.Second,
	}

	client, err := gossh.Dial("tcp", addr, cfg)
	if err != nil {
		t.Fatalf("ssh.Dial: %v", err)
	}
	defer client.Close()

	sess, err := client.NewSession()
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer sess.Close()

	// Set env vars (the SSH server reads NULL_SPACE_TERM, NULL_SPACE_CLIENT).
	for _, e := range envVars {
		k, v, _ := splitEnv(e)
		if err := sess.Setenv(k, v); err != nil {
			// Some servers reject Setenv; the null-space server accepts it.
			t.Logf("Setenv %s: %v (continuing)", e, err)
		}
	}

	// Request a PTY with fixed dimensions and xterm-256color.
	modes := gossh.TerminalModes{gossh.ECHO: 0, gossh.IGNCR: 1}
	if err := sess.RequestPty("xterm-256color", termH, termW, modes); err != nil {
		t.Fatalf("RequestPty: %v", err)
	}

	var buf bytes.Buffer
	sess.Stdout = &buf

	if err := sess.Shell(); err != nil {
		t.Fatalf("Shell: %v", err)
	}

	// Give the server time to render the initial frame (at least 3 ticks @ 50ms).
	time.Sleep(time.Duration(captureMs) * time.Millisecond)

	sess.Close()
	client.Close()

	// Reconstruct the terminal screen from the captured byte stream.
	scr := newVT100(termH, termW)
	scr.feed(buf.Bytes())
	return scr.String()
}

func splitEnv(e string) (key, val, _ string) {
	for i, c := range e {
		if c == '=' {
			return e[:i], e[i+1:], ""
		}
	}
	return e, "", ""
}

// ─── Integration test variants ───────────────────────────────────────────────

// integrationVariant describes an SSH connection configuration that corresponds
// to one of the four real-world execution paths.
type integrationVariant struct {
	name    string
	envVars []string // SSH env vars to set before connecting
}

// integrationVariants maps to the same four paths as chromeVariants:
//
//	a) server --local     → plain terminal, no special env vars
//	b) server + SSH       → plain terminal, TERM=xterm-256color (set via PTY)
//	c) client --local     → enhanced terminal client
//	d) server + client.exe → enhanced graphical client
var integrationVariants = []integrationVariant{
	{
		name:    "server_local",
		envVars: []string{"NULL_SPACE_TERM=ascii"},
	},
	{
		name:    "server_ssh",
		envVars: []string{"NULL_SPACE_TERM=ascii"},
	},
	{
		name:    "client_local",
		envVars: []string{"NULL_SPACE_TERM=ascii", "NULL_SPACE_CLIENT=terminal"},
	},
	{
		name:    "client_remote",
		envVars: []string{"NULL_SPACE_TERM=ascii", "NULL_SPACE_CLIENT=enhanced"},
	},
}

// ─── Tests ───────────────────────────────────────────────────────────────────

// TestChromeRendersIntegration runs each lobby scenario through all four real
// SSH execution paths and compares the reconstructed screen against
// testdata/<scenario>_chrome_ascii.txt. All integration variants use
// NULL_SPACE_TERM=ascii (monochrome), so they always compare against the ascii
// golden file.
//
// Scenarios marked noIntegration are skipped because they cannot be faithfully
// reproduced over a real SSH connection:
//   - playing/splash: late joiners stay in the lobby, not the game view.
//   - menu/dialog: the integration harness does not send keyboard input.
//
// The setup() function must not pre-add the playerID player. The SSH client
// connects as playerID, and the server adds the player naturally — producing
// the same state as renderChrome's auto-join logic in the unit tests.
//
// sanitize() is applied to both unit and integration output before golden-file
// comparison, so dynamic values (wall-clock timestamp, uptime) match.
func TestChromeRendersIntegration(t *testing.T) {
	for _, sc := range scenarios {
		sc := sc
		if sc.noIntegration {
			continue
		}
		t.Run(sc.name, func(t *testing.T) {
			for _, variant := range integrationVariants {
				variant := variant
				t.Run(variant.name, func(t *testing.T) {
					addr, cancel := startTestServer(t, sc)
					defer cancel()

					playerName := sc.playerID
					if playerName == "" {
						playerName = "alice"
					}

					raw := sshCapture(t, addr, playerName, variant.envVars, 300)
					got := sanitize(raw)

					path := goldenPath(sc.name, "chrome", "ascii")
					checkOrUpdate(t, path, got)
				})
			}
		})
	}
}
