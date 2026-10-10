// Package netutil holds small networking helpers shared by the proxy, the
// health checker and the CLI.
package netutil

import (
	"context"
	"net"
	"time"
)

// DialLocal dials addr like d.DialContext, with one addition: when addr is a
// loopback IP literal and the connection fails, it retries the other loopback
// family on the same port.
//
// Dev servers bind inconsistently. Node 17+ resolves "localhost" to ::1 and
// listens only there, while most other tools listen only on 127.0.0.1. A
// service registered with the default 127.0.0.1 target should still work when
// the server is on ::1, and vice versa. The error of the first attempt is
// returned if both fail.
func DialLocal(ctx context.Context, d *net.Dialer, network, addr string) (net.Conn, error) {
	conn, err := d.DialContext(ctx, network, addr)
	if err == nil {
		return conn, nil
	}
	host, port, splitErr := net.SplitHostPort(addr)
	if splitErr != nil || ctx.Err() != nil {
		return nil, err
	}
	var alt string
	switch host {
	case "127.0.0.1":
		alt = "::1"
	case "::1":
		alt = "127.0.0.1"
	default:
		return nil, err
	}
	if conn, altErr := d.DialContext(ctx, network, net.JoinHostPort(alt, port)); altErr == nil {
		return conn, nil
	}
	return nil, err
}

// CanConnect reports whether a TCP connection to host:port succeeds within
// timeout, using the same loopback fallback as DialLocal. It only connects; it
// never binds the port.
func CanConnect(host, port string, timeout time.Duration) bool {
	if host == "" {
		host = "127.0.0.1"
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	conn, err := DialLocal(ctx, &net.Dialer{Timeout: timeout}, "tcp", net.JoinHostPort(host, port))
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
