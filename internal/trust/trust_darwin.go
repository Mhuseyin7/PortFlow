//go:build darwin

package trust

import (
	"fmt"
	"os/exec"
)

// Install adds the CA to the macOS System keychain as a trusted root.
// macOS will prompt for admin credentials via the security tool.
func Install(certPath string) error {
	cmd := exec.Command("sudo", "security", "add-trusted-cert", "-d", "-r", "trustRoot",
		"-k", "/Library/Keychains/System.keychain", certPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("security add-trusted-cert: %w: %s", err, string(out))
	}
	return nil
}

// Uninstall removes the CA from the System keychain.
func Uninstall(certPath string) error {
	cmd := exec.Command("sudo", "security", "remove-trusted-cert", "-d", certPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("security remove-trusted-cert: %w: %s", err, string(out))
	}
	return nil
}

// IsInstalled looks for the CA CN in the System keychain.
func IsInstalled(certPath string) (bool, error) {
	cmd := exec.Command("security", "find-certificate", "-c", "PortFlow Local Development CA",
		"/Library/Keychains/System.keychain")
	if err := cmd.Run(); err != nil {
		return false, nil
	}
	return true, nil
}
