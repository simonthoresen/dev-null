package server

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/huin/goupnp/dcps/internetgateway2"
)

// upnpMapping holds state for a UPnP port mapping so it can be cleaned up on
// shutdown.
type upnpMapping struct {
	client       *internetgateway2.WANIPConnection2
	externalPort uint16
}

// tryUPnP attempts to create a port mapping on the local router via UPnP IGD.
// It tries WANIPConnection2 first, then WANIPConnection1, then WANPPPConnection1.
// Returns a non-nil *upnpMapping on success so the caller can remove it later.
func tryUPnP(port string) *upnpMapping {
	p, err := strconv.ParseUint(port, 10, 16)
	if err != nil {
		slog.Warn("upnp: invalid port", "port", port, "error", err)
		return nil
	}
	externalPort := uint16(p)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Try IGD v2 WANIPConnection2 first (most modern routers).
	if mapping := tryWANIPConnection2(ctx, externalPort); mapping != nil {
		return mapping
	}

	// Fall back to IGD v1 WANIPConnection1.
	if tryWANIPConnection1(ctx, externalPort) {
		slog.Info("upnp: mapped via WANIPConnection1 (no cleanup handle)")
		return nil // v1 clients have a different type; best-effort
	}

	// Fall back to WANPPPConnection1 (DSL modems).
	if tryWANPPPConnection1(ctx, externalPort) {
		slog.Info("upnp: mapped via WANPPPConnection1 (no cleanup handle)")
		return nil
	}

	slog.Info("upnp: no IGD gateway found on this network")
	return nil
}

func tryWANIPConnection2(ctx context.Context, port uint16) *upnpMapping {
	clients, _, err := internetgateway2.NewWANIPConnection2ClientsCtx(ctx)
	if err != nil || len(clients) == 0 {
		return nil
	}
	for _, client := range clients {
		err := client.AddPortMappingCtx(
			ctx,
			"",          // remote host (empty = any)
			port,        // external port
			"TCP",       // protocol
			port,        // internal port
			client.LocalAddr().String(), // internal client IP
			true,        // enabled
			"null-space", // description
			0,           // lease duration (0 = permanent until removed)
		)
		if err != nil {
			slog.Debug("upnp: WANIPConnection2 AddPortMapping failed", "error", err)
			continue
		}
		slog.Info("upnp: port mapped via WANIPConnection2",
			"external_port", port,
			"internal_ip", client.LocalAddr().String(),
		)
		return &upnpMapping{client: client, externalPort: port}
	}
	return nil
}

func tryWANIPConnection1(ctx context.Context, port uint16) bool {
	clients, _, err := internetgateway2.NewWANIPConnection1ClientsCtx(ctx)
	if err != nil || len(clients) == 0 {
		return false
	}
	for _, client := range clients {
		err := client.AddPortMappingCtx(
			ctx,
			"",
			port,
			"TCP",
			port,
			client.LocalAddr().String(),
			true,
			"null-space",
			0,
		)
		if err != nil {
			slog.Debug("upnp: WANIPConnection1 AddPortMapping failed", "error", err)
			continue
		}
		slog.Info("upnp: port mapped via WANIPConnection1",
			"external_port", port,
			"internal_ip", client.LocalAddr().String(),
		)
		return true
	}
	return false
}

func tryWANPPPConnection1(ctx context.Context, port uint16) bool {
	clients, _, err := internetgateway2.NewWANPPPConnection1ClientsCtx(ctx)
	if err != nil || len(clients) == 0 {
		return false
	}
	for _, client := range clients {
		err := client.AddPortMappingCtx(
			ctx,
			"",
			port,
			"TCP",
			port,
			client.LocalAddr().String(),
			true,
			"null-space",
			0,
		)
		if err != nil {
			slog.Debug("upnp: WANPPPConnection1 AddPortMapping failed", "error", err)
			continue
		}
		slog.Info("upnp: port mapped via WANPPPConnection1",
			"external_port", port,
			"internal_ip", client.LocalAddr().String(),
		)
		return true
	}
	return false
}

// removeMapping removes a previously created UPnP port mapping.
func (m *upnpMapping) removeMapping() {
	if m == nil || m.client == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := m.client.DeletePortMappingCtx(ctx, "", m.externalPort, "TCP")
	if err != nil {
		slog.Warn("upnp: failed to remove port mapping", "port", m.externalPort, "error", err)
	} else {
		slog.Info("upnp: removed port mapping", "port", m.externalPort)
	}
}
