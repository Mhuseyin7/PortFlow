package portflow

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/portflow/portflow/internal/config"
	"github.com/portflow/portflow/internal/daemon"
	"github.com/spf13/cobra"
)

func newDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the PortFlow background daemon",
	}
	cmd.AddCommand(newDaemonStartCmd(), newDaemonStopCmd())
	return cmd
}

func newDaemonStartCmd() *cobra.Command {
	var (
		httpAddr   string
		httpsAddr  string
		apiAddr    string
		fg         bool
		skipHosts  bool
		allowLAN   bool
	)
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the PortFlow daemon (foreground with -f)",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := config.DataDir(cfgFile)
			if err != nil {
				return err
			}
			opts := daemon.Options{
				DataDir:              dir,
				HTTPAddr:             httpAddr,
				HTTPSAddr:            httpsAddr,
				APIAddr:              apiAddr,
				Version:              versionStr,
				Commit:               commitStr,
				SkipHosts:            skipHosts,
				AllowExternalTargets: allowLAN,
			}
			d, err := daemon.New(opts)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
			go func() {
				<-sigCh
				fmt.Fprintln(os.Stderr, "\nshutting down...")
				cancel()
			}()

			fmt.Printf("PortFlow daemon starting\n  api   %s\n  http  %s\n  https %s\n  data  %s\n\n",
				apiAddr, httpAddr, httpsAddr, dir)
			return d.Run(ctx)
		},
	}
	cmd.Flags().StringVar(&httpAddr, "http", "127.0.0.1:80", "loopback HTTP proxy listen address")
	cmd.Flags().StringVar(&httpsAddr, "https", "127.0.0.1:443", "loopback HTTPS proxy listen address")
	cmd.Flags().StringVar(&apiAddr, "api", "127.0.0.1:9280", "management API listen address")
	cmd.Flags().BoolVarP(&fg, "foreground", "f", true, "run in foreground (currently the only mode)")
	cmd.Flags().BoolVar(&skipHosts, "skip-hosts", false, "do not manage the OS hosts file (you handle DNS yourself)")
	cmd.Flags().BoolVar(&allowLAN, "allow-external-targets", false, "allow proxying to non-loopback targets (off by default)")
	_ = fg // reserved for a future --background mode
	return cmd
}

func newDaemonStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Ask the running daemon to shut down",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			if !c.alive() {
				fmt.Println("daemon is not running")
				return nil
			}
			return c.post("/api/shutdown", nil, nil)
		},
	}
}
