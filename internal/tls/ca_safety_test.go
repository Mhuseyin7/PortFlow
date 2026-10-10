package tls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestLoadOrCreateRefusesToOverwriteCorruptCA(t *testing.T) {
	dir := t.TempDir()
	ca, err := LoadOrCreateCA(dir)
	if err != nil {
		t.Fatal(err)
	}
	certBefore := mustRead(t, ca.CertPath())
	if err := os.WriteFile(ca.KeyPath(), []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreateCA(dir); err == nil {
		t.Fatal("expected error for corrupt CA key")
	}
	if mustRead(t, ca.CertPath()) != certBefore {
		t.Fatal("CA certificate was overwritten")
	}
}

func TestLoadOrCreateRefusesIncompleteCA(t *testing.T) {
	dir := t.TempDir()
	ca, err := LoadOrCreateCA(dir)
	if err != nil {
		t.Fatal(err)
	}
	certBefore := mustRead(t, ca.CertPath())
	if err := os.Remove(ca.KeyPath()); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreateCA(dir); err == nil {
		t.Fatal("expected error when only the key is missing")
	}
	if mustRead(t, ca.CertPath()) != certBefore {
		t.Fatal("CA certificate was overwritten")
	}
}

func TestLoadOrCreateRejectsMismatchedKey(t *testing.T) {
	a, err := LoadOrCreateCA(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	b, err := LoadOrCreateCA(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.KeyPath(), []byte(mustRead(t, b.KeyPath())), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCA(filepath.Dir(a.dir)); err == nil {
		t.Fatal("expected key/cert mismatch error")
	}
}

func TestLoadCADoesNotCreate(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadCA(dir); !errors.Is(err, ErrNoCA) {
		t.Fatalf("want ErrNoCA, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "ca")); err == nil {
		t.Fatal("LoadCA must not create the CA directory")
	}
}

func TestCAKeyIsPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission bits are not enforced on Windows")
	}
	ca, err := LoadOrCreateCA(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(ca.KeyPath())
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("CA key mode = %v, want 0600", fi.Mode().Perm())
	}
}

func TestSignLeafRefusesNonDevelopmentNames(t *testing.T) {
	ca, err := LoadOrCreateCA(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{"google.com", "evil.test.com", "localhost", "", "a..test", "x.test\nevil.com"} {
		if _, _, err := ca.SignLeaf(h); err == nil {
			t.Errorf("SignLeaf(%q) should fail", h)
		}
	}
	if _, _, err := ca.SignLeaf("App.Shop.TEST."); err != nil {
		t.Errorf("normalizable name should be accepted: %v", err)
	}
}

func TestCANameConstraintsBlockOtherDomains(t *testing.T) {
	ca, err := LoadOrCreateCA(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(42),
		Subject:      pkix.Name{CommonName: "google.com"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"google.com"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca.cert)
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, DNSName: "google.com"}); err == nil {
		t.Fatal("name-constrained CA must not validate a certificate for google.com")
	}
}
