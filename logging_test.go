package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseLogLevel(t *testing.T) {
	for _, tc := range []struct {
		in      string
		want    logLevel
		invalid bool
	}{
		{"debug", levelDebug, false},
		{"INFO", levelInfo, false},
		{"warn", levelWarn, false},
		{"warning", levelWarn, false},
		{"error", levelError, false},
		{"", 0, true},
		{"trace", 0, true},
	} {
		got, err := parseLogLevel(tc.in)
		if tc.invalid {
			if err == nil {
				t.Errorf("parseLogLevel(%q): want error, got %v", tc.in, got)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("parseLogLevel(%q) = %v, %v; want %v", tc.in, got, err, tc.want)
		}
	}
}

func TestParseLogFormat(t *testing.T) {
	for _, tc := range []struct {
		in      string
		want    logFormat
		invalid bool
	}{
		{"text", formatText, false},
		{"JSON", formatJSON, false},
		{"", 0, true},
		{"xml", 0, true},
	} {
		got, err := parseLogFormat(tc.in)
		if tc.invalid {
			if err == nil {
				t.Errorf("parseLogFormat(%q): want error, got %v", tc.in, got)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("parseLogFormat(%q) = %v, %v; want %v", tc.in, got, err, tc.want)
		}
	}
}

func TestLoggerLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	lg := newLogger(&buf, levelWarn, formatText)
	lg.Infof("info %d", 1)
	lg.Warnf("warn %d", 2)
	lg.Debug("debug")
	lg.Error("error", "endpoint", "x")
	text := buf.String()
	if strings.Contains(text, "info 1") || strings.Contains(text, "debug") {
		t.Errorf("low-priority messages must be filtered out at warn level:\n%s", text)
	}
	if !strings.Contains(text, "WARN warn 2") {
		t.Errorf("missing warn message:\n%s", text)
	}
	if !strings.Contains(text, "ERROR error endpoint=\"x\"") {
		t.Errorf("missing error message with fields:\n%s", text)
	}
}

func TestLoggerTextOutputWithFields(t *testing.T) {
	var buf bytes.Buffer
	lg := newLogger(&buf, levelInfo, formatText)
	lg.Error("collect failed", "endpoint", "proxies", "error", `get "http://x"`)
	line := buf.String()
	for _, want := range []string{
		" ERROR collect failed",
		`endpoint="proxies"`,
		`error="get \"http://x\""`,
	} {
		if !strings.Contains(line, want) {
			t.Errorf("text log missing %q:\n%s", want, line)
		}
	}
}

func TestLoggerJSONOutput(t *testing.T) {
	var buf bytes.Buffer
	lg := newLogger(&buf, levelInfo, formatJSON)
	lg.Error("collect failed", "endpoint", "proxies", "error", "boom")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("invalid JSON log line %q: %v", buf.String(), err)
	}
	for _, key := range []string{"ts", "level", "msg", "endpoint", "error"} {
		if _, ok := entry[key]; !ok {
			t.Errorf("JSON entry missing key %q: %v", key, entry)
		}
	}
	if entry["level"] != "error" || entry["msg"] != "collect failed" || entry["endpoint"] != "proxies" || entry["error"] != "boom" {
		t.Errorf("unexpected JSON entry: %v", entry)
	}
	if _, err := time.Parse(time.RFC3339Nano, entry["ts"].(string)); err != nil {
		t.Errorf("unparsable ts %q: %v", entry["ts"], err)
	}
}

func TestLoggerOddKeyValues(t *testing.T) {
	var buf bytes.Buffer
	lg := newLogger(&buf, levelInfo, formatJSON)
	lg.Info("msg", "only-key")
	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("invalid JSON %q: %v", buf.String(), err)
	}
	if entry["only-key"] != "?" {
		t.Errorf("odd key-value tail should render as ?, got %v", entry["only-key"])
	}
}

func TestErrorBackoff(t *testing.T) {
	b := newErrorBackoff(time.Millisecond, 4*time.Millisecond)
	t0 := time.Unix(0, 0)

	// first failure is logged immediately
	if !b.next("proxies", t0) {
		t.Fatal("first failure must be logged")
	}
	// suppressed until base interval passes
	if b.next("proxies", t0.Add(time.Millisecond/2)) {
		t.Fatal("failure inside backoff window must be suppressed")
	}
	// logged again after base interval: delay grows to 2*base
	if !b.next("proxies", t0.Add(2*time.Millisecond)) {
		t.Fatal("failure after backoff window must be logged")
	}
	// capped: even far in the future the next allowed slot is base+2*base
	if !b.next("proxies", t0.Add(6*time.Millisecond)) {
		t.Fatal("capped delay must allow logging again")
	}
	// reset on recovery
	b.reset("proxies")
	if !b.next("proxies", t0.Add(time.Microsecond)) {
		t.Fatal("reset must allow immediate logging")
	}
	// backoff state is per key
	if !b.next("version", t0) {
		t.Fatal("independent key must log immediately")
	}
}

func TestLoggerReceivesPollFailures(t *testing.T) {
	f := newFakeAPI()
	f.versionFail.Store(true)
	f.connectionsFail.Store(true)
	f.proxiesFail.Store(true)
	srv := httptest.NewServer(f.handler())
	defer srv.Close()

	var buf bytes.Buffer
	e := newExporter(&options{apiBase: srv.URL, interval: time.Minute}, newLogger(&buf, levelError, formatText))
	e.collect(context.Background())

	text := buf.String()
	for _, endpoint := range []string{"version", "connections", "proxies"} {
		if !strings.Contains(text, `endpoint="`+endpoint+`"`) {
			t.Errorf("missing log line for failed endpoint %q:\n%s", endpoint, text)
		}
	}
}
