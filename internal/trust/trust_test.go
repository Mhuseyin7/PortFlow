package trust

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	pfca "github.com/portflow/portflow/internal/tls"
)

func TestFingerprint(t *testing.T) {
	ca, err := pfca.LoadOrCreateCA(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fp, err := fingerprint(ca.CertPath())
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[0-9A-F]{40}$`).MatchString(fp) {
		t.Fatalf("fingerprint %q is not 40 upper-case hex characters", fp)
	}
	again, _ := fingerprint(ca.CertPath())
	if again != fp {
		t.Fatal("fingerprint must be stable")
	}
}

func TestFingerprintRejectsNonCertificates(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "x.pem")
	if err := os.WriteFile(bad, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := fingerprint(bad); err == nil {
		t.Fatal("expected error for non-PEM input")
	}
	if _, err := fingerprint(filepath.Join(dir, "missing.pem")); err == nil {
		t.Fatal("expected error for missing file")
	}
}
