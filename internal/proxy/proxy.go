// Package proxy is PortFlow's local reverse proxy: hostname-routed HTTP/1.1
// and HTTP/2 (over TLS), with WebSocket and SSE pass-through.
//
// Design constraints:
//   - Bind only to loopback by default. LAN mode is off unless the operator
//     explicitly opts in.
//   - Target URLs are resolved from the registry (source of truth), never
//     from a caller-supplied Host or path. This blocks SSRF-style abuse.
//   - Target host defaults to 127.0.0.1. Anything else is rejected unless
//     AllowExternalTargets is on.
//   - The Host header sent upstream matches the target hostname so that
//     dev servers like Next.js don't reject requests with a "wrong" Host.
package proxy

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/portflow/portflow/internal/registry"
	pfca "github.com/portflow/portflow/internal/tls"
)

// Resolver looks up a service by hostname.
type Resolver interface {
	GetByHostname(host string) (*registry.Service, error)
}

// Options configures the proxy.
type Options struct {
	HTTPAddr             string // e.g. "127.0.0.1:80"
	HTTPSAddr            string // e.g. "127.0.0.1:443"
	Resolver             Resolver
	CertStore            *pfca.CertStore
	AllowExternalTargets bool // OFF by default; only enable for advanced users
	Logger               *log.Logger
}

// Server owns the two listener goroutines.
type Server struct {
	opts  Options
	http  *http.Server
	https *http.Server
}

// New builds a Server; call Run to start listening.
func New(opts Options) *Server {
	if opts.Logger == nil {
		opts.Logger = log.Default()
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serve(opts, w, r)
	})
	tlsCfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
		NextProtos: []string{"h2", "http/1.1"},
		GetCertificate: func(chi *tls.ClientHelloInfo) (*tls.Certificate, error) {
			name := strings.ToLower(chi.ServerName)
			if name == "" {
				return nil, errors.New("SNI required")
			}
			svc, err := opts.Resolver.GetByHostname(name)
			if err != nil || !svc.TLS {
				return nil, fmt.Errorf("no cert for %q", name)
			}
			return opts.CertStore.For(name)
		},
	}
	return &Server{
		opts: opts,
		http: &http.Server{
			Addr:              opts.HTTPAddr,
			Handler:           handler,
			ReadHeaderTimeout: 15 * time.Second,
		},
		https: &http.Server{
			Addr:              opts.HTTPSAddr,
			Handler:           handler,
			TLSConfig:         tlsCfg,
			ReadHeaderTimeout: 15 * time.Second,
		},
	}
}

// Run listens until ctx is cancelled; returns the first error encountered.
func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 2)
	if s.opts.HTTPAddr != "" {
		go func() {
			s.opts.Logger.Printf("proxy: listening http on %s", s.opts.HTTPAddr)
			err := s.http.ListenAndServe()
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				errCh <- fmt.Errorf("http: %w", err)
				return
			}
			errCh <- nil
		}()
	}
	if s.opts.HTTPSAddr != "" {
		go func() {
			s.opts.Logger.Printf("proxy: listening https on %s", s.opts.HTTPSAddr)
			err := s.https.ListenAndServeTLS("", "")
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				errCh <- fmt.Errorf("https: %w", err)
				return
			}
			errCh <- nil
		}()
	}
	select {
	case <-ctx.Done():
	case err := <-errCh:
		return err
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.http.Shutdown(shutdownCtx)
	_ = s.https.Shutdown(shutdownCtx)
	return nil
}

func serve(opts Options, w http.ResponseWriter, r *http.Request) {
	host := strings.ToLower(hostOnly(r.Host))
	svc, err := opts.Resolver.GetByHostname(host)
	if err != nil {
		writeDiag(w, http.StatusNotFound, "unknown host", host,
			"No PortFlow service is registered for this hostname.",
			"Try: portflow add "+host+" <port>")
		return
	}
	target, err := resolveTarget(svc, opts.AllowExternalTargets)
	if err != nil {
		writeDiag(w, http.StatusBadGateway, "misconfigured target", host, err.Error(),
			"Check target_host in the service; only loopback is allowed by default.")
		return
	}
	if isWebSocket(r) {
		proxyWebSocket(w, r, target, svc, opts.Logger)
		return
	}
	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.Host = target.Host
			pr.SetXForwarded()
			scheme := "http"
			if r.TLS != nil {
				scheme = "https"
			}
			pr.Out.Header.Set("X-Forwarded-Proto", scheme)
			pr.Out.Header.Set("X-Forwarded-Host", host)
		},
		FlushInterval: -1, // stream SSE / long-poll immediately
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, e error) {
			writeDiag(w, http.StatusBadGateway, "upstream unreachable", host,
				fmt.Sprintf("Could not reach %s: %v", target, e),
				"Is your dev server running on that port?")
		},
	}
	rp.ServeHTTP(w, r)
}

func resolveTarget(s *registry.Service, allowExternal bool) (*url.URL, error) {
	th := strings.ToLower(strings.TrimSpace(s.TargetHost))
	if th == "" {
		th = "127.0.0.1"
	}
	if !allowExternal && !isLoopbackTarget(th) {
		return nil, fmt.Errorf("target %q is not loopback; enable external targets explicitly", th)
	}
	scheme := "http"
	u := &url.URL{Scheme: scheme, Host: net.JoinHostPort(th, itoa(s.TargetPort))}
	return u, nil
}

func isLoopbackTarget(h string) bool {
	switch h {
	case "127.0.0.1", "::1", "localhost", "0.0.0.0":
		return true
	}
	if ip := net.ParseIP(h); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

func hostOnly(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return h
	}
	return hostport
}

func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}

func isWebSocket(r *http.Request) bool {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return false
	}
	for _, tok := range strings.Split(r.Header.Get("Connection"), ",") {
		if strings.EqualFold(strings.TrimSpace(tok), "upgrade") {
			return true
		}
	}
	return false
}

func writeDiag(w http.ResponseWriter, code int, title, host, detail, hint string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-PortFlow", "diagnostic")
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
