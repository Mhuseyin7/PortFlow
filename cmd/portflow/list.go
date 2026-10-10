package portflow

import (
	"fmt"
	"strconv"
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
			u := c.urls()
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			project := "\x00" // the daemon returns services ordered by project
			for _, s := range svcs {
				if s.Project != project {
					if project != "\x00" {
						fmt.Fprintln(w)
					}
					project = s.Project
					name := project
					if name == "" {
						name = "(no project)"
					}
					fmt.Fprintf(w, "%s\n", name)
				}
				fmt.Fprintf(w, "  %s\t:%s\t%s\n", u.of(s), strconv.Itoa(s.TargetPort), s.Status)
			}
			return w.Flush()
		},
	}
}
