//go:build windows

package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/xpty"

	"dev-null/internal/runlog"
)

var tcpAddressPattern = regexp.MustCompile(`tcp://([A-Za-z0-9.-]+):(\d+)`)

type config struct {
	listenAddress string
	host          string
	port          int
	user          string
	cols          int
	rows          int
	readyTimeout  time.Duration
	statusFile    string
}

type pinggyEndpoint struct {
	tcpAddress   string
	host         string
	port         string
	joinCommand  string
	passwordSent bool
}

type outputTail struct {
	mu   sync.Mutex
	text string
	max  int
}

type statusReporter struct {
	mu   sync.Mutex
	file *os.File
}

type lineEmitter struct {
	mu       sync.Mutex
	buffer   strings.Builder
	lastLine string
	reporter *statusReporter
}

func main() {
	cleanupLog, err := runlog.ConfigureFromEnv("pinggy-helper")
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not configure logging: %v\n", err)
		os.Exit(1)
	}
	defer cleanupLog() //nolint:errcheck

	cfg := config{}
	flag.StringVar(&cfg.listenAddress, "listen", "127.0.0.1:23234", "local address to forward through Pinggy")
	flag.StringVar(&cfg.host, "host", "a.pinggy.io", "Pinggy host")
	flag.IntVar(&cfg.port, "port", 443, "Pinggy SSH port")
	flag.StringVar(&cfg.user, "user", "tcp", "Pinggy SSH user")
	flag.IntVar(&cfg.cols, "cols", 120, "pseudo terminal width")
	flag.IntVar(&cfg.rows, "rows", 40, "pseudo terminal height")
	flag.DurationVar(&cfg.readyTimeout, "timeout", 45*time.Second, "maximum time to wait for the public tunnel address")
	flag.StringVar(&cfg.statusFile, "status-file", "", "path to a file that receives machine-readable tunnel status updates")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	slog.Info("starting pinggy helper", "listen", cfg.listenAddress, "host", cfg.host, "port", cfg.port, "status_file", cfg.statusFile)
	if err := run(ctx, cfg); err != nil {
		slog.Error("pinggy helper failed", "error", err)
		fmt.Fprintf(os.Stderr, "pinggy helper failed: %v\n", err)
		os.Exit(1)
	}
	slog.Info("pinggy helper stopped")
}

