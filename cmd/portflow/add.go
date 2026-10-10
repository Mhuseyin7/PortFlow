package portflow

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// createResult is the daemon's answer to a registration.
type createResult struct {
	Service
	Warnings []string `json:"warnings"`
}

func newAddCmd() *cobra.Command {
	var (
		project string
		noTLS   bool
		target  string
	)
	cmd := &cobra.Command{
		Use:   "add <domain> <port>",
		Short: "Register a local domain that proxies to a local port",
		Long: `Register a local domain that proxies to a local port.

The domain must end in .test, .localhost or .local (reserved for local
development). The target must be a loopback address unless the daemon was
started with --allow-external-targets.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			port, err := strconv.Atoi(args[1])
			if err != nil || port < 1 || port > 65535 {
				return fmt.Errorf("invalid port %q", args[1])
			}
			c := newClient()
			if err := ensureDaemon(c); err != nil {
				return err
			}
			var res createResult
			body := map[string]any{
				"hostname":    args[0],
				"target_host": target,
				"target_port": port,
				"tls":         !noTLS,
				"project":     project,
			}
			if err := c.post("/api/services", body, &res); err != nil {
				return err
			}
			var st statusReport
			_ = c.get("/api/status", &st)

			fmt.Printf("Registered:\n\n  %s\n  → http://%s\n\n", c.urls().of(res.Service),
				net.JoinHostPort(strings.Trim(res.TargetHost, "[]"), strconv.Itoa(res.TargetPort)))
			switch {
			case noTLS:
				fmt.Println("TLS:\n  disabled (plain HTTP)")
			case st.CATrusted:
				fmt.Println("TLS:\n  trusted local certificate")
			default:
				fmt.Println("TLS:\n  local certificate, but the CA is not trusted yet — run: portflow trust")
			}
			fmt.Println()
			if portListening(res.TargetHost, res.TargetPort) {
				fmt.Println("Status:\n  target is reachable — routing is live")
			} else {
				fmt.Printf("Status:\n  waiting for service on port %d\n", res.TargetPort)
			}
			for _, w := range res.Warnings {
				fmt.Printf("\nwarning: %s\n", w)
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "attach service to a project group")
	cmd.Flags().BoolVar(&noTLS, "no-tls", false, "serve plaintext HTTP only (default: HTTPS)")
	cmd.Flags().StringVar(&target, "target", "127.0.0.1", "target host to proxy to (loopback only unless the daemon allows external targets)")
	return cmd
}

// pathEscape makes a hostname safe to embed in an API path.
func pathEscape(host string) string { return url.PathEscape(host) }
