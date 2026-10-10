package portflow

import (
	"fmt"
	"text/tabwriter"

	"github.com/portflow/portflow/internal/reqlog"
	"github.com/spf13/cobra"
)

func newLogsCmd() *cobra.Command {
	var enable, disable, clearLog bool
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Show the development request log (off by default)",
		Long: `Show the development request log.

The log is off by default and lives only in the daemon's memory (the most
recent 500 requests). It records method, host, path, status and latency —
never headers, cookies, bodies or sensitive query values.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			if err := ensureDaemon(c); err != nil {
				return err
			}
			switch {
			case enable:
				if err := c.post("/api/requests/enable", nil, nil); err != nil {
					return err
				}
				fmt.Println("request log enabled (in memory only; headers and bodies are never recorded)")
				return nil
			case disable:
				if err := c.post("/api/requests/disable", nil, nil); err != nil {
					return err
				}
				fmt.Println("request log disabled")
				return nil
			case clearLog:
				if err := c.del("/api/requests", nil); err != nil {
					return err
				}
				fmt.Println("request log cleared")
				return nil
			}
			var res struct {
				Enabled bool           `json:"enabled"`
				Entries []reqlog.Entry `json:"entries"`
			}
			if err := c.get("/api/requests", &res); err != nil {
				return err
			}
			if !res.Enabled {
				fmt.Println("request log is off — enable it with: portflow logs --enable")
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			for _, e := range res.Entries {
				fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%.1fms\t%s\n",
					e.Time.Local().Format("15:04:05"), e.Host, e.Method, e.Status, e.DurationMS, e.Path)
			}
			return w.Flush()
		},
	}
	cmd.Flags().BoolVar(&enable, "enable", false, "start recording requests")
	cmd.Flags().BoolVar(&disable, "disable", false, "stop recording requests")
	cmd.Flags().BoolVar(&clearLog, "clear", false, "discard recorded requests")
	return cmd
}
