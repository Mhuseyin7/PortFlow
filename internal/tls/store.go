package tls

import (
	"crypto/tls"
	"crypto/x509"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/portflow/portflow/internal/config"
	"github.com/portflow/portflow/internal/hostname"
)

// renewBefore is how long before expiry a leaf certificate is replaced.
// It is a variable so tests can force rotation.
var renewBefore = 7 * 24 * time.Hour

type cacheEntry struct {
	cert     *tls.Certificate
	notAfter time.Time
}

// CertStore caches leaf certificates in memory and on disk and rotates them
// shortly before they expire.
type CertStore struct {
	ca  *CA
	dir string

	mu    sync.RWMutex
	cache map[string]cacheEntry
}

// NewCertStore returns a store rooted at dataDir/certs.
func NewCertStore(ca *CA, dataDir string) (*CertStore, error) {
	dir := filepath.Join(dataDir, "certs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &CertStore{ca: ca, dir: dir, cache: map[string]cacheEntry{}}, nil
}

func fresh(e cacheEntry) bool { return time.Until(e.notAfter) > renewBefore }

// For returns a leaf certificate for host, minting one if none exists, the
// cached one is about to expire, or it was issued by a different CA.
func (s *CertStore) For(host string) (*tls.Certificate, error) {
	host = hostname.Normalize(host)
	if err := hostname.Validate(host); err != nil {
		return nil, err
	}
	s.mu.RLock()
	e, ok := s.cache[host]
	s.mu.RUnlock()
	if ok && fresh(e) {
		return e.cert, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.cache[host]; ok && fresh(e) {
		return e.cert, nil
	}
	if e, ok := s.loadFromDisk(host); ok && fresh(e) {
		s.cache[host] = e
		return e.cert, nil
	}
	certPEM, keyPEM, err := s.ca.SignLeaf(host)
	if err != nil {
		return nil, err
	}
	certPath, keyPath := s.leafPaths(host)
	if err := config.AtomicWrite(keyPath, keyPEM, 0o600); err != nil {
		return nil, err
	}
	if err := config.AtomicWrite(certPath, certPEM, 0o644); err != nil {
		return nil, err
	}
	e, err = parsePair(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}
	s.cache[host] = e
	return e.cert, nil
}

func parsePair(certPEM, keyPEM []byte) (cacheEntry, error) {
	kp, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return cacheEntry{}, err
	}
	leaf, err := x509.ParseCertificate(kp.Certificate[0])
	if err != nil {
		return cacheEntry{}, err
	}
	kp.Leaf = leaf
	return cacheEntry{cert: &kp, notAfter: leaf.NotAfter}, nil
}

// loadFromDisk returns a previously issued leaf, but only if it names host
// and chains to the current CA (the CA may have been regenerated).
func (s *CertStore) loadFromDisk(host string) (cacheEntry, bool) {
	certPath, keyPath := s.leafPaths(host)
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return cacheEntry{}, false
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return cacheEntry{}, false
	}
	e, err := parsePair(certPEM, keyPEM)
	if err != nil {
		return cacheEntry{}, false
	}
	leaf := e.cert.Leaf
	if leaf.VerifyHostname(host) != nil || leaf.CheckSignatureFrom(s.ca.cert) != nil {
		return cacheEntry{}, false
	}
	return e, true
}

// Purge removes the cached and on-disk certificate for a hostname.
func (s *CertStore) Purge(host string) error {
	host = hostname.Normalize(host)
	if err := hostname.Validate(host); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.cache, host)
	s.mu.Unlock()
	certPath, keyPath := s.leafPaths(host)
	_ = os.Remove(certPath)
	_ = os.Remove(keyPath)
	return nil
}

// leafPaths maps an already-validated hostname to its files. The "leaf-"
// prefix keeps names like "con.test" or "nul.test" from colliding with
// reserved device names on Windows.
func (s *CertStore) leafPaths(host string) (certPath, keyPath string) {
	base := filepath.Join(s.dir, "leaf-"+host)
	return base + ".pem", base + ".key"
}
