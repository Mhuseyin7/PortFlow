package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/portflow/portflow/internal/hosts"
	"github.com/portflow/portflow/internal/registry"
	"github.com/portflow/portflow/internal/reqlog"
	pfca "github.com/portflow/portflow/internal/tls"
)

type env struct {
	ts        *httptest.Server
	deps      Deps
	hostsPath string
	dataDir   string
}

func newEnv(t *testing.T, mutate func(*Deps)) *env {
	t.Helper()
	dir := t.TempDir()
	store, err := registry.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	ca, err := pfca.LoadOrCreateCA(dir)
	if err != nil {
		t.Fatal(err)
	}
	certs, err := pfca.NewCertStore(ca, dir)
	if err != nil {
		t.Fatal(err)
	}
	hostsPath := filepath.Join(dir, "hosts")
	if err := os.WriteFile(hostsPath, []byte("127.0.0.1 localhost\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deps := Deps{
		Store:     store,
		Hosts:     hosts.NewManagerAt(hostsPath),
		CA:        ca,
		CertStore: certs,
		Log:       reqlog.New(10),
		StartedAt: time.Now(),
		Version:   "test",
	}
	if mutate != nil {
		mutate(&deps)
	}
	ts := httptest.NewServer(New(deps).Handler())
	t.Cleanup(ts.Close)
	return &env{ts: ts, deps: deps, hostsPath: hostsPath, dataDir: dir}
}

type opt func(*http.Request)

func withHeader(k, v string) opt { return func(r *http.Request) { r.Header.Set(k, v) } }
func withHost(h string) opt      { return func(r *http.Request) { r.Host = h } }
func noCSRF() opt                { return func(r *http.Request) { r.Header.Del(CSRFHeader) } }

func (e *env) call(t *testing.T, method, path, body string, opts ...opt) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(method, e.ts.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(CSRFHeader, "1")
	req.Header.Set("Content-Type", "application/json")
	for _, o := range opts {
		o(req)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, string(b)
}

const goodBody = `{"hostname":"api.shop.test","target_port":8000,"project":"shop"}`

func TestRegisterListGetDeleteLifecycle(t *testing.T) {
	e := newEnv(t, nil)

	resp, body := e.call(t, "POST", "/api/services", goodBody)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: %d %s", resp.StatusCode, body)
	}
	var created createResponse
	if err := json.Unmarshal([]byte(body), &created); err != nil {
		t.Fatal(err)
	}
	if !created.TLS || created.TargetHost != "127.0.0.1" || created.Hostname != "api.shop.test" || len(created.Warnings) != 0 {
		t.Fatalf("unexpected created service (tls must default to true): %+v", created)
	}
	if h, _ := os.ReadFile(e.hostsPath); !strings.Contains(string(h), "api.shop.test") || !strings.Contains(string(h), "127.0.0.1 localhost") {
		t.Fatalf("hosts file not updated correctly:\n%s", h)
	}
	if _, err := os.Stat(filepath.Join(e.dataDir, "certs", "leaf-api.shop.test.pem")); err != nil {
		t.Fatalf("certificate should be issued at registration: %v", err)
	}

	if resp, _ := e.call(t, "POST", "/api/services", goodBody); resp.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate hostname: got %d, want 409", resp.StatusCode)
	}
	if resp, body := e.call(t, "GET", "/api/services/API.shop.test", ""); resp.StatusCode != 200 || !strings.Contains(body, "api.shop.test") {
		t.Fatalf("get is case-insensitive: %d %s", resp.StatusCode, body)
	}

	if resp, _ := e.call(t, "DELETE", "/api/services/api.shop.test", ""); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d", resp.StatusCode)
	}
	if h, _ := os.ReadFile(e.hostsPath); strings.Contains(string(h), "api.shop.test") {
		t.Fatal("hosts entry should be removed")
	}
	if _, err := os.Stat(filepath.Join(e.dataDir, "certs", "leaf-api.shop.test.pem")); err == nil {
		t.Fatal("certificate should be purged")
	}
	if _, body := e.call(t, "GET", "/api/services", ""); strings.TrimSpace(body) != "[]" {
		t.Fatalf("empty list must be [] not null, got %q", body)
	}
}

