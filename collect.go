package main

import (
	"context"
	"errors"
	"sort"
	"time"
)

// Clash API endpoints polled by the exporter. These strings double as the
// store section names and as the "endpoint" label of singbox_endpoint_up.
const (
	sectionVersion     = "version"
	sectionConnections = "connections"
	sectionProxies     = "proxies"
	endpoints          = 3
)

// isGroup reports whether an outbound is a proxy group in the Clash API.
func isGroup(p proxyObject) bool {
	switch p.Type {
	case "URLTest", "Selector", "Fallback":
		return true
	default:
		return false
	}
}

// proxyTypeSet lists outbound types that are actual VPN server proxies.
// Everything else returned by /proxies (Direct, Reject, DNS, endpoints such
// as WireGuard or Tailscale, ...) carries network latency semantics of its
// own, so it only gets singbox_proxy_info and is not flagged as dead.
var proxyTypeSet = map[string]bool{
	"Shadowsocks":  true,
	"ShadowsocksR": true,
	"VMess":        true,
	"Trojan":       true,
	"VLESS":        true,
	"Hysteria":     true,
	"Hysteria2":    true,
	"ShadowTLS":    true,
	"SSH":          true,
	"AnyTLS":       true,
	"TUIC":         true,
	"Snell":        true,
	"Tor":          true,
	"SOCKS":        true,
	"HTTP":         true,
	"Naive":        true,
}

func isProxyType(t string) bool { return proxyTypeSet[t] }

