package portflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/portflow/portflow/internal/registry"
)

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

func (c *client) get(path string, out any) error {
	return c.do("GET", path, nil, out)
}

func (c *client) post(path string, in, out any) error {
	return c.do("POST", path, in, out)
}

func (c *client) del(path string, out any) error {
	return c.do("DELETE", path, nil, out)
}

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
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("daemon unreachable at %s — is `portflow daemon start` running? (%w)", c.base, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("%s %s: %s", method, path, strings.TrimSpace(string(b)))
	}
	if out != nil && len(b) > 0 {
		return json.Unmarshal(b, out)
	}
	return nil
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

// portInUse is a small helper used by CLI conflict messages.
func portInUse(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return true
	}
	_ = ln.Close()
	return false
}

// ensureDaemon returns a helpful error if the daemon isn't running.
func ensureDaemon(c *client) error {
	if c.alive() {
		return nil
	}
	return fmt.Errorf("daemon not running — start it with:\n  portflow daemon start")
}

// Type re-exports for the CLI package so we don't pull registry into every file.
type (
	Service = registry.Service
)
