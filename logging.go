package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// logLevel is a filtering threshold for the exporter logger.
type logLevel int

const (
	levelDebug logLevel = iota
	levelInfo
	levelWarn
	levelError
)

func parseLogLevel(s string) (logLevel, error) {
	switch strings.ToLower(s) {
	case "debug":
		return levelDebug, nil
	case "info":
		return levelInfo, nil
	case "warn", "warning":
		return levelWarn, nil
	case "error":
		return levelError, nil
	default:
		return 0, fmt.Errorf("unknown log level %q (want debug, info, warn or error)", s)
	}
}

func (l logLevel) label() string {
	switch l {
	case levelDebug:
		return "DEBUG"
	case levelInfo:
		return "INFO"
	case levelWarn:
		return "WARN"
	case levelError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// logFormat controls the output encoding.
type logFormat int

const (
	formatText logFormat = iota
	formatJSON
)

func parseLogFormat(s string) (logFormat, error) {
	switch strings.ToLower(s) {
	case "text":
		return formatText, nil
	case "json":
		return formatJSON, nil
	default:
		return 0, fmt.Errorf("unknown log format %q (want text or json)", s)
	}
}

// logger writes level-filtered, optionally structured log lines to a single
// writer. It is a tiny stdlib-only logger (no external logging libraries).
type logger struct {
	mu     sync.Mutex
	out    io.Writer
	level  logLevel
	format logFormat
}

func newLogger(out io.Writer, level logLevel, format logFormat) *logger {
	return &logger{out: out, level: level, format: format}
}

func (l *logger) enabled(lv logLevel) bool { return lv >= l.level }

func (l *logger) log(lv logLevel, msg string, kv ...any) {
	if !l.enabled(lv) || l.out == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.format == formatJSON {
		l.writeJSON(lv, msg, kv...)
	} else {
		l.writeText(lv, msg, kv...)
	}
}

func (l *logger) writeText(lv logLevel, msg string, kv ...any) {
	var b strings.Builder
	b.WriteString(time.Now().Format("2006/01/02 15:04:05"))
	b.WriteByte(' ')
	b.WriteString(lv.label())
	b.WriteByte(' ')
	b.WriteString(msg)
	writeKeyValues(&b, kv)
	b.WriteByte('\n')
	_, _ = io.WriteString(l.out, b.String())
}

func (l *logger) writeJSON(lv logLevel, msg string, kv ...any) {
	entry := make(map[string]any, len(kv)/2+3)
	entry["ts"] = time.Now().Format(time.RFC3339Nano)
	entry["level"] = strings.ToLower(lv.label())
	entry["msg"] = msg
	writeKeyValuesMap(entry, kv)
	data, err := json.Marshal(entry)
	if err != nil {
		_, _ = fmt.Fprintf(l.out, `{"ts":%q,"level":"error","msg":"failed to encode log entry: %v"}`+"\n",
			time.Now().Format(time.RFC3339Nano), err)
		return
	}
	data = append(data, '\n')
	_, _ = l.out.Write(data)
}

// writeKeyValues renders trailing key/value pairs as ` key="value"` for text
// output. An odd number of pairs gets a "?" value placeholder.
func writeKeyValues(b *strings.Builder, kv []any) {
	for i := 0; i+1 < len(kv); i += 2 {
		_, _ = fmt.Fprintf(b, " %v=%q", kv[i], fmt.Sprint(kv[i+1]))
	}
	if len(kv)%2 == 1 {
		_, _ = fmt.Fprintf(b, " %v=%q", kv[len(kv)-1], "?")
	}
}

// writeKeyValuesMap fills the JSON entry map with typed field values: numbers,
// booleans and durations keep their native JSON type instead of being flattened
// into strings, so log shippers can filter and aggregate on them.
func writeKeyValuesMap(m map[string]any, kv []any) {
	for i := 0; i+1 < len(kv); i += 2 {
		m[fmt.Sprint(kv[i])] = jsonFieldValue(kv[i+1])
	}
	if len(kv)%2 == 1 {
		m[fmt.Sprint(kv[len(kv)-1])] = "?"
	}
}

// jsonFieldValue converts a log field value to its JSON-native representation.
// Durations become second-precision floats (the same value the text format
// renders as "41.2ms"); errors become their message; everything else preserves
// its native JSON type.
func jsonFieldValue(v any) any {
	switch x := v.(type) {
	case nil:
		return nil
	case time.Duration:
		return x.Seconds()
	case error:
		return x.Error()
	case bool,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64, uintptr,
		float32, float64, string:
		return x
	default:
		return fmt.Sprint(x)
	}
}

// Debugf/Infof/Warnf/Errorf log a printf-style message.
func (l *logger) Debugf(format string, args ...any) { l.log(levelDebug, fmt.Sprintf(format, args...)) }
func (l *logger) Infof(format string, args ...any)  { l.log(levelInfo, fmt.Sprintf(format, args...)) }
func (l *logger) Warnf(format string, args ...any)  { l.log(levelWarn, fmt.Sprintf(format, args...)) }
func (l *logger) Errorf(format string, args ...any) { l.log(levelError, fmt.Sprintf(format, args...)) }

// Debug/Info/Warn/Error log a plain message with optional key/value fields.
func (l *logger) Debug(msg string, kv ...any) { l.log(levelDebug, msg, kv...) }
func (l *logger) Info(msg string, kv ...any)  { l.log(levelInfo, msg, kv...) }
func (l *logger) Warn(msg string, kv ...any)  { l.log(levelWarn, msg, kv...) }
func (l *logger) Error(msg string, kv ...any) { l.log(levelError, msg, kv...) }

// errorBackoff rate-limits repeated failure messages per endpoint: the first
// failure is logged immediately, later ones at exponentially growing intervals
// (capped) until the endpoint recovers.
type errorBackoff struct {
	mu       sync.Mutex
	base     time.Duration
	cap      time.Duration
	attempts map[string]int
	nextAt   map[string]time.Time
}

func newErrorBackoff(base, cap time.Duration) *errorBackoff {
	return &errorBackoff{
		base:     base,
		cap:      cap,
		attempts: make(map[string]int),
		nextAt:   make(map[string]time.Time),
	}
}

// next reports whether a failure for key should be logged at time now.
func (b *errorBackoff) next(key string, now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if nextAt, ok := b.nextAt[key]; ok && now.Before(nextAt) {
		return false
	}
	delay := b.base
	for n := b.attempts[key]; n > 0 && delay < b.cap; n-- {
		delay *= 2
	}
	if delay > b.cap {
		delay = b.cap
	}
	b.attempts[key]++
	b.nextAt[key] = now.Add(delay)
	return true
}

// isFailing reports whether key has any recorded failure history, i.e. it is
// currently in an error state that a recovery log line would resolve.
func (b *errorBackoff) isFailing(key string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.attempts[key] > 0
}

// reset forgets the failure history of key, e.g. when the endpoint recovered.
func (b *errorBackoff) reset(key string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.attempts, key)
	delete(b.nextAt, key)
}
