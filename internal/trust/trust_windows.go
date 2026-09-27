//go:build windows

package trust

import (
	"fmt"
	"os/exec"
)

// Install adds the CA to the Windows current-user Root store via certutil.
// The user sees the standard UAC / consent dialog.
func Install(certPath string) error {
	cmd := exec.Command("certutil", "-user", "-addstore", "Root", certPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("certutil: %w: %s", err, string(out))
	}
	return nil
}

// Uninstall removes the CA from the current-user Root store.
func Uninstall(certPath string) error {
	cmd := exec.Command("certutil", "-user", "-delstore", "Root", "PortFlow Local Development CA")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("certutil: %w: %s", err, string(out))
	}
	return nil
}

// IsInstalled probes for the CA in the current-user Root store.
func IsInstalled(certPath string) (bool, error) {
	cmd := exec.Command("certutil", "-user", "-verifystore", "Root", "PortFlow Local Development CA")
	if err := cmd.Run(); err != nil {
		return false, nil
	}
	return true, nil
}
