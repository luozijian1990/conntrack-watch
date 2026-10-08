package egress

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func TestMetricsSemanticsAndBoundedLabels(t *testing.T) {
	m := NewMetrics([]uint16{443})
	ms := 12.0
	for _, event := range []string{"attempt", "established", "closed_before_established"} {
		m.Record(Event{DestinationPort: 443, Event: event, ConnectDurationMS: &ms})
	}
	recorder := httptest.NewRecorder()
	promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{}).ServeHTTP(recorder, httptest.NewRequest("GET", "/metrics", nil))
	body := recorder.Body.String()
	for _, want := range []string{
		`egress_connections_total{event="attempt",port="443"} 1`,
		`egress_connections_total{event="established",port="443"} 1`,
		`egress_connections_total{event="closed_before_established",port="443"} 1`,
		`egress_connect_duration_seconds_count{port="443"} 1`,
		`egress_kernel_errors_total{reason="ringbuf_full"} 0`,
		`egress_collector_ready 0`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s", want)
		}
	}
	for _, label := range []string{"src_ip=", "dst_ip=", "pid=", "cgroup_id="} {
		if strings.Contains(body, label) {
			t.Fatal("high cardinality label", label)
		}
	}
}
