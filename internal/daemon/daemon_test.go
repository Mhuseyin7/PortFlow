package daemon

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/portflow/portflow/internal/hosts"
	"github.com/portflow/portflow/internal/registry"
)

func TestCheckLoopback(t *testing.T) {
	ok := []string{"", "127.0.0.1:80", "[::1]:80", "127.0.0.1:80,[::1]:80", "localhost:8080", " 127.0.0.1:1 , [::1]:1 ", "127.5.5.5:1"}
	bad := []string{"0.0.0.0:80", ":80", "[::]:80", "192.168.1.5:80", "127.0.0.1:80,0.0.0.0:80", "example.com:80", "10.0.0.1:443", "noport"}
	for _, s := range ok {
		if err := checkLoopback(s); err != nil {
			t.Errorf("checkLoopback(%q): %v", s, err)
		}
	}
	for _, s := range bad {
		if err := checkLoopback(s); err == nil {
			t.Errorf("checkLoopback(%q) must fail", s)
		}
	}
}

func TestValidateBind(t *testing.T) {
	base := Options{HTTPAddr: "127.0.0.1:80", HTTPSAddr: "127.0.0.1:443", APIAddr: "127.0.0.1:9280"}
	if err := validateBind(base); err != nil {
		t.Fatal(err)
	}

	lan := base
	lan.HTTPSAddr = "0.0.0.0:443"
	if err := validateBind(lan); err == nil {
		t.Fatal("LAN listener must be refused without --lan")
	}
	lan.AllowLAN = true
	if err := validateBind(lan); err != nil {
		t.Fatalf("explicit LAN mode should be allowed: %v", err)
	}

	api := base
	api.AllowLAN = true
	for _, a := range []string{"0.0.0.0:9280", "192.168.1.5:9280", "127.0.0.1:9280,[::1]:9280", ""} {
		api.APIAddr = a
		if err := validateBind(api); err == nil {
			t.Errorf("management API address %q must be refused even in LAN mode", a)
		}
	}
}

func TestNewRejectsLANBindBeforeTouchingDisk(t *testing.T) {
	dir := t.TempDir()
	_, err := New(Options{DataDir: dir, HTTPAddr: "0.0.0.0:80", HTTPSAddr: "127.0.0.1:443", SkipHosts: true})
	if err == nil {
		t.Fatal("expected error")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("validation must run before any file is created, found %d entries", len(entries))
	}
}

func TestProbe(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	svc := registry.Service{TargetHost: "127.0.0.1", TargetPort: port}
	if got := probe(svc, false); got != registry.StatusUp {
		t.Errorf("listening port: %s", got)
	}
	ln.Close()
	if got := probe(svc, false); got != registry.StatusDown {
		t.Errorf("closed port: %s", got)
	}
	svc.TargetHost = ""
	if got := probe(svc, false); got != registry.StatusDown {
		t.Errorf("empty host defaults to loopback: %s", got)
	}
	for _, h := range []string{"10.0.0.1", "169.254.169.254", "example.com"} {
		svc.TargetHost = h
		if got := probe(svc, false); got != registry.StatusMisconfigured {
			t.Errorf("%s: %s (the status check must not dial non-loopback targets)", h, got)
		}
	}
}

func TestSyncHostsSkipsInvalidLegacyRows(t *testing.T) {
	dir := t.TempDir()
	d, err := New(Options{DataDir: dir, HTTPAddr: "127.0.0.1:0", HTTPSAddr: "127.0.0.1:0", APIAddr: "127.0.0.1:0", SkipHosts: true})
	if err != nil {
		t.Fatal(err)
	}
	defer d.store.Close()
	hostsPath := filepath.Join(dir, "hosts")
	d.hosts = hosts.NewManagerAt(hostsPath)

	for _, h := range []string{"good.shop.test", "google.com", "bad_name.test"} {
		if _, err := d.store.Create(registry.Service{Hostname: h, TargetHost: "127.0.0.1", TargetPort: 1}); err != nil {
			t.Fatal(err)
		}
	}
	d.syncHosts()
	b, _ := os.ReadFile(hostsPath)
	if !strings.Contains(string(b), "good.shop.test") {
		t.Fatalf("valid entry missing:\n%s", b)
	}
	if strings.Contains(string(b), "google.com") || strings.Contains(string(b), "bad_name") {
		t.Fatalf("invalid legacy rows must never reach the hosts file:\n%s", b)
	}
}

func newLoopbackDaemon(t *testing.T, mutate func(*Options)) *Daemon {
	t.Helper()
	opts := Options{DataDir: t.TempDir(), HTTPAddr: "127.0.0.1:0", HTTPSAddr: "127.0.0.1:0", APIAddr: "127.0.0.1:0", SkipHosts: true}
	if mutate != nil {
		mutate(&opts)
	}
	d, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestRunReturnsProxyBindErrorInsteadOfHanging(t *testing.T) {
	occupant, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupant.Close()
	d := newLoopbackDaemon(t, func(o *Options) { o.HTTPAddr = occupant.Addr().String() })

	done := make(chan error, 1)
	go func() { done <- d.Run(context.Background()) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Run must report that the proxy could not start")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run hung with a dead proxy (API up, nothing proxied)")
	}
}

func TestRunStopsCleanlyOnContextCancel(t *testing.T) {
	d := newLoopbackDaemon(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	time.Sleep(300 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("clean shutdown returned %v", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("Run did not stop after cancellation")
	}
}

func TestRunFailsWhenAPIPortIsTaken(t *testing.T) {
	occupant, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupant.Close()
	d := newLoopbackDaemon(t, func(o *Options) { o.APIAddr = occupant.Addr().String() })
	if err := d.Run(context.Background()); err == nil {
		t.Fatal("expected an API bind error")
	}
}
