package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"
)

// exporter polls the sing-box Clash API and keeps the latest snapshot.
type exporter struct {
	apiBase  string
	apiToken string
	client   *http.Client
	interval time.Duration

	logger *logger
	errLog *errorBackoff

	state          *store
	scrapeErrors   atomic.Int64
	lastSuccessSec float64
	lastVersion    string
}

// errorBackoffDefaults rate-limit repeated failure logs: first failure is
// logged immediately, then 1m/2m/4m/... capped at 30m.
const (
	errorBackoffBase = time.Minute
	errorBackoffCap  = 30 * time.Minute
)

func newExporter(o *options, lg *logger) *exporter {
	return &exporter{
		apiBase:  o.apiBase,
		apiToken: o.apiToken,
		interval: o.interval,
		client:   &http.Client{Timeout: 5 * time.Second},
		logger:   lg,
		errLog:   newErrorBackoff(errorBackoffBase, errorBackoffCap),
		state:    newStore(),
	}
}

func (e *exporter) getJSON(ctx context.Context, path string, output any) error {
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.apiBase+path, nil)
	if err != nil {
		e.logger.Debug("api request", "endpoint", path, "error", err, "duration", time.Since(start))
		return err
	}
	if e.apiToken != "" {
		req.Header.Set("Authorization", "Bearer "+e.apiToken)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		e.logger.Debug("api request", "endpoint", path, "error", err, "duration", time.Since(start))
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	duration := time.Since(start)
	if err != nil {
		e.logger.Debug("api request", "endpoint", path, "error", err, "duration", duration)
		return err
	}
	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("unexpected status %d for %s", resp.StatusCode, path)
		e.logger.Debug("api request", "endpoint", path, "status", resp.StatusCode, "error", err, "duration", duration)
		return err
	}
	if err := json.Unmarshal(body, output); err != nil {
		e.logger.Debug("api request", "endpoint", path, "error", err, "duration", duration)
		return err
	}
	e.logger.Debug("api request", "endpoint", path, "status", resp.StatusCode, "bytes", len(body), "duration", duration)
	return nil
}
