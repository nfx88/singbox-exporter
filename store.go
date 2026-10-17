package main

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// sample is a single Prometheus time series in the text exposition format.
type sample struct {
	name   string
	help   string
	typ    string
	labels map[string]string
	value  float64
}

// store keeps the latest collected snapshot per Clash API section.
//
// Sections are updated independently, so a transient failure of one endpoint
// keeps the last successful values of that section instead of dropping its
// series from /metrics; the failure is only visible in singbox_up,
// singbox_endpoint_up and singbox_scrape_errors_total.
type store struct {
	mu       sync.RWMutex
	sections map[string][]sample
	up       bool
}

func newStore() *store {
	return &store{sections: make(map[string][]sample)}
}

func (s *store) setSection(name string, samples []sample) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(samples) == 0 {
		delete(s.sections, name)
		return
	}
	s.sections[name] = samples
}

func (s *store) setUp(up bool) {
	s.mu.Lock()
	s.up = up
	s.mu.Unlock()
}

func (s *store) isUp() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.up
}

// writeTo renders the merged snapshot in the Prometheus text format.
func (s *store) writeTo(w io.Writer) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	type metric struct {
		help, typ string
		samples   []sample
	}
	byName := make(map[string]*metric)
	var order []string
	names := make([]string, 0, len(s.sections))
	for name := range s.sections {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, section := range names {
		for _, sm := range s.sections[section] {
			m := byName[sm.name]
			if m == nil {
				m = &metric{help: sm.help, typ: sm.typ}
				byName[sm.name] = m
				order = append(order, sm.name)
			}
			m.samples = append(m.samples, sm)
		}
	}
	sort.Strings(order)

	for _, name := range order {
		m := byName[name]
		_, _ = fmt.Fprintf(w, "# HELP %s %s\n", name, m.help)
		_, _ = fmt.Fprintf(w, "# TYPE %s %s\n", name, m.typ)
		sort.Slice(m.samples, func(i, j int) bool {
			return labelString(m.samples[i].labels) < labelString(m.samples[j].labels)
		})
		for _, sm := range m.samples {
			if len(sm.labels) == 0 {
				_, _ = fmt.Fprintf(w, "%s %s\n", sm.name, strconv.FormatFloat(sm.value, 'f', -1, 64))
			} else {
				_, _ = fmt.Fprintf(w, "%s{%s} %s\n", sm.name, labelString(sm.labels), strconv.FormatFloat(sm.value, 'f', -1, 64))
			}
		}
	}
}

func labelString(labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		if b.Len() > 0 {
			b.WriteString(",")
		}
		b.WriteString(k)
		b.WriteString(`="`)
		b.WriteString(escapeValue(labels[k]))
		b.WriteString(`"`)
	}
	return b.String()
}

func escapeValue(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, `"`, `\"`)
	v = strings.ReplaceAll(v, "\n", `\n`)
	return v
}

func addSample(list *[]sample, name, help, typ string, labels map[string]string, value float64) {
	*list = append(*list, sample{name: name, help: help, typ: typ, labels: labels, value: value})
}
