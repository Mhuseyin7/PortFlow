package tls

import (
	"crypto/tls"
	"crypto/x509"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// CertStore caches leaf certificates on disk and in memory, and rotates
// them a week before expiry.
type CertStore struct {
	ca  *CA
	dir string

	mu    sync.Mutex
	cache map[string]*tls.Certificate
}

// NewCertStore returns a store rooted at dataDir/certs.
func NewCertStore(ca *CA, dataDir string) (*CertStore, error) {
	dir := filepath.Join(dataDir, "certs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &CertStore{ca: ca, dir: dir, cache: map[string]*tls.Certificate{}}, nil
}

// For returns a leaf certificate for the given hostname, minting one
// (and caching it) if none exists or the cached one is expiring.
func (s *CertStore) For(hostname string) (*tls.Certificate, error) {
	hostname = strings.ToLower(hostname)
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.cache[hostname]; ok {
		if leaf, _ := x509.ParseCertificate(c.Certificate[0]); leaf != nil && time.Until(leaf.NotAfter) > 7*24*time.Hour {
			return c, nil
		}
	}
	certPath, keyPath := s.paths(hostname)
	if certPEM, err := os.ReadFile(certPath); err == nil {
		if keyPEM, err := os.ReadFile(keyPath); err == nil {
			if kp, err := tls.X509KeyPair(certPEM, keyPEM); err == nil {
				if leaf, _ := x509.ParseCertificate(kp.Certificate[0]); leaf != nil && time.Until(leaf.NotAfter) > 7*24*time.Hour {
					s.cache[hostname] = &kp
					return &kp, nil
				}
			}
		}
	}
	certPEM, keyPEM, err := s.ca.SignLeaf(hostname)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(certPath, certPEM, 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return nil, err
	}
	kp, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}
	s.cache[hostname] = &kp
	return &kp, nil
}

// Purge removes the on-disk cert for a hostname.
func (s *CertStore) Purge(hostname string) error {
	hostname = strings.ToLower(hostname)
	s.mu.Lock()
	delete(s.cache, hostname)
	s.mu.Unlock()
	certPath, keyPath := s.paths(hostname)
	_ = os.Remove(certPath)
	_ = os.Remove(keyPath)
	return nil
}

func (s *CertStore) paths(hostname string) (string, string) {
	safe := strings.ReplaceAll(hostname, string(filepath.Separator), "_")
	safe = strings.ReplaceAll(safe, "..", "_")
	return filepath.Join(s.dir, safe+".pem"), filepath.Join(s.dir, safe+".key")
}
