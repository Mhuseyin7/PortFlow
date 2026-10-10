// Package discovery scans local processes and (optionally) Docker to help
// developers register services without hand-typing ports.
package discovery

import (
	"strings"

	"github.com/shirou/gopsutil/v3/net"
	"github.com/shirou/gopsutil/v3/process"
)

// Hit describes a detected local listener.
type Hit struct {
	Port    int    `json:"port"`
	Process string `json:"process"`
	PID     int32  `json:"pid"`
	Guess   string `json:"guess"` // a slug like "app", "api", "docs"
	// Exposed is true when the listener is bound to all interfaces instead of
	// loopback only, so other machines on the network can reach it directly.
	Exposed bool `json:"exposed"`
}

// ScanLocal returns every unique loopback listener we find.
func ScanLocal() ([]Hit, error) {
	conns, err := net.Connections("tcp")
	if err != nil {
		return nil, err
	}
	seen := map[int]int{} // port -> index in out
	var out []Hit
	for _, c := range conns {
		if c.Status != "LISTEN" || c.Laddr.Port == 0 {
			continue
		}
		ip := c.Laddr.IP
		wildcard := ip == "0.0.0.0" || ip == "::"
		if ip != "127.0.0.1" && ip != "::1" && !wildcard {
			continue
		}
		if i, dup := seen[int(c.Laddr.Port)]; dup {
			out[i].Exposed = out[i].Exposed || wildcard
			continue
		}
		seen[int(c.Laddr.Port)] = len(out)
		h := Hit{Port: int(c.Laddr.Port), PID: c.Pid, Exposed: wildcard}
		if c.Pid > 0 {
			if p, err := process.NewProcess(c.Pid); err == nil {
				if name, err := p.Name(); err == nil {
					h.Process = name
				}
				if cl, err := p.CmdlineSlice(); err == nil {
					h.Guess = guessFrom(cl, h.Port)
				}
			}
		}
		if h.Guess == "" {
			h.Guess = guessFromPort(h.Port)
		}
		out = append(out, h)
	}
	return out, nil
}

func guessFromPort(port int) string {
	switch port {
	case 3000, 3001, 5173, 5174, 8080, 4200:
		return "app"
	case 8000, 8001, 4000, 5000, 9000:
		return "api"
	case 8025, 8081:
		return "admin"
	case 5432, 3306, 6379, 27017:
		return "db"
	default:
		return "svc"
	}
}

func guessFrom(cmdline []string, port int) string {
	joined := strings.ToLower(strings.Join(cmdline, " "))
	switch {
	case strings.Contains(joined, "next"):
		return "app"
	case strings.Contains(joined, "vite"):
		return "app"
	case strings.Contains(joined, "webpack"):
		return "app"
	case strings.Contains(joined, "fastapi"), strings.Contains(joined, "uvicorn"), strings.Contains(joined, "django"):
		return "api"
	case strings.Contains(joined, "flask"):
		return "api"
	case strings.Contains(joined, "express"), strings.Contains(joined, "nestjs"):
		return "api"
	case strings.Contains(joined, "rails"):
		return "app"
	case strings.Contains(joined, "storybook"):
		return "docs"
	}
	return guessFromPort(port)
}
