package probe

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"strings"
)

// DefaultGateway returns the IPv4 default gateway from /proc/net/route (Linux).
func DefaultGateway() (string, error) {
	f, err := os.Open("/proc/net/route")
	if err != nil {
		return "", fmt.Errorf("open /proc/net/route: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Scan() // skip header
	bestMetric := uint32(^uint32(0))
	var gw string

	for scanner.Scan() {
		cand, ok := parseRouteCandidate(scanner.Text())
		if !ok {
			continue
		}
		if cand.metric <= bestMetric {
			bestMetric = cand.metric
			gw = cand.ip
		}
	}
	if gw == "" {
		return "", fmt.Errorf("no default gateway found")
	}
	return gw, nil
}

type routeCandidate struct {
	ip     string
	metric uint32
}

// parseRouteCandidate returns a valid default-gateway candidate from a
// /proc/net/route line, or ok=false if the line does not represent one.
func parseRouteCandidate(line string) (routeCandidate, bool) {
	fields := strings.Fields(line)
	if len(fields) < 4 {
		return routeCandidate{}, false
	}
	dest := fields[1]
	gwHex := fields[2]
	flagsHex := fields[3]
	if dest != "00000000" {
		return routeCandidate{}, false
	}
	flags, err := parseHex32(flagsHex)
	if err != nil || flags&0x2 == 0 { // RTF_GATEWAY
		return routeCandidate{}, false
	}
	ip, err := hexIP(gwHex)
	if err != nil || ip == "0.0.0.0" {
		return routeCandidate{}, false
	}
	metric := uint32(0)
	if len(fields) > 6 {
		metric, _ = parseHex32(fields[6])
	}
	return routeCandidate{ip: ip, metric: metric}, true
}

func parseHex32(s string) (uint32, error) {
	var v uint32
	_, err := fmt.Sscanf(s, "%x", &v)
	return v, err
}

func hexIP(hex string) (string, error) {
	var n uint32
	if _, err := fmt.Sscanf(hex, "%x", &n); err != nil {
		return "", err
	}
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, n)
	return net.IP(b).String(), nil
}
