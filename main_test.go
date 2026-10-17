package main

import (
	"testing"
)

func TestParseFlagsTrimsTrailingSlash(t *testing.T) {
	o, err := parseFlags([]string{"-api", "http://127.0.0.1:9097/"})
	if err != nil {
		t.Fatal(err)
	}
	if o.apiBase != "http://127.0.0.1:9097" {
		t.Errorf("apiBase = %q, want %q", o.apiBase, "http://127.0.0.1:9097")
	}
}

func TestParseFlagsAcceptsDefaults(t *testing.T) {
	o, err := parseFlags(nil)
	if err != nil {
		t.Fatal(err)
	}
	if o.apiBase != defaultAPI {
		t.Errorf("apiBase = %q, want %q", o.apiBase, defaultAPI)
	}
	if o.interval <= 0 {
		t.Errorf("interval = %s, want positive", o.interval)
	}
}

func TestParseFlagsRejectsNonPositiveInterval(t *testing.T) {
	for _, arg := range []string{"0", "-1s", "-500ms"} {
		if _, err := parseFlags([]string{"-interval", arg}); err == nil {
			t.Errorf("interval=%s: expected error, got nil", arg)
		}
	}
}

func TestParseFlagsRejectsInvalidPort(t *testing.T) {
	for _, arg := range []string{"-1", "0", "65536", "abc"} {
		if _, err := parseFlags([]string{"-port", arg}); err == nil {
			t.Errorf("port=%s: expected error, got nil", arg)
		}
	}
}
