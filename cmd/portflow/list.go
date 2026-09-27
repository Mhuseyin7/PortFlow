package portflow

import (
	"fmt"
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all registered services",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			if err := ensureDaemon(c); err != nil {
				return err
			}
			var svcs []Service
			if err := c.get("/api/services", &svcs); err != nil {
				return err
			}
			if len(svcs) == 0 {
				fmt.Println("no services registered — try `portflow add app.shop.test 3000`")
				return nil
			}
			sort.SliceStable(svcs, func(i, j int) bool {
				if svcs[i].Project != svcs[j].Project {
					return svcs[i].Project < svcs[j].Project
				}
				return svcs[i].Hostname < svcs[j].Hostname
			})
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			var project string
			for _, s := range svcs {
				if s.Project != project {
					if project != "" {
						fmt.Fprintln(w)
					}
					project = s.Project
					name := project
					if name == "" {
						name = "(no project)"
					}
					fmt.Fprintf(w, "%s\n", name)
				}
				scheme := "http"
				if s.TLS {
					scheme = "https"
				}
				fmt.Fprintf(w, "  %s\t:%d\t%s\n", s.Hostname+"  ("+scheme+")", s.TargetPort, s.Status)
			}
			return w.Flush()
		},
	}
}
