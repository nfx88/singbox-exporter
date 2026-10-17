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
	"errors"
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

// parseFlags reads CLI flags. args is the value of os.Args[1:], kept as a
// parameter so the function is unit-testable.
func parseFlags(args []string) (options, error) {
	var o options
	fs := flag.NewFlagSet("singbox-exporter", flag.ContinueOnError)
	fs.IntVar(&o.port, "port", 9098, "TCP port for Prometheus /metrics")
	fs.StringVar(&o.apiBase, "api", defaultAPI, "sing-box Clash API base URL")
	fs.StringVar(&o.apiToken, "token", "", "optional Bearer token for the Clash API")
	fs.DurationVar(&o.interval, "interval", 15*time.Second, "how often to poll the Clash API")
	fs.StringVar(&o.logLevel, "log-level", "info", "log verbosity: debug, info, warn or error")
	fs.StringVar(&o.logFormat, "log-format", "text", "log output format: text or json")
	fs.BoolVar(&o.showVersion, "version", false, "print version and exit")
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	o.apiBase = strings.TrimRight(o.apiBase, "/")
	if o.interval <= 0 {
		return o, fmt.Errorf("interval must be positive, got %s", o.interval)
	}
	if o.port < 1 || o.port > 65535 {
		return o, fmt.Errorf("port must be in 1..65535, got %d", o.port)
	}
	return o, nil
}

func main() {
	opts, err := parseFlags(os.Args[1:])
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		fmt.Fprintln(os.Stderr, "singbox-exporter:", err)
		os.Exit(2)
	}

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

	e := newExporter(&opts, lg)
	go e.pollLoop(ctx)

	mux := newHTTPHandler(e)

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", opts.port),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	lg.Infof("singbox-exporter %s: listening on %s, API %s, interval %s", version, srv.Addr, opts.apiBase, opts.interval)

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.ListenAndServe() }()

	select {
	case err := <-serveErr:
		fatalServer(lg, err)
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

// fatalServer logs a terminal HTTP server error and exits. It lives in its own
// function so os.Exit does not bypass the defers registered in main.
func fatalServer(lg *logger, err error) {
	lg.Error("http server stopped", "error", err)
	os.Exit(1)
}

// ---------------- HTTP handlers ----------------

// newHTTPHandler routes /metrics and /healthz, both read-only GET/HEAD only.
func newHTTPHandler(e *exporter) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", getOnly(e.metricsHandler))
	mux.HandleFunc("/healthz", getOnly(e.healthzHandler))
	return mux
}

// getOnly rejects everything but GET and HEAD with 405.
func getOnly(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h(w, r)
	}
}

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
