package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeAPI emulates the sing-box Clash API with realistic payloads and
// per-endpoint failure switches.
type fakeAPI struct {
	versionFail     atomic.Bool
	connectionsFail atomic.Bool
	proxiesFail     atomic.Bool
}

func newFakeAPI() *fakeAPI { return &fakeAPI{} }

func (f *fakeAPI) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/version", func(w http.ResponseWriter, _ *http.Request) {
		if f.versionFail.Load() {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(apiVersion{Version: "sing-box 1.14.3", Premium: true, Meta: true})
	})
	mux.HandleFunc("/connections", func(w http.ResponseWriter, _ *http.Request) {
		if f.connectionsFail.Load() {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(apiConnections{
			UploadTotal:   500,
			DownloadTotal: 1000,
			Memory:        45600000,
			Connections: []connObject{
				{
					Chains:   []string{"node1", "Auto"},
					Rule:     "geosite:netflix => route",
					Upload:   100,
					Download: 300,
					Metadata: connMetadata{Network: "tcp", Type: "mixed/mixed-in"},
				},
				{
					Chains:   []string{"direct", "Auto"},
					Rule:     "final",
					Upload:   50,
					Download: 60,
					Metadata: connMetadata{Network: "udp", Type: "tun"},
				},
			},
		})
	})
	mux.HandleFunc("/proxies", func(w http.ResponseWriter, _ *http.Request) {
		if f.proxiesFail.Load() {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(apiProxies{
			Proxies: map[string]proxyObject{
				"GLOBAL": {Type: "Fallback", Name: "GLOBAL", Now: "node1"},
				"Auto":   {Type: "URLTest", Name: "Auto", Now: "node1", History: []historyItem{{Time: "2026-10-15T09:59:00Z", Delay: 123}}},
				"Manual": {Type: "Selector", Name: "Manual", Now: "node2"},
				"node1":  {Type: "VMess", Name: "node1", History: []historyItem{{Time: "2026-10-15T10:00:00Z", Delay: 80}}},
				"node2":  {Type: "Trojan", Name: "node2"},
				"direct": {Type: "Direct", Name: "direct"},
				"wg":     {Type: "WireGuard", Name: "wg"},
			},
		})
	})
	return mux
}

func newExporterWithFake(t *testing.T, f *fakeAPI) (*exporter, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)
	lg := newLogger(io.Discard, levelError, formatText)
	e := newExporter(&options{apiBase: srv.URL, interval: time.Minute}, lg)
	return e, srv
}

func render(e *exporter) string {
	var buf bytes.Buffer
	e.state.writeTo(&buf)
	return buf.String()
}

func TestCollectFull(t *testing.T) {
	e, _ := newExporterWithFake(t, newFakeAPI())
	e.collect(context.Background())
	text := render(e)

	for _, want := range []string{
		// availability
		"singbox_up 1",
		`singbox_endpoint_up{endpoint="version"} 1`,
		`singbox_endpoint_up{endpoint="connections"} 1`,
		`singbox_endpoint_up{endpoint="proxies"} 1`,
		"# HELP singbox_scrape_duration_seconds",
		"# HELP singbox_scrape_timestamp_seconds",
		"# HELP singbox_last_success_timestamp_seconds",
		// version
		`singbox_version_info{version="sing-box 1.14.3"} 1`,
		// connections
		"singbox_traffic_upload_bytes_total 500",
		"singbox_traffic_download_bytes_total 1000",
		"singbox_memory_bytes 45600000",
		"singbox_active_connections 2",
		`singbox_connections_by_outbound{outbound="node1"} 1`,
		`singbox_connections_by_outbound{outbound="direct"} 1`,
		`singbox_connections_traffic_upload_bytes{outbound="node1"} 100`,
		`singbox_connections_traffic_download_bytes{outbound="node1"} 300`,
		`singbox_connections_traffic_upload_bytes{outbound="direct"} 50`,
		`singbox_connections_traffic_download_bytes{outbound="direct"} 60`,
		`singbox_connections_by_network{network="tcp"} 1`,
		`singbox_connections_by_network{network="udp"} 1`,
		`singbox_connections_by_inbound{inbound="tun"} 1`,
		`singbox_connections_by_inbound{inbound="mixed/mixed-in"} 1`,
		`singbox_connections_by_rule{rule="final"} 1`,
		`singbox_connections_by_rule{rule="geosite:netflix => route"} 1`,
		// proxies
		`singbox_proxy_info{proxy="direct",type="Direct"} 1`,
		`singbox_proxy_delay_ms{proxy="node1"} 80`,
		`singbox_proxy_delay_ms{proxy="Auto"} 123`,
		`singbox_proxy_down{proxy="node1"} 0`,
		`singbox_proxy_down{proxy="node2"} 1`,
		`singbox_proxy_selected{group="Auto",proxy="node1"} 1`,
		`singbox_proxy_selected{group="Manual",proxy="node2"} 1`,
		`singbox_proxy_delay_timestamp_seconds{proxy="node1"} `,
		`singbox_proxy_delay_timestamp_seconds{proxy="Auto"} `,
		"singbox_proxy_delay_age_seconds{proxy=\"node1\"} ",
		"singbox_proxy_delay_age_seconds{proxy=\"Auto\"} ",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q\n---\n%s", want, text)
		}
	}

	// non-proxy outbounds must not be marked dead.
	for _, bad := range []string{
		`singbox_proxy_down{proxy="direct"}`,
		`singbox_proxy_down{proxy="wg"}`,
		`singbox_proxy_down{proxy="Auto"}`,
	} {
		if strings.Contains(text, bad) {
			t.Errorf("unexpected series %q in output\n---\n%s", bad, text)
		}
	}
}

