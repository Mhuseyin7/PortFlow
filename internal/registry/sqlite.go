// Package registry is PortFlow's authoritative service store (SQLite).
//
// SQLite is the durable source of truth; an in-memory index serves hostname
// lookups so the proxy's request path never touches the database.
package registry

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// ErrNotFound is returned when a service is missing.
var ErrNotFound = errors.New("service not found")

// ErrDuplicate is returned when a hostname is registered twice.
var ErrDuplicate = errors.New("hostname already registered")

// Store is PortFlow's SQLite-backed registry.
type Store struct {
	db   *sql.DB
	path string

	mu    sync.RWMutex // guards cache and serializes writers
	cache map[string]Service
}

// Open opens (or creates) the SQLite registry under dataDir.
func Open(dataDir string) (*Store, error) {
	path := filepath.Join(dataDir, "registry.db")
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, path: path}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	if err := s.loadCache(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

const serviceColumns = `id, project, hostname, target_host, target_port, protocol, tls, status, created_at, updated_at`

func (s *Store) loadCache() error {
	list, err := s.queryAll()
	if err != nil {
		return err
	}
	cache := make(map[string]Service, len(list))
	for _, sv := range list {
		cache[sv.Hostname] = sv
	}
	s.mu.Lock()
	s.cache = cache
	s.mu.Unlock()
	return nil
}

func (s *Store) queryAll() ([]Service, error) {
	rows, err := s.db.Query(`SELECT ` + serviceColumns + ` FROM services ORDER BY project, hostname`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Service
	for rows.Next() {
		var sv Service
		var tls int
		if err := rows.Scan(&sv.ID, &sv.Project, &sv.Hostname, &sv.TargetHost, &sv.TargetPort, &sv.Protocol, &tls, &sv.Status, &sv.CreatedAt, &sv.UpdatedAt); err != nil {
			return nil, err
		}
		sv.TLS = tls != 0
		out = append(out, sv)
	}
	return out, rows.Err()
}

// Close closes the underlying DB.
func (s *Store) Close() error { return s.db.Close() }

// List returns all services ordered by project then hostname.
func (s *Store) List() ([]Service, error) {
	s.mu.RLock()
	out := make([]Service, 0, len(s.cache))
	for _, sv := range s.cache {
		out = append(out, sv)
	}
	s.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].Project != out[j].Project {
			return out[i].Project < out[j].Project
		}
		return out[i].Hostname < out[j].Hostname
	})
	return out, nil
}

// GetByHostname returns one service by exact hostname (case-insensitive).
// It is served from memory and safe to call on every proxied request.
func (s *Store) GetByHostname(host string) (*Service, error) {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	s.mu.RLock()
	sv, ok := s.cache[host]
	s.mu.RUnlock()
	if !ok {
		return nil, ErrNotFound
	}
	return &sv, nil
}

// Create inserts a new service. Fails with ErrDuplicate on hostname conflict.
func (s *Store) Create(sv Service) (*Service, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sv.Hostname = strings.TrimSuffix(strings.ToLower(sv.Hostname), ".")
	if sv.Protocol == "" {
		sv.Protocol = "http"
	}
	if sv.Status == "" {
		sv.Status = StatusUnknown
	}
	now := time.Now().UTC()
	sv.CreatedAt = now
	sv.UpdatedAt = now
	tls := 0
	if sv.TLS {
		tls = 1
	}
	res, err := s.db.Exec(
		`INSERT INTO services(project, hostname, target_host, target_port, protocol, tls, status, created_at, updated_at)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sv.Project, sv.Hostname, sv.TargetHost, sv.TargetPort, sv.Protocol, tls, sv.Status, sv.CreatedAt, sv.UpdatedAt,
	)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil, ErrDuplicate
		}
		return nil, err
	}
	sv.ID, _ = res.LastInsertId()
	s.cache[sv.Hostname] = sv
	out := sv
	return &out, nil
}

// Delete removes a service by hostname.
func (s *Store) Delete(hostname string) error {
	hostname = strings.TrimSuffix(strings.ToLower(hostname), ".")
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(`DELETE FROM services WHERE hostname = ?`, hostname)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	delete(s.cache, hostname)
	return nil
}

// UpdateStatus sets the current health status on a service.
func (s *Store) UpdateStatus(hostname, status string) error {
	hostname = strings.TrimSuffix(strings.ToLower(hostname), ".")
	s.mu.Lock()
	defer s.mu.Unlock()
	sv, ok := s.cache[hostname]
	if !ok {
		return ErrNotFound
	}
	now := time.Now().UTC()
	if _, err := s.db.Exec(`UPDATE services SET status = ?, updated_at = ? WHERE hostname = ?`, status, now, hostname); err != nil {
		return fmt.Errorf("update status: %w", err)
	}
	sv.Status = status
	sv.UpdatedAt = now
	s.cache[hostname] = sv
	return nil
}
