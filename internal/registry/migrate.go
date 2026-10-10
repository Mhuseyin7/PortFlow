package registry

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
)

type migration struct {
	version    int
	statements []string
}

// migrations is append-only. Never edit a released migration; add a new one.
var migrations = []migration{
	{version: 1, statements: []string{
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
		)`,
		`CREATE INDEX IF NOT EXISTS idx_services_project ON services(project)`,
	}},
}

func latestVersion() int { return migrations[len(migrations)-1].version }

func (s *Store) schemaVersion() (int, error) {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER PRIMARY KEY)`); err != nil {
		return 0, err
	}
	var v sql.NullInt64
	if err := s.db.QueryRow(`SELECT MAX(version) FROM schema_version`).Scan(&v); err != nil {
		return 0, err
	}
	return int(v.Int64), nil
}

// migrate brings the database up to the latest schema. Existing databases are
// copied to registry.db.bak-v<N> first, each migration runs in its own
// transaction, and a database written by a newer PortFlow is refused rather
// than silently downgraded.
func (s *Store) migrate() error {
	cur, err := s.schemaVersion()
	if err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	latest := latestVersion()
	if cur > latest {
		return fmt.Errorf("registry schema v%d is newer than this PortFlow supports (v%d); upgrade PortFlow", cur, latest)
	}
	if cur == latest {
		return nil
	}
	if cur > 0 {
		if err := s.backup(cur); err != nil {
			return fmt.Errorf("back up registry before migrating: %w", err)
		}
	}
	for _, m := range migrations {
		if m.version <= cur {
			continue
		}
		if err := s.apply(m); err != nil {
			return fmt.Errorf("migrate to schema v%d: %w", m.version, err)
		}
	}
	return nil
}

func (s *Store) apply(m migration) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range m.statements {
		if _, err := tx.Exec(q); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`DELETE FROM schema_version`); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO schema_version(version) VALUES (?)`, m.version); err != nil {
		return err
	}
	return tx.Commit()
}

// backup writes a consistent copy of the database (safe in WAL mode).
func (s *Store) backup(fromVersion int) error {
	dst := fmt.Sprintf("%s.bak-v%d", s.path, fromVersion)
	if err := os.Remove(dst); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	_, err := s.db.Exec("VACUUM INTO '" + strings.ReplaceAll(dst, "'", "''") + "'")
	return err
}
