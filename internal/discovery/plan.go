package discovery

import (
	"context"
	"net"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/portflow/portflow/internal/hostname"
	"github.com/portflow/portflow/internal/netutil"
)

// Candidate is one detected service that `portflow detect` can register.
type Candidate struct {
	Hostname string
	Port     int
	Source   string // "process" or "docker"
	Detail   string // process or container name
	Exposed  bool   // listens on all interfaces, so the LAN can reach it directly
}

// Plan turns raw scan results into registrable candidates:
//   - ports in skip (PortFlow's own listeners, already-registered targets) are dropped;
//   - a process listening on a port a container publishes is the Docker proxy,
//     so only the container is kept;
//   - names are unique: the second "app" becomes "app-<port>", never a conflict;
//   - container names are slugged into valid DNS labels.
//
// The result is ordered by source, then port, so names are stable between runs.
func Plan(hits []Hit, docker []DockerHit, project string, skip map[int]bool) []Candidate {
	suffix := "." + hostname.Slug(project) + ".test"
	if hostname.Slug(project) == "" {
		suffix = ".dev.test"
	}

	published := map[int]bool{}
	for _, d := range docker {
		published[d.Port] = true
	}

	procs := append([]Hit(nil), hits...)
	sort.Slice(procs, func(i, j int) bool { return procs[i].Port < procs[j].Port })
	cons := append([]DockerHit(nil), docker...)
	sort.Slice(cons, func(i, j int) bool {
		if cons[i].Port != cons[j].Port {
			return cons[i].Port < cons[j].Port
		}
		return cons[i].Container < cons[j].Container
	})

	used := map[string]bool{}
	take := func(label string, port int) string {
		if label == "" {
			label = "svc"
		}
		name := label + suffix
		if used[name] {
			name = label + "-" + strconv.Itoa(port) + suffix
		}
		used[name] = true
		return name
	}

	var out []Candidate
	seen := map[int]bool{}
	for _, h := range procs {
		if skip[h.Port] || published[h.Port] || seen[h.Port] {
			continue
		}
		seen[h.Port] = true
		out = append(out, Candidate{Hostname: take(hostname.Slug(h.Guess), h.Port), Port: h.Port, Source: "process", Detail: h.Process, Exposed: h.Exposed})
	}
	for _, d := range cons {
		if skip[d.Port] || seen[d.Port] {
			continue
		}
		seen[d.Port] = true
		out = append(out, Candidate{Hostname: take(hostname.Slug(d.Container), d.Port), Port: d.Port, Source: "docker", Detail: d.Container + " / " + d.Image, Exposed: d.Exposed})
	}
	return out
}

// ProbeHTTP reports whether something on loopback:port answers an HTTP
// request. It filters out databases, SSH and other non-HTTP listeners, which a
// reverse proxy cannot serve. Any HTTP status counts, including 4xx and 5xx.
func ProbeHTTP(ctx context.Context, port int) bool {
	d := &net.Dialer{Timeout: 400 * time.Millisecond}
	hc := &http.Client{
		Timeout:       800 * time.Millisecond,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Transport: &http.Transport{
			Proxy:             nil,
			DisableKeepAlives: true,
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return netutil.DialLocal(ctx, d, network, addr)
			},
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, "http://127.0.0.1:"+strconv.Itoa(port)+"/", nil)
	if err != nil {
		return false
	}
	resp, err := hc.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return true
}
