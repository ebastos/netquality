package probe

import (
	"context"
	"fmt"
	"net"
	"time"
)

// DNSResult holds the outcome of a DNS lookup probe.
type DNSResult struct {
	LatencyMs float64
	OK        bool
	Resolver  string
}

// DNSProbe performs a DNS lookup using the system resolver (or the one in config).
func DNSProbe(ctx context.Context, queryHost string, timeout time.Duration) (DNSResult, error) {
	return DNSProbeResolver(ctx, queryHost, "", timeout)
}

// DNSProbeResolver is the lower-level variant that allows specifying an explicit resolver IP.
func DNSProbeResolver(ctx context.Context, queryHost, resolverIP string, timeout time.Duration) (DNSResult, error) {
	res := DNSResult{Resolver: "system"}
	dialer := &net.Dialer{Timeout: timeout}
	var r net.Resolver
	if resolverIP != "" {
		res.Resolver = resolverIP
		r = net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				return dialer.DialContext(ctx, "udp", net.JoinHostPort(resolverIP, "53"))
			},
		}
	} else {
		r = net.Resolver{}
	}

	start := time.Now()
	_, err := r.LookupHost(ctx, queryHost)
	elapsed := time.Since(start)
	res.LatencyMs = float64(elapsed.Milliseconds())
	if err != nil {
		res.OK = false
		res.LatencyMs = float64(timeout.Milliseconds())
		if res.LatencyMs < 5000 {
			res.LatencyMs = 5000
		}
		return res, fmt.Errorf("dns lookup %s: %w", queryHost, err)
	}
	res.OK = true
	return res, nil
}
