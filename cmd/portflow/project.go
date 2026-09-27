package portflow

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/portflow/portflow/internal/config"
	"github.com/spf13/cobra"
)

func newProjectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Manage portflow.yml project files",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "init",
		Short: "Create a starter portflow.yml in the current directory",
		RunE: func(cmd *cobra.Command, args []string) error {
			wd, err := os.Getwd()
			if err != nil {
				return err
			}
			base := filepath.Base(wd)
			path := filepath.Join(wd, "portflow.yml")
			if _, err := os.Stat(path); err == nil {
				return fmt.Errorf("%s already exists", path)
			}
			p := &config.Project{
				Version: 1,
				Project: base,
				Services: map[string]config.ProjectService{
					"app": {Domain: "app." + base + ".test", Port: 3000},
					"api": {Domain: "api." + base + ".test", Port: 8000},
				},
			}
			if err := config.WriteProject(path, p); err != nil {
				return err
			}
			fmt.Printf("wrote %s\n", path)
			fmt.Println("edit it, then run: portflow up")
			return nil
		},
	})
	return cmd
}
