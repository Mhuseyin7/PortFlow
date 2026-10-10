package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/portflow/portflow/internal/hostname"
	"github.com/portflow/portflow/internal/registry"
)

const maxBodyBytes = 64 << 10

type createRequest struct {
	Hostname   string `json:"hostname"`
	TargetHost string `json:"target_host"`
	TargetPort int    `json:"target_port"`
	TLS        *bool  `json:"tls"` // defaults to true
	Project    string `json:"project"`
}

type createResponse struct {
	registry.Service
	Warnings []string `json:"warnings,omitempty"`
}

func (s *Server) suffixes() []string {
	if len(s.deps.Suffixes) == 0 {
		return hostname.DefaultSuffixes
	}
	return s.deps.Suffixes
}

func suffixList(ss []string) string {
	out := make([]string, len(ss))
	for i, x := range ss {
		out[i] = "." + strings.TrimPrefix(x, ".")
	}
	return strings.Join(out, ", ")
}

// validate normalizes and checks a registration request.
func (s *Server) validate(req *createRequest) error {
	req.Hostname = hostname.Normalize(req.Hostname)
	if err := hostname.Validate(req.Hostname); err != nil {
		return err
	}
	if !hostname.AllowedSuffix(req.Hostname, s.suffixes()) {
		return fmt.Errorf("hostname must end in one of %s (reserved for local development, so it can never shadow a real website)", suffixList(s.suffixes()))
	}
	if err := hostname.ValidateProject(req.Project); err != nil {
		return err
	}
	req.TargetHost = strings.ToLower(strings.TrimSpace(req.TargetHost))
	if req.TargetHost == "" {
		req.TargetHost = "127.0.0.1"
	}
	if err := hostname.ValidateTarget(req.TargetHost); err != nil {
		return err
	}
	if !s.deps.AllowExternalTargets && !hostname.IsLoopbackHost(req.TargetHost) {
		return errors.New("target_host must be a loopback address (127.0.0.1, ::1 or localhost); start the daemon with --allow-external-targets to proxy elsewhere")
	}
	if req.TargetPort < 1 || req.TargetPort > 65535 {
		return errors.New("target_port must be between 1 and 65535")
	}
	return nil
}

func (s *Server) handleServices(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		list, err := s.deps.Store.List()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, list)
	case http.MethodPost:
		s.createService(w, r)
	default:
		writeErr(w, http.StatusMethodNotAllowed, errMethod)
	}
}

func (s *Server) createService(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var req createRequest
	if err := dec.Decode(&req); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeErr(w, http.StatusRequestEntityTooLarge, errors.New("request body too large"))
			return
		}
		writeErr(w, http.StatusBadRequest, fmt.Errorf("invalid JSON body: %w", err))
		return
	}
	if err := s.validate(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	useTLS := true
	if req.TLS != nil {
		useTLS = *req.TLS
	}
	svc, err := s.deps.Store.Create(registry.Service{
		Project:    req.Project,
		Hostname:   req.Hostname,
		TargetHost: req.TargetHost,
		TargetPort: req.TargetPort,
		TLS:        useTLS,
		Protocol:   "http",
		Status:     registry.StatusStarting,
	})
	if err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, registry.ErrDuplicate) {
			code = http.StatusConflict
		}
		writeErr(w, code, err)
		return
	}

	resp := createResponse{Service: *svc}
	if s.deps.Hosts != nil {
		if err := s.deps.Hosts.Add(svc.Hostname); err != nil {
			resp.Warnings = append(resp.Warnings, fmt.Sprintf(
				"could not update the hosts file (%v): %s will not resolve until the daemon runs with permission to edit it (administrator/root)",
				err, svc.Hostname))
		}
	}
	if svc.TLS && s.deps.CertStore != nil {
		if _, err := s.deps.CertStore.For(svc.Hostname); err != nil {
			resp.Warnings = append(resp.Warnings, "could not issue a certificate: "+err.Error())
		}
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (s *Server) handleService(w http.ResponseWriter, r *http.Request) {
	host := hostname.Normalize(strings.TrimPrefix(r.URL.Path, "/api/services/"))
	if host == "" {
		writeErr(w, http.StatusBadRequest, errors.New("hostname required"))
		return
	}
	switch r.Method {
	case http.MethodGet:
		svc, err := s.deps.Store.GetByHostname(host)
		if err != nil {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		writeJSON(w, http.StatusOK, svc)
	case http.MethodDelete:
		if err := s.deps.Store.Delete(host); err != nil {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		if s.deps.Hosts != nil {
			_ = s.deps.Hosts.Remove(host)
		}
		if s.deps.CertStore != nil {
			_ = s.deps.CertStore.Purge(host)
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeErr(w, http.StatusMethodNotAllowed, errMethod)
	}
}
