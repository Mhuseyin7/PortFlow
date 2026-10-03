package portflow

import (
	"fmt"
	"net"
	"os"
	"runtime"
	"time"

	"github.com/portflow/portflow/internal/config"
	pfca "github.com/portflow/portflow/internal/tls"
	"github.com/portflow/portflow/internal/trust"
	"github.com/spf13/cobra"
)

// humanDuration formats a duration as "X days" / "X months" / "X years"
// — good enough for cert expiry display.
func humanDuration(d time.Duration) string {
	days := int(d.Hours() / 24)
	switch {
	case days < 0:
		return "expired"
	case days < 60:
		return fmt.Sprintf("%d days", days)
	case days < 730:
		return fmt.Sprintf("%d months", days/30)
	default:
		return fmt.Sprintf("%d years", days/365)
	}
}

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
				remain := time.Until(ca.NotAfter())
				switch {
				case remain < 30*24*time.Hour:
					warn(fmt.Sprintf("CA expires in %s — rotate it soon (delete %s and re-run `portflow trust`)",
						humanDuration(remain), ca.CertPath()))
				case remain < 180*24*time.Hour:
					warn(fmt.Sprintf("CA expires in %s", humanDuration(remain)))
				default:
					pass("CA valid for " + humanDuration(remain))
				}
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
