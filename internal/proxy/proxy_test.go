package proxy

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/portflow/portflow/internal/registry"
	"github.com/portflow/portflow/internal/reqlog"
	pfca "github.com/portflow/portflow/internal/tls"
)

// fakeResolver lets tests inject arbitrary services.
type fakeResolver map[string]*registry.Service

func (f fakeResolver) GetByHostname(h string) (*registry.Service, error) {
	if s, ok := f[strings.ToLower(h)]; ok {
		return s, nil
	}
	return nil, registry.ErrNotFound
}

func portOf(t *testing.T, rawURL string) int {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	p, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func svcFor(host string, port int) fakeResolver {
	return fakeResolver{host: {Hostname: host, TargetHost: "127.0.0.1", TargetPort: port}}
}

func do(h http.Handler, rawURL string, mutate func(*http.Request)) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", rawURL, nil)
	if mutate != nil {
		mutate(req)
	}
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)
	return rw
}

func waitFor(t *testing.T, d time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}

func TestUnknownHostReturns404Diagnostic(t *testing.T) {
	rw := do(New(Options{Resolver: fakeResolver{}}).Handler(), "http://unknown.test/", nil)
	if rw.Code != http.StatusNotFound {
		t.Fatalf("want 404 got %d", rw.Code)
	}
	if rw.Header().Get("X-PortFlow") != "diagnostic" {
		t.Fatal("missing diagnostic marker")
	}
}

func TestDiagnosticPageEscapesHostHeader(t *testing.T) {
	rw := do(New(Options{Resolver: fakeResolver{}}).Handler(), "http://x/", func(r *http.Request) {
		r.Host = `<img src=x onerror=alert(1)>.test`
	})
	body := rw.Body.String()
	if strings.Contains(body, "<img") {
		t.Fatalf("Host header reached the diagnostic page unescaped:\n%s", body)
	}
	if !strings.Contains(body, "&lt;img") {
		t.Fatalf("expected escaped host in page:\n%s", body)
	}
	if rw.Header().Get("Content-Security-Policy") == "" || rw.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("diagnostic page must carry CSP and nosniff headers")
	}
}

type seen struct{ host, xff, xfh, xfp, uri string }

func recordingUpstream(t *testing.T) (*httptest.Server, chan seen) {
	t.Helper()
	got := make(chan seen, 4)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- seen{r.Host, r.Header.Get("X-Forwarded-For"), r.Header.Get("X-Forwarded-Host"), r.Header.Get("X-Forwarded-Proto"), r.RequestURI}
		_, _ = io.WriteString(w, "hello from upstream")
	}))
	t.Cleanup(up.Close)
	return up, got
}

func TestProxiesAndSetsForwardingHeaders(t *testing.T) {
	up, got := recordingUpstream(t)
	h := New(Options{Resolver: svcFor("api.shop.test", portOf(t, up.URL))}).Handler()
	rw := do(h, "http://api.shop.test/a/b?x=1", func(r *http.Request) {
		r.Header.Set("X-Forwarded-For", "6.6.6.6") // client-supplied, must not be trusted
	})
	if rw.Code != 200 || !strings.Contains(rw.Body.String(), "hello from upstream") {
		t.Fatalf("status=%d body=%s", rw.Code, rw.Body.String())
	}
	s := <-got
	if s.xff != "192.0.2.1" {
		t.Errorf("X-Forwarded-For = %q, want the observed client address only", s.xff)
	}
	if s.xfh != "api.shop.test" || s.xfp != "http" {
		t.Errorf("X-Forwarded-Host/Proto = %q/%q", s.xfh, s.xfp)
	}
	if s.uri != "/a/b?x=1" {
		t.Errorf("upstream saw %q", s.uri)
	}
	if want := strings.TrimPrefix(up.URL, "http://"); s.host != want {
		t.Errorf("default Host strategy should target the upstream: got %q want %q", s.host, want)
	}
}

