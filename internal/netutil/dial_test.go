package netutil

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"
)

func listenOrSkip(t *testing.T, addr string) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Skipf("cannot listen on %s here: %v", addr, err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	return ln
}

func portOf(ln net.Listener) string { return strconv.Itoa(ln.Addr().(*net.TCPAddr).Port) }

func dial(addr string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c, err := DialLocal(ctx, &net.Dialer{Timeout: time.Second}, "tcp", addr)
	if err == nil {
		c.Close()
	}
	return err
}

func TestIPv4TargetFallsBackToIPv6OnlyServer(t *testing.T) {
	ln := listenOrSkip(t, "[::1]:0")
	if err := dial("127.0.0.1:" + portOf(ln)); err != nil {
		t.Fatalf("a server bound only to ::1 must be reachable via the default 127.0.0.1 target: %v", err)
	}
}

func TestIPv6TargetFallsBackToIPv4OnlyServer(t *testing.T) {
	ln := listenOrSkip(t, "127.0.0.1:0")
	if err := dial("[::1]:" + portOf(ln)); err != nil {
		t.Fatalf("a server bound only to 127.0.0.1 must be reachable via a ::1 target: %v", err)
	}
}

func TestNoFallbackForNonLoopbackHosts(t *testing.T) {
	ln := listenOrSkip(t, "127.0.0.1:0")
	port := portOf(ln)
	// A non-loopback name must never be silently redirected to loopback.
	if err := dial("203.0.113.1:" + port); err == nil {
		t.Fatal("dialing a non-loopback address must not fall back to loopback")
	}
}

func TestCanConnect(t *testing.T) {
	ln := listenOrSkip(t, "127.0.0.1:0")
	port := portOf(ln)
	if !CanConnect("", port, time.Second) || !CanConnect("127.0.0.1", port, time.Second) {
		t.Fatal("expected a connection to the listening port")
	}
	ln.Close()
	if CanConnect("127.0.0.1", port, 300*time.Millisecond) {
		t.Fatal("closed port must not connect")
	}
}
