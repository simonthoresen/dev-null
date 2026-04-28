// Package invite decodes DevNull's base64url invite token into a
// prioritised list of SSH endpoints. The token format is documented
// in internal/server/server.go (inviteToken) and matches join.ps1's
// PowerShell decoder.
package invite

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strconv"
)

// Endpoint is a single SSH endpoint candidate (host:port).
type Endpoint struct {
	Host string
	Port int
}

// Decode parses a base64url invite token and returns endpoints in
// priority order: localhost first, then LAN IP, then public IP, then
// the Pinggy tunnel. Absent fields are omitted from the result.
func Decode(token string) ([]Endpoint, error) {
	if token == "" {
		return nil, errors.New("empty token")
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		// Tolerate padding from sloppy senders.
		if alt, e2 := base64.URLEncoding.DecodeString(token); e2 == nil {
			raw = alt
		} else {
			return nil, fmt.Errorf("decode base64url: %w", err)
		}
	}
	if len(raw) < 2 {
		return nil, fmt.Errorf("token too short: %d bytes", len(raw))
	}

	sshPort := int(binary.BigEndian.Uint16(raw[0:2]))
	endpoints := []Endpoint{{Host: "localhost", Port: sshPort}}

	// LAN IP (bytes 2–5).
	if len(raw) >= 6 {
		ip := net.IPv4(raw[2], raw[3], raw[4], raw[5])
		if !ip.Equal(net.IPv4zero) {
			endpoints = append(endpoints, Endpoint{Host: ip.String(), Port: sshPort})
		}
	}

	// Public/UPnP IP (bytes 6–9).
	if len(raw) >= 10 {
		ip := net.IPv4(raw[6], raw[7], raw[8], raw[9])
		if !ip.Equal(net.IPv4zero) {
			endpoints = append(endpoints, Endpoint{Host: ip.String(), Port: sshPort})
		}
	}

	// Pinggy (bytes 10–11 port, 12+ hostname).
	if len(raw) >= 12 {
		pPort := int(binary.BigEndian.Uint16(raw[10:12]))
		if pPort != 0 && len(raw) > 12 {
			endpoints = append(endpoints, Endpoint{
				Host: string(raw[12:]),
				Port: pPort,
			})
		}
	}

	return endpoints, nil
}

// FormatHostPort returns "host:port" suitable for ssh / dial.
func (e Endpoint) FormatHostPort() string {
	return net.JoinHostPort(e.Host, strconv.Itoa(e.Port))
}
