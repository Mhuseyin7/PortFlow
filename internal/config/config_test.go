package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveDataDirHasNoSideEffects(t *testing.T) {
	target := filepath.Join(t.TempDir(), "pf-home")
	got, err := ResolveDataDir(target)
	if err != nil || got != target {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := os.Stat(target); err == nil {
		t.Fatal("ResolveDataDir must not create the directory")
	}
}

func TestDataDirCreatesPrivateDirectory(t *testing.T) {
	target := filepath.Join(t.TempDir(), "pf-home")
	got, err := DataDir(target)
	if err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(got); err != nil || !fi.IsDir() {
		t.Fatalf("directory not created: %v", err)
	}
}

func TestDataDirHonoursEnvironment(t *testing.T) {
	env := filepath.Join(t.TempDir(), "from-env")
	t.Setenv("PORTFLOW_HOME", env)
	got, err := ResolveDataDir("")
	if err != nil || got != env {
		t.Fatalf("got %q, %v", got, err)
	}
	other := filepath.Join(t.TempDir(), "flag")
	if got, _ := ResolveDataDir(other); got != other {
		t.Fatal("explicit override must win over the environment")
	}
}

func TestAtomicWriteReplacesAtomically(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "f.txt")
	if err := AtomicWrite(path, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWrite(path, []byte("two"), 0o600); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "two" {
		t.Fatalf("got %q", b)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("temp files left behind: %v", entries)
	}
}
