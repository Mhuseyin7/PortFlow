package portflow

import (
	"fmt"
	"os/exec"
	"runtime"

	"github.com/portflow/portflow/internal/hostname"
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
			if err := c.get("/api/services/"+pathEscape(hostname.Normalize(args[0])), &svc); err != nil {
				return err
			}
			target := c.urls().of(svc)
			fmt.Println("opening", target)
			return openBrowser(target)
		},
	}
}

// openBrowser launches the default browser. The URL is built from a
// validated hostname, never from raw user input.
func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
