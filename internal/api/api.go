// Package api is the daemon's management HTTP API — the surface both the
// CLI and the dashboard talk to.
//
// It controls things that matter (hosts file entries, certificates, what the
// proxy forwards to), so every request passes these gates:
//
//  1. the TCP peer must be a loopback address;
//  2. the Host header must be a loopback name — this defeats DNS rebinding,
//     where a hostile web page re-points its own domain at 127.0.0.1;
//  3. state-changing requests must carry the X-PortFlow header (browsers
//     cannot attach it cross-origin without a CORS preflight, which this API
//     never approves) and, if an Origin is present, it must be this server.
package api

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/portflow/portflow/internal/hosts"
	"github.com/portflow/portflow/internal/registry"
	"github.com/portflow/portflow/internal/reqlog"
	pfca "github.com/portflow/portflow/internal/tls"
	"github.com/portflow/portflow/internal/ui"
)

// CSRFHeader must be present on every state-changing request.
const CSRFHeader = "X-PortFlow"

const trustTTL = 30 * time.Second

// Deps are what the API handlers need from the daemon.
type Deps struct {
	Store     *registry.Store
	Hosts     *hosts.Manager // nil disables hosts-file management
	CA        *pfca.CA
	CertStore *pfca.CertStore
	Log       *reqlog.Log // nil disables the request-log endpoints

	// Trusted reports whether the CA is in the OS trust store. It may spawn a
	// process, so the API caches the answer for trustTTL.
	Trusted func() bool

	Suffixes             []string // allowed hostname suffixes; empty = defaults
	AllowExternalTargets bool

	Version      string
	Commit       string
	StartedAt    time.Time
	ProxyHTTP    string
	ProxyHTTPS   string
	APIAddr      string
	ShutdownFunc func()
}

// Server wraps the http.ServeMux.
type Server struct {
	deps Deps
	mux  *http.ServeMux
	shut int32

	trustMu sync.Mutex
	trustAt time.Time
	trustOK bool
}

// New returns a Server ready to serve on APIAddr.
func New(deps Deps) *Server {
	s := &Server{deps: deps, mux: http.NewServeMux()}
	s.routes()
	return s
}

// Handler returns the API wrapped in its security gates.
func (s *Server) Handler() http.Handler { return s.secure(s.mux) }

func (s *Server) routes() {
	s.mux.HandleFunc("/health", s.handleHealth)
	s.mux.HandleFunc("/api/status", s.handleStatus)
	s.mux.HandleFunc("/api/services", s.handleServices)
	s.mux.HandleFunc("/api/services/", s.handleService)
	s.mux.HandleFunc("/api/requests", s.handleRequests)
	s.mux.HandleFunc("/api/requests/", s.handleRequestsState)
	s.mux.HandleFunc("/api/shutdown", s.handleShutdown)
	s.mux.Handle("/", ui.Handler())
}

func (s *Server) secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !loopbackPeer(r.RemoteAddr) {
			http.Error(w, "forbidden: loopback only", http.StatusForbidden)
			return
		}
		if !allowedHost(r.Host) {
			http.Error(w, "forbidden: unexpected Host header", http.StatusForbidden)
			return
		}
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		h.Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		if mutating(r.Method) {
			if r.Header.Get(CSRFHeader) == "" {
				http.Error(w, "forbidden: missing "+CSRFHeader+" header", http.StatusForbidden)
				return
			}
			if o := r.Header.Get("Origin"); o != "" && !sameOrigin(o, r.Host) {
				http.Error(w, "forbidden: cross-origin request", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func mutating(method string) bool {
	return method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions
}

func loopbackPeer(remote string) bool {
	h, _, err := net.SplitHostPort(remote)
	if err != nil {
		h = remote
	}
	ip := net.ParseIP(strings.Trim(h, "[]"))
	return ip != nil && ip.IsLoopback()
}

func allowedHost(hostport string) bool {
	h := hostport
	if hh, _, err := net.SplitHostPort(hostport); err == nil {
		h = hh
	}
	h = strings.ToLower(strings.Trim(h, "[]"))
	return h == "localhost" || h == "127.0.0.1" || h == "::1"
}

func sameOrigin(origin, host string) bool {
	u, err := url.Parse(origin)
	return err == nil && u.Scheme == "http" && strings.EqualFold(u.Host, host)
}

func (s *Server) caTrusted() bool {
	if s.deps.Trusted == nil {
		return false
	}
	s.trustMu.Lock()
	defer s.trustMu.Unlock()
	if !s.trustAt.IsZero() && time.Since(s.trustAt) < trustTTL {
		return s.trustOK
	}
	s.trustOK = s.deps.Trusted()
	s.trustAt = time.Now()
	return s.trustOK
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	list, _ := s.deps.Store.List()
	writeJSON(w, http.StatusOK, map[string]any{
		"version":                s.deps.Version,
		"commit":                 s.deps.Commit,
		"uptime":                 time.Since(s.deps.StartedAt).Round(time.Second).String(),
		"services":               len(list),
		"proxy_http":             s.deps.ProxyHTTP,
		"proxy_https":            s.deps.ProxyHTTPS,
		"api_addr":               s.deps.APIAddr,
		"ca_ready":               s.deps.CA != nil,
		"ca_trusted":             s.caTrusted(),
		"request_log":            s.deps.Log.Enabled(),
		"allow_external_targets": s.deps.AllowExternalTargets,
	})
}

func (s *Server) handleShutdown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.WriteHeader(http.StatusOK)
	if !atomic.CompareAndSwapInt32(&s.shut, 0, 1) {
		return
	}
	go func() {
		time.Sleep(200 * time.Millisecond)
		if s.deps.ShutdownFunc != nil {
			s.deps.ShutdownFunc()
		}
	}()
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

var errMethod = errors.New("method not allowed")
