package registry

import "fmt"

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
