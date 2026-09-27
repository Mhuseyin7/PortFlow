// Package api is the daemon's management HTTP API — the surface both the
// CLI and the dashboard talk to. It always listens on loopback and never
// exposes destructive endpoints without an explicit request from the CLI.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/portflow/portflow/internal/hosts"
	"github.com/portflow/portflow/internal/registry"
	pfca "github.com/portflow/portflow/internal/tls"
	"github.com/portflow/portflow/internal/trust"
	"github.com/portflow/portflow/internal/ui"
)

// Deps are what the API handlers need from the daemon.
type Deps struct {
	Store         *registry.Store
	Hosts         *hosts.Manager
	CA            *pfca.CA
	CertStore     *pfca.CertStore
	Version       string
	Commit        string
	StartedAt     time.Time
	ProxyHTTP     string
	ProxyHTTPS    string
	APIAddr       string
	ShutdownFunc  func()
	ServiceCount  func() int
}

// Server wraps the http.ServeMux.
type Server struct {
	deps Deps
	mux  *http.ServeMux
	shut int32
}

// New returns a Server ready to serve on APIAddr.
func New(deps Deps) *Server {
	s := &Server{deps: deps, mux: http.NewServeMux()}
	s.routes()
	return s
}

// Handler exposes the mux for embedding.
func (s *Server) Handler() http.Handler {
	return s.loopbackOnly(s.mux)
}

func (s *Server) routes() {
	s.mux.HandleFunc("/health", s.handleHealth)
	s.mux.HandleFunc("/api/status", s.handleStatus)
	s.mux.HandleFunc("/api/services", s.handleServices)
	s.mux.HandleFunc("/api/services/", s.handleService)
	s.mux.HandleFunc("/api/shutdown", s.handleShutdown)
	s.mux.Handle("/", ui.Handler())
}

// loopbackOnly enforces that requests came from loopback. This is belt-and-
// suspenders — the listener is already bound to 127.0.0.1 — but protects
// against future misconfiguration.
func (s *Server) loopbackOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.RemoteAddr
		if i := strings.LastIndex(host, ":"); i > 0 {
			host = host[:i]
		}
		host = strings.Trim(host, "[]")
		if host != "127.0.0.1" && host != "::1" && host != "localhost" {
			http.Error(w, "forbidden: loopback only", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	installed, _ := trust.IsInstalled(s.deps.CA.CertPath())
	writeJSON(w, 200, map[string]any{
		"version":     s.deps.Version,
		"commit":      s.deps.Commit,
		"uptime":      time.Since(s.deps.StartedAt).Round(time.Second).String(),
		"services":    s.deps.ServiceCount(),
		"proxy_http":  s.deps.ProxyHTTP,
		"proxy_https": s.deps.ProxyHTTPS,
		"api_addr":    s.deps.APIAddr,
		"ca_ready":    s.deps.CA != nil,
		"ca_trusted":  installed,
	})
}

func (s *Server) handleServices(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		list, err := s.deps.Store.List()
		if err != nil {
			writeErr(w, 500, err)
			return
		}
		writeJSON(w, 200, list)
	case http.MethodPost:
		var body struct {
			Hostname   string `json:"hostname"`
			TargetHost string `json:"target_host"`
			TargetPort int    `json:"target_port"`
			TLS        bool   `json:"tls"`
			Project    string `json:"project"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, 400, err)
			return
		}
		if err := validateHostname(body.Hostname); err != nil {
			writeErr(w, 400, err)
			return
		}
		if body.TargetHost == "" {
			body.TargetHost = "127.0.0.1"
		}
		if body.TargetPort < 1 || body.TargetPort > 65535 {
			writeErr(w, 400, errors.New("invalid target_port"))
			return
		}
		svc, err := s.deps.Store.Create(registry.Service{
			Project:    body.Project,
			Hostname:   body.Hostname,
			TargetHost: body.TargetHost,
			TargetPort: body.TargetPort,
			TLS:        body.TLS,
			Protocol:   "http",
			Status:     registry.StatusStarting,
		})
		if err != nil {
			code := 500
			if errors.Is(err, registry.ErrDuplicate) {
				code = 409
			}
			writeErr(w, code, err)
			return
		}
		if s.deps.Hosts != nil {
			_ = s.deps.Hosts.Add(svc.Hostname)
		}
		writeJSON(w, 201, svc)
	default:
		http.Error(w, "method not allowed", 405)
	}
}

func (s *Server) handleService(w http.ResponseWriter, r *http.Request) {
	host := strings.TrimPrefix(r.URL.Path, "/api/services/")
	if host == "" {
		http.Error(w, "hostname required", 400)
		return
	}
	switch r.Method {
	case http.MethodGet:
		svc, err := s.deps.Store.GetByHostname(host)
		if err != nil {
			writeErr(w, 404, err)
			return
		}
		writeJSON(w, 200, svc)
	case http.MethodDelete:
		if err := s.deps.Store.Delete(host); err != nil {
			writeErr(w, 404, err)
			return
		}
		if s.deps.Hosts != nil {
			_ = s.deps.Hosts.Remove(host)
		}
		if s.deps.CertStore != nil {
			_ = s.deps.CertStore.Purge(host)
		}
		w.WriteHeader(204)
	default:
		http.Error(w, "method not allowed", 405)
	}
}

func (s *Server) handleShutdown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	if !atomic.CompareAndSwapInt32(&s.shut, 0, 1) {
		w.WriteHeader(200)
		return
	}
	w.WriteHeader(200)
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
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

// validateHostname keeps outside garbage out of the registry.
func validateHostname(h string) error {
	h = strings.TrimSpace(strings.ToLower(h))
	if h == "" {
		return errors.New("hostname required")
	}
	if len(h) > 253 {
		return errors.New("hostname too long")
	}
	for _, r := range h {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '.' || r == '-':
		default:
			return fmt.Errorf("invalid character %q in hostname", r)
		}
	}
	if strings.HasPrefix(h, ".") || strings.HasSuffix(h, ".") || strings.Contains(h, "..") {
		return errors.New("malformed hostname")
	}
	return nil
}

