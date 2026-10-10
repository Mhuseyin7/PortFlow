package portflow

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/portflow/portflow/internal/config"
	"github.com/portflow/portflow/internal/hostname"
	"github.com/spf13/cobra"
)

func newUpCmd() *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:   "up",
		Short: "Register every service listed in portflow.yml",
		Long: `Register every service listed in portflow.yml.

PortFlow only registers routes. It never starts your application commands.
Running it again is safe: services that are already registered with the same
settings are reported as unchanged.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if file == "" {
				wd, err := os.Getwd()
				if err != nil {
					return err
				}
				file = filepath.Join(wd, "portflow.yml")
			}
			p, err := config.LoadProject(file)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return fmt.Errorf("no %s found — create one with `portflow project init`", file)
				}
				return fmt.Errorf("load %s: %w", file, err)
			}
			c := newClient()
			if err := ensureDaemon(c); err != nil {
				return err
			}
			u := c.urls()

			names := make([]string, 0, len(p.Services))
			for name := range p.Services {
				names = append(names, name)
			}
			sort.Strings(names)

			failed := 0
			for _, name := range names {
				svc := p.Services[name]
				host := strings.ToLower(firstNonEmpty(svc.Host, "127.0.0.1"))
				domain := hostname.Normalize(svc.Domain)
				body := map[string]any{
					"hostname":    domain,
					"target_host": host,
					"target_port": svc.Port,
					"tls":         !svc.NoTLS,
					"project":     p.Project,
				}
				var res createResult
				err := c.post("/api/services", body, &res)
				var apiErr *apiError
				switch {
				case err == nil:
					fmt.Printf("  %-12s %s → :%d\n", name, u.of(res.Service), svc.Port)
					for _, w := range res.Warnings {
						fmt.Printf("               warning: %s\n", w)
					}
				case errors.As(err, &apiErr) && apiErr.Status == http.StatusConflict:
					var cur Service
					same := c.get("/api/services/"+pathEscape(domain), &cur) == nil &&
						cur.TargetPort == svc.Port && cur.TLS == !svc.NoTLS && cur.TargetHost == host
					if same {
						fmt.Printf("  %-12s %s (unchanged)\n", name, u.of(cur))
					} else {
						failed++
						fmt.Printf("  %-12s %s — already registered with different settings; run `portflow remove %s` first\n", name, domain, domain)
					}
				default:
					failed++
					fmt.Printf("  %-12s %s — %v\n", name, domain, err)
				}
			}
			if failed > 0 {
				return fmt.Errorf("%d of %d service(s) could not be registered", failed, len(names))
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
