package probe

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

type HTTPResult struct {
	LatencyMs float64
	OK        bool
	Status    int
}

func ProbeTarget(ctx context.Context, name, url, host string, port int, method, mode string, timeout time.Duration) (HTTPResult, error) {
	mode = strings.ToLower(mode)
	if mode == "" {
		if url != "" {
			mode = "http"
		} else {
			mode = "tcp"
		}
	}
	switch mode {
	case "tcp":
		return TCPProbe(ctx, name, host, port, timeout)
	case "http", "https":
		return HTTPProbe(ctx, url, method, timeout)
	default:
		return HTTPResult{}, fmt.Errorf("unknown mode %q", mode)
	}
}

func TCPProbe(ctx context.Context, _, host string, port int, timeout time.Duration) (HTTPResult, error) {
	if port == 0 {
		port = 443
	}
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	dialer := net.Dialer{Timeout: timeout}
	start := time.Now()
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	elapsed := time.Since(start)
	res := HTTPResult{LatencyMs: float64(elapsed.Milliseconds())}
	if err != nil {
		res.OK = false
		return res, err
	}
	_ = conn.Close()
	res.OK = true
	return res, nil
}

func HTTPProbe(ctx context.Context, rawURL, method string, timeout time.Duration) (HTTPResult, error) {
	if method == "" {
		method = http.MethodHead
	}
	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: false},
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, nil)
	if err != nil {
		return HTTPResult{}, err
	}
	start := time.Now()
	resp, err := client.Do(req)
	elapsed := time.Since(start)
	res := HTTPResult{LatencyMs: float64(elapsed.Milliseconds())}
	if err != nil {
		res.OK = false
		return res, err
	}
	defer resp.Body.Close()
	res.Status = resp.StatusCode
	res.OK = resp.StatusCode < 500
	return res, nil
}