func run(ctx context.Context, cfg config) error {
	reporter, err := newStatusReporter(cfg.statusFile)
	if err != nil {
		return fmt.Errorf("open status file: %w", err)
	}
	defer reporter.Close() //nolint:errcheck

	pty, err := xpty.NewPty(cfg.cols, cfg.rows)
	if err != nil {
		reporter.SetError(fmt.Sprintf("create pseudo terminal: %v", err))
		return fmt.Errorf("create pseudo terminal: %w", err)
	}
	defer pty.Close() //nolint:errcheck

	destination := fmt.Sprintf("%s@%s", cfg.user, cfg.host)
	args := []string{
		"-p", strconv.Itoa(cfg.port),
		"-o", "ServerAliveInterval=30",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "ExitOnForwardFailure=yes",
		"-R0:" + cfg.listenAddress,
		destination,
	}

	cmd := exec.Command("ssh", args...)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	if err := pty.Start(cmd); err != nil {
		reporter.SetError(fmt.Sprintf("start ssh tunnel: %v", err))
		slog.Error("failed to start ssh tunnel", "error", err)
		return fmt.Errorf("start ssh tunnel: %w", err)
	}
	slog.Info("ssh tunnel process started", "destination", destination)

	reporter.SetStatus("starting")
	emitPinggyLog(reporter, "starting tunnel helper")

	tail := &outputTail{max: 32768}
	endpointCh := make(chan pinggyEndpoint, 1)
	readErrCh := make(chan error, 1)
	waitErrCh := make(chan error, 1)
	emitter := &lineEmitter{reporter: reporter}

	go func() {
		readErrCh <- monitorOutput(pty, tail, endpointCh, emitter)
	}()

	go func() {
		waitErrCh <- xpty.WaitProcess(ctx, cmd)
	}()

	endpointFound := false
	timeout := time.NewTimer(cfg.readyTimeout)
	defer timeout.Stop()

	for {
		select {
		case endpoint := <-endpointCh:
			if endpointFound {
				continue
			}

			endpointFound = true
			reporter.SetField("TCP", endpoint.tcpAddress)
			reporter.SetField("HOST", endpoint.host)
			reporter.SetField("PORT", endpoint.port)
			reporter.SetField("JOIN", endpoint.joinCommand)
			if endpoint.passwordSent {
				reporter.SetStatus("password-submitted")
				emitPinggyLog(reporter, "submitted blank password to Pinggy")
			}
			slog.Info("pinggy endpoint ready", "tcp", endpoint.tcpAddress, "join", endpoint.joinCommand)
			emitPinggyLog(reporter, fmt.Sprintf("public tunnel ready: %s", endpoint.tcpAddress))
			emitPinggyLog(reporter, fmt.Sprintf("join with: %s", endpoint.joinCommand))
			reporter.SetStatus("ready")
			if !timeout.Stop() {
				select {
				case <-timeout.C:
				default:
				}
			}

		case err := <-readErrCh:
			if err != nil && !errors.Is(err, io.EOF) {
				reporter.SetError(fmt.Sprintf("read tunnel output: %v", err))
				slog.Error("failed reading tunnel output", "error", err)
				return fmt.Errorf("read tunnel output: %w", err)
			}

		case err := <-waitErrCh:
			if ctx.Err() != nil {
				return nil
			}
			if endpointFound {
				if err != nil {
					reporter.SetError(fmt.Sprintf("ssh tunnel exited: %v", err))
					slog.Error("ssh tunnel exited with error", "error", err)
					return fmt.Errorf("ssh tunnel exited: %w", err)
				}
				reporter.SetStatus("exited")
				slog.Info("ssh tunnel exited cleanly")
				emitPinggyLog(reporter, "tunnel exited")
				return nil
			}

			lastOutput := strings.TrimSpace(tail.String())
			if lastOutput == "" {
				lastOutput = "(no terminal output captured)"
			}

			if err != nil {
				reporter.SetError(fmt.Sprintf("ssh tunnel exited before endpoint was detected: %v | last output: %s", err, squashedOutput(lastOutput)))
				slog.Error("ssh tunnel exited before endpoint detection", "error", err, "last_output", squashedOutput(lastOutput))
				return fmt.Errorf("ssh tunnel exited before endpoint was detected: %w\nlast output:\n%s", err, lastOutput)
			}

			reporter.SetError(fmt.Sprintf("ssh tunnel exited before endpoint was detected | last output: %s", squashedOutput(lastOutput)))
			slog.Error("ssh tunnel exited before endpoint detection", "last_output", squashedOutput(lastOutput))
			return fmt.Errorf("ssh tunnel exited before endpoint was detected\nlast output:\n%s", lastOutput)

		case <-timeout.C:
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			reporter.SetError(fmt.Sprintf("timed out waiting %s for Pinggy to publish a tunnel address | last output: %s", cfg.readyTimeout, squashedOutput(strings.TrimSpace(tail.String()))))
			slog.Error("timed out waiting for pinggy endpoint", "timeout", cfg.readyTimeout, "last_output", squashedOutput(strings.TrimSpace(tail.String())))
			return fmt.Errorf("timed out waiting %s for Pinggy to publish a tunnel address\nlast output:\n%s", cfg.readyTimeout, strings.TrimSpace(tail.String()))

		case <-ctx.Done():
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			reporter.SetStatus("stopped")
			slog.Info("pinggy helper context cancelled")
			return nil
		}
	}
}

