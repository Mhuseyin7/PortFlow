package portflow

import (
	"fmt"

	"github.com/portflow/portflow/internal/config"
	pfca "github.com/portflow/portflow/internal/tls"
	"github.com/portflow/portflow/internal/trust"
	"github.com/spf13/cobra"
)

func newTrustCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "trust",
		Short: "Install PortFlow's local CA into your OS trust store (requires approval)",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := config.DataDir(cfgFile)
			if err != nil {
				return err
			}
			ca, err := pfca.LoadOrCreateCA(dir)
			if err != nil {
				return err
			}
			fmt.Println("PortFlow will install the local CA at:")
			fmt.Println(" ", ca.CertPath())
			fmt.Println()
			fmt.Println("This is a one-time action. Your OS will ask for elevation/approval.")
			fmt.Println("You can undo it any time with `portflow untrust`.")
			fmt.Println()
			if err := trust.Install(ca.CertPath()); err != nil {
				return fmt.Errorf("install failed: %w", err)
			}
			fmt.Println("installed.")
			return nil
		},
	}
}

func newUntrustCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "untrust",
		Short: "Remove PortFlow's local CA from your OS trust store",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := config.DataDir(cfgFile)
			if err != nil {
				return err
			}
			ca, err := pfca.LoadOrCreateCA(dir)
			if err != nil {
				return err
			}
			return trust.Uninstall(ca.CertPath())
		},
	}
}
