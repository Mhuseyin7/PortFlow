// PortFlow — turn messy local dev ports into stable local HTTPS domains.
//
// Entry point. Delegates to the cobra-based CLI in cmd/portflow.
package main

import (
	"fmt"
	"os"

	"github.com/portflow/portflow/cmd/portflow"
)

var (
	version = "0.1.0"
	commit  = "dev"
)

func main() {
	portflow.SetVersion(version, commit)
	if err := portflow.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "portflow:", err)
		os.Exit(1)
	}
}
