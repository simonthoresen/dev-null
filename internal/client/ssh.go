package client

import (
	"fmt"
	"io"
	"net"
	"sync"

	"golang.org/x/crypto/ssh"
)

// SSHConn manages the SSH connection to a dev-null server.
type SSHConn struct {
	client  *ssh.Client
	session *ssh.Session
	stdin   io.WriteCloser
	stdout  io.Reader
	mu      sync.Mutex
	closed  bool
}

// Dial connects to the server and sets up a PTY session.
// It sends DEV_NULL_CLIENT=enhanced so the server knows we support charmaps.
// If terminalMode is true, it sends DEV_NULL_CLIENT=terminal instead (local
// rendering in a terminal, no charmap/canvas support).
// termOverride, if non-empty, is sent as DEV_NULL_TERM to request a specific
// color profile for this session (values: truecolor, 256color, ansi, ascii).
// ptyW and ptyH set the initial PTY dimensions; pass 0 for each to use the
// default (120×50). Callers should pass the actual terminal size to avoid a
// race where the first frame is rendered at the wrong dimensions.
func Dial(host string, port int, player string, terminalMode bool, termOverride string, ptyW, ptyH int) (*SSHConn, error) {
	addr := fmt.Sprintf("%s:%d", host, port)

	config := &ssh.ClientConfig{
		User: player,
		Auth: []ssh.AuthMethod{
			ssh.Password(""), // dev-null uses passwordless SSH
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	conn, err := net.DialTimeout("tcp", addr, 10e9) // 10s timeout
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, config)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("SSH handshake: %w", err)
	}

	client := ssh.NewClient(sshConn, chans, reqs)
	session, err := client.NewSession()
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("new session: %w", err)
	}

	// Tell the server what kind of client we are.
	clientType := "enhanced"
	if terminalMode {
		clientType = "terminal"
	}
	if err := session.Setenv("DEV_NULL_CLIENT", clientType); err != nil {
		// Setenv may fail if the server doesn't accept this env var.
		// Fall back silently — we'll still work, just without charmap support.
		_ = err
	}
	if termOverride != "" {
		_ = session.Setenv("DEV_NULL_TERM", termOverride)
	}

	// Request a PTY.
	modes := ssh.TerminalModes{
		ssh.ECHO:  0,
		ssh.IGNCR: 1,
	}
	if ptyW <= 0 {
		ptyW = 120
	}
	if ptyH <= 0 {
		ptyH = 50
	}
	if err := session.RequestPty("xterm-256color", ptyH, ptyW, modes); err != nil {
		session.Close()
		client.Close()
		return nil, fmt.Errorf("request PTY: %w", err)
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		session.Close()
		client.Close()
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		client.Close()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	if err := session.Shell(); err != nil {
		session.Close()
		client.Close()
		return nil, fmt.Errorf("start shell: %w", err)
	}

	return &SSHConn{
		client:  client,
		session: session,
		stdin:   stdin,
		stdout:  stdout,
	}, nil
}

// Read reads from the SSH stdout stream.
func (c *SSHConn) Read(p []byte) (int, error) {
	return c.stdout.Read(p)
}

// Write sends data to the SSH stdin stream.
func (c *SSHConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stdin.Write(p)
}

// SendWindowChange notifies the server of a terminal resize.
func (c *SSHConn) SendWindowChange(width, height int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	return c.session.WindowChange(height, width)
}

// Close shuts down the SSH session and connection.
func (c *SSHConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	c.session.Close()
	return c.client.Close()
}
