package daemon

import (
	"context"
	"net"
	"strconv"
	"time"

	"github.com/portflow/portflow/internal/registry"
)

// healthLoop pokes each service's target port on a slow cadence and updates
// its status. We deliberately keep this gentle — the goal is a UI signal,
// not a stress test on the user's dev servers.
func (d *Daemon) healthLoop(ctx context.Context) {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			svcs, err := d.store.List()
			if err != nil {
				continue
			}
			for _, s := range svcs {
				status := probe(s)
				if status != s.Status {
					_ = d.store.UpdateStatus(s.Hostname, status)
				}
			}
		}
	}
}

func probe(s registry.Service) string {
	target := net.JoinHostPort(s.TargetHost, strconv.Itoa(s.TargetPort))
	c, err := net.DialTimeout("tcp", target, 500*time.Millisecond)
	if err != nil {
		return registry.StatusDown
	}
	_ = c.Close()
	return registry.StatusUp
}
