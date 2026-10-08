//go:build linux

package egress

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"syscall"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"golang.org/x/sys/unix"
)

func Run(ctx context.Context, c Config, m *Metrics, out *Output, log *slog.Logger) error {
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		return fmt.Errorf("supported architectures: linux/amd64 and linux/arm64")
	}
	filter, err := NewFilter(c)
	if err != nil {
		return err
	}
	var format []byte
	for _, root := range []string{"/sys/kernel/tracing", "/sys/kernel/debug/tracing"} {
		format, err = os.ReadFile(root + "/events/sock/inet_sock_set_state/format")
		if err == nil {
			break
		}
	}
	if err != nil {
		return fmt.Errorf("read tracepoint format (mount tracefs): %w", err)
	}
	if err = ValidateFormat(format); err != nil {
		return err
	}
	info, err := os.Stat(c.HostNetNSPath)
	if err != nil {
		return fmt.Errorf("host network namespace: %w", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Ino == 0 || stat.Ino > 1<<32-1 {
		return fmt.Errorf("invalid host netns inode")
	}
	hostNS := uint32(stat.Ino)
	// Best effort on kernels >=5.11, which normally account BPF memory via memcg.
	if err = rlimit.RemoveMemlock(); err != nil {
		log.Warn("could not lift memlock; trying kernel load", "error", err)
	}
	spec, err := ebpf.LoadCollectionSpec(c.ObjectPath)
	if err != nil {
		return fmt.Errorf("load BPF object: %w", err)
	}
	collection, err := ebpf.NewCollection(spec)
	if err != nil {
		return fmt.Errorf("load BPF programs/maps (requires kernel BTF and BPF privileges): %w", err)
	}
	defer collection.Close()
	for _, name := range []string{"ports", "host_netns", "events", "health", "pending"} {
		if collection.Maps[name] == nil {
			return fmt.Errorf("missing BPF map %s", name)
		}
	}
	if collection.Programs["trace_state"] == nil {
		return fmt.Errorf("missing trace_state program")
	}
	for _, port := range c.Ports {
		if err = collection.Maps["ports"].Put(port, uint8(1)); err != nil {
			return err
		}
	}
	if err = collection.Maps["host_netns"].Put(uint32(0), hostNS); err != nil {
		return err
	}
	reader, err := ringbuf.NewReader(collection.Maps["events"])
	if err != nil {
		return err
	}
	defer reader.Close()
	attached, err := link.Tracepoint("sock", "inet_sock_set_state", collection.Programs["trace_state"], nil)
	if err != nil {
		return fmt.Errorf("attach tracepoint: %w", err)
	}
	defer attached.Close()
	m.Ready.Set(1)
	defer m.Ready.Set(0)
	log.Info("collector attached", "tracepoint", "sock:inet_sock_set_state", "host_netns", hostNS, "ports", c.Ports, "node", c.Node)
	var previous [3]uint64
	nextHealth := time.Now()
	for ctx.Err() == nil {
		if !time.Now().Before(nextHealth) {
			for key, reason := range []string{"ringbuf_full", "pending_map_full", "namespace_read"} {
				var perCPU []uint64
				if err := collection.Maps["health"].Lookup(uint32(key), &perCPU); err != nil {
					return fmt.Errorf("read kernel health: %w", err)
				}
				var total uint64
				for _, v := range perCPU {
					total += v
				}
				if total >= previous[key] {
					m.KernelErrors.WithLabelValues(reason).Add(float64(total - previous[key]))
				}
				previous[key] = total
			}
			nextHealth = time.Now().Add(time.Second)
		}
		reader.SetDeadline(nextHealth)
		record, err := reader.Read()
		if errors.Is(err, os.ErrDeadlineExceeded) {
			continue
		}
		if err != nil {
			return fmt.Errorf("read ring buffer: %w", err)
		}
		var mono unix.Timespec
		if err = unix.ClockGettime(unix.CLOCK_MONOTONIC, &mono); err != nil {
			return err
		}
		event, err := Decode(record.RawSample, c.Node, time.Now(), uint64(mono.Nano()))
		if err != nil {
			m.DecodeErrors.Inc()
			continue
		}
		if !filter.Match(event) {
			continue
		}
		m.Record(event)
		if event.Event == "attempt" && !c.LogAttempts {
			continue
		}
		if err = out.Write(event); err != nil {
			m.LogErrors.Inc()
			return fmt.Errorf("write connection JSON: %w", err)
		}
	}
	return nil
}
