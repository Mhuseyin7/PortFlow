// Package registry is PortFlow's authoritative service store (SQLite).
package registry

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
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
	db *sql.DB
	mu sync.Mutex
}

// Open opens (or creates) the SQLite registry under dataDir.
func Open(dataDir string) (*Store, error) {
	path := filepath.Join(dataDir, "registry.db")
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER PRIMARY KEY);`,
		`CREATE TABLE IF NOT EXISTS services (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project TEXT NOT NULL DEFAULT '',
			hostname TEXT NOT NULL UNIQUE,
			target_host TEXT NOT NULL,
			target_port INTEGER NOT NULL,
			protocol TEXT NOT NULL DEFAULT 'http',
			tls INTEGER NOT NULL DEFAULT 1,
			status TEXT NOT NULL DEFAULT 'UNKNOWN',
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_services_project ON services(project);`,
		`INSERT OR IGNORE INTO schema_version(version) VALUES (1);`,
	}
	for _, q := range stmts {
		if _, err := s.db.Exec(q); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return nil
}

// Close closes the underlying DB.
func (s *Store) Close() error { return s.db.Close() }

// List returns all services.
func (s *Store) List() ([]Service, error) {
	rows, err := s.db.Query(`SELECT id, project, hostname, target_host, target_port, protocol, tls, status, created_at, updated_at FROM services ORDER BY project, hostname`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Service
	for rows.Next() {
		var s Service
		var tls int
		if err := rows.Scan(&s.ID, &s.Project, &s.Hostname, &s.TargetHost, &s.TargetPort, &s.Protocol, &tls, &s.Status, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		s.TLS = tls != 0
		out = append(out, s)
	}
	return out, rows.Err()
}

// GetByHostname returns one service by exact hostname (case-insensitive).
func (s *Store) GetByHostname(host string) (*Service, error) {
	host = strings.ToLower(host)
	row := s.db.QueryRow(`SELECT id, project, hostname, target_host, target_port, protocol, tls, status, created_at, updated_at FROM services WHERE hostname = ?`, host)
	var sv Service
	var tls int
	err := row.Scan(&sv.ID, &sv.Project, &sv.Hostname, &sv.TargetHost, &sv.TargetPort, &sv.Protocol, &tls, &sv.Status, &sv.CreatedAt, &sv.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	sv.TLS = tls != 0
	return &sv, nil
}

// Create inserts a new service. Fails with ErrDuplicate on hostname conflict.
func (s *Store) Create(sv Service) (*Service, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sv.Hostname = strings.ToLower(sv.Hostname)
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
	id, _ := res.LastInsertId()
	sv.ID = id
	return &sv, nil
}

// Delete removes a service by hostname.
func (s *Store) Delete(hostname string) error {
	res, err := s.db.Exec(`DELETE FROM services WHERE hostname = ?`, strings.ToLower(hostname))
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateStatus sets the current health status on a service.
func (s *Store) UpdateStatus(hostname, status string) error {
	_, err := s.db.Exec(`UPDATE services SET status = ?, updated_at = ? WHERE hostname = ?`,
		status, time.Now().UTC(), strings.ToLower(hostname))
	return err
}
