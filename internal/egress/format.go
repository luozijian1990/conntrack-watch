package egress

import (
	"fmt"
	"regexp"
	"strconv"
)

var formatField = regexp.MustCompile(`field:[^;]*\s([A-Za-z_][A-Za-z_0-9]*)(?:\[[0-9]+\])?;\s*offset:(\d+);\s*size:(\d+);`)

// Reject unexpected tracepoint layouts rather than silently decoding wrong IPs.
func ValidateFormat(data []byte) error {
	want := map[string][2]int{"skaddr": {8, 8}, "oldstate": {16, 4}, "newstate": {20, 4}, "sport": {24, 2}, "dport": {26, 2}, "family": {28, 2}, "protocol": {30, 2}, "saddr": {32, 4}, "daddr": {36, 4}, "saddr_v6": {40, 16}, "daddr_v6": {56, 16}}
	found := map[string][2]int{}
	for _, m := range formatField.FindAllSubmatch(data, -1) {
		offset, _ := strconv.Atoi(string(m[2]))
		size, _ := strconv.Atoi(string(m[3]))
		found[string(m[1])] = [2]int{offset, size}
	}
	for name, layout := range want {
		if found[name] != layout {
			return fmt.Errorf("tracepoint field %s: got offset/size %v, expected %v", name, found[name], layout)
		}
	}
	return nil
}
