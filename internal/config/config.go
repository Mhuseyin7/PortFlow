// Package config resolves PortFlow's data directory and reads/writes
// the daemon-level config. Project-level portflow.yml lives in project.go.
package config

import (
	"errors"
	"os"
	"path/filepath"
)

// ResolveDataDir returns the effective data directory (~/.portflow by
// default, PORTFLOW_HOME or override when set) without creating anything, so
// read-only commands like `portflow doctor` have no side effects.
func ResolveDataDir(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	if env := os.Getenv("PORTFLOW_HOME"); env != "" {
		return env, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if home == "" {
		return "", errors.New("could not resolve home directory")
	}
	return filepath.Join(home, ".portflow"), nil
}

// DataDir resolves the data directory and creates it (mode 0700) if needed.
func DataDir(override string) (string, error) {
	dir, err := ResolveDataDir(override)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// AtomicWrite writes data to path via a temp file + rename.
func AtomicWrite(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".pf-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