func TestPreserveHostForwardsOriginalHost(t *testing.T) {
	up, got := recordingUpstream(t)
	h := New(Options{Resolver: svcFor("api.shop.test", portOf(t, up.URL)), PreserveHost: true}).Handler()
	do(h, "http://api.shop.test/", nil)
	if s := <-got; s.host != "api.shop.test" {
		t.Fatalf("Host = %q, want the original hostname", s.host)
	}
}

func TestResolveTargetPolicy(t *testing.T) {
	mk := func(host string, port int) *registry.Service {
		return &registry.Service{TargetHost: host, TargetPort: port}
	}
	for _, h := range []string{"10.0.0.1", "169.254.169.254", "0.0.0.0", "192.168.1.5", "example.com", "127.0.0.1.evil.com"} {
		if _, err := resolveTarget(mk(h, 80), false); err == nil {
			t.Errorf("target %q must be rejected by default", h)
		}
	}
	for h, want := range map[string]string{
		"":          "127.0.0.1:80",
		"127.0.0.1": "127.0.0.1:80",
		"localhost": "localhost:80",
		"::1":       "[::1]:80",
		"[::1]":     "[::1]:80",
	} {
		u, err := resolveTarget(mk(h, 80), false)
		if err != nil || u.Host != want {
			t.Errorf("target %q: got %v, %v want %s", h, u, err, want)
		}
	}
	if _, err := resolveTarget(mk("10.0.0.1", 80), true); err != nil {
		t.Errorf("external target must be allowed when enabled: %v", err)
	}
	for _, p := range []int{0, -1, 65536} {
		if _, err := resolveTarget(mk("127.0.0.1", p), false); err == nil {
			t.Errorf("port %d must be rejected", p)
		}
	}
}

func TestExternalTargetsRejectedByDefault(t *testing.T) {
	r := fakeResolver{"lan.test": {Hostname: "lan.test", TargetHost: "10.0.0.1", TargetPort: 80}}
	rw := do(New(Options{Resolver: r}).Handler(), "http://lan.test/", nil)
	if rw.Code != http.StatusBadGateway {
		t.Fatalf("want 502 got %d", rw.Code)
	}
}

func TestUpstreamDownGivesUsefulDiagnostics(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close() // nothing listens here any more

	rw := do(New(Options{Resolver: svcFor("gone.shop.test", port)}).Handler(), "http://gone.shop.test/", nil)
	if rw.Code != http.StatusBadGateway {
		t.Fatalf("want 502 got %d", rw.Code)
	}
	if !strings.Contains(rw.Body.String(), "Nothing is listening on port "+strconv.Itoa(port)) {
		t.Fatalf("diagnostic page should name the dead port:\n%s", rw.Body.String())
	}
}

func TestTLSServicesRedirectFromPlainHTTP(t *testing.T) {
	res := fakeResolver{"api.shop.test": {Hostname: "api.shop.test", TargetHost: "127.0.0.1", TargetPort: 9, TLS: true}}
	s := New(Options{Resolver: res})

	s.httpsPort = "443"
	rw := do(s.Handler(), "http://api.shop.test/p?q=1", nil)
	if rw.Code != http.StatusTemporaryRedirect || rw.Header().Get("Location") != "https://api.shop.test/p?q=1" {
		t.Fatalf("got %d %q", rw.Code, rw.Header().Get("Location"))
	}

	s.httpsPort = "8443"
	rw = do(s.Handler(), "http://api.shop.test:8080/p?q=1", nil)
	if rw.Header().Get("Location") != "https://api.shop.test:8443/p?q=1" {
		t.Fatalf("Location = %q", rw.Header().Get("Location"))
	}
}

