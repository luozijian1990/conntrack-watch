//go:build linux

package egress

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/cilium/ebpf"
)

func TestEmbeddedObjectTraceOffsets(t *testing.T) {
	for _, shift := range []int{0, 8} {
		spec, err := ebpf.LoadCollectionSpecFromReader(bytes.NewReader(bpfObject))
		if err != nil {
			t.Fatal(err)
		}
		layout, err := ParseFormat([]byte(traceFormat(shift)))
		if err != nil {
			t.Fatal(err)
		}
		if err := spec.RewriteConstants(map[string]interface{}{"trace_offsets": layout}); err != nil {
			t.Fatal(err)
		}
		data, ok := spec.Maps[".rodata"].Contents[0].Value.([]byte)
		if !ok || len(data) != 44 {
			t.Fatalf("unexpected trace layout ABI: %T %v", spec.Maps[".rodata"].Contents[0].Value, data)
		}
		for i, offset := range layout {
			if got := binary.LittleEndian.Uint32(data[i*4:]); got != offset {
				t.Fatalf("field %s: embedded offset %d, wanted %d", traceFields[i], got, offset)
			}
		}
		if spec.Programs["trace_state"] == nil || spec.Maps["health"].MaxEntries != uint32(len(kernelErrorReasons)) {
			t.Fatal("incomplete embedded object")
		}
	}
}
