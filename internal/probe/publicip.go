package probe

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

type PublicIPResult struct {
	IP    string
	CGNAT bool
}

func PublicIP(ctx context.Context, endpoint string, timeout time.Duration) (PublicIPResult, error) {
	client := &http.Client{
		Timeout: timeout,
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return PublicIPResult{}, err
	}
	req.Header.Set("User-Agent", "netqualityd (+https://github.com/ebastos/netquality)")

	start := time.Now()
	resp, err := client.Do(req)
	_ = start // latency not needed for v1
	if err != nil {
		return PublicIPResult{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return PublicIPResult{}, fmt.Errorf("unexpected status %d from %s", resp.StatusCode, endpoint)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 128))
	if err != nil {
		return PublicIPResult{}, err
	}
	ipStr := strings.TrimSpace(string(body))
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return PublicIPResult{}, fmt.Errorf("response is not a valid IP: %q", ipStr)
	}

	return PublicIPResult{
		IP:    ipStr,
		CGNAT: isCGNAT(ip),
	}, nil
}

func isCGNAT(ip net.IP) bool {
	if ip4 := ip.To4(); ip4 != nil {
		_, cgnatNet, _ := net.ParseCIDR("100.64.0.0/10")
		return cgnatNet.Contains(ip4)
	}
	return false
}
