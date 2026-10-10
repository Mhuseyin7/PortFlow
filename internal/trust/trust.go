// Package trust integrates PortFlow's local CA with the operating system's
// trust store. Install/Uninstall are the only functions that ever modify
// trust settings, and they only run when a user explicitly invokes them.
package trust

import (
	"crypto/sha1"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// caCommonName is the subject CN of the certificate PortFlow generates.
const caCommonName = "PortFlow Local Development CA"

// run executes a command with the terminal attached so sudo, UAC and keychain
// prompts are visible to the user.
func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

// fingerprint returns the upper-case hex SHA-1 of the certificate in
// certPath. SHA-1 is used only because OS trust tools select certificates by
// it; it is an identifier here, not a security boundary.
func fingerprint(certPath string) (string, error) {
	b, err := os.ReadFile(certPath)
	if err != nil {
		return "", err
	}
	block, _ := pem.Decode(b)
	if block == nil || block.Type != "CERTIFICATE" {
		return "", errors.New("not a PEM certificate: " + certPath)
	}
	if _, err := x509.ParseCertificate(block.Bytes); err != nil {
		return "", err
	}
	sum := sha1.Sum(block.Bytes)
	return strings.ToUpper(hex.EncodeToString(sum[:])), nil
}
