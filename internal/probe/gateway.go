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
	scanner.Scan() // header
	bestMetric := uint32(^uint32(0))
	var gw string

	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 4 {
			continue
		}
		dest := fields[1]
		gatewayHex := fields[2]
		flagsHex := fields[3]
		if dest != "00000000" {
			continue
		}
		flags, err := parseHex32(flagsHex)
		if err != nil || flags&0x2 == 0 { // RTF_GATEWAY
			continue
		}
		ip, err := hexIP(gatewayHex)
		if err != nil || ip == "0.0.0.0" {
			continue
		}
		metric := uint32(0)
		if len(fields) > 6 {
			metric, _ = parseHex32(fields[6])
		}
		if metric <= bestMetric {
			bestMetric = metric
			gw = ip
		}
	}
	if gw == "" {
		return "", fmt.Errorf("no default gateway found")
	}
	return gw, nil
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
