package portflow

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/portflow/portflow/internal/discovery"
	"github.com/spf13/cobra"
)

// portsOf returns every port in a comma-separated list of host:port addresses.
func portsOf(spec string) []int {
	var out []int
	for _, a := range strings.Split(spec, ",") {
		if _, p, err := net.SplitHostPort(strings.TrimSpace(a)); err == nil {
			if n, err := strconv.Atoi(p); err == nil {
				out = append(out, n)
			}
		}
	}
	return out
}

func newDetectCmd() *cobra.Command {
	var (
		register   bool
		project    string
		dockerScan bool
		all        bool
	)
	cmd := &cobra.Command{
		Use:   "detect",
		Short: "Detect local web servers (and optionally Docker containers) you could register",
		Long: `Detect local web servers and suggest a domain for each.

Only listeners that answer HTTP are suggested (databases and other protocols
are skipped unless you pass --all). PortFlow's own ports and ports that are
already registered are left out. Nothing is registered without --register.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			hits, err := discovery.ScanLocal()
			if err != nil {
				return err
			}
			var dockerHits []discovery.DockerHit
			if dockerScan {
				dockerHits, _ = discovery.ScanDocker(ctx)
			}

			c := newClient()
			skip := map[int]bool{}
			registeredPorts := 0
			if c.alive() {
				var st statusReport
				if c.get("/api/status", &st) == nil {
					for _, spec := range []string{st.ProxyHTTP, st.ProxyHTTPS, st.APIAddr} {
						for _, p := range portsOf(spec) {
							skip[p] = true
						}
					}
				}
				var svcs []Service
				if c.get("/api/services", &svcs) == nil {
					for _, s := range svcs {
						if !skip[s.TargetPort] {
							skip[s.TargetPort] = true
							registeredPorts++
						}
					}
				}
			}

			if !all {
				web := hits[:0:0]
				for _, h := range hits {
					if discovery.ProbeHTTP(ctx, h.Port) {
						web = append(web, h)
					}
				}
				hits = web
				webD := dockerHits[:0:0]
				for _, d := range dockerHits {
					if discovery.ProbeHTTP(ctx, d.Port) {
						webD = append(webD, d)
					}
				}
				dockerHits = webD
			}

			cands := discovery.Plan(hits, dockerHits, project, skip)
			if len(cands) == 0 {
				fmt.Println("no new web servers detected on local ports")
				if registeredPorts > 0 {
					fmt.Printf("(%d already registered port(s) were left out)\n", registeredPorts)
				}
				return nil
			}
			fmt.Println("Detected:")
			exposed := false
			for _, cd := range cands {
				note := ""
				if cd.Exposed {
					note = "  [listens on all interfaces]"
					exposed = true
				}
				fmt.Printf("  %-34s :%d  (%s: %s)%s\n", cd.Hostname, cd.Port, cd.Source, cd.Detail, note)
			}
			if exposed {
				fmt.Println("\nnote: services bound to all interfaces are reachable from your network directly,\n      bypassing PortFlow. Bind them to 127.0.0.1 if that is not intended.")
			}
			if !register {
				fmt.Println("\nre-run with --register to add them")
				return nil
			}
			if err := ensureDaemon(c); err != nil {
				return err
			}
			u := c.urls()
			failed := 0
			for _, cd := range cands {
				var res createResult
				body := map[string]any{
					"hostname":    cd.Hostname,
					"target_host": "127.0.0.1",
					"target_port": cd.Port,
					"tls":         true,
					"project":     project,
				}
				if err := c.post("/api/services", body, &res); err != nil {
					failed++
					fmt.Printf("  skip %s (%v)\n", cd.Hostname, err)
					continue
				}
				fmt.Printf("  added %s → :%d\n", u.of(res.Service), res.TargetPort)
				for _, w := range res.Warnings {
					fmt.Printf("        warning: %s\n", w)
				}
			}
			if failed > 0 {
				return fmt.Errorf("%d of %d service(s) could not be registered", failed, len(cands))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&register, "register", false, "register the detected services with the daemon")
	cmd.Flags().StringVarP(&project, "project", "p", "dev", "project group name for auto-registered services")
	cmd.Flags().BoolVar(&dockerScan, "docker", false, "also scan published Docker containers")
	cmd.Flags().BoolVar(&all, "all", false, "include listeners that do not answer HTTP (databases, etc.)")
	return cmd
}
