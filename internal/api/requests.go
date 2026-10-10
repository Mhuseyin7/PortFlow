package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/portflow/portflow/internal/reqlog"
)

var errNoLog = errors.New("request log is not available")

// handleRequests serves GET (list) and DELETE (clear) for the request log.
func (s *Server) handleRequests(w http.ResponseWriter, r *http.Request) {
	if s.deps.Log == nil {
		writeErr(w, http.StatusNotFound, errNoLog)
		return
	}
	switch r.Method {
	case http.MethodGet:
		entries := s.deps.Log.Snapshot()
		if entries == nil {
			entries = []reqlog.Entry{}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"enabled": s.deps.Log.Enabled(),
			"entries": entries,
		})
	case http.MethodDelete:
		s.deps.Log.Clear()
		w.WriteHeader(http.StatusNoContent)
	default:
		writeErr(w, http.StatusMethodNotAllowed, errMethod)
	}
}

// handleRequestsState serves POST /api/requests/enable and /disable.
func (s *Server) handleRequestsState(w http.ResponseWriter, r *http.Request) {
	if s.deps.Log == nil {
		writeErr(w, http.StatusNotFound, errNoLog)
		return
	}
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, errMethod)
		return
	}
	switch strings.TrimPrefix(r.URL.Path, "/api/requests/") {
	case "enable":
		s.deps.Log.SetEnabled(true)
	case "disable":
		s.deps.Log.SetEnabled(false)
	default:
		writeErr(w, http.StatusNotFound, errors.New("unknown action"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": s.deps.Log.Enabled()})
}
