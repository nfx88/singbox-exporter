// Command singbox-exporter exposes sing-box traffic, connection and
// proxy-health metrics in the Prometheus text format.
//
// It is a single static binary with no external dependencies (stdlib only)
// and talks to the sing-box "Clash API" over plain HTTP, so it is intended
// to run on the same host as sing-box, for example as a container with
// network_mode: host.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// version is injected at build time via:
//
//	go build -ldflags "-X main.version=v1.2.3"
var version = "dev"

const defaultAPI = "http://127.0.0.1:9097"

type options struct {
	port        int
	apiBase     string
	apiToken    string
	interval    time.Duration
	showVersion bool
	logLevel    string
	logFormat   string
}

func parseFlags() options {
	var o options
	flag.IntVar(&o.port, "port", 9098, "TCP port for Prometheus /metrics")
	flag.StringVar(&o.apiBase, "api", defaultAPI, "sing-box Clash API base URL")
	flag.StringVar(&o.apiToken, "token", "", "optional Bearer token for the Clash API")
	flag.DurationVar(&o.interval, "interval", 15*time.Second, "how often to poll the Clash API")
	flag.StringVar(&o.logLevel, "log-level", "info", "log verbosity: debug, info, warn or error")
	flag.StringVar(&o.logFormat, "log-format", "text", "log output format: text or json")
	flag.BoolVar(&o.showVersion, "version", false, "print version and exit")
	flag.Parse()
	o.apiBase = strings.TrimRight(o.apiBase, "/")
	return o
}

func main() {
	opts := parseFlags()

	level, err := parseLogLevel(opts.logLevel)
	if err != nil {
		fmt.Fprintln(os.Stderr, "singbox-exporter:", err)
		os.Exit(2)
	}
	format, err := parseLogFormat(opts.logFormat)
	if err != nil {
		fmt.Fprintln(os.Stderr, "singbox-exporter:", err)
		os.Exit(2)
	}
	if opts.showVersion {
		fmt.Printf("singbox-exporter %s\n", version)
		return
	}

	lg := newLogger(os.Stderr, level, format)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	e := newExporter(opts, lg)
	go e.pollLoop(ctx)

	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", e.metricsHandler)
	mux.HandleFunc("/healthz", e.healthzHandler)

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", opts.port),
		Handler: mux,
	}
	lg.Infof("singbox-exporter %s: listening on %s, API %s, interval %s", version, srv.Addr, opts.apiBase, opts.interval)

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.ListenAndServe() }()

	select {
	case err := <-serveErr:
		lg.Error("http server stopped", "error", err)
		os.Exit(1)
	case <-ctx.Done():
	}

	stop()
	lg.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		lg.Error("shutdown failed", "error", err)
	}
}

// ---------------- HTTP handlers ----------------

func (e *exporter) metricsHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	e.state.writeTo(w)
}

func (e *exporter) healthzHandler(w http.ResponseWriter, _ *http.Request) {
	if e.state.isUp() {
		fmt.Fprintln(w, "ok")
		return
	}
	http.Error(w, "sing-box unreachable", http.StatusServiceUnavailable)
}
