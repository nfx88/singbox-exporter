package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestEscapeValue(t *testing.T) {
	cases := map[string]string{
		"":      "",
		"plain": "plain",
		`a"b`:   `a\"b`,
		"a\nb":  `a\nb`, // actual newline becomes backslash-n
		`a\b`:   `a\\b`,
		`a"b\c`: `a\"b\\c`,
	}
	for in, want := range cases {
		if got := escapeValue(in); got != want {
			t.Errorf("escapeValue(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLabelString(t *testing.T) {
	labels := map[string]string{"b": "2", "a": `x"y`}
	want := `a="x\"y",b="2"`
	if got := labelString(labels); got != want {
		t.Errorf("labelString = %q, want %q", got, want)
	}
}

func TestStoreWriteToSorted(t *testing.T) {
	s := newStore()
	s.setSection(sectionConnections, []sample{
		{name: "singbox_traffic_upload_bytes_total", help: "up", typ: "counter", value: 500},
		{name: "singbox_connections_by_outbound", help: "c", typ: "gauge", labels: map[string]string{"outbound": "b"}, value: 1},
		{name: "singbox_connections_by_outbound", help: "c", typ: "gauge", labels: map[string]string{"outbound": "a"}, value: 2},
	})
	s.setSection("status", []sample{
		{name: "singbox_up", help: "u", typ: "gauge", value: 1},
	})

	var buf bytes.Buffer
	s.writeTo(&buf)
	text := buf.String()

	for _, want := range []string{
		"# HELP singbox_up u",
		"# TYPE singbox_up gauge",
		"singbox_up 1",
		"singbox_traffic_upload_bytes_total 500",
		`singbox_connections_by_outbound{outbound="a"} 2`,
		`singbox_connections_by_outbound{outbound="b"} 1`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("output missing %q\n---\n%s", want, text)
		}
	}
	if idx := strings.Index(text, `{outbound="a"}`); idx < 0 {
		t.Fatalf("sorted sample not found")
	} else if idx2 := strings.Index(text, `{outbound="b"}`); idx2 < idx {
		t.Errorf("samples not sorted by label: a after b")
	}

	var second bytes.Buffer
	s.writeTo(&second)
	if second.String() != text {
		t.Error("writeTo output is not deterministic")
	}
}

func TestStoreKeepsSectionOnFailedPoll(t *testing.T) {
	s := newStore()
	s.setSection(sectionProxies, []sample{
		{name: "singbox_proxy_info", help: "i", typ: "gauge", labels: map[string]string{"proxy": "n1", "type": "VMess"}, value: 1},
	})
	// a later poll fails for /proxies and never calls setSection for it.

	var buf bytes.Buffer
	s.writeTo(&buf)
	if !strings.Contains(buf.String(), `singbox_proxy_info{proxy="n1"`) {
		t.Error("last successful section values must survive a failed poll")
	}
}
