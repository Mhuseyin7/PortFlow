package tls

import (
	"crypto/x509"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newStore(t *testing.T) (*CertStore, *CA, string) {
	t.Helper()
	dir := t.TempDir()
	ca, err := LoadOrCreateCA(dir)
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewCertStore(ca, dir)
	if err != nil {
		t.Fatal(err)
	}
	return s, ca, dir
}

func TestStoreSetsParsedLeaf(t *testing.T) {
	s, _, _ := newStore(t)
	c, err := s.For("api.shop.test")
	if err != nil {
		t.Fatal(err)
	}
	if c.Leaf == nil {
		t.Fatal("Leaf must be pre-parsed so handshakes don't re-parse the certificate")
	}
}

func TestStoreRotatesNearExpiry(t *testing.T) {
	s, _, _ := newStore(t)
	a, err := s.For("api.shop.test")
	if err != nil {
		t.Fatal(err)
	}
	old := renewBefore
	t.Cleanup(func() { renewBefore = old })
	renewBefore = 200 * 24 * time.Hour // longer than a leaf's 90-day life
	b, err := s.For("api.shop.test")
	if err != nil {
		t.Fatal(err)
	}
	if a == b || a.Leaf.SerialNumber.Cmp(b.Leaf.SerialNumber) == 0 {
		t.Fatal("expected a freshly issued certificate once inside the renewal window")
	}
}

func TestStoreReissuesWhenCAChanged(t *testing.T) {
	shared := t.TempDir()
	ca1, err := LoadOrCreateCA(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ca2, err := LoadOrCreateCA(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s1, _ := NewCertStore(ca1, shared)
	if _, err := s1.For("api.shop.test"); err != nil {
		t.Fatal(err)
	}
	s2, _ := NewCertStore(ca2, shared) // same cert directory, different CA
	c, err := s2.For("api.shop.test")
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca2.cert)
	if _, err := c.Leaf.Verify(x509.VerifyOptions{Roots: roots, DNSName: "api.shop.test"}); err != nil {
		t.Fatalf("leaf from a stale CA was served: %v", err)
	}
}

func TestStoreFilenamesAreSafe(t *testing.T) {
	s, _, dir := newStore(t)
	for _, bad := range []string{"../evil.test", "a/b.test", `a\b.test`, "a..test", ""} {
		if _, err := s.For(bad); err == nil {
			t.Errorf("For(%q) should be rejected", bad)
		}
	}
	if _, err := s.For("con.test"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "certs", "leaf-con.test.pem")); err != nil {
		t.Fatalf("expected prefixed filename: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "certs"))
	for _, e := range entries {
		if e.Name()[:5] != "leaf-" {
			t.Errorf("unexpected file in cert dir: %s", e.Name())
		}
	}
}

func TestStorePurge(t *testing.T) {
	s, _, dir := newStore(t)
	if _, err := s.For("x.shop.test"); err != nil {
		t.Fatal(err)
	}
	if err := s.Purge("x.shop.test"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "certs", "leaf-x.shop.test.key")); err == nil {
		t.Fatal("key should be removed")
	}
}
