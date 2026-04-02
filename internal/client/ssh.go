package client

import (
	"fmt"
	"io"
	"net"
	"sync"

	"golang.org/x/crypto/ssh"
)

// SSHConn manages the SSH connection to a null-space server.
type SSHConn struct {
	client  *ssh.Client
	session *ssh.Session
	stdin   io.WriteCloser
	stdout  io.Reader
	mu      sync.Mutex
	closed  bool
}

// Dial connects to the server and sets up a PTY session.
// It sends NULL_SPACE_CLIENT=enhanced so the server knows we support charmaps.
func Dial(host string, port int, player string) (*SSHConn, error) {
	addr := fmt.Sprintf("%s:%d", host, port)

	config := &ssh.ClientConfig{
		User: player,
		Auth: []ssh.AuthMethod{
			ssh.Password(""), // null-space uses passwordless SSH
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

	// Tell the server we're an enhanced client.
	if err := session.Setenv("NULL_SPACE_CLIENT", "enhanced"); err != nil {
		// Setenv may fail if the server doesn't accept this env var.
		// Fall back silently — we'll still work, just without charmap support.
		_ = err
	}

	// Request a PTY.
	modes := ssh.TerminalModes{
		ssh.ECHO:  0,
		ssh.IGNCR: 1,
	}
	if err := session.RequestPty("xterm-256color", 50, 120, modes); err != nil {
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