func TestCollectKeepsSectionOnFailure(t *testing.T) {
	f := newFakeAPI()
	e, _ := newExporterWithFake(t, f)
	e.collect(context.Background())
	f.proxiesFail.Store(true)
	e.collect(context.Background())
	text := render(e)

	for _, want := range []string{
		// proxy section kept its last successful values
		`singbox_proxy_delay_ms{proxy="node1"} 80`,
		`singbox_proxy_info{proxy="wg",type="WireGuard"} 1`,
		// failure is visible in the endpoint flag and error counter
		`singbox_endpoint_up{endpoint="proxies"} 0`,
		`singbox_endpoint_up{endpoint="version"} 1`,
		"singbox_up 1",
		"singbox_scrape_errors_total 1",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q\n---\n%s", want, text)
		}
	}
}

func TestCollectAllEndpointsDown(t *testing.T) {
	f := newFakeAPI()
	f.versionFail.Store(true)
	f.connectionsFail.Store(true)
	f.proxiesFail.Store(true)
	e, _ := newExporterWithFake(t, f)
	e.collect(context.Background())
	text := render(e)

	for _, want := range []string{
		"singbox_up 0",
		`singbox_endpoint_up{endpoint="version"} 0`,
		`singbox_endpoint_up{endpoint="connections"} 0`,
		`singbox_endpoint_up{endpoint="proxies"} 0`,
		"singbox_scrape_errors_total 3",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q\n---\n%s", want, text)
		}
	}
	if strings.Contains(text, "singbox_version_info") {
		t.Error("version section must be empty when /version failed")
	}
}

func TestHealthz(t *testing.T) {
	f := newFakeAPI()
	e, _ := newExporterWithFake(t, f)

	rec := httptest.NewRecorder()
	e.healthzHandler(rec, nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("before first poll healthz code = %d, want 503", rec.Code)
	}

	e.collect(context.Background())
	rec = httptest.NewRecorder()
	e.healthzHandler(rec, nil)
	if rec.Code != http.StatusOK || rec.Body.String() != "ok\n" {
		t.Errorf("healthz after successful poll = %d %q, want 200 ok", rec.Code, rec.Body.String())
	}

	f.versionFail.Store(true)
	f.connectionsFail.Store(true)
	f.proxiesFail.Store(true)
	e.collect(context.Background())
	rec = httptest.NewRecorder()
	e.healthzHandler(rec, nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("healthz when API down = %d, want 503", rec.Code)
	}
}

func TestCollectMemoryZeroStillEmitted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(apiConnections{
			UploadTotal:   1,
			DownloadTotal: 2,
			Memory:        0,
			Connections:   nil,
		})
	}))
	t.Cleanup(srv.Close)
	e := newExporter(&options{apiBase: srv.URL, interval: time.Minute},
		newLogger(io.Discard, levelError, formatText))
	e.collect(context.Background())
	if text := render(e); !strings.Contains(text, "singbox_memory_bytes 0") {
		t.Errorf("Memory==0 must emit singbox_memory_bytes 0\n---\n%s", text)
	}
}

func TestHTTPMethods(t *testing.T) {
	e, _ := newExporterWithFake(t, newFakeAPI())
	e.collect(context.Background())
	h := newHTTPHandler(e)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/metrics", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /metrics = %d, want 405", rec.Code)
	}

	for _, path := range []string{"/metrics", "/healthz"} {
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, rec.Code)
		}
	}
}