func TestRegisterRejectsDangerousInput(t *testing.T) {
	e := newEnv(t, nil)
	cases := map[string]string{
		"real domain":         `{"hostname":"google.com","target_port":80}`,
		"suffix lookalike":    `{"hostname":"google.com.test.io","target_port":80}`,
		"hosts injection":     `{"hostname":"a.test\n127.0.0.1 google.com","target_port":80}`,
		"path traversal":      `{"hostname":"../../etc/x.test","target_port":80}`,
		"single label":        `{"hostname":"localhost","target_port":80}`,
		"metadata target":     `{"hostname":"a.shop.test","target_host":"169.254.169.254","target_port":80}`,
		"lan target":          `{"hostname":"a.shop.test","target_host":"192.168.1.10","target_port":80}`,
		"dns target":          `{"hostname":"a.shop.test","target_host":"example.com","target_port":80}`,
		"wildcard target":     `{"hostname":"a.shop.test","target_host":"0.0.0.0","target_port":80}`,
		"xss project":         `{"hostname":"a.shop.test","target_port":80,"project":"<img src=x onerror=alert(1)>"}`,
		"port zero":           `{"hostname":"a.shop.test","target_port":0}`,
		"port too large":      `{"hostname":"a.shop.test","target_port":70000}`,
		"unknown field":       `{"hostname":"a.shop.test","target_port":80,"command":"rm -rf /"}`,
		"not json":            `hostname=a.shop.test`,
		"empty body":          ``,
		"port as string":      `{"hostname":"a.shop.test","target_port":"80"}`,
	}
	for name, body := range cases {
		resp, out := e.call(t, "POST", "/api/services", body)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: got %d (%s), want 400", name, resp.StatusCode, out)
		}
	}
	if _, body := e.call(t, "GET", "/api/services", ""); strings.TrimSpace(body) != "[]" {
		t.Fatalf("nothing must have been registered, got %s", body)
	}
	if h, _ := os.ReadFile(e.hostsPath); string(h) != "127.0.0.1 localhost\n" {
		t.Fatalf("hosts file must be untouched:\n%s", h)
	}
}

func TestOversizedBodyIsRejected(t *testing.T) {
	e := newEnv(t, nil)
	big := `{"hostname":"a.shop.test","target_port":80,"project":"` + strings.Repeat("x", maxBodyBytes) + `"}`
	if resp, _ := e.call(t, "POST", "/api/services", big); resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("got %d, want 413", resp.StatusCode)
	}
}

