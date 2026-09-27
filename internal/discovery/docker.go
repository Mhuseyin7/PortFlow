// Docker discovery uses the Docker Engine HTTP API over the local
// Unix socket (Linux/macOS). Windows named-pipe support is deferred to
// v0.2 — we return a soft "not available" instead of crashing.
//
// We deliberately do NOT depend on github.com/docker/docker: that
// package pulls a large transitive graph (opencontainers/image-spec,
// moby/sys, etc.) and has known cross-platform build issues. Since we
// only need `GET /containers/json`, a hand-written HTTP client over the
// UNIX socket is smaller, faster, and portable.

package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"
)

// DockerHit describes a Docker container with a published loopback port.
type DockerHit struct {
	Container  string `json:"container"`
	Image      string `json:"image"`
	Port       int    `json:"port"`
	TargetPort int    `json:"target_port"`
	Proto      string `json:"proto"`
}

// ScanDocker lists containers with published loopback ports.
// Returns (nil, nil) — not an error — if Docker is unreachable.
func ScanDocker(ctx context.Context) ([]DockerHit, error) {
	socket, err := dockerSocket()
	if err != nil {
		return nil, nil
	}
	hc := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", socket)
			},
		},
		Timeout: 3 * time.Second,
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", "http://docker/containers/json", nil)
	if err != nil {
		return nil, nil
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, nil
	}
	var raw []struct {
		Names []string `json:"Names"`
		Image string   `json:"Image"`
		Ports []struct {
			IP          string `json:"IP"`
			PrivatePort int    `json:"PrivatePort"`
			PublicPort  int    `json:"PublicPort"`
			Type        string `json:"Type"`
		} `json:"Ports"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, nil
	}
	var out []DockerHit
	for _, c := range raw {
		name := "(unnamed)"
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/")
		}
		for _, p := range c.Ports {
			if p.PublicPort == 0 {
				continue
			}
			if p.IP != "" && !isLoopback(p.IP) && p.IP != "0.0.0.0" && p.IP != "::" {
				continue
			}
			out = append(out, DockerHit{
				Container:  name,
				Image:      c.Image,
				Port:       p.PublicPort,
				TargetPort: p.PrivatePort,
				Proto:      p.Type,
			})
		}
	}
	return out, nil
}

// dockerSocket returns the local Docker Engine socket path if we know
// how to reach it on the current OS. Windows pipes are not yet supported.
func dockerSocket() (string, error) {
	c, err := net.DialTimeout("unix", "/var/run/docker.sock", 200*time.Millisecond)
	if err == nil {
		_ = c.Close()
		return "/var/run/docker.sock", nil
	}
	return "", errors.New("docker socket not available")
}

func isLoopback(ip string) bool { return ip == "127.0.0.1" || ip == "::1" }