func (e *exporter) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(e.interval)
	defer ticker.Stop()
	for {
		e.collect(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

type collectResult struct {
	name     string
	samples  []sample
	duration time.Duration
	err      error
}

func (e *exporter) collect(ctx context.Context) {
	start := time.Now()
	nowSec := float64(time.Now().Unix())

	results := make(chan collectResult, endpoints)
	go e.collectVersion(ctx, results)
	go e.collectConnections(ctx, results)
	go e.collectProxies(ctx, results, nowSec)

	apiOK := false
	failed := 0
	endpointUp := map[string]bool{
		sectionVersion:     false,
		sectionConnections: false,
		sectionProxies:     false,
	}
	for range endpoints {
		r := <-results
		if r.err != nil {
			failed++
			if e.errLog.next(r.name, time.Now()) {
				e.logger.Error("collect failed", "endpoint", r.name, "error", r.err)
			}
			e.logger.Debug("collect endpoint", "endpoint", r.name, "error", r.err, "duration", r.duration)
			e.scrapeErrors.Add(1)
			continue
		}
		if e.errLog.isFailing(r.name) {
			e.logger.Info("endpoint recovered", "endpoint", r.name)
		}
		e.errLog.reset(r.name)
		apiOK = true
		endpointUp[r.name] = true
		e.state.setSection(r.name, r.samples)
		e.logger.Debug("collect endpoint", "endpoint", r.name, "samples", len(r.samples), "duration", r.duration)
	}

	if apiOK {
		e.lastSuccessSec = nowSec
	}
	e.logger.Debug("poll complete", "up", apiOK, "failed", failed, "duration", time.Since(start))

	var status []sample
	if apiOK {
		addSample(&status, "singbox_up", "Whether the sing-box Clash API was reachable at the last poll.", "gauge", nil, 1)
	} else {
		addSample(&status, "singbox_up", "Whether the sing-box Clash API was reachable at the last poll.", "gauge", nil, 0)
	}
	for _, ep := range []string{sectionVersion, sectionConnections, sectionProxies} {
		v := 0.0
		if endpointUp[ep] {
			v = 1
		}
		addSample(&status, "singbox_endpoint_up", "Whether the Clash API endpoint was reachable at the last poll.", "gauge", map[string]string{"endpoint": ep}, v)
	}
	addSample(&status, "singbox_scrape_errors_total", "Total number of failed Clash API requests since start.", "counter", nil, float64(e.scrapeErrors.Load()))
	addSample(&status, "singbox_scrape_duration_seconds", "Duration of the last Clash API poll.", "gauge", nil, time.Since(start).Seconds())
	addSample(&status, "singbox_scrape_timestamp_seconds", "Unix time of the last Clash API poll.", "gauge", nil, nowSec)
	addSample(&status, "singbox_last_success_timestamp_seconds", "Unix time of the last poll in which the Clash API answered.", "gauge", nil, e.lastSuccessSec)

	e.state.setUp(apiOK)
	e.state.setSection("status", status)
}

func (e *exporter) collectVersion(ctx context.Context, results chan<- collectResult) {
	start := time.Now()
	var ver apiVersion
	if err := e.getJSON(ctx, "/version", &ver); err != nil {
		results <- collectResult{name: sectionVersion, duration: time.Since(start), err: err}
		return
	}
	if ver.Version == "" {
		results <- collectResult{name: sectionVersion, duration: time.Since(start), err: errors.New("empty version in /version response")}
		return
	}
	if e.lastVersion == "" {
		e.logger.Info("sing-box connected", "version", ver.Version)
	} else if e.lastVersion != ver.Version {
		e.logger.Warn("sing-box version changed", "old", e.lastVersion, "new", ver.Version)
	}
	e.lastVersion = ver.Version
	var out []sample
	addSample(&out, "singbox_version_info", "sing-box version information.", "gauge", map[string]string{"version": ver.Version}, 1)
	results <- collectResult{name: sectionVersion, samples: out, duration: time.Since(start)}
}

func (e *exporter) collectConnections(ctx context.Context, results chan<- collectResult) {
	start := time.Now()
	var conn apiConnections
	if err := e.getJSON(ctx, "/connections", &conn); err != nil {
		results <- collectResult{name: sectionConnections, duration: time.Since(start), err: err}
		return
	}

	var out []sample
	addSample(&out, "singbox_traffic_upload_bytes_total", "Cumulative bytes uploaded through sing-box since the process started.", "counter", nil, float64(conn.UploadTotal))
	addSample(&out, "singbox_traffic_download_bytes_total", "Cumulative bytes downloaded through sing-box since the process started.", "counter", nil, float64(conn.DownloadTotal))
	addSample(&out, "singbox_active_connections", "Currently active connections tracked by sing-box.", "gauge", nil, float64(len(conn.Connections)))
	addSample(&out, "singbox_memory_bytes", "sing-box process inuse memory.", "gauge", nil, float64(conn.Memory))

	byOutbound := map[string]int{}
	byNetwork := map[string]int{}
	byInbound := map[string]int{}
	byRule := map[string]int{}
	trafficUp := map[string]int64{}
	trafficDown := map[string]int64{}

	for _, c := range conn.Connections {
		ob := firstOf(c.Chains)
		if ob != "" {
			byOutbound[ob]++
			trafficUp[ob] += c.Upload
			trafficDown[ob] += c.Download
		}
		network := c.Metadata.Network
		if network == "" {
			network = "unknown"
		}
		byNetwork[network]++
		inbound := c.Metadata.Type
		if inbound == "" {
			inbound = "unknown"
		}
		byInbound[inbound]++
		rule := c.Rule
		if rule == "" {
			rule = "final"
		}
		byRule[rule]++
	}

	for _, ob := range sortedKeys(byOutbound) {
		addSample(&out, "singbox_connections_by_outbound", "Active connections grouped by the outbound chain.", "gauge", map[string]string{"outbound": ob}, float64(byOutbound[ob]))
		addSample(&out, "singbox_connections_traffic_upload_bytes", "Bytes uploaded by currently active connections, grouped by outbound.", "gauge", map[string]string{"outbound": ob}, float64(trafficUp[ob]))
		addSample(&out, "singbox_connections_traffic_download_bytes", "Bytes downloaded by currently active connections, grouped by outbound.", "gauge", map[string]string{"outbound": ob}, float64(trafficDown[ob]))
	}
	for _, n := range sortedKeys(byNetwork) {
		addSample(&out, "singbox_connections_by_network", "Active connections grouped by network protocol.", "gauge", map[string]string{"network": n}, float64(byNetwork[n]))
	}
	for _, in := range sortedKeys(byInbound) {
		addSample(&out, "singbox_connections_by_inbound", "Active connections grouped by inbound.", "gauge", map[string]string{"inbound": in}, float64(byInbound[in]))
	}
	for _, r := range sortedKeys(byRule) {
		addSample(&out, "singbox_connections_by_rule", "Active connections grouped by the matched routing rule.", "gauge", map[string]string{"rule": r}, float64(byRule[r]))
	}

	results <- collectResult{name: sectionConnections, samples: out, duration: time.Since(start)}
}

func (e *exporter) collectProxies(ctx context.Context, results chan<- collectResult, nowSec float64) {
	start := time.Now()
	var proxies apiProxies
	if err := e.getJSON(ctx, "/proxies", &proxies); err != nil {
		results <- collectResult{name: sectionProxies, duration: time.Since(start), err: err}
		return
	}

	var out []sample
	names := make([]string, 0, len(proxies.Proxies))
	for n := range proxies.Proxies {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		p := proxies.Proxies[name]
		proxyLabels := map[string]string{"proxy": p.Name}
		addSample(&out, "singbox_proxy_info", "sing-box outbound metadata (type).", "gauge", map[string]string{"proxy": p.Name, "type": p.Type}, 1)

		var lastDelay int64
		var lastTime string
		if len(p.History) > 0 {
			last := p.History[len(p.History)-1]
			lastDelay = last.Delay
			lastTime = last.Time
		}

		switch {
		case isGroup(p):
			addSample(&out, "singbox_proxy_delay_ms", "Last latency measured by the URL test in milliseconds (0 when unknown or dead).", "gauge", proxyLabels, float64(lastDelay))
			addDelayTimestamp(&out, lastTime, proxyLabels, nowSec)
			if p.Now != "" {
				addSample(&out, "singbox_proxy_selected", "Currently selected proxy inside the group.", "gauge", map[string]string{"group": p.Name, "proxy": p.Now}, 1)
			}
		case isProxyType(p.Type):
			addSample(&out, "singbox_proxy_delay_ms", "Last latency measured by the URL test in milliseconds (0 when unknown or dead).", "gauge", proxyLabels, float64(lastDelay))
			addDelayTimestamp(&out, lastTime, proxyLabels, nowSec)
			down := 0.0
			if len(p.History) == 0 || lastDelay <= 0 {
				down = 1
			}
			addSample(&out, "singbox_proxy_down", "Proxy is considered dead when it has no valid latency.", "gauge", proxyLabels, down)
		}
	}

	results <- collectResult{name: sectionProxies, samples: out, duration: time.Since(start)}
}

func addDelayTimestamp(out *[]sample, value string, labels map[string]string, nowSec float64) {
	if value == "" {
		return
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return
	}
	ts := float64(parsed.Unix())
	addSample(out, "singbox_proxy_delay_timestamp_seconds", "Unix time of the last URL test that produced the reported delay.", "gauge", labels, ts)
	age := nowSec - ts
	if age < 0 {
		age = 0
	}
	addSample(out, "singbox_proxy_delay_age_seconds", "Age in seconds of the last URL-test delay.", "gauge", labels, age)
}

func firstOf(s []string) string {
	if len(s) > 0 {
		return s[0]
	}
	return ""
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
