package tls

import (
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"
)

func TestCACreateAndReload(t *testing.T) {
	dir := t.TempDir()
	ca, err := LoadOrCreateCA(dir)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !ca.cert.IsCA {
		t.Fatal("cert is not a CA")
	}
	if time.Until(ca.NotAfter()) < 365*24*time.Hour {
		t.Fatal("CA expiry too short")
	}
	// Reload from disk.
	ca2, err := LoadOrCreateCA(dir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if ca2.cert.SerialNumber.Cmp(ca.cert.SerialNumber) != 0 {
		t.Fatal("expected reloaded CA to match")
	}
}

func TestSignLeafValidatesAgainstCA(t *testing.T) {
	dir := t.TempDir()
	ca, err := LoadOrCreateCA(dir)
	if err != nil {
		t.Fatal(err)
	}
	certPEM, _, err := ca.SignLeaf("app.shop.test")
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("no PEM")
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca.cert)
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, DNSName: "app.shop.test"}); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestCertStoreReusesLeaf(t *testing.T) {
	dir := t.TempDir()
	ca, _ := LoadOrCreateCA(dir)
	store, err := NewCertStore(ca, dir)
	if err != nil {
		t.Fatal(err)
	}
	a, err := store.For("api.shop.test")
	if err != nil {
		t.Fatal(err)
	}
	b, err := store.For("api.shop.test")
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatal("expected cached leaf")
	}
}