func TestExternalTargetsRequireExplicitOptIn(t *testing.T) {
	e := newEnv(t, func(d *Deps) { d.AllowExternalTargets = true })
	resp, body := e.call(t, "POST", "/api/services", `{"hostname":"db.shop.test","target_host":"10.1.2.3","target_port":80}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("with --allow-external-targets: %d %s", resp.StatusCode, body)
	}
}

func TestExplicitPlainHTTP(t *testing.T) {
	e := newEnv(t, nil)
	resp, body := e.call(t, "POST", "/api/services", `{"hostname":"web.shop.test","target_port":80,"tls":false}`)
	if resp.StatusCode != http.StatusCreated || strings.Contains(body, `"tls":true`) {
		t.Fatalf("tls:false must be honoured: %d %s", resp.StatusCode, body)
	}
}

func TestHostsFailureIsReportedNotSwallowed(t *testing.T) {
	e := newEnv(t, func(d *Deps) {
		d.Hosts = hosts.NewManagerAt(t.TempDir()) // a directory: cannot be read as a file
	})
	resp, body := e.call(t, "POST", "/api/services", goodBody)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	var out createResponse
	_ = json.Unmarshal([]byte(body), &out)
	if len(out.Warnings) == 0 || !strings.Contains(out.Warnings[0], "hosts file") {
		t.Fatalf("expected a hosts-file warning, got %+v", out.Warnings)
	}
}

func TestDNSRebindingIsBlocked(t *testing.T) {
	e := newEnv(t, nil)
	for _, h := range []string{"evil.example.com", "evil.example.com:9280", "127.0.0.1.evil.com", "192.168.1.5:9280", "localhost.evil.com"} {
		resp, _ := e.call(t, "GET", "/api/services", "", withHost(h))
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("Host %q: got %d, want 403", h, resp.StatusCode)
		}
	}
	for _, h := range []string{"localhost", "localhost:9280", "127.0.0.1:9280", "[::1]:9280"} {
		if resp, _ := e.call(t, "GET", "/health", "", withHost(h)); resp.StatusCode != http.StatusOK {
			t.Errorf("Host %q should be allowed, got %d", h, resp.StatusCode)
		}
	}
}

func TestCSRFProtection(t *testing.T) {
	e := newEnv(t, nil)
	host := strings.TrimPrefix(e.ts.URL, "http://")

	if resp, _ := e.call(t, "POST", "/api/services", goodBody, noCSRF()); resp.StatusCode != http.StatusForbidden {
		t.Errorf("missing %s header: got %d, want 403", CSRFHeader, resp.StatusCode)
	}
	if resp, _ := e.call(t, "POST", "/api/services", goodBody, withHeader("Origin", "http://evil.example.com")); resp.StatusCode != http.StatusForbidden {
		t.Errorf("foreign Origin: got %d, want 403", resp.StatusCode)
	}
	if resp, _ := e.call(t, "POST", "/api/services", goodBody, withHeader("Origin", "null")); resp.StatusCode != http.StatusForbidden {
		t.Errorf("null Origin: got %d, want 403", resp.StatusCode)
	}
	if resp, _ := e.call(t, "DELETE", "/api/services/x.shop.test", "", noCSRF()); resp.StatusCode != http.StatusForbidden {
		t.Errorf("DELETE without header: got %d, want 403", resp.StatusCode)
	}
	if resp, _ := e.call(t, "POST", "/api/shutdown", "", noCSRF()); resp.StatusCode != http.StatusForbidden {
		t.Errorf("shutdown without header: got %d, want 403", resp.StatusCode)
	}
	if resp, _ := e.call(t, "POST", "/api/services", goodBody, withHeader("Origin", "http://"+host)); resp.StatusCode != http.StatusCreated {
		t.Errorf("same-origin dashboard request must work: got %d", resp.StatusCode)
	}
	if resp, _ := e.call(t, "GET", "/api/services", "", noCSRF()); resp.StatusCode != http.StatusOK {
		t.Errorf("read-only GET needs no header: got %d", resp.StatusCode)
	}
}

func TestNonLoopbackPeerIsRejected(t *testing.T) {
	e := newEnv(t, nil)
	h := New(e.deps).Handler()
	for _, remote := range []string{"10.0.0.5:4000", "192.168.1.2:1", "203.0.113.7:99", "[2001:db8::1]:80"} {
		req := httptest.NewRequest("GET", "http://127.0.0.1:9280/api/status", nil)
		req.RemoteAddr = remote
		rw := httptest.NewRecorder()
		h.ServeHTTP(rw, req)
		if rw.Code != http.StatusForbidden {
			t.Errorf("peer %s: got %d, want 403", remote, rw.Code)
		}
	}
	for _, remote := range []string{"127.0.0.1:4000", "[::1]:4000"} {
		req := httptest.NewRequest("GET", "http://127.0.0.1:9280/health", nil)
		req.RemoteAddr = remote
		rw := httptest.NewRecorder()
		h.ServeHTTP(rw, req)
		if rw.Code != http.StatusOK {
			t.Errorf("peer %s: got %d, want 200", remote, rw.Code)
		}
	}
}

func TestSecurityHeaders(t *testing.T) {
	e := newEnv(t, nil)
	resp, _ := e.call(t, "GET", "/", "")
	for k, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Cache-Control":          "no-store",
	} {
		if got := resp.Header.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if !strings.Contains(resp.Header.Get("Content-Security-Policy"), "default-src 'self'") {
		t.Error("dashboard needs a restrictive CSP")
	}
}

func TestStatusCachesTheTrustCheck(t *testing.T) {
	var calls int32
	e := newEnv(t, func(d *Deps) {
		d.Trusted = func() bool { atomic.AddInt32(&calls, 1); return true }
	})
	for i := 0; i < 6; i++ {
		_, body := e.call(t, "GET", "/api/status", "")
		if !strings.Contains(body, `"ca_trusted":true`) {
			t.Fatalf("status = %s", body)
		}
	}
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Fatalf("trust check ran %d times for 6 polls; it spawns a process and must be cached", n)
	}
}

func TestShutdownRunsOnce(t *testing.T) {
	var n int32
	done := make(chan struct{}, 4)
	e := newEnv(t, func(d *Deps) {
		d.ShutdownFunc = func() { atomic.AddInt32(&n, 1); done <- struct{}{} }
	})
	if resp, _ := e.call(t, "GET", "/api/shutdown", ""); resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET shutdown: %d", resp.StatusCode)
	}
	for i := 0; i < 3; i++ {
		if resp, _ := e.call(t, "POST", "/api/shutdown", ""); resp.StatusCode != http.StatusOK {
			t.Fatalf("shutdown: %d", resp.StatusCode)
		}
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ShutdownFunc was not called")
	}
	time.Sleep(400 * time.Millisecond)
	if got := atomic.LoadInt32(&n); got != 1 {
		t.Fatalf("ShutdownFunc called %d times", got)
	}
}

func TestRequestLogEndpoints(t *testing.T) {
	e := newEnv(t, nil)
	_, body := e.call(t, "GET", "/api/requests", "")
	if !strings.Contains(body, `"enabled":false`) || !strings.Contains(body, `"entries":[]`) {
		t.Fatalf("log must start disabled and empty: %s", body)
	}
	if resp, body := e.call(t, "POST", "/api/requests/enable", ""); resp.StatusCode != 200 || !strings.Contains(body, "true") {
		t.Fatalf("enable: %d %s", resp.StatusCode, body)
	}
	if !e.deps.Log.Enabled() {
		t.Fatal("log should be enabled")
	}
	e.deps.Log.Add(reqlog.Entry{Host: "a.test", Path: "/x", Status: 200})
	if _, body := e.call(t, "GET", "/api/requests", ""); !strings.Contains(body, `"/x"`) {
		t.Fatalf("entry missing: %s", body)
	}
	if resp, _ := e.call(t, "DELETE", "/api/requests", ""); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("clear: %d", resp.StatusCode)
	}
	if resp, _ := e.call(t, "POST", "/api/requests/bogus", ""); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown action: %d", resp.StatusCode)
	}
	e.call(t, "POST", "/api/requests/disable", "")
	if e.deps.Log.Enabled() {
		t.Fatal("log should be disabled")
	}
}
