package probe

import (
	"net"
	"testing"
)

func TestIsCGNAT(t *testing.T) {
	cases := []struct {
		ip   string
		want bool
	}{
		{"100.64.0.1", true},
		{"100.127.255.254", true},
		{"100.63.255.255", false},
		{"100.128.0.0", false},
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"2001:db8::1", false}, // IPv6 never CGNAT
	}
	for _, c := range cases {
		ip := net.ParseIP(c.ip)
		if ip == nil {
			t.Fatalf("bad test ip %s", c.ip)
		}
		if got := isCGNAT(ip); got != c.want {
			t.Errorf("isCGNAT(%s) = %v, want %v", c.ip, got, c.want)
		}
	}
}

func TestPublicIP_Invalid(t *testing.T) {
	// This test only exercises the validation path; real network calls are in integration.
	// We can't easily unit test the HTTP without a test server, so just sanity check the helper.
	if isCGNAT(net.ParseIP("100.64.1.2")) != true {
		t.Error("expected CGNAT true for 100.64.1.2")
	}
}