func TestSSEIsStreamedNotBuffered(t *testing.T) {
	release := make(chan struct{})
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: 1\n\n")
		w.(http.Flusher).Flush()
		<-release
		_, _ = io.WriteString(w, "data: 2\n\n")
	}))
	t.Cleanup(up.Close)

	lg := reqlog.New(8)
	lg.SetEnabled(true) // also exercises the Flush path of the logging writer
	front := httptest.NewServer(New(Options{Resolver: svcFor("api.shop.test", portOf(t, up.URL)), Log: lg}).Handler())
	t.Cleanup(front.Close)
	// Registered last so it runs first: the stream must be released before
	// front.Close() waits for in-flight requests, even if the test fails early.
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})

	req, _ := http.NewRequest("GET", front.URL+"/events", nil)
	req.Host = "api.shop.test"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	lines := make(chan string, 1)
	go func() {
		l, _ := bufio.NewReader(resp.Body).ReadString('\n')
		lines <- l
	}()
	select {
	case l := <-lines:
		if l != "data: 1\n" {
			t.Fatalf("first event = %q", l)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("first SSE event was not delivered while the stream is still open (response is being buffered)")
	}
	close(release)
}

func TestWebSocketTunnel(t *testing.T) {
	type hs struct{ uri, xff, xfp string }
	got := make(chan hs, 1)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			http.Error(w, "upgrade required", http.StatusBadRequest)
			return
		}
		got <- hs{r.RequestURI, r.Header.Get("X-Forwarded-For"), r.Header.Get("X-Forwarded-Proto")}
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
		_ = rw.Flush()
		buf := make([]byte, 4)
		if _, err := io.ReadFull(rw, buf); err == nil {
			_, _ = conn.Write(buf)
		}
	}))
	t.Cleanup(up.Close)

	lg := reqlog.New(8)
	lg.SetEnabled(true) // exercises the Hijack path of the logging writer
	front := httptest.NewServer(New(Options{Resolver: svcFor("api.shop.test", portOf(t, up.URL)), Log: lg}).Handler())
	t.Cleanup(front.Close)

	c, err := net.Dial("tcp", strings.TrimPrefix(front.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	fmt.Fprint(c, "GET /chat?room=1 HTTP/1.1\r\nHost: api.shop.test\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n"+
		"Sec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nX-Forwarded-For: 6.6.6.6\r\n\r\n")
	br := bufio.NewReader(c)
	resp, err := http.ReadResponse(br, &http.Request{Method: "GET"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status = %d, want 101", resp.StatusCode)
	}
	if _, err := c.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	echo := make([]byte, 4)
	if _, err := io.ReadFull(br, echo); err != nil || string(echo) != "ping" {
		t.Fatalf("echo = %q, err = %v", echo, err)
	}
	h := <-got
	if h.uri != "/chat?room=1" {
		t.Errorf("upstream saw %q", h.uri)
	}
	if h.xff == "6.6.6.6" || h.xff == "" || h.xfp != "http" {
		t.Errorf("forwarding headers on websocket: xff=%q proto=%q", h.xff, h.xfp)
	}
	c.Close()
	waitFor(t, 3*time.Second, func() bool {
		e := lg.Snapshot()
		return len(e) == 1 && e[0].Status == http.StatusSwitchingProtocols
	})
}

func TestRequestLogNeverRecordsSecrets(t *testing.T) {
	up, _ := recordingUpstream(t)
	lg := reqlog.New(8)
	h := New(Options{Resolver: svcFor("api.shop.test", portOf(t, up.URL)), Log: lg}).Handler()

	do(h, "http://api.shop.test/before", nil) // log is off: nothing recorded
	if len(lg.Snapshot()) != 0 {
		t.Fatal("request log must be off by default")
	}

	lg.SetEnabled(true)
	do(h, "http://api.shop.test/cb?token=SECRETVALUE&page=2", func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer TOPSECRET")
		r.Header.Set("Cookie", "sid=COOKIESECRET")
	})
	entries := lg.Snapshot()
	if len(entries) != 1 || entries[0].Status != 200 || entries[0].Method != "GET" {
		t.Fatalf("unexpected entries: %+v", entries)
	}
	b, _ := json.Marshal(entries)
	for _, secret := range []string{"SECRETVALUE", "TOPSECRET", "COOKIESECRET"} {
		if strings.Contains(string(b), secret) {
			t.Fatalf("%s leaked into the request log: %s", secret, b)
		}
	}
	if !strings.Contains(entries[0].Path, "page=2") {
		t.Errorf("harmless query values should stay readable: %q", entries[0].Path)
	}
}

func TestHTTPSUsesLocalCAAndRejectsUnregisteredNames(t *testing.T) {
	dir := t.TempDir()
	ca, err := pfca.LoadOrCreateCA(dir)
	if err != nil {
		t.Fatal(err)
	}
	store, err := pfca.NewCertStore(ca, dir)
	if err != nil {
		t.Fatal(err)
	}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "secure ok") }))
	t.Cleanup(up.Close)
	port := portOf(t, up.URL)
	res := fakeResolver{
		"api.shop.test":   {Hostname: "api.shop.test", TargetHost: "127.0.0.1", TargetPort: port, TLS: true},
		"plain.shop.test": {Hostname: "plain.shop.test", TargetHost: "127.0.0.1", TargetPort: port, TLS: false},
	}
	s := New(Options{Resolver: res, CertStore: store})
	front := httptest.NewUnstartedServer(s.Handler())
	front.TLS = s.tlsConfig()
	front.StartTLS()
	t.Cleanup(front.Close)

	caPEM, err := os.ReadFile(ca.CertPath())
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(caPEM)
	client := func(serverName string, insecure bool) *http.Client {
		return &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: pool, ServerName: serverName, InsecureSkipVerify: insecure},
		}}
	}

	req, _ := http.NewRequest("GET", front.URL+"/", nil)
	req.Host = "api.shop.test"
	resp, err := client("api.shop.test", false).Do(req)
	if err != nil {
		t.Fatalf("trusted HTTPS request failed: %v", err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(b) != "secure ok" {
		t.Fatalf("body = %q", b)
	}

	for _, name := range []string{"plain.shop.test", "unregistered.shop.test", "google.com"} {
		req, _ := http.NewRequest("GET", front.URL+"/", nil)
		req.Host = name
		if resp, err := client(name, true).Do(req); err == nil {
			resp.Body.Close()
			t.Errorf("TLS handshake for %q must be refused", name)
		}
	}
}

func TestListenFailsCleanlyWhenPortInUse(t *testing.T) {
	occupant, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupant.Close()
	_, err = listen("127.0.0.1:0,"+occupant.Addr().String(), testLogger())
	if err == nil {
		t.Fatal("expected an error for an address already in use")
	}
	if !strings.Contains(err.Error(), "hint") {
		t.Errorf("error should explain how to fix it: %v", err)
	}
}

func TestListenBindsLoopbackAndToleratesMissingIPv6(t *testing.T) {
	lns, err := listen("127.0.0.1:0, [::1]:0", testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if len(lns) < 1 {
		t.Fatal("expected at least the IPv4 loopback listener")
	}
	for _, ln := range lns {
		if ip := ln.Addr().(*net.TCPAddr).IP; !ip.IsLoopback() {
			t.Errorf("listener bound to non-loopback %v", ip)
		}
		ln.Close()
	}
	if lns, err := listen("", testLogger()); err != nil || lns != nil {
		t.Errorf("empty spec should be a no-op, got %v, %v", lns, err)
	}
}

func testLogger() *log.Logger { return log.New(io.Discard, "", 0) }

func TestRunReturnsBindErrorInsteadOfHanging(t *testing.T) {
	occupant, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupant.Close()
	s := New(Options{HTTPAddr: occupant.Addr().String(), Resolver: fakeResolver{}})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Run must report the bind failure")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run hung instead of failing fast")
	}
}
