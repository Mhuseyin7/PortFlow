package registry

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func withMigrations(t *testing.T, extra ...migration) {
	t.Helper()
	old := migrations
	t.Cleanup(func() { migrations = old })
	migrations = append(append([]migration{}, old...), extra...)
}

func seed(t *testing.T, dir string) {
	t.Helper()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(Service{Hostname: "a.test", TargetHost: "127.0.0.1", TargetPort: 1}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestMigrationBacksUpAndUpgrades(t *testing.T) {
	dir := t.TempDir()
	seed(t, dir)
	withMigrations(t, migration{version: 2, statements: []string{
		`ALTER TABLE services ADD COLUMN note TEXT NOT NULL DEFAULT ''`,
	}})

	s, err := Open(dir)
	if err != nil {
		t.Fatalf("open after adding migration: %v", err)
	}
	defer s.Close()

	if _, err := os.Stat(filepath.Join(dir, "registry.db.bak-v1")); err != nil {
		t.Fatalf("expected backup of v1 database: %v", err)
	}
	if v, _ := s.schemaVersion(); v != 2 {
		t.Fatalf("schema version = %d, want 2", v)
	}
	var note string
	if err := s.db.QueryRow(`SELECT note FROM services WHERE hostname = 'a.test'`).Scan(&note); err != nil {
		t.Fatalf("new column missing: %v", err)
	}
	if _, err := s.GetByHostname("a.test"); err != nil {
		t.Fatalf("data lost across migration: %v", err)
	}
}

func TestFailedMigrationRollsBack(t *testing.T) {
	dir := t.TempDir()
	seed(t, dir)

	old := migrations
	migrations = append(append([]migration{}, old...), migration{version: 2, statements: []string{
		`ALTER TABLE services ADD COLUMN ok_col TEXT`,
		`ALTER TABLE does_not_exist ADD COLUMN x TEXT`,
	}})
	if _, err := Open(dir); err == nil {
		migrations = old
		t.Fatal("expected migration failure")
	}
	migrations = old

	s, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen after failed migration: %v", err)
	}
	defer s.Close()
	if v, _ := s.schemaVersion(); v != 1 {
		t.Fatalf("schema version = %d, want 1 (rolled back)", v)
	}
	if _, err := s.db.Exec(`SELECT ok_col FROM services`); err == nil {
		t.Fatal("partial migration leaked: ok_col exists")
	}
}

func TestNewerSchemaRefused(t *testing.T) {
	dir := t.TempDir()
	seed(t, dir)
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`DELETE FROM schema_version`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO schema_version(version) VALUES (99)`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if _, err := Open(dir); err == nil {
		t.Fatal("expected refusal for newer schema")
	}
}

func TestLegacyV010DatabaseOpens(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "registry.db"))
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`CREATE TABLE schema_version (version INTEGER PRIMARY KEY)`,
		`CREATE TABLE services (
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
		`INSERT INTO schema_version(version) VALUES (1)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC()
	if _, err := db.Exec(`INSERT INTO services(hostname, target_host, target_port, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"legacy.test", "127.0.0.1", 80, now, now); err != nil {
		t.Fatal(err)
	}
	db.Close()

	s, err := Open(dir)
	if err != nil {
		t.Fatalf("legacy db should open: %v", err)
	}
	defer s.Close()
	if _, err := s.GetByHostname("legacy.test"); err != nil {
		t.Fatalf("legacy row not loaded: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "registry.db.bak-v1")); err == nil {
		t.Fatal("no backup expected when already at latest version")
	}
}

func TestListSortedAndStatusCached(t *testing.T) {
	s := openTemp(t)
	for _, h := range []string{"b.test", "a.test"} {
		if _, err := s.Create(Service{Hostname: h, TargetHost: "127.0.0.1", TargetPort: 1, Project: "p"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.UpdateStatus("a.test", StatusUp); err != nil {
		t.Fatal(err)
	}
	list, _ := s.List()
	if len(list) != 2 || list[0].Hostname != "a.test" || list[0].Status != StatusUp {
		t.Fatalf("unexpected list: %+v", list)
	}
	if err := s.UpdateStatus("missing.test", StatusUp); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
