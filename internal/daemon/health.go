package daemon

import (
	"context"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/portflow/portflow/internal/hostname"
	"github.com/portflow/portflow/internal/registry"
)

// healthInterval is deliberately gentle: one TCP connect per service every
// ten seconds is enough for a status badge without bothering dev servers.
const healthInterval = 10 * time.Second

func (d *Daemon) healthLoop(ctx context.Context) {
	d.checkAll()
	tick := time.NewTicker(healthInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			d.checkAll()
		}
	}
}

func (d *Daemon) checkAll() {
	svcs, err := d.store.List()
	if err != nil {
		return
	}
	for _, s := range svcs {
		if status := probe(s, d.opts.AllowExternalTargets); status != s.Status {
			_ = d.store.UpdateStatus(s.Hostname, status)
		}
	}
}

// probe reports a service's health. It never dials a non-loopback target
// unless external targets are enabled, so the status check cannot be turned
// into a port scanner.
func probe(s registry.Service, allowExternal bool) string {
	host := strings.TrimSpace(s.TargetHost)
	if host == "" {
		host = "127.0.0.1" // same default the proxy applies
	}
	if !allowExternal && !hostname.IsLoopbackHost(host) {
		return registry.StatusMisconfigured
	}
	target := net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(s.TargetPort))
	c, err := net.DialTimeout("tcp", target, 500*time.Millisecond)
	if err != nil {
		return registry.StatusDown
	}
	_ = c.Close()
	return registry.StatusUp
}
