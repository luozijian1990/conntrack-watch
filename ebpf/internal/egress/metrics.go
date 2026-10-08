package egress

import (
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

type Metrics struct {
	Registry     *prometheus.Registry
	Connections  *prometheus.CounterVec
	Duration     *prometheus.HistogramVec
	KernelErrors *prometheus.CounterVec
	DecodeErrors prometheus.Counter
	LogErrors    prometheus.Counter
	Ready        prometheus.Gauge
}

func NewMetrics(ports []uint16) *Metrics {
	m := &Metrics{Registry: prometheus.NewRegistry(),
		Connections:  prometheus.NewCounterVec(prometheus.CounterOpts{Name: "egress_connections_total", Help: "Observed active TCP opens and outcomes after configured IP/port filters; not HTTP requests. Outcomes may cross scrape windows."}, []string{"port", "event"}),
		Duration:     prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "egress_connect_duration_seconds", Help: "Time from observed SYN_SENT to successful establishment.", Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30}}, []string{"port"}),
		KernelErrors: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "egress_kernel_errors_total", Help: "Kernel collector errors before userspace CIDR filtering: ringbuf_full, pending_map_full, namespace_read."}, []string{"reason"}),
		DecodeErrors: prometheus.NewCounter(prometheus.CounterOpts{Name: "egress_decode_errors_total", Help: "Rejected malformed or incompatible events."}),
		LogErrors:    prometheus.NewCounter(prometheus.CounterOpts{Name: "egress_log_errors_total", Help: "Connection JSON write failures."}),
		Ready:        prometheus.NewGauge(prometheus.GaugeOpts{Name: "egress_collector_ready", Help: "One while tracepoint is attached and the collector is running."}),
	}
	m.Registry.MustRegister(m.Connections, m.Duration, m.KernelErrors, m.DecodeErrors, m.LogErrors, m.Ready, collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	for _, p := range ports {
		label := strconv.Itoa(int(p))
		for _, event := range []string{"attempt", "established", "closed_before_established"} {
			m.Connections.WithLabelValues(label, event)
		}
		m.Duration.WithLabelValues(label)
	}
	for _, r := range []string{"ringbuf_full", "pending_map_full", "namespace_read"} {
		m.KernelErrors.WithLabelValues(r)
	}
	return m
}
func (m *Metrics) Record(e Event) {
	port := strconv.Itoa(int(e.DestinationPort))
	m.Connections.WithLabelValues(port, e.Event).Inc()
	if e.Event == "established" && e.ConnectDurationMS != nil {
		m.Duration.WithLabelValues(port).Observe(*e.ConnectDurationMS / 1000)
	}
}
