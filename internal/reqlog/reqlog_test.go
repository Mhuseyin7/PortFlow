package reqlog

import (
	"strings"
	"testing"
)

func TestDisabledByDefault(t *testing.T) {
	l := New(4)
	l.Add(Entry{Path: "/x"})
	if len(l.Snapshot()) != 0 {
		t.Fatal("a disabled log must not record anything")
	}
	var nilLog *Log
	if nilLog.Enabled() {
		t.Fatal("nil log must be disabled")
	}
}

func TestRingBufferKeepsNewestInOrder(t *testing.T) {
	l := New(3)
	l.SetEnabled(true)
	for _, p := range []string{"/1", "/2", "/3", "/4", "/5"} {
		l.Add(Entry{Path: p})
	}
	got := l.Snapshot()
	if len(got) != 3 || got[0].Path != "/3" || got[2].Path != "/5" {
		t.Fatalf("unexpected snapshot: %+v", got)
	}
	l.Clear()
	if len(l.Snapshot()) != 0 {
		t.Fatal("Clear should empty the log")
	}
}

func TestRedactURL(t *testing.T) {
	cases := []struct {
		path, query string
		want        string
		mustNot     string
	}{
		{"/api", "page=2&sort=asc", "/api?page=2&sort=asc", ""},
		{"/cb", "code=abc123&state=xyz", "/cb?code=[redacted]&state=xyz", "abc123"},
		{"/x", "access_token=eyJhbGciOi&q=1", "/x?access_token=[redacted]&q=1", "eyJhbGciOi"},
		{"/x", "API_KEY=sekrit", "/x?API_KEY=[redacted]", "sekrit"},
		{"/x", "ref=0123456789abcdef0123456789abcdef", "/x?ref=[redacted]", "0123456789abcdef"},
		{"/reset/0123456789abcdef0123456789abcdef", "", "/reset/[redacted]", "0123456789abcdef"},
		{"/plain/path", "", "/plain/path", ""},
		{"/x", "flag", "/x?flag", ""},
	}
	for _, c := range cases {
		got := RedactURL(c.path, c.query)
		if got != c.want {
			t.Errorf("RedactURL(%q,%q) = %q want %q", c.path, c.query, got, c.want)
		}
		if c.mustNot != "" && strings.Contains(got, c.mustNot) {
			t.Errorf("secret %q leaked in %q", c.mustNot, got)
		}
	}
}
