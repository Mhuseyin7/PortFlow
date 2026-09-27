//go:build linux

package trust

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// Install copies the CA into /usr/local/share/ca-certificates and refreshes
// the system CA bundle. Works on Debian/Ubuntu and derivatives; on other
// distributions install manually to their preferred location.
func Install(certPath string) error {
	dst := "/usr/local/share/ca-certificates/portflow-ca.crt"
	if err := copyFile(certPath, dst); err != nil {
		return fmt.Errorf("copy CA to %s: %w (try re-running with sudo)", dst, err)
	}
	if _, err := exec.LookPath("update-ca-certificates"); err == nil {
		out, err := exec.Command("sudo", "update-ca-certificates").CombinedOutput()
		if err != nil {
			return fmt.Errorf("update-ca-certificates: %w: %s", err, string(out))
		}
		return nil
	}
	if _, err := exec.LookPath("update-ca-trust"); err == nil {
		if err := os.MkdirAll("/etc/pki/ca-trust/source/anchors", 0o755); err == nil {
			_ = copyFile(certPath, filepath.Join("/etc/pki/ca-trust/source/anchors", "portflow-ca.crt"))
		}
		out, err := exec.Command("sudo", "update-ca-trust", "extract").CombinedOutput()
		if err != nil {
			return fmt.Errorf("update-ca-trust: %w: %s", err, string(out))
		}
		return nil
	}
	return fmt.Errorf("no supported ca-certificates tool found; install %s manually", dst)
}

// Uninstall removes the CA and refreshes the bundle.
func Uninstall(_ string) error {
	_ = os.Remove("/usr/local/share/ca-certificates/portflow-ca.crt")
	_ = os.Remove("/etc/pki/ca-trust/source/anchors/portflow-ca.crt")
	if _, err := exec.LookPath("update-ca-certificates"); err == nil {
		_, _ = exec.Command("sudo", "update-ca-certificates", "--fresh").CombinedOutput()
	}
	if _, err := exec.LookPath("update-ca-trust"); err == nil {
		_, _ = exec.Command("sudo", "update-ca-trust", "extract").CombinedOutput()
	}
	return nil
}

// IsInstalled returns true if the CA is present in a well-known anchor dir.
func IsInstalled(_ string) (bool, error) {
	candidates := []string{
		"/usr/local/share/ca-certificates/portflow-ca.crt",
		"/etc/pki/ca-trust/source/anchors/portflow-ca.crt",
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return true, nil
		}
	}
	return false, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
