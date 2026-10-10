package proxy

import (
	"fmt"
	"log"
	"net"
	"os"
	"strings"
)

// splitAddrs splits a comma-separated list of listen addresses.
func splitAddrs(spec string) []string {
	var out []string
	for _, a := range strings.Split(spec, ",") {
		if a = strings.TrimSpace(a); a != "" {
			out = append(out, a)
		}
	}
	return out
}

func isIPv6Addr(addr string) bool {
	h, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.To4() == nil
}

// listen binds every address in spec. IPv6 addresses that cannot be bound
// (IPv6 disabled on the host) are skipped with a log line; any other failure
// closes everything already bound and is returned, so the daemon fails fast
// instead of running half-configured.
func listen(spec string, logger *log.Logger) ([]net.Listener, error) {
	addrs := splitAddrs(spec)
	if len(addrs) == 0 {
		return nil, nil
	}
	var lns []net.Listener
	for _, addr := range addrs {
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			if isIPv6Addr(addr) {
				logger.Printf("proxy: skipping %s: %v", addr, err)
				continue
			}
			for _, l := range lns {
				_ = l.Close()
			}
			return nil, fmt.Errorf("cannot listen on %s: %w%s", addr, err, bindHint(err))
		}
		lns = append(lns, ln)
	}
	if len(lns) == 0 {
		return nil, fmt.Errorf("cannot listen on %q: no address could be bound", spec)
	}
	return lns, nil
}

func bindHint(err error) string {
	msg := strings.ToLower(err.Error())
	switch {
	case os.IsPermission(err), strings.Contains(msg, "permission denied"), strings.Contains(msg, "access permissions"):
		return "\n  hint: ports below 1024 need elevated privileges. Run as administrator/root, or choose high ports:\n" +
			"        portflow daemon start --http 127.0.0.1:8080,[::1]:8080 --https 127.0.0.1:8443,[::1]:8443"
	case strings.Contains(msg, "address already in use"), strings.Contains(msg, "only one usage of each socket address"):
		return "\n  hint: another program (IIS, nginx, Apache, Docker, or another PortFlow) already uses this port. Stop it or pick another port with --http/--https."
	}
	return ""
}
