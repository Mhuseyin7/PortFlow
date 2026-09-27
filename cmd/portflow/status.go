package portflow

import (
	"fmt"

	"github.com/spf13/cobra"
)

type statusReport struct {
	Version    string `json:"version"`
	Commit     string `json:"commit"`
	Uptime     string `json:"uptime"`
	Services   int    `json:"services"`
	ProxyHTTP  string `json:"proxy_http"`
	ProxyHTTPS string `json:"proxy_https"`
	APIAddr    string `json:"api_addr"`
	CAReady    bool   `json:"ca_ready"`
	CATrusted  bool   `json:"ca_trusted"`
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show daemon status",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			if !c.alive() {
				fmt.Println("daemon: not running")
				fmt.Println("  start with: portflow daemon start")
				return nil
			}
			var s statusReport
			if err := c.get("/api/status", &s); err != nil {
				return err
			}
			fmt.Printf("daemon:     running (v%s / %s, up %s)\n", s.Version, s.Commit, s.Uptime)
			fmt.Printf("proxy:      %s (http)  %s (https)\n", s.ProxyHTTP, s.ProxyHTTPS)
			fmt.Printf("api:        %s\n", s.APIAddr)
			fmt.Printf("services:   %d registered\n", s.Services)
			fmt.Printf("local CA:   ready=%v trusted=%v\n", s.CAReady, s.CATrusted)
			if s.CAReady && !s.CATrusted {
				fmt.Println("\n  run `portflow trust` to install the local CA in your OS trust store")
			}
			return nil
		},
	}
}
