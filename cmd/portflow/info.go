package portflow

import (
	"fmt"
	"strconv"
	"time"

	"github.com/portflow/portflow/internal/hostname"
	"github.com/spf13/cobra"
)

func newInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info <domain>",
		Short: "Show one service's full details and a live probe",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			if err := ensureDaemon(c); err != nil {
				return err
			}
			var svc Service
			if err := c.get("/api/services/"+pathEscape(hostname.Normalize(args[0])), &svc); err != nil {
				return err
			}
			fmt.Printf("hostname:    %s\n", svc.Hostname)
			fmt.Printf("url:         %s\n", c.urls().of(svc))
			fmt.Printf("target:      %s:%d\n", svc.TargetHost, svc.TargetPort)
			fmt.Printf("project:     %s\n", nonEmptyOr(svc.Project, "(none)"))
			fmt.Printf("protocol:    %s\n", svc.Protocol)
			fmt.Printf("tls:         %v\n", svc.TLS)
			fmt.Printf("status:      %s\n", svc.Status)
			fmt.Printf("created:     %s\n", svc.CreatedAt.Local().Format(time.RFC3339))
			fmt.Printf("updated:     %s\n", svc.UpdatedAt.Local().Format(time.RFC3339))
			fmt.Printf("live probe:  %s:%s ... ", svc.TargetHost, strconv.Itoa(svc.TargetPort))
			if portListening(svc.TargetHost, svc.TargetPort) {
				fmt.Println("UP")
			} else {
				fmt.Println("DOWN (nothing is listening on that port)")
			}
			return nil
		},
	}
}

func nonEmptyOr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
