package metrics

import (
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	// ConntrackNewConnectionsTotal 新连接计数器，按端口区分
	ConntrackNewConnectionsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "conntrack_new_connections_total",
			Help: "Total number of new conntrack connections by destination port",
		},
		[]string{"port"},
	)
)

func init() {
	prometheus.MustRegister(ConntrackNewConnectionsTotal)
}

// RecordNewConnection 记录新连接指标
func RecordNewConnection(port uint16) {
	ConntrackNewConnectionsTotal.WithLabelValues(strconv.Itoa(int(port))).Inc()
}
