package portflow

import (
	"fmt"
	"net"
	"os"
	"runtime"

	"github.com/portflow/portflow/internal/config"
	pfca "github.com/portflow/portflow/internal/tls"
	"github.com/portflow/portflow/internal/trust"
	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose PortFlow install and environment",
		RunE: func(cmd *cobra.Command, args []string) error {
			pass := func(msg string) { fmt.Println("  ✓", msg) }
			warn := func(msg string) { fmt.Println("  !", msg) }
			fail := func(msg string) { fmt.Println("  ✗", msg) }

			fmt.Println("PortFlow doctor")
			fmt.Println()

			fmt.Printf("os: %s/%s\n", runtime.GOOS, runtime.GOARCH)

			dir, err := config.DataDir(cfgFile)
			if err != nil {
				fail("data dir: " + err.Error())
			} else if _, err := os.Stat(dir); err == nil {
				pass("data dir: " + dir)
			} else {
				warn("data dir missing (will be created on first daemon start): " + dir)
			}

			if _, err := net.Listen("tcp", "127.0.0.1:9280"); err == nil {
				warn("daemon api port 9280 is free — daemon appears NOT running")
			} else {
				pass("daemon api port 9280 is bound (daemon likely running)")
			}

			if _, err := net.Listen("tcp", "127.0.0.1:443"); err == nil {
				warn("proxy https port 443 is free — daemon not bound (start it)")
			} else {
				pass("proxy https port 443 is bound")
			}

			if ca, err := pfca.LoadOrCreateCA(dir); err == nil {
				pass("local CA present: " + ca.CertPath())
				installed, _ := trust.IsInstalled(ca.CertPath())
				if installed {
					pass("CA is in OS trust store")
				} else {
					warn("CA is NOT in OS trust store — run: portflow trust")
				}
			} else {
				fail("CA: " + err.Error())
			}
			return nil
		},
	}
}
