package portflow

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/portflow/portflow/internal/config"
	"github.com/portflow/portflow/internal/hosts"
	pfca "github.com/portflow/portflow/internal/tls"
	"github.com/portflow/portflow/internal/trust"
	"github.com/spf13/cobra"
)

// humanDuration formats a duration as "X days" / "X months" / "X years"
// — good enough for cert expiry display.
func humanDuration(d time.Duration) string {
	days := int(d.Hours() / 24)
	switch {
	case days < 0:
		return "expired"
	case days < 60:
		return fmt.Sprintf("%d days", days)
	case days < 730:
		return fmt.Sprintf("%d months", days/30)
	default:
		return fmt.Sprintf("%d years", days/365)
	}
}

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose PortFlow install and environment (read-only)",
		Long: `Diagnose PortFlow install and environment.

doctor only reads: it never creates a CA, edits the hosts file, or starts
anything.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			problems := 0
			ok := func(m string) { fmt.Println("  OK    ", m) }
			warn := func(m string) { fmt.Println("  WARN  ", m) }
			fail := func(m string) { fmt.Println("  FAIL  ", m); problems++ }

			fmt.Printf("PortFlow doctor — v%s, %s/%s\n\n", versionStr, runtime.GOOS, runtime.GOARCH)

			dir, err := config.ResolveDataDir(cfgFile)
			switch {
			case err != nil:
				fail("data dir: " + err.Error())
			default:
				if fi, err := os.Stat(dir); err != nil {
					warn("data dir does not exist yet (created on first use): " + dir)
				} else {
					ok("data dir: " + dir)
					if runtime.GOOS != "windows" && fi.Mode().Perm()&0o077 != 0 {
						warn(fmt.Sprintf("data dir is accessible by other users (mode %o); run: chmod 700 %s", fi.Mode().Perm(), dir))
					}
				}
			}

			c := newClient()
			var st statusReport
			alive := c.alive()
			if alive && c.get("/api/status", &st) == nil {
				ok(fmt.Sprintf("daemon running (v%s, up %s, %d service(s))", st.Version, st.Uptime, st.Services))
				checkListeners(st, ok, warn)
				if st.AllowExternalTargets {
					warn("external (non-loopback) proxy targets are enabled")
				}
			} else {
				alive = false
				warn("daemon is not running — start it with: portflow daemon start")
			}

			if dir != "" {
				checkCA(dir, ok, warn, fail)
			}

			hm := hosts.NewManager()
			if err := hm.Writable(); err != nil {
				warn(fmt.Sprintf("cannot edit %s as this user (%v); the daemon needs administrator/root to manage names, or use .localhost names", hm.Path(), err))
			} else {
				ok("hosts file is writable: " + hm.Path())
			}

			if alive {
				checkServices(c, ok, warn)
			}

			fmt.Println()
			if problems > 0 {
				return fmt.Errorf("%d problem(s) found", problems)
			}
			fmt.Println("no blocking problems found.")
			return nil
		},
	}
}

func checkListeners(st statusReport, ok, warn func(string)) {
	for _, spec := range []struct{ name, addrs string }{{"http", st.ProxyHTTP}, {"https", st.ProxyHTTPS}} {
		for _, addr := range strings.Split(spec.addrs, ",") {
			addr = strings.TrimSpace(addr)
			if addr == "" {
				continue
			}
			conn, err := net.DialTimeout("tcp", addr, 400*time.Millisecond)
			if err != nil {
				warn(fmt.Sprintf("proxy %s listener %s is not reachable (%v)", spec.name, addr, err))
				continue
			}
			_ = conn.Close()
			ok(fmt.Sprintf("proxy %s listener %s is reachable", spec.name, addr))
		}
	}
}

func checkCA(dir string, ok, warn, fail func(string)) {
	ca, err := pfca.LoadCA(dir)
	switch {
	case errors.Is(err, pfca.ErrNoCA):
		warn("no local CA yet — it is created by `portflow trust` or the first daemon start")
		return
	case err != nil:
		fail("local CA is unusable: " + err.Error())
		return
	}
	ok("local CA present: " + ca.CertPath())
	switch remain := time.Until(ca.NotAfter()); {
	case remain < 30*24*time.Hour:
		warn(fmt.Sprintf("CA expires in %s — back up and delete %s, then re-run `portflow trust`", humanDuration(remain), dir))
	case remain < 180*24*time.Hour:
		warn("CA expires in " + humanDuration(remain))
	default:
		ok("CA valid for " + humanDuration(remain))
	}
	if installed, _ := trust.IsInstalled(ca.CertPath()); installed {
		ok("CA is in the OS trust store")
	} else {
		warn("CA is NOT in the OS trust store — run: portflow trust")
	}
}

// checkServices verifies each registered name resolves to loopback, which is
// what the proxy needs, and flags targets nothing listens on.
func checkServices(c *client, ok, warn func(string)) {
	var svcs []Service
	if err := c.get("/api/services", &svcs); err != nil {
		warn("could not list services: " + err.Error())
		return
	}
	for _, s := range svcs {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		addrs, err := net.DefaultResolver.LookupIPAddr(ctx, s.Hostname)
		cancel()
		switch {
		case err != nil:
			warn(fmt.Sprintf("%s does not resolve (hosts entry missing? run the daemon as administrator/root)", s.Hostname))
			continue
		case !allLoopback(addrs):
			warn(fmt.Sprintf("%s resolves to a non-loopback address %v; check your hosts file and VPN/DNS settings", s.Hostname, addrs))
			continue
		}
		if portListening(s.TargetHost, s.TargetPort) {
			ok(fmt.Sprintf("%s → %s:%d (resolves to loopback, target is up)", s.Hostname, s.TargetHost, s.TargetPort))
		} else {
			warn(fmt.Sprintf("%s → %s:%d resolves, but nothing is listening on that port yet", s.Hostname, s.TargetHost, s.TargetPort))
		}
	}
}

func allLoopback(addrs []net.IPAddr) bool {
	for _, a := range addrs {
		if !a.IP.IsLoopback() {
			return false
		}
	}
	return len(addrs) > 0
}
