package hosts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetAndCurrent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hosts")
	if err := os.WriteFile(path, []byte("127.0.0.1 already-there\n# comment\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewManagerAt(path)
	if err := m.Set([]string{"a.test", "b.test"}); err != nil {
		t.Fatal(err)
	}
	got, err := m.Current()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "a.test" || got[1] != "b.test" {
		t.Fatalf("got %v", got)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "127.0.0.1\ta.test") {
		t.Fatalf("missing ipv4 entry:\n%s", b)
	}
	if !strings.Contains(string(b), "::1\ta.test") {
		t.Fatalf("missing ipv6 entry:\n%s", b)
	}
	if !strings.Contains(string(b), "already-there") {
		t.Fatalf("existing lines were destroyed:\n%s", b)
	}
}

func TestAddRemove(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hosts")
	os.WriteFile(path, []byte{}, 0o644)
	m := NewManagerAt(path)
	if err := m.Add("x.test"); err != nil {
		t.Fatal(err)
	}
	if err := m.Add("x.test"); err != nil { // idempotent
		t.Fatal(err)
	}
	if got, _ := m.Current(); len(got) != 1 {
		t.Fatalf("want 1 got %v", got)
	}
	if err := m.Remove("x.test"); err != nil {
		t.Fatal(err)
	}
	if got, _ := m.Current(); len(got) != 0 {
		t.Fatalf("want 0 got %v", got)
	}
}

func TestBlockIsolatedFromUserContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hosts")
	original := "10.0.0.1 my-own.thing\n"
	os.WriteFile(path, []byte(original), 0o644)
	m := NewManagerAt(path)
	if err := m.Set([]string{"a.test"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Set([]string{"b.test"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(b), original) {
		t.Fatalf("user block should stay at top:\n%s", b)
	}
	if strings.Contains(string(b), "a.test") {
		t.Fatalf("previous PortFlow block leaked:\n%s", b)
	}
}
