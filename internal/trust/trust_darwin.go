//go:build darwin

package trust

import (
	"os/exec"
	"strings"
)

const systemKeychain = "/Library/Keychains/System.keychain"

// Install adds the CA to the System keychain as a trusted root. macOS asks
// for administrator credentials.
func Install(certPath string) error {
	return run("sudo", "security", "add-trusted-cert", "-d", "-r", "trustRoot", "-k", systemKeychain, certPath)
}

// Uninstall removes the trust setting and then the certificate itself,
// selected by SHA-1 so no other certificate is touched.
func Uninstall(certPath string) error {
	fp, err := fingerprint(certPath)
	if err != nil {
		return err
	}
	_ = run("sudo", "security", "remove-trusted-cert", "-d", certPath) // may already be gone
	return run("sudo", "security", "delete-certificate", "-Z", fp, systemKeychain)
}

// IsInstalled reports whether this exact certificate is in the System keychain.
func IsInstalled(certPath string) (bool, error) {
	fp, err := fingerprint(certPath)
	if err != nil {
		return false, err
	}
	out, err := exec.Command("security", "find-certificate", "-a", "-Z", "-c", caCommonName, systemKeychain).Output()
	if err != nil {
		return false, nil
	}
	return strings.Contains(strings.ToUpper(string(out)), fp), nil
}
