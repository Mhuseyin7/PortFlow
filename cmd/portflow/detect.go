package portflow

import (
	"fmt"

	"github.com/portflow/portflow/internal/discovery"
	"github.com/spf13/cobra"
)

func newDetectCmd() *cobra.Command {
	var register bool
	var project string
	cmd := &cobra.Command{
		Use:   "detect",
		Short: "Detect local dev servers listening on loopback",
		RunE: func(cmd *cobra.Command, args []string) error {
			hits, err := discovery.ScanLocal()
			if err != nil {
				return err
			}
			if len(hits) == 0 {
				fmt.Println("no local dev servers detected on loopback ports")
				return nil
			}
			suffix := ".test"
			if project == "" {
				project = "dev"
			}
			fmt.Println("Detected local servers:")
			for _, h := range hits {
				fmt.Printf("  %-24s :%d  (%s)\n", h.Guess+"."+project+suffix, h.Port, h.Process)
			}
			if !register {
				fmt.Println("\nre-run with --register to add them")
				return nil
			}
			c := newClient()
			if err := ensureDaemon(c); err != nil {
				return err
			}
			for _, h := range hits {
				hostname := h.Guess + "." + project + suffix
				body := map[string]any{
					"hostname":    hostname,
					"target_host": "127.0.0.1",
					"target_port": h.Port,
					"tls":         true,
					"project":     project,
				}
				var svc Service
				if err := c.post("/api/services", body, &svc); err != nil {
					fmt.Printf("  skip %s (%v)\n", hostname, err)
					continue
				}
				fmt.Printf("  added https://%s → :%d\n", svc.Hostname, svc.TargetPort)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&register, "register", false, "register the detected services with the daemon")
	cmd.Flags().StringVarP(&project, "project", "p", "dev", "project group name for auto-registered services")
	return cmd
}
