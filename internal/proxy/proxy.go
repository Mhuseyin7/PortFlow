// Package proxy is PortFlow's local reverse proxy: hostname-routed HTTP/1.1
// and HTTP/2 (over TLS), with WebSocket and SSE pass-through.
//
// Design constraints:
//   - Bind only to loopback by default. LAN mode is off unless the operator
//     explicitly opts in (enforced by the daemon).
//   - Targets are resolved from the registry (source of truth), never from a
//     caller-supplied Host or path. This blocks SSRF-style abuse.
//   - The target host must be loopback unless AllowExternalTargets is on.
//   - By default the Host header sent upstream is the target address, so dev
//     servers that validate Host (Vite, webpack-dev-server, Next.js) accept
//     the request. PreserveHost forwards the original Host instead.
//   - The request path never touches the database: lookups are served from
//     the registry's in-memory index.
package proxy

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/portflow/portflow/internal/hostname"
	"github.com/portflow/portflow/internal/registry"
	"github.com/portflow/portflow/internal/reqlog"
	pfca "github.com/portflow/portflow/internal/tls"
)

// Resolver looks up a service by hostname.
type Resolver interface {
	GetByHostname(host string) (*registry.Service, error)
}

// Options configures the proxy.
type Options struct {
	// HTTPAddr and HTTPSAddr are comma-separated listen addresses, e.g.
	// "127.0.0.1:80,[::1]:80". Empty disables that listener.
	HTTPAddr  string
	HTTPSAddr string

	Resolver  Resolver
	CertStore *pfca.CertStore

	AllowExternalTargets bool // OFF by default; only enable for advanced users
	PreserveHost         bool // forward the original Host header upstream
	Log                  *reqlog.Log
	Logger               *log.Logger
}

type routeKey struct{}

// route is what serve resolved for a request; the shared ReverseProxy reads it
// back from the request context.
type route struct {
	host   string
	target *url.URL
}

// Server owns the listeners and the shared reverse proxy.
type Server struct {
	opts  Options
	http  *http.Server
	https *http.Server
	rp    *httputil.ReverseProxy

	httpsPort string // port of the first bound HTTPS listener; "" when none
}

// New builds a Server; call Run to start listening.
func New(opts Options) *Server {
	if opts.Logger == nil {
		opts.Logger = log.Default()
	}
	s := &Server{opts: opts}
	s.rp = &httputil.ReverseProxy{
		Rewrite:       s.rewrite,
		FlushInterval: -1, // stream SSE and long-polling immediately
		Transport:     newTransport(),
		ErrorLog:      opts.Logger,
		ErrorHandler:  s.upstreamError,
	}
	handler := s.Handler()
	s.http = &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       2 * time.Minute,
		ErrorLog:          opts.Logger,
	}
	s.https = &http.Server{
		Handler:           handler,
		TLSConfig:         s.tlsConfig(),
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       2 * time.Minute,
		ErrorLog:          opts.Logger,
	}
	return s
}

// Handler returns the request handler shared by the HTTP and HTTPS listeners.
func (s *Server) Handler() http.Handler { return http.HandlerFunc(s.serve) }

// newTransport builds the upstream transport. It deliberately ignores
// HTTP(S)_PROXY from the environment: dev traffic must go straight to the
// local target.
func newTransport() *http.Transport {
	return &http.Transport{
		Proxy:               nil,
		DialContext:         (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 16,
		IdleConnTimeout:     90 * time.Second,
		DisableCompression:  true, // forward bodies exactly as the dev server sent them
	}
}

func (s *Server) tlsConfig() *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		NextProtos: []string{"h2", "http/1.1"},
		GetCertificate: func(chi *tls.ClientHelloInfo) (*tls.Certificate, error) {
			name := hostname.Normalize(chi.ServerName)
			if name == "" {
				return nil, errors.New("SNI required: connect using the registered hostname")
			}
			svc, err := s.opts.Resolver.GetByHostname(name)
			if err != nil || !svc.TLS {
				return nil, fmt.Errorf("no certificate for %q", name)
			}
			return s.opts.CertStore.For(name)
		},
	}
}

