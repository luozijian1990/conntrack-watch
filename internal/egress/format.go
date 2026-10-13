package egress

import (
	"fmt"
	"regexp"
	"strconv"
)

var formatField = regexp.MustCompile(`field:[^;]*\s([A-Za-z_][A-Za-z_0-9]*)(?:\[[0-9]+\])?;\s*offset:(\d+);\s*size:(\d+);`)

// Order matches trace_layout in bpf/egress.c. These are sizes, not fixed offsets.
var traceFields = [...]string{"skaddr", "oldstate", "newstate", "sport", "dport", "family", "protocol", "saddr", "daddr", "saddr_v6", "daddr_v6"}
var traceSizes = [...]int{8, 4, 4, 2, 2, 2, 2, 4, 4, 16, 16}

type TraceLayout [11]uint32

// ParseFormat uses the running kernel's tracefs layout, including vendor changes.
// Required fields must be present, correctly sized, bounded and non-overlapping.
func ParseFormat(data []byte) (TraceLayout, error) {
	var offsets TraceLayout
	found := map[string][2]int{}
	for _, m := range formatField.FindAllSubmatch(data, -1) {
		name := string(m[1])
		if _, exists := found[name]; exists {
			return offsets, fmt.Errorf("duplicate tracepoint field %s", name)
		}
		offset, err := strconv.Atoi(string(m[2]))
		if err != nil {
			return offsets, fmt.Errorf("tracepoint field %s: invalid offset", name)
		}
		size, err := strconv.Atoi(string(m[3]))
		if err != nil {
			return offsets, fmt.Errorf("tracepoint field %s: invalid size", name)
		}
		found[name] = [2]int{offset, size}
	}
	for i, name := range traceFields {
		field, ok := found[name]
		if !ok {
			return offsets, fmt.Errorf("missing tracepoint field %s", name)
		}
		if field[1] != traceSizes[i] || field[0] < 8 || field[0] > 4096-field[1] {
			return offsets, fmt.Errorf("tracepoint field %s: invalid offset/size %v (expected size %d)", name, field, traceSizes[i])
		}
		for j := 0; j < i; j++ {
			start := int(offsets[j])
			if field[0] < start+traceSizes[j] && start < field[0]+field[1] {
				return offsets, fmt.Errorf("tracepoint fields %s and %s overlap", name, traceFields[j])
			}
		}
		offsets[i] = uint32(field[0])
	}
	return offsets, nil
}

func ValidateFormat(data []byte) error {
	_, err := ParseFormat(data)
	return err
}
