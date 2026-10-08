package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"conntrack-watch/egress/internal/egress"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := run(log); err != nil {
		log.Error("egress-watch stopped", "error", err)
		os.Exit(1)
	}
}
func run(log *slog.Logger) error {
	path := flag.String("config", "config.yaml", "YAML configuration")
	check := flag.Bool("check-config", false, "validate configuration and exit without attaching eBPF")
	flag.Parse()
	c, err := egress.LoadConfig(*path)
	if err != nil {
		return err
	}
	if *check {
		log.Info("configuration valid")
		return nil
	}
	out, err := egress.NewOutput(c)
	if err != nil {
		return err
	}
	defer out.Close()
	m := egress.NewMetrics(c.Ports)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{}))
	// Readiness uses the same metric as the dashboard, without another state source.
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { fmt.Fprintln(w, "ok") })
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		families, err := m.Registry.Gather()
		if err == nil {
			for _, f := range families {
				if f.GetName() == "egress_collector_ready" && len(f.Metric) > 0 && f.Metric[0].GetGauge().GetValue() == 1 {
					fmt.Fprintln(w, "ready")
					return
				}
			}
		}
		http.Error(w, "collector not attached", http.StatusServiceUnavailable)
	})
	listener, err := net.Listen("tcp", c.ListenAddr)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 30 * time.Second}
	serverErrors := make(chan error, 1)
	go func() {
		err := server.Serve(listener)
		if err != nil && err != http.ErrServerClosed {
			serverErrors <- err
			stop()
		}
	}()
	log.Info("HTTP server listening", "address", listener.Addr().String())
	err = egress.Run(ctx, c, m, out, log)
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	shutdownErr := server.Shutdown(shutdown)
	if err != nil {
		return err
	}
	select {
	case serveErr := <-serverErrors:
		return serveErr
	default:
	}
	return shutdownErr
}
