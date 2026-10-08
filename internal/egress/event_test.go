package egress

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func sample() WireEvent {
	w := WireEvent{StartedNS: 1_000_000, ObservedNS: 4_000_000, Cgroup: 123, PID: 42, NetNS: 77, SourcePort: 45678, DestinationPort: 443, Family: 2, Kind: 2, Version: 1}
	copy(w.Source[:], []byte{10, 244, 1, 23})
	copy(w.Destination[:], []byte{172, 16, 10, 50})
	copy(w.Comm[:], "curl")
	return w
}
func encode(t *testing.T, w WireEvent) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := binary.Write(&b, binary.LittleEndian, w); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func TestDecodeAndJSON(t *testing.T) {
	w := sample()
	now := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	e, err := Decode(encode(t, w), "worker-1", now, 10_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if e.SourceIP.String() != "10.244.1.23" || e.DestinationIP.String() != "172.16.10.50" || *e.ConnectDurationMS != 3 || e.Timestamp != now.Add(-6*time.Millisecond) {
		t.Fatalf("bad event: %+v", e)
	}
	var out bytes.Buffer
	o := Output{encoder: json.NewEncoder(&out)}
	if err = o.Write(e); err != nil {
		t.Fatal(err)
	}
	var record map[string]interface{}
	if err = json.Unmarshal(out.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record["src_ip"] != "10.244.1.23" || record["cgroup_id"] != "123" || record["event"] != "established" || record["src_port_known"] != true {
		t.Fatal(record)
	}
	if strings.Contains(out.String(), "skaddr") || strings.Contains(out.String(), "snat") {
		t.Fatal("misleading NAT data or pointer leaked")
	}
}
func TestAttemptsDoNotInventPortOrLatency(t *testing.T) {
	w := sample()
	w.Kind = 1
	w.SourcePort = 0
	w.ObservedNS = w.StartedNS
	e, err := Decode(encode(t, w), "node", time.Now(), 10_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if e.SourcePortKnown || e.ConnectDurationMS != nil || e.Event != "attempt" {
		t.Fatal(e)
	}
}
func TestIPv6AndMappedIPv4(t *testing.T) {
	for _, ip := range []string{"fd00::42", "::ffff:10.244.1.23"} {
		w := sample()
		w.Family = 10
		w.Source = netip.MustParseAddr(ip).As16()
		w.Destination = netip.MustParseAddr("2001:db8::80").As16()
		e, err := Decode(encode(t, w), "node", time.Now(), 10_000_000)
		if err != nil {
			t.Fatal(err)
		}
		if e.SourceIP != netip.MustParseAddr(ip).Unmap() || e.Family != "ipv6" {
			t.Fatal(e)
		}
	}
}
func TestMalformedEvents(t *testing.T) {
	for _, mutate := range []func(*WireEvent){func(w *WireEvent) { w.Version = 2 }, func(w *WireEvent) { w.Kind = 0 }, func(w *WireEvent) { w.Family = 1 }, func(w *WireEvent) { w.ObservedNS = 0 }, func(w *WireEvent) { w.NetNS = 0 }} {
		w := sample()
		mutate(&w)
		if _, err := Decode(encode(t, w), "n", time.Now(), 10_000_000); err == nil {
			t.Fatal("accepted malformed event")
		}
	}
	if _, err := Decode([]byte{1}, "n", time.Now(), 10_000_000); err == nil {
		t.Fatal("accepted truncated event")
	}
}
