//go:build linux

package trust

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	debAnchor  = "/usr/local/share/ca-certificates/portflow-ca.crt" // Debian, Ubuntu, Alpine
	rhelAnchor = "/etc/pki/ca-trust/source/anchors/portflow-ca.crt" // Fedora, RHEL, CentOS
)

func have(cmd string) bool {
	_, err := exec.LookPath(cmd)
	return err == nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// priv runs a command as root, through sudo when we are not root already.
func priv(name string, args ...string) error {
	if os.Geteuid() == 0 {
		return run(name, args...)
	}
	if !have("sudo") {
		return errors.New("root privileges are required and sudo was not found")
	}
	return run("sudo", append([]string{name}, args...)...)
}

// Install adds the CA to the system trust store and, when certutil is
// available, to the per-user NSS databases used by Chrome, Chromium and Firefox.
func Install(certPath string) error {
	var err error
	switch {
	case have("update-ca-certificates"):
		if err = priv("install", "-m", "0644", certPath, debAnchor); err == nil {
			err = priv("update-ca-certificates")
		}
	case have("update-ca-trust"):
		if err = priv("install", "-m", "0644", certPath, rhelAnchor); err == nil {
			err = priv("update-ca-trust", "extract")
		}
	case have("trust"): // p11-kit, e.g. Arch
		err = priv("trust", "anchor", "--store", certPath)
	default:
		err = errors.New("no supported system trust tool found (update-ca-certificates, update-ca-trust, trust); import " + certPath + " manually")
	}
	if err != nil {
		return err
	}
	installNSS(certPath)
	return nil
}

// Uninstall removes the CA from the system store and the per-user NSS databases.
func Uninstall(certPath string) error {
	var first error
	keep := func(err error) {
		if err != nil && first == nil {
			first = err
		}
	}
	removedAnchor := false
	if exists(debAnchor) {
		removedAnchor = true
		keep(priv("rm", "-f", debAnchor))
		if have("update-ca-certificates") {
			keep(priv("update-ca-certificates", "--fresh"))
		}
	}
	if exists(rhelAnchor) {
		removedAnchor = true
		keep(priv("rm", "-f", rhelAnchor))
		if have("update-ca-trust") {
			keep(priv("update-ca-trust", "extract"))
		}
	}
	if !removedAnchor && have("trust") {
		_ = priv("trust", "anchor", "--remove", certPath) // not installed this way? fine
	}
	uninstallNSS()
	return first
}

// IsInstalled reports whether this exact certificate is in the system store.
func IsInstalled(certPath string) (bool, error) {
	want, err := os.ReadFile(certPath)
	if err != nil {
		return false, err
	}
	for _, p := range []string{debAnchor, rhelAnchor} {
		if got, err := os.ReadFile(p); err == nil && bytes.Equal(bytes.TrimSpace(got), bytes.TrimSpace(want)) {
			return true, nil
		}
	}
	if have("trust") {
		if out, err := exec.Command("trust", "list").Output(); err == nil && strings.Contains(string(out), caCommonName) {
			return true, nil
		}
	}
	return false, nil
}

// nssDirs lists the NSS databases of the current user. NSS databases are
// per-user, so this is skipped when running as root.
func nssDirs() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	dirs := []string{filepath.Join(home, ".pki", "nssdb")}
	for _, pat := range []string{
		filepath.Join(home, ".mozilla", "firefox", "*", "cert9.db"),
		filepath.Join(home, "snap", "firefox", "common", ".mozilla", "firefox", "*", "cert9.db"),
	} {
		matches, _ := filepath.Glob(pat)
		for _, m := range matches {
			dirs = append(dirs, filepath.Dir(m))
		}
	}
	return dirs
}

func installNSS(certPath string) {
	if os.Geteuid() == 0 {
		fmt.Println("note: run `portflow trust` without sudo to also trust the CA in Chrome, Chromium and Firefox (their certificate databases are per-user).")
		return
	}
	if !have("certutil") {
		fmt.Println("note: Chrome, Chromium and Firefox on Linux use their own certificate database.")
		fmt.Println("      Install libnss3-tools (certutil) and re-run `portflow trust`, or import this file in the browser settings:")
		fmt.Println("      " + certPath)
		return
	}
	for _, dir := range nssDirs() {
		db := "sql:" + dir
		if !exists(filepath.Join(dir, "cert9.db")) {
			// Create only the shared Chromium database; never invent Firefox profiles.
			if filepath.Base(dir) != "nssdb" {
				continue
			}
			if err := os.MkdirAll(dir, 0o700); err != nil {
				continue
			}
			_ = exec.Command("certutil", "-d", db, "-N", "--empty-password").Run()
		}
		_ = exec.Command("certutil", "-d", db, "-D", "-n", caCommonName).Run() // drop any older copy
		if err := exec.Command("certutil", "-d", db, "-A", "-t", "C,,", "-n", caCommonName, "-i", certPath).Run(); err != nil {
			fmt.Printf("note: could not add the CA to %s: %v\n", dir, err)
		}
	}
}

func uninstallNSS() {
	if os.Geteuid() == 0 || !have("certutil") {
		return
	}
	for _, dir := range nssDirs() {
		if exists(filepath.Join(dir, "cert9.db")) {
			_ = exec.Command("certutil", "-d", "sql:"+dir, "-D", "-n", caCommonName).Run()
		}
	}
}
