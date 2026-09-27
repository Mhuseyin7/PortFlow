// Package tls manages PortFlow's local development Certificate Authority
// and on-demand leaf certificates for registered domains.
//
// The CA is self-signed, stored under <data>/ca/, and only ever used to
// sign leaf certificates for local domains. It is trusted only after the
// user runs `portflow trust`, which invokes the OS-specific trust store.
package tls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

// CA is a persistent local development CA.
type CA struct {
	dir     string
	cert    *x509.Certificate
	key     *ecdsa.PrivateKey
	certDER []byte
}

// LoadOrCreateCA returns the CA for dataDir, creating one if none exists.
func LoadOrCreateCA(dataDir string) (*CA, error) {
	dir := filepath.Join(dataDir, "ca")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	ca := &CA{dir: dir}
	if err := ca.load(); err == nil {
		return ca, nil
	}
	if err := ca.create(); err != nil {
		return nil, err
	}
	return ca, nil
}

// CertPath returns the on-disk path to the CA certificate PEM.
func (c *CA) CertPath() string { return filepath.Join(c.dir, "portflow-ca.pem") }

// KeyPath returns the path to the CA private key PEM (mode 0600).
func (c *CA) KeyPath() string { return filepath.Join(c.dir, "portflow-ca.key") }

func (c *CA) load() error {
	certPEM, err := os.ReadFile(c.CertPath())
	if err != nil {
		return err
	}
	keyPEM, err := os.ReadFile(c.KeyPath())
	if err != nil {
		return err
	}
	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		return errors.New("ca cert: bad pem")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return err
	}
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return errors.New("ca key: bad pem")
	}
	key, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		return err
	}
	c.cert = cert
	c.key = key
	c.certDER = certBlock.Bytes
	return nil
}

func (c *CA) create() error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "PortFlow Local Development CA",
			Organization: []string{"PortFlow"},
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	if err := os.WriteFile(c.CertPath(), certPEM, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(c.KeyPath(), keyPEM, 0o600); err != nil {
		return err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return err
	}
	c.cert = cert
	c.key = key
	c.certDER = der
	return nil
}

// SignLeaf issues a short-lived leaf cert for the given hostname.
// The returned certPEM includes the CA cert so browsers get the chain.
func (c *CA) SignLeaf(hostname string) (certPEM, keyPEM []byte, err error) {
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: hostname},
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().AddDate(0, 0, 90),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{hostname},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, c.cert, &leafKey.PublicKey, c.key)
	if err != nil {
		return nil, nil, err
	}
	leafPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.certDER})
	certPEM = append(leafPEM, caPEM...)
	keyDER, err := x509.MarshalECPrivateKey(leafKey)
	if err != nil {
		return nil, nil, err
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM, nil
}

// CACertPEM returns the CA cert PEM bytes (for install/uninstall flows).
func (c *CA) CACertPEM() []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.certDER})
}

// NotAfter reports the CA's expiry.
func (c *CA) NotAfter() time.Time { return c.cert.NotAfter }

func (c *CA) String() string {
	return fmt.Sprintf("PortFlow CA (expires %s)", c.NotAfter().Format("2006-01-02"))
}
