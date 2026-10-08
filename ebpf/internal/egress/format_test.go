package egress

import (
	"fmt"
	"strings"
	"testing"
)

func TestFormatCompatibility(t *testing.T) {
	var b strings.Builder
	fields := []struct {
		decl         string
		offset, size int
	}{
		{"const void * skaddr", 8, 8}, {"int oldstate", 16, 4}, {"int newstate", 20, 4}, {"__u16 sport", 24, 2}, {"__u16 dport", 26, 2}, {"__u16 family", 28, 2}, {"__u16 protocol", 30, 2},
		{"__u8 saddr[4]", 32, 4}, {"__u8 daddr[4]", 36, 4}, {"__u8 saddr_v6[16]", 40, 16}, {"__u8 daddr_v6[16]", 56, 16},
	}
	for _, f := range fields {
		fmt.Fprintf(&b, "field:%s; offset:%d; size:%d; signed:0;\n", f.decl, f.offset, f.size)
	}
	if err := ValidateFormat([]byte(b.String())); err != nil {
		t.Fatal(err)
	}
	if err := ValidateFormat([]byte(strings.Replace(b.String(), "offset:8; size:8", "offset:8; size:4", 1))); err == nil {
		t.Fatal("accepted incompatible pointer layout")
	}
	if err := ValidateFormat(nil); err == nil {
		t.Fatal("accepted missing fields")
	}
}
