package egress

import (
	"net/netip"
	"testing"
)

func TestStrictConfig(t *testing.T) {
	for _, yaml := range []string{
		"ports: [443]", "source_cidrs: [bad]", "source_cidrs: [10.0.0.0/8]\nports: [0]", "source_cidrs: [10.0.0.0/8]\nports: [443,443]",
		"source_cidrs: [10.0.0.0/8]\nport: [80]", "source_cidrs: [10.0.0.0/8]\n---\nports: [80]", "source_cidrs: [10.0.0.0/8]\nlog:\n  max_backups: 0",
	} {
		if _, err := ParseConfig([]byte(yaml)); err == nil {
			t.Fatalf("accepted %q", yaml)
		}
	}
	c, err := ParseConfig([]byte("source_cidrs: [10.244.0.0/16]"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Ports[1] != 443 || c.ListenAddr != ":9359" {
		t.Fatal(c)
	}
}
func TestFilter(t *testing.T) {
	c := Config{Ports: []uint16{443}, SourceCIDRs: []string{"10.244.0.0/16", "fd00::/64"}, DestinationCIDRs: []string{"172.16.0.0/16", "2001:db8::/32"}, ExcludeCIDRs: []string{"172.16.1.0/24"}}
	f, err := NewFilter(c)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		src, dst string
		port     uint16
		want     bool
	}{
		{"10.244.1.2", "172.16.2.1", 443, true},  // private external target is valid
		{"10.244.1.2", "172.16.1.1", 443, false}, // exclusion wins
		{"192.168.1.2", "172.16.2.1", 443, false},
		{"10.244.1.2", "8.8.8.8", 443, false},
		{"10.244.1.2", "172.16.2.1", 80, false},
		{"fd00::42", "2001:db8::80", 443, true},
	} {
		if got := f.Match(Event{SourceIP: netip.MustParseAddr(tc.src), DestinationIP: netip.MustParseAddr(tc.dst), DestinationPort: tc.port}); got != tc.want {
			t.Fatal(tc, got)
		}
	}
}
