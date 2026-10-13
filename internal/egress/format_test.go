package egress

import (
	"fmt"
	"strings"
	"testing"
)

func traceFormat(shift int) string {
	var b strings.Builder
	fields := []struct {
		decl         string
		offset, size int
	}{
		{"const void * skaddr", 8, 8}, {"int oldstate", 16, 4}, {"int newstate", 20, 4},
		{"__u16 sport", 24, 2}, {"__u16 dport", 26, 2}, {"__u16 family", 28, 2}, {"__u16 protocol", 30, 2},
		{"__u8 saddr[4]", 32, 4}, {"__u8 daddr[4]", 36, 4}, {"__u8 saddr_v6[16]", 40, 16}, {"__u8 daddr_v6[16]", 56, 16},
	}
	for _, field := range fields {
		fmt.Fprintf(&b, "field:%s; offset:%d; size:%d; signed:0;\n", field.decl, field.offset+shift, field.size)
	}
	return b.String()
}

func TestFormatCompatibility(t *testing.T) {
	for _, shift := range []int{0, 8, 16} {
		t.Run(fmt.Sprint(shift), func(t *testing.T) {
			layout, err := ParseFormat([]byte(traceFormat(shift)))
			if err != nil {
				t.Fatal(err)
			}
			if layout[0] != uint32(8+shift) || layout[7] != uint32(32+shift) {
				t.Fatal(layout)
			}
		})
	}
	for _, data := range []string{
		"", strings.Replace(traceFormat(0), "offset:8; size:8", "offset:8; size:4", 1),
	} {
		if err := ValidateFormat([]byte(data)); err == nil {
			t.Fatal("accepted incompatible layout")
		}
	}
}

func TestFormatRejectsUnsafeFields(t *testing.T) {
	good := traceFormat(0)
	for _, data := range []string{
		strings.Replace(good, "offset:32; size:4", "offset:4094; size:4", 1),
		strings.Replace(good, "offset:32; size:4", "offset:9999999999999999999999; size:4", 1),
		strings.Replace(good, "offset:32; size:4", "offset:24; size:4", 1),
		strings.Replace(good, "offset:8; size:8", "offset:0; size:8", 1),
		good + "field:__u8 saddr[4]; offset:32; size:4; signed:0;\n",
		strings.Replace(good, " saddr[4];", " missing[4];", 1),
	} {
		if _, err := ParseFormat([]byte(data)); err == nil {
			t.Fatalf("accepted unsafe format %q", data)
		}
	}
}

func TestFormatIndependentAddressOffsets(t *testing.T) {
	data := traceFormat(0)
	// Extra vendor fields before the addresses, without moving the scalar fields.
	for _, offset := range []int{56, 40, 36, 32} {
		data = strings.Replace(data, fmt.Sprintf("offset:%d;", offset), fmt.Sprintf("offset:%d;", offset+8), 1)
	}
	layout, err := ParseFormat([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if layout[0] != 8 || layout[7] != 40 || layout[10] != 64 {
		t.Fatal(layout)
	}
}