// Run binds all listeners and serves until ctx is cancelled or a listener
// fails. Bind errors are returned immediately.
func (s *Server) Run(ctx context.Context) error {
	httpLns, err := listen(s.opts.HTTPAddr, s.opts.Logger)
	if err != nil {
		return err
	}
	httpsLns, err := listen(s.opts.HTTPSAddr, s.opts.Logger)
	if err != nil {
		for _, ln := range httpLns {
			_ = ln.Close()
		}
		return err
	}
	if len(httpLns)+len(httpsLns) == 0 {
		return errors.New("proxy: no listen address configured")
	}
	if len(httpsLns) > 0 {
		if _, port, err := net.SplitHostPort(httpsLns[0].Addr().String()); err == nil {
			s.httpsPort = port
		}
	}

	errCh := make(chan error, len(httpLns)+len(httpsLns))
	for _, ln := range httpLns {
		s.opts.Logger.Printf("proxy: http  listening on %s", ln.Addr())
		go func(ln net.Listener) { errCh <- s.http.Serve(ln) }(ln)
	}
	for _, ln := range httpsLns {
		s.opts.Logger.Printf("proxy: https listening on %s", ln.Addr())
		go func(ln net.Listener) { errCh <- s.https.ServeTLS(ln, "", "") }(ln)
	}

	var runErr error
	select {
	case <-ctx.Done():
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			runErr = fmt.Errorf("proxy: %w", err)
		}
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.http.Shutdown(shutdownCtx)
	_ = s.https.Shutdown(shutdownCtx)
	return runErr
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	if s.opts.Log.Enabled() {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		w = sw
		defer func() {
			status := sw.status
			if status == 0 {
				status = http.StatusOK
			}
			s.opts.Log.Add(reqlog.Entry{
				Time:       start,
				Host:       cleanHost(r.Host),
				Method:     r.Method,
				Path:       reqlog.RedactURL(r.URL.Path, r.URL.RawQuery),
				Status:     status,
				DurationMS: float64(time.Since(start).Microseconds()) / 1000,
			})
		}()
	}

	host := cleanHost(r.Host)
	svc, err := s.opts.Resolver.GetByHostname(host)
	if err != nil {
		writeDiag(w, http.StatusNotFound, "unknown host", host,
			"No PortFlow service is registered for this hostname.",
			"Try: portflow add "+host+" <port>")
		return
	}
	target, err := resolveTarget(svc, s.opts.AllowExternalTargets)
	if err != nil {
		writeDiag(w, http.StatusBadGateway, "misconfigured target", host, err.Error(),
			"Only loopback targets are allowed by default. Fix it with: portflow remove "+host)
		return
	}
	if svc.TLS && r.TLS == nil && s.httpsPort != "" && !isWebSocket(r) {
		to := "https://" + host
		if s.httpsPort != "443" {
			to += ":" + s.httpsPort
		}
		http.Redirect(w, r, to+r.URL.RequestURI(), http.StatusTemporaryRedirect)
		return
	}
	if isWebSocket(r) {
		s.proxyWebSocket(w, r, target, host)
		return
	}
	ctx := context.WithValue(r.Context(), routeKey{}, &route{host: host, target: target})
	s.rp.ServeHTTP(w, r.WithContext(ctx))
}

func (s *Server) rewrite(pr *httputil.ProxyRequest) {
	rt, ok := pr.In.Context().Value(routeKey{}).(*route)
	if !ok {
		return
	}
	pr.SetURL(rt.target) // also rewrites Host to the target
	if s.opts.PreserveHost {
		pr.Out.Host = pr.In.Host
	}
	// ReverseProxy has already dropped client-supplied X-Forwarded-* and
	// Forwarded headers, so this sets them from what we observed.
	pr.SetXForwarded()
}

func (s *Server) upstreamError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, context.Canceled) {
		w.WriteHeader(499) // the client went away; there is nobody to show a page to
		return
	}
	var host, addr string
	if rt, ok := r.Context().Value(routeKey{}).(*route); ok {
		host, addr = rt.host, rt.target.Host
	}
	hint := "Is your dev server running on that port?"
	if msg := strings.ToLower(err.Error()); strings.Contains(msg, "refused") {
		_, port, _ := net.SplitHostPort(addr)
		hint = fmt.Sprintf("Nothing is listening on port %s. Start your dev server, or point the domain at the right port: portflow remove %s && portflow add %s <port>", port, host, host)
	}
	writeDiag(w, http.StatusBadGateway, "upstream unreachable", host,
		fmt.Sprintf("Could not reach %s: %v", addr, err), hint)
}

func resolveTarget(s *registry.Service, allowExternal bool) (*url.URL, error) {
	th := strings.ToLower(strings.TrimSpace(s.TargetHost))
	if th == "" {
		th = "127.0.0.1"
	}
	if !allowExternal && !hostname.IsLoopbackHost(th) {
		return nil, fmt.Errorf("target %q is not loopback; external targets are disabled", th)
	}
	if s.TargetPort < 1 || s.TargetPort > 65535 {
		return nil, fmt.Errorf("target port %d is out of range", s.TargetPort)
	}
	return &url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(strings.Trim(th, "[]"), strconv.Itoa(s.TargetPort)),
	}, nil
}

// cleanHost extracts the lower-case hostname from a Host header value.
func cleanHost(hostport string) string {
	h := hostport
	if hh, _, err := net.SplitHostPort(hostport); err == nil {
		h = hh
	}
	return hostname.Normalize(h)
}

func isWebSocket(r *http.Request) bool {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return false
	}
	for _, c := range r.Header.Values("Connection") {
		for _, tok := range strings.Split(c, ",") {
			if strings.EqualFold(strings.TrimSpace(tok), "upgrade") {
				return true
			}
		}
	}
	return false
}

// writeDiag renders the diagnostic page. Every value is HTML-escaped: host
// comes straight from the request's Host header and detail can contain
// upstream error text, so none of it may reach the page as markup.
func writeDiag(w http.ResponseWriter, code int, title, host, detail, hint string) {
	title, host, detail, hint = html.EscapeString(title), html.EscapeString(host), html.EscapeString(detail), html.EscapeString(hint)
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("X-PortFlow", "diagnostic")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_, _ = io.WriteString(w, `<!doctype html>
<html><head><meta charset="utf-8"><title>PortFlow — `+title+`</title>
<style>
body{font:14px/1.5 -apple-system,Segoe UI,system-ui,sans-serif;max-width:640px;margin:8vh auto;padding:0 20px;color:#1a1a1a}
h1{font-size:20px;margin:0 0 6px}
.host{color:#5b21b6;font-weight:600}
.detail{background:#f5f5f7;border-radius:8px;padding:12px 14px;margin:12px 0;white-space:pre-wrap}
.hint{color:#374151;border-left:3px solid #10b981;padding:8px 12px;background:#ecfdf5;border-radius:4px}
code{background:#eef2ff;padding:1px 6px;border-radius:4px}
</style></head><body>
<h1>PortFlow · `+title+`</h1>
<div>Host: <span class="host">`+host+`</span></div>
<div class="detail">`+detail+`</div>
<div class="hint">`+hint+`</div>
</body></html>`)
}
