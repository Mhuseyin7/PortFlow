package proxy

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/portflow/portflow/internal/registry"
)

// fakeResolver lets tests inject arbitrary services.
type fakeResolver map[string]*registry.Service

func (f fakeResolver) GetByHostname(h string) (*registry.Service, error) {
	if s, ok := f[strings.ToLower(h)]; ok {
		return s, nil
	}
	return nil, registry.ErrNotFound
}

func TestServeUnknownHostReturns404Diagnostic(t *testing.T) {
	r := fakeResolver{}
	req := httptest.NewRequest("GET", "http://unknown.test/", nil)
	rw := httptest.NewRecorder()
	serve(Options{Resolver: r}, rw, req)
	if rw.Code != http.StatusNotFound {
		t.Fatalf("want 404 got %d", rw.Code)
	}
	if rw.Header().Get("X-PortFlow") != "diagnostic" {
		t.Fatalf("missing diagnostic marker")
	}
}

func TestServeProxiesToUpstream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Forwarded-Host"); got != "api.shop.test" {
			t.Errorf("X-Forwarded-Host = %q", got)
		}
		// The proxy rewrites Host to the target host:port so upstream sees
		// the loopback address it's actually bound to.
		t.Logf("upstream Host: %s", r.Host)
		io.WriteString(w, "hello from upstream")
	}))
	t.Cleanup(upstream.Close)

	// Parse the port off the httptest server URL.
	_, portStr, _ := net.SplitHostPort(strings.TrimPrefix(upstream.URL, "http://"))
	port, _ := strconv.Atoi(portStr)

	r := fakeResolver{
		"api.shop.test": {Hostname: "api.shop.test", TargetHost: "127.0.0.1", TargetPort: port},
	}
	req := httptest.NewRequest("GET", "http://api.shop.test/", nil)
	rw := httptest.NewRecorder()
	serve(Options{Resolver: r}, rw, req)
	if rw.Code != 200 {
		t.Fatalf("want 200 got %d, body=%s", rw.Code, rw.Body.String())
	}
	if !strings.Contains(rw.Body.String(), "hello from upstream") {
		t.Fatalf("body: %s", rw.Body.String())
	}
}

func TestExternalTargetsRejectedByDefault(t *testing.T) {
	r := fakeResolver{
		"lan.test": {Hostname: "lan.test", TargetHost: "10.0.0.1", TargetPort: 80},
	}
	req := httptest.NewRequest("GET", "http://lan.test/", nil)
	rw := httptest.NewRecorder()
	serve(Options{Resolver: r, AllowExternalTargets: false}, rw, req)
	if rw.Code != http.StatusBadGateway {
		t.Fatalf("want 502 got %d", rw.Code)
	}
}

// Sanity: dial an httptest upstream over a real listener within the test's
// deadline. Not testing PortFlow directly — just guards the test env.
func TestUpstreamReachableWithinDeadline(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
	}))
	t.Cleanup(upstream.Close)
	c, err := net.DialTimeout("tcp", strings.TrimPrefix(upstream.URL, "http://"), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
}
