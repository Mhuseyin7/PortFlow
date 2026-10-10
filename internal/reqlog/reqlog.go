// Package reqlog is PortFlow's optional, development-only request log.
//
// It is OFF by default and keeps only a bounded in-memory ring buffer; nothing
// is written to disk. Only method, host, path, status and latency are
// recorded. Headers (including Authorization, Cookie and Set-Cookie) and
// bodies are never captured, and query values that look sensitive are
// redacted before an entry is stored.
package reqlog

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Entry is one proxied request.
type Entry struct {
	Time       time.Time `json:"time"`
	Host       string    `json:"host"`
	Method     string    `json:"method"`
	Path       string    `json:"path"` // path plus redacted query
	Status     int       `json:"status"`
	DurationMS float64   `json:"duration_ms"`
}

// Log is a fixed-size ring buffer of Entry values.
type Log struct {
	enabled atomic.Bool

	mu   sync.Mutex
	buf  []Entry
	next int
	full bool
}

// New returns a disabled log holding at most capacity entries.
func New(capacity int) *Log {
	if capacity < 1 {
		capacity = 1
	}
	return &Log{buf: make([]Entry, capacity)}
}

// Enabled reports whether requests are currently being recorded.
func (l *Log) Enabled() bool { return l != nil && l.enabled.Load() }

// SetEnabled turns recording on or off. Turning it off keeps existing entries.
func (l *Log) SetEnabled(on bool) { l.enabled.Store(on) }

// Add records an entry if the log is enabled.
func (l *Log) Add(e Entry) {
	if !l.Enabled() {
		return
	}
	l.mu.Lock()
	l.buf[l.next] = e
	l.next = (l.next + 1) % len(l.buf)
	if l.next == 0 {
		l.full = true
	}
	l.mu.Unlock()
}

// Snapshot returns the recorded entries, oldest first.
func (l *Log) Snapshot() []Entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.full {
		return append([]Entry(nil), l.buf[:l.next]...)
	}
	out := make([]Entry, 0, len(l.buf))
	out = append(out, l.buf[l.next:]...)
	out = append(out, l.buf[:l.next]...)
	return out
}

// Clear drops all recorded entries.
func (l *Log) Clear() {
	l.mu.Lock()
	l.next, l.full = 0, false
	l.mu.Unlock()
}

const redacted = "[redacted]"

// sensitiveKeyParts: a query key containing any of these has its value redacted.
var sensitiveKeyParts = []string{
	"token", "secret", "password", "passwd", "pwd", "auth", "key", "session",
	"sig", "code", "jwt", "credential", "cookie", "bearer", "otp", "pin",
}

func sensitiveKey(k string) bool {
	k = strings.ToLower(k)
	for _, p := range sensitiveKeyParts {
		if strings.Contains(k, p) {
			return true
		}
	}
	return false
}

// looksOpaque reports whether s resembles a token: long, no spaces, made only
// of URL-safe credential characters.
func looksOpaque(s string) bool {
	if len(s) < 24 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '-', c == '_', c == '.', c == '=', c == '+', c == '/', c == '%', c == '~':
		default:
			return false
		}
	}
	return true
}

// RedactURL returns path plus query with sensitive material removed:
//   - query values whose key looks sensitive,
//   - any query value or path segment that looks like an opaque token.
func RedactURL(path, rawQuery string) string {
	segs := strings.Split(path, "/")
	for i, s := range segs {
		if looksOpaque(s) {
			segs[i] = redacted
		}
	}
	out := strings.Join(segs, "/")
	if rawQuery == "" {
		return out
	}
	pairs := strings.Split(rawQuery, "&")
	for i, p := range pairs {
		k, v, hasVal := strings.Cut(p, "=")
		if hasVal && (sensitiveKey(k) || looksOpaque(v)) {
			pairs[i] = k + "=" + redacted
		}
	}
	return out + "?" + strings.Join(pairs, "&")
}
