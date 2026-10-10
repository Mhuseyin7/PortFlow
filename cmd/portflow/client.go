package portflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/portflow/portflow/internal/api"
	"github.com/portflow/portflow/internal/registry"
)

// Service is the registry's service record as the daemon returns it.
type Service = registry.Service

// client is the CLI's thin wrapper around the daemon's HTTP management API.
type client struct {
	base string
	hc   *http.Client
}

func newClient() *client {
	return &client{
		base: strings.TrimRight(daemonURL, "/"),
		hc:   &http.Client{Timeout: 5 * time.Second},
	}
}

// apiError is an error response from the daemon.
type apiError struct {
	Status  int
	Message string
}

func (e *apiError) Error() string { return e.Message }

func (c *client) get(path string, out any) error  { return c.do("GET", path, nil, out) }
func (c *client) post(path string, in, out any) error { return c.do("POST", path, in, out) }
func (c *client) del(path string, out any) error  { return c.do("DELETE", path, nil, out) }

func (c *client) do(method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.base+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(api.CSRFHeader, "1") // required by the daemon for state-changing calls
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("daemon unreachable at %s — is `portflow daemon start` running? (%w)", c.base, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return &apiError{Status: resp.StatusCode, Message: errorMessage(b, resp.Status)}
	}
	if out != nil && len(b) > 0 {
		return json.Unmarshal(b, out)
	}
	return nil
}

// errorMessage extracts {"error": "..."} or falls back to the raw text.
func errorMessage(body []byte, status string) string {
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil && e.Error != "" {
		return e.Error
	}
	if s := strings.TrimSpace(string(body)); s != "" {
		return s
	}
	return status
}

// alive returns true if the daemon answers /health.
func (c *client) alive() bool {
	req, _ := http.NewRequest("GET", c.base+"/health", nil)
	resp, err := (&http.Client{Timeout: 300 * time.Millisecond}).Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200
}

// ensureDaemon returns a helpful error if the daemon isn't running.
func ensureDaemon(c *client) error {
	if c.alive() {
		return nil
	}
	return fmt.Errorf("daemon not running — start it with:\n  portflow daemon start")
}

// portListening reports whether something accepts TCP connections on host:port.
// It only connects; it never binds the port, so it cannot disturb a dev
// server that is just starting.
func portListening(host string, port int) bool {
	if host == "" {
		host = "127.0.0.1"
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(port)), 300*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// urls builds browser addresses for services, adding the proxy's port when it
// is not the scheme's default (for example when the daemon runs on 8443).
type urls struct{ httpPort, httpsPort string }

// urls asks the daemon which ports its proxy listens on.
func (c *client) urls() urls {
	u := urls{httpPort: "80", httpsPort: "443"}
	var st statusReport
	if err := c.get("/api/status", &st); err == nil {
		if p := firstPort(st.ProxyHTTP); p != "" {
			u.httpPort = p
		}
		if p := firstPort(st.ProxyHTTPS); p != "" {
			u.httpsPort = p
		}
	}
	return u
}

func (u urls) of(svc Service) string {
	scheme, port, def := "http", u.httpPort, "80"
	if svc.TLS {
		scheme, port, def = "https", u.httpsPort, "443"
	}
	if port == "" || port == def {
		return scheme + "://" + svc.Hostname
	}
	return scheme + "://" + svc.Hostname + ":" + port
}

// firstPort returns the port of the first address in a comma-separated list.
func firstPort(spec string) string {
	first := strings.TrimSpace(strings.Split(spec, ",")[0])
	_, p, err := net.SplitHostPort(first)
	if err != nil {
		return ""
	}
	return p
}
