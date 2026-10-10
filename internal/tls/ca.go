// Package tls manages PortFlow's local development Certificate Authority
// and on-demand leaf certificates for registered domains.
//
// The CA is self-signed, stored under <data>/ca/, and only ever used to
// sign leaf certificates for local domains. It is name-constrained to the
// reserved development suffixes, so even a stolen CA key cannot mint a
// certificate for a real website that a browser would accept. It is trusted
// only after the user runs `portflow trust`.
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

	"github.com/portflow/portflow/internal/hostname"
)

// ErrNoCA is returned by LoadCA when no CA has been created yet.
var ErrNoCA = errors.New("no local CA found; run `portflow trust` or start the daemon to create one")

// CA is a persistent local development CA.
type CA struct {
	dir     string
	cert    *x509.Certificate
	key     *ecdsa.PrivateKey
	certDER []byte
}

// LoadCA loads an existing CA without ever creating or modifying files.
func LoadCA(dataDir string) (*CA, error) {
	ca := &CA{dir: filepath.Join(dataDir, "ca")}
	if err := ca.load(); err != nil {
		return nil, err
	}
	return ca, nil
}

// LoadOrCreateCA returns the CA for dataDir. A new CA is created only when
// neither the certificate nor the key exists; any other load failure is
// reported instead of overwriting a CA the user may already have trusted.
func LoadOrCreateCA(dataDir string) (*CA, error) {
	dir := filepath.Join(dataDir, "ca")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	ca := &CA{dir: dir}
	err := ca.load()
	if err == nil {
		return ca, nil
	}
	if !errors.Is(err, ErrNoCA) {
		return nil, fmt.Errorf("existing CA in %s is unusable (refusing to overwrite it): %w", dir, err)
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
	certPEM, certErr := os.ReadFile(c.CertPath())
	keyPEM, keyErr := os.ReadFile(c.KeyPath())
	switch {
	case errors.Is(certErr, os.ErrNotExist) && errors.Is(keyErr, os.ErrNotExist):
		return ErrNoCA
	case certErr != nil:
		return fmt.Errorf("read CA certificate: %w", certErr)
	case keyErr != nil:
		return fmt.Errorf("read CA key: %w", keyErr)
	}
	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		return errors.New("CA certificate is not valid PEM")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return fmt.Errorf("parse CA certificate: %w", err)
	}
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return errors.New("CA key is not valid PEM")
	}
	key, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		return fmt.Errorf("parse CA key: %w", err)
	}
	if !key.PublicKey.Equal(cert.PublicKey) {
		return errors.New("CA key does not match CA certificate")
	}
	c.cert = cert
	c.key = key
	c.certDER = certBlock.Bytes
	return nil
}

func newSerial() (*big.Int, error) {
	return rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
}

// writeNew creates path exclusively so an existing file is never clobbered.
func writeNew(path string, data []byte, perm os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	return f.Close()
}

func (c *CA) create() error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serial, err := newSerial()
	if err != nil {
		return err
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "PortFlow Local Development CA",
			Organization: []string{"PortFlow"},
		},
		NotBefore:                   time.Now().Add(-1 * time.Hour),
		NotAfter:                    time.Now().AddDate(10, 0, 0),
		KeyUsage:                    x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid:       true,
		IsCA:                        true,
		MaxPathLen:                  0,
		MaxPathLenZero:              true,
		PermittedDNSDomainsCritical: true,
		PermittedDNSDomains:         hostname.DefaultSuffixes,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	if err := writeNew(c.KeyPath(), keyPEM, 0o600); err != nil {
		return fmt.Errorf("write CA key: %w", err)
	}
	if err := writeNew(c.CertPath(), certPEM, 0o644); err != nil {
		os.Remove(c.KeyPath())
		return fmt.Errorf("write CA certificate: %w", err)
	}
	c.cert = cert
	c.key = key
	c.certDER = der
	return nil
}

// SignLeaf issues a short-lived leaf cert for the given hostname. Only names
// under the permitted development suffixes are accepted. The returned
// certPEM includes the CA cert so clients receive the full chain.
func (c *CA) SignLeaf(host string) (certPEM, keyPEM []byte, err error) {
	host = hostname.Normalize(host)
	if err := hostname.Validate(host); err != nil {
		return nil, nil, err
	}
	if !hostname.AllowedSuffix(host, nil) {
		return nil, nil, fmt.Errorf("refusing to issue a certificate for %q: not under a development suffix", host)
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serial, err := newSerial()
	if err != nil {
		return nil, nil, err
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: host},
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().AddDate(0, 0, 90),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{host},
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
