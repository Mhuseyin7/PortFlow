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
		httpAddr     string
		httpsAddr    string
		apiAddr      string
		skipHosts    bool
		lan          bool
		external     bool
		preserveHost bool
		requestLog   bool
	)
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the PortFlow daemon in the foreground",
		Long: `Start the PortFlow daemon in the foreground (Ctrl+C stops it).

By default everything listens on loopback only. Ports 80 and 443 need
administrator/root; use --http/--https with high ports to avoid that.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Refuse to race another instance on the same ports.
			if (&client{base: "http://" + apiAddr}).alive() {
				return fmt.Errorf("a PortFlow daemon is already running on %s — stop it first with: portflow daemon stop", apiAddr)
			}
			dir, err := config.DataDir(cfgFile)
			if err != nil {
				return err
			}
			d, err := daemon.New(daemon.Options{
				DataDir:              dir,
				HTTPAddr:             httpAddr,
				HTTPSAddr:            httpsAddr,
				APIAddr:              apiAddr,
				Version:              versionStr,
				Commit:               commitStr,
				SkipHosts:            skipHosts,
				AllowLAN:             lan,
				AllowExternalTargets: external,
				PreserveHost:         preserveHost,
				RequestLog:           requestLog,
			})
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

			fmt.Printf("PortFlow daemon starting\n  api   %s\n  http  %s\n  https %s\n  data  %s\n", apiAddr, httpAddr, httpsAddr, dir)
			if lan {
				fmt.Println("  WARNING: --lan lets other machines on your network reach your dev services")
			}
			if external {
				fmt.Println("  WARNING: external (non-loopback) proxy targets are enabled")
			}
			fmt.Println()
			return d.Run(ctx)
		},
	}
	cmd.Flags().StringVar(&httpAddr, "http", "127.0.0.1:80,[::1]:80", "HTTP proxy listen addresses (comma-separated, loopback unless --lan)")
	cmd.Flags().StringVar(&httpsAddr, "https", "127.0.0.1:443,[::1]:443", "HTTPS proxy listen addresses (comma-separated, loopback unless --lan)")
	cmd.Flags().StringVar(&apiAddr, "api", "127.0.0.1:9280", "management API listen address (always loopback)")
	cmd.Flags().BoolVar(&skipHosts, "skip-hosts", false, "do not manage the OS hosts file (you handle DNS yourself)")
	cmd.Flags().BoolVar(&lan, "lan", false, "allow the proxy to listen on non-loopback addresses (exposes dev services to your network)")
	cmd.Flags().BoolVar(&external, "allow-external-targets", false, "allow registering non-loopback proxy targets (off by default)")
	cmd.Flags().BoolVar(&preserveHost, "preserve-host", false, "forward the original Host header to the dev server instead of its own address")
	cmd.Flags().BoolVar(&requestLog, "request-log", false, "start with the in-memory development request log enabled")
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
			if err := c.post("/api/shutdown", nil, nil); err != nil {
				return err
			}
			fmt.Println("daemon is shutting down")
			return nil
		},
	}
}
