package portflow

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/portflow/portflow/internal/config"
	"github.com/spf13/cobra"
)

func newUpCmd() *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:   "up",
		Short: "Register every service listed in portflow.yml",
		RunE: func(cmd *cobra.Command, args []string) error {
			if file == "" {
				wd, _ := os.Getwd()
				file = filepath.Join(wd, "portflow.yml")
			}
			p, err := config.LoadProject(file)
			if err != nil {
				return fmt.Errorf("load %s: %w", file, err)
			}
			c := newClient()
			if err := ensureDaemon(c); err != nil {
				return err
			}
			for name, svc := range p.Services {
				body := map[string]any{
					"hostname":    svc.Domain,
					"target_host": firstNonEmpty(svc.Host, "127.0.0.1"),
					"target_port": svc.Port,
					"tls":         !svc.NoTLS,
					"project":     p.Project,
				}
				var out Service
				if err := c.post("/api/services", body, &out); err != nil {
					fmt.Printf("  %-16s %s  (skip: %v)\n", name, svc.Domain, err)
					continue
				}
				scheme := "https"
				if svc.NoTLS {
					scheme = "http"
				}
				fmt.Printf("  %-16s %s://%s → :%d\n", name, scheme, svc.Domain, svc.Port)
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "", "path to portflow.yml (default: ./portflow.yml)")
	return cmd
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
