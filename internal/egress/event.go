package egress

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net/netip"
	"time"
)

const WireSize = 96

type WireEvent struct {
	StartedNS, ObservedNS, Cgroup             uint64
	PID, NetNS                                uint32
	SourcePort, DestinationPort, Family, Kind uint16
	Source, Destination                       [16]byte
	Comm                                      [16]byte
	Version, Padding                          uint32
}

type Event struct {
	Timestamp          time.Time  `json:"ts"`
	Type               string     `json:"type"`
	Source             string     `json:"source"`
	Event              string     `json:"event"`
	Node               string     `json:"node"`
	Protocol           string     `json:"protocol"`
	Family             string     `json:"family"`
	SourceIP           netip.Addr `json:"src_ip"`
	SourcePort         uint16     `json:"src_port"`
	SourcePortKnown    bool       `json:"src_port_known"`
	DestinationIP      netip.Addr `json:"dst_ip"`
	DestinationPort    uint16     `json:"dst_port"`
	NetNS              uint32     `json:"netns"`
	PID                uint32     `json:"pid"`
	CgroupID           string     `json:"cgroup_id"`
	Comm               string     `json:"comm"`
	StartedMonotonicNS uint64     `json:"started_monotonic_ns"`
	ConnectDurationMS  *float64   `json:"connect_duration_ms,omitempty"`
}

// now/monotonicNS are sampled together at decode time. Event time is reconstructed
// from monotonic age, so userspace queue delay is not reported as connection latency.
func Decode(data []byte, node string, now time.Time, monotonicNS uint64) (Event, error) {
	var w WireEvent
	if len(data) != WireSize {
		return Event{}, fmt.Errorf("event size %d, expected %d", len(data), WireSize)
	}
	if err := binary.Read(bytes.NewReader(data), binary.LittleEndian, &w); err != nil {
		return Event{}, err
	}
	if w.Version != 1 || w.Kind < 1 || w.Kind > 3 || w.ObservedNS < w.StartedNS || monotonicNS < w.ObservedNS || w.NetNS == 0 {
		return Event{}, fmt.Errorf("invalid event ABI, kind, timestamps or network namespace")
	}
	var src, dst netip.Addr
	family := "ipv4"
	switch w.Family {
	case 2:
		src = netip.AddrFrom4([4]byte(w.Source[:4]))
		dst = netip.AddrFrom4([4]byte(w.Destination[:4]))
	case 10:
		family = "ipv6"
		src = netip.AddrFrom16(w.Source).Unmap()
		dst = netip.AddrFrom16(w.Destination).Unmap()
	default:
		return Event{}, fmt.Errorf("unsupported address family %d", w.Family)
	}
	e := Event{Timestamp: now.Add(-time.Duration(monotonicNS - w.ObservedNS)).UTC(), Type: "egress_connection", Source: "ebpf_tracepoint",
		Event: map[uint16]string{1: "attempt", 2: "established", 3: "closed_before_established"}[w.Kind], Node: node, Protocol: "tcp", Family: family,
		SourceIP: src, SourcePort: w.SourcePort, SourcePortKnown: w.SourcePort != 0, DestinationIP: dst, DestinationPort: w.DestinationPort,
		NetNS: w.NetNS, PID: w.PID, CgroupID: fmt.Sprint(w.Cgroup), Comm: string(bytes.TrimRight(w.Comm[:], "\x00")), StartedMonotonicNS: w.StartedNS}
	if w.Kind != 1 {
		ms := float64(w.ObservedNS-w.StartedNS) / 1e6
		e.ConnectDurationMS = &ms
	}
	return e, nil
}
