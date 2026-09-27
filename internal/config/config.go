// Package config resolves PortFlow's data directory and reads/writes
// the daemon-level config. Project-level portflow.yml lives in project.go.
package config

import (
	"errors"
	"os"
	"path/filepath"
)

// DataDir returns the effective data directory (~/.portflow by default).
// override, if non-empty, is used as-is.
func DataDir(override string) (string, error) {
	if override != "" {
		if err := os.MkdirAll(override, 0o700); err != nil {
			return "", err
		}
		return override, nil
	}
	if env := os.Getenv("PORTFLOW_HOME"); env != "" {
		if err := os.MkdirAll(env, 0o700); err != nil {
			return "", err
		}
		return env, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if home == "" {
		return "", errors.New("could not resolve home directory")
	}
	dir := filepath.Join(home, ".portflow")
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
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