func monitorOutput(pty xpty.Pty, tail *outputTail, endpointCh chan<- pinggyEndpoint, emitter *lineEmitter) error {
	buffer := make([]byte, 4096)
	passwordSent := false
	endpointSent := false

	for {
		n, err := pty.Read(buffer)
		if n > 0 {
			cleaned := sanitizeTerminalText(string(buffer[:n]))
			if cleaned != "" {
				tail.Append(cleaned)
				emitter.Append(cleaned)
				slog.Debug("received tunnel output", "chunk", squashedOutput(cleaned))
			}

			snapshot := tail.String()
			lowerSnapshot := strings.ToLower(snapshot)
			if !passwordSent && strings.Contains(lowerSnapshot, "password:") {
				if _, writeErr := pty.Write([]byte("\r\n")); writeErr != nil {
					return fmt.Errorf("submit blank password: %w", writeErr)
				}
				slog.Info("submitted blank password to pinggy")
				passwordSent = true
			}

			if !endpointSent {
				match := tcpAddressPattern.FindStringSubmatch(snapshot)
				if len(match) == 3 {
					endpointSent = true
					endpointCh <- pinggyEndpoint{
						tcpAddress:   match[0],
						host:         match[1],
						port:         match[2],
						joinCommand:  fmt.Sprintf("ssh your-name@%s -p %s", match[1], match[2]),
						passwordSent: passwordSent,
					}
				}
			}
		}

		if err != nil {
			emitter.Flush()
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

func newStatusReporter(path string) (*statusReporter, error) {
	if path == "" {
		return &statusReporter{}, nil
	}

	file, err := os.Create(path)
	if err != nil {
		return nil, err
	}

	return &statusReporter{file: file}, nil
}

func (r *statusReporter) Close() error {
	if r.file == nil {
		return nil
	}
	return r.file.Close()
}

func (r *statusReporter) SetStatus(value string) {
	r.writeLine("PINGGY_STATUS=" + value)
}

func (r *statusReporter) SetField(key string, value string) {
	r.writeLine("PINGGY_" + key + "=" + value)
}

func (r *statusReporter) SetError(value string) {
	r.writeLine("PINGGY_ERROR=" + value)
}

func (r *statusReporter) AppendLog(value string) {
	r.writeLine("PINGGY_LOG=" + value)
}

func (r *statusReporter) writeLine(line string) {
	if r.file == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	writer := bufio.NewWriter(r.file)
	_, _ = writer.WriteString(line + "\n")
	_ = writer.Flush()
}

func (e *lineEmitter) Append(chunk string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	for _, r := range chunk {
		if r == '\r' || r == '\n' {
			e.flushLocked()
			continue
		}

		e.buffer.WriteRune(r)
	}
}

func (e *lineEmitter) Flush() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.flushLocked()
}

func (e *lineEmitter) flushLocked() {
	line := strings.TrimSpace(e.buffer.String())
	e.buffer.Reset()
	if line == "" || line == e.lastLine {
		return
	}

	e.lastLine = line
	if e.reporter == nil || e.reporter.file == nil {
		logPinggy(line)
	}
}

func logPinggy(message string) {
	if strings.TrimSpace(message) == "" {
		return
	}
	fmt.Printf("[%s] [pinggy] %s\n", time.Now().Format("15:04:05"), message)
}

func emitPinggyLog(reporter *statusReporter, message string) {
	if reporter != nil && reporter.file != nil {
		reporter.AppendLog(message)
		return
	}
	logPinggy(message)
}

func squashedOutput(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func sanitizeTerminalText(raw string) string {
	stripped := ansi.Strip(raw)
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\r', r == '\n', r == '\t':
			return r
		case unicode.IsControl(r):
			return -1
		default:
			return r
		}
	}, stripped)
}

func (t *outputTail) Append(chunk string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.text += chunk
	if len(t.text) <= t.max {
		return
	}

	t.text = t.text[len(t.text)-t.max:]
}

func (t *outputTail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.text
}
