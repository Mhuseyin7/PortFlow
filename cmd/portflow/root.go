package portflow

import (
	"github.com/spf13/cobra"
)

var (
	cfgFile     string
	daemonURL   string
	versionStr  = "0.1.0"
	commitStr   = "dev"
	rootCmd     *cobra.Command
)

// SetVersion is called by main() before Execute() to inject build info.
func SetVersion(v, c string) {
	versionStr = v
	commitStr = c
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "portflow",
		Short: "PortFlow — stable local HTTPS domains for development",
		Long: `PortFlow turns messy local development ports into stable local HTTPS domains.

Instead of remembering localhost:3000, localhost:5173, localhost:8000 you register
services like app.shop.test, api.shop.test and PortFlow handles TLS, hosts entries
and reverse proxying to your local processes.

PortFlow is strictly a LOCAL development tool. It never exposes services to the
public internet and never modifies your OS trust store without an explicit command.`,
		Version:      versionStr + " (" + commitStr + ")",
		SilenceUsage: true,
	}

	cmd.PersistentFlags().StringVar(&cfgFile, "config", "", "path to portflow config directory (default: ~/.portflow)")
	cmd.PersistentFlags().StringVar(&daemonURL, "daemon", "http://127.0.0.1:9280", "daemon management API URL")

	cmd.AddCommand(
		newAddCmd(),
		newRemoveCmd(),
		newListCmd(),
		newStatusCmd(),
		newDetectCmd(),
		newOpenCmd(),
		newProjectCmd(),
		newTrustCmd(),
		newUntrustCmd(),
		newDoctorCmd(),
		newUpCmd(),
		newDaemonCmd(),
	)
	return cmd
}

// Execute runs the root command.
func Execute() error {
	rootCmd = newRootCmd()
	return rootCmd.Execute()
}
