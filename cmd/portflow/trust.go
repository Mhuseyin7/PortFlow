package portflow

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/portflow/portflow/internal/config"
	pfca "github.com/portflow/portflow/internal/tls"
	"github.com/portflow/portflow/internal/trust"
	"github.com/spf13/cobra"
)

// confirm asks a yes/no question on the terminal. It refuses to guess when
// stdin is not interactive: changing the OS trust store must be deliberate.
func confirm(question string) (bool, error) {
	fi, err := os.Stdin.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return false, errors.New("this changes your system's trust settings; re-run in a terminal, or pass --yes to confirm")
	}
	fmt.Printf("%s [y/N]: ", question)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	}
	return false, nil
}

func newTrustCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "trust",
		Short: "Install PortFlow's local CA into your OS trust store (asks for approval)",
		Long: `Install PortFlow's local CA into your OS trust store.

PortFlow never does this on its own. The CA is restricted to .test, .localhost
and .local names, so even if its key leaked it could not vouch for a real
website. Your OS will show its own approval prompt as well.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := config.DataDir(cfgFile)
			if err != nil {
				return err
			}
			ca, err := pfca.LoadOrCreateCA(dir)
			if err != nil {
				return err
			}
			fmt.Println("PortFlow will install this local CA into your system trust store:")
			fmt.Println(" ", ca.CertPath())
			fmt.Println("It can only vouch for .test, .localhost and .local names.")
			fmt.Println("Undo it any time with `portflow untrust`.")
			fmt.Println()
			if !yes {
				ok, err := confirm("Install the CA now?")
				if err != nil {
					return err
				}
				if !ok {
					fmt.Println("cancelled; nothing was changed.")
					return nil
				}
			}
			if err := trust.Install(ca.CertPath()); err != nil {
				return fmt.Errorf("install failed: %w", err)
			}
			fmt.Println("installed. Restart your browser if it was already open.")
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")
	return cmd
}

func newUntrustCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "untrust",
		Short: "Remove PortFlow's local CA from your OS trust store",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := config.ResolveDataDir(cfgFile)
			if err != nil {
				return err
			}
			ca, err := pfca.LoadCA(dir) // never creates a CA just to remove it
			if errors.Is(err, pfca.ErrNoCA) {
				fmt.Println("no PortFlow CA found; nothing to remove.")
				return nil
			}
			if err != nil {
				return err
			}
			if !yes {
				ok, err := confirm("Remove the PortFlow CA from your trust store?")
				if err != nil {
					return err
				}
				if !ok {
					fmt.Println("cancelled; nothing was changed.")
					return nil
				}
			}
			if err := trust.Uninstall(ca.CertPath()); err != nil {
				return err
			}
			fmt.Println("removed.")
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")
	return cmd
}
