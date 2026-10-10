//go:build windows

package trust

import "os/exec"

// Install adds the CA to the current user's Root store via certutil. Windows
// shows its standard security prompt asking the user to approve the root.
func Install(certPath string) error {
	return run("certutil", "-user", "-addstore", "Root", certPath)
}

// Uninstall removes exactly this certificate (matched by thumbprint) from the
// current user's Root store.
func Uninstall(certPath string) error {
	fp, err := fingerprint(certPath)
	if err != nil {
		return err
	}
	return run("certutil", "-user", "-delstore", "Root", fp)
}

// IsInstalled reports whether this exact certificate is in the user's Root store.
func IsInstalled(certPath string) (bool, error) {
	fp, err := fingerprint(certPath)
	if err != nil {
		return false, err
	}
	return exec.Command("certutil", "-user", "-store", "Root", fp).Run() == nil, nil
}
