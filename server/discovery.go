package server

import (
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// detectPublicIP queries an external service to discover this machine's public
// IP address. Returns an empty string on failure so callers can fall back
// gracefully.
func detectPublicIP() string {
	services := []string{
		"https://ifconfig.me/ip",
		"https://api.ipify.org",
		"https://icanhazip.com",
	}
	client := &http.Client{Timeout: 5 * time.Second}
	for _, url := range services {
		resp, err := client.Get(url)
		if err != nil {
			slog.Debug("public IP detection failed", "url", url, "error", err)
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 64))
		resp.Body.Close() //nolint:errcheck
		if err != nil || resp.StatusCode != http.StatusOK {
			continue
		}
		ip := strings.TrimSpace(string(body))
		if ip != "" {
			slog.Info("detected public IP", "ip", ip)
			return ip
		}
	}
	slog.Warn("could not detect public IP from any service")
	return ""
}
