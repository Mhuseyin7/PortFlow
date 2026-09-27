package portflow

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
)

func newAddCmd() *cobra.Command {
	var (
		project string
		noTLS   bool
		target  string
	)
	cmd := &cobra.Command{
		Use:   "add <domain> <port>",
		Short: "Register a local domain that proxies to a local port",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			port, err := strconv.Atoi(args[1])
			if err != nil || port < 1 || port > 65535 {
				return fmt.Errorf("invalid port %q", args[1])
			}
			c := newClient()
			if err := ensureDaemon(c); err != nil {
				return err
			}
			var svc Service
			body := map[string]any{
				"hostname":    args[0],
				"target_host": target,
				"target_port": port,
				"tls":         !noTLS,
				"project":     project,
			}
			if err := c.post("/api/services", body, &svc); err != nil {
				return err
			}
			scheme := "https"
			if noTLS {
				scheme = "http"
			}
			fmt.Printf("Registered:\n\n  %s://%s\n  → http://%s:%d\n\n",
				scheme, svc.Hostname, svc.TargetHost, svc.TargetPort)
			if !noTLS {
				fmt.Println("TLS:\n  trusted local certificate (run `portflow trust` once if you haven't)")
				fmt.Println()
			}
			if portInUse(port) {
				fmt.Println("Status:\n  target port is reachable — routing is live")
			} else {
				fmt.Println("Status:\n  waiting for service on port", port)
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "attach service to a project group")
	cmd.Flags().BoolVar(&noTLS, "no-tls", false, "serve plaintext HTTP only (default: HTTPS)")
	cmd.Flags().StringVar(&target, "target", "127.0.0.1", "target host to proxy to (must resolve to loopback unless --allow-external)")
	return cmd
}
