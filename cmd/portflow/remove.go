package portflow

import (
	"fmt"

	"github.com/portflow/portflow/internal/hostname"
	"github.com/spf13/cobra"
)

func newRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "remove <domain>",
		Aliases: []string{"rm", "delete"},
		Short:   "Remove a registered domain",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			if err := ensureDaemon(c); err != nil {
				return err
			}
			host := hostname.Normalize(args[0])
			if err := c.del("/api/services/"+pathEscape(host), nil); err != nil {
				return err
			}
			fmt.Printf("Removed %s\n", host)
			return nil
		},
	}
}
