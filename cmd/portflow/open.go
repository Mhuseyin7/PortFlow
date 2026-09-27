package portflow

import (
	"fmt"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"
)

func newOpenCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "open <domain>",
		Short: "Open a registered domain in your browser",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			if err := ensureDaemon(c); err != nil {
				return err
			}
			var svc Service
			if err := c.get("/api/services/"+args[0], &svc); err != nil {
				return err
			}
			scheme := "http"
			if svc.TLS {
				scheme = "https"
			}
			url := fmt.Sprintf("%s://%s", scheme, svc.Hostname)
			return openBrowser(url)
		},
	}
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/C", "start", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
