package portflow

import (
	"context"
	"fmt"

	"github.com/portflow/portflow/internal/discovery"
	"github.com/spf13/cobra"
)

func newDetectCmd() *cobra.Command {
	var (
		register   bool
		project    string
		dockerScan bool
	)
	cmd := &cobra.Command{
		Use:   "detect",
		Short: "Detect local dev servers listening on loopback (and optionally Docker containers)",
		RunE: func(cmd *cobra.Command, args []string) error {
			hits, err := discovery.ScanLocal()
			if err != nil {
				return err
			}
			var dockerHits []discovery.DockerHit
			if dockerScan {
				dockerHits, _ = discovery.ScanDocker(context.Background())
			}
			if len(hits) == 0 && len(dockerHits) == 0 {
				fmt.Println("no local dev servers or Docker containers detected on loopback ports")
				return nil
			}
			suffix := ".test"
			if project == "" {
				project = "dev"
			}
			if len(hits) > 0 {
				fmt.Println("Detected local processes:")
				for _, h := range hits {
					fmt.Printf("  %-28s :%d  (%s)\n", h.Guess+"."+project+suffix, h.Port, h.Process)
				}
			}
			if len(dockerHits) > 0 {
				fmt.Println("\nDetected Docker containers:")
				for _, d := range dockerHits {
					fmt.Printf("  %-28s :%d → :%d  (%s / %s)\n",
						d.Container+"."+project+suffix, d.Port, d.TargetPort, d.Container, d.Image)
				}
			}
			if !register {
				fmt.Println("\nre-run with --register to add them")
				return nil
			}
			c := newClient()
			if err := ensureDaemon(c); err != nil {
				return err
			}
			registerOne := func(hostname, host string, port int) {
				body := map[string]any{
					"hostname":    hostname,
					"target_host": host,
					"target_port": port,
					"tls":         true,
					"project":     project,
				}
				var svc Service
				if err := c.post("/api/services", body, &svc); err != nil {
					fmt.Printf("  skip %s (%v)\n", hostname, err)
					return
				}
				fmt.Printf("  added https://%s → :%d\n", svc.Hostname, svc.TargetPort)
			}
			for _, h := range hits {
				registerOne(h.Guess+"."+project+suffix, "127.0.0.1", h.Port)
			}
			for _, d := range dockerHits {
				registerOne(d.Container+"."+project+suffix, "127.0.0.1", d.Port)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&register, "register", false, "register the detected services with the daemon")
	cmd.Flags().StringVarP(&project, "project", "p", "dev", "project group name for auto-registered services")
	cmd.Flags().BoolVar(&dockerScan, "docker", false, "also scan published Docker containers")
	return cmd
}
