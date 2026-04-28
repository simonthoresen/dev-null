package network

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/grandcat/zeroconf"
)

const (
	lanDiscoveryService = "_devnull._tcp"
	lanDiscoveryDomain  = "local."
)

// LANAdvertiser announces a DevNull server on the local network via mDNS.
type LANAdvertiser struct {
	server *zeroconf.Server
}

// StartLANAdvertiser starts mDNS advertisement for a DevNull SSH endpoint.
func StartLANAdvertiser(port int) (*LANAdvertiser, error) {
	if port <= 0 {
		return nil, fmt.Errorf("invalid LAN discovery port: %d", port)
	}

	instance := strings.TrimSpace(defaultLANInstanceName())
	if instance == "" {
		instance = "DevNull"
	}

	server, err := zeroconf.Register(instance, lanDiscoveryService, lanDiscoveryDomain, port, []string{"v=1"}, nil)
	if err != nil {
		return nil, err
	}

	slog.Info("lan discovery: advertising service", "instance", instance, "port", port)
	return &LANAdvertiser{server: server}, nil
}

func defaultLANInstanceName() string {
	host, err := os.Hostname()
	if err != nil {
		return "DevNull"
	}
	host = strings.TrimSpace(host)
	if host == "" {
		return "DevNull"
	}
	return host
}

// Close stops LAN advertisement.
func (a *LANAdvertiser) Close() {
	if a == nil || a.server == nil {
		return
	}
	a.server.Shutdown()
	a.server = nil
	slog.Info("lan discovery: advertisement stopped")
}

// LANServer describes a discovered DevNull server on the local network.
type LANServer struct {
	Name string
	Host string
	Port int
}

// DiscoverLANServers scans mDNS for DevNull servers for up to timeout duration.
func DiscoverLANServers(timeout time.Duration) ([]LANServer, error) {
	if timeout <= 0 {
		timeout = 800 * time.Millisecond
	}

	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	entries := make(chan *zeroconf.ServiceEntry)
	found := make(map[string]LANServer)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-ctx.Done():
				return
			case entry, ok := <-entries:
				if !ok || entry == nil {
					return
				}
				host := pickLANHost(entry)
				if host == "" || entry.Port <= 0 {
					continue
				}
				name := normalizeLANName(entry.Instance)
				if name == "" {
					name = normalizeLANName(entry.HostName)
				}
				if name == "" {
					name = host
				}
				key := net.JoinHostPort(host, strconv.Itoa(entry.Port))
				found[key] = LANServer{
					Name: name,
					Host: host,
					Port: entry.Port,
				}
			}
		}
	}()

	if err := resolver.Browse(ctx, lanDiscoveryService, lanDiscoveryDomain, entries); err != nil {
		cancel()
		<-done
		return nil, err
	}

	<-ctx.Done()
	<-done

	servers := make([]LANServer, 0, len(found))
	for _, server := range found {
		servers = append(servers, server)
	}
	sort.Slice(servers, func(i, j int) bool {
		ni := strings.ToLower(servers[i].Name)
		nj := strings.ToLower(servers[j].Name)
		if ni != nj {
			return ni < nj
		}
		hi := strings.ToLower(servers[i].Host)
		hj := strings.ToLower(servers[j].Host)
		if hi != hj {
			return hi < hj
		}
		return servers[i].Port < servers[j].Port
	})
	return servers, nil
}

func pickLANHost(entry *zeroconf.ServiceEntry) string {
	for _, ip := range entry.AddrIPv4 {
		if ip == nil || ip.IsLoopback() || ip.IsUnspecified() {
			continue
		}
		return ip.String()
	}
	for _, ip := range entry.AddrIPv4 {
		if ip == nil || ip.IsUnspecified() {
			continue
		}
		return ip.String()
	}
	for _, ip := range entry.AddrIPv6 {
		if ip == nil || ip.IsLoopback() || ip.IsUnspecified() {
			continue
		}
		return ip.String()
	}
	for _, ip := range entry.AddrIPv6 {
		if ip == nil || ip.IsUnspecified() {
			continue
		}
		return ip.String()
	}
	return ""
}

func normalizeLANName(raw string) string {
	name := strings.TrimSpace(raw)
	name = strings.TrimSuffix(name, ".")
	name = strings.TrimSuffix(name, ".local")
	name = strings.TrimSuffix(name, ".local.")
	return strings.TrimSpace(name)
}
