package hosts

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func newHosts(t *testing.T, content string) (*Manager, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "hosts")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return NewManagerAt(path), path
}

func TestConcurrentAddsDoNotLoseEntries(t *testing.T) {
	m, _ := newHosts(t, "")
	const n = 25
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := m.Add(fmt.Sprintf("svc%d.shop.test", i)); err != nil {
				t.Errorf("add %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	got, err := m.Current()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != n {
		t.Fatalf("lost updates: have %d of %d entries", len(got), n)
	}
}

func TestInvalidNamesNeverReachTheFile(t *testing.T) {
	m, path := newHosts(t, "10.0.0.1 keep.me\n")
	for _, bad := range []string{
		"evil.test\n127.0.0.1 google.com",
		"a b.test",
		"",
		"nodot",
		"../x.test",
	} {
		if err := m.Add(bad); err == nil {
			t.Errorf("Add(%q) should fail", bad)
		}
	}
	b, _ := os.ReadFile(path)
	if string(b) != "10.0.0.1 keep.me\n" {
		t.Fatalf("file was modified by rejected input:\n%s", b)
	}
}

func TestUnterminatedBlockIsRefused(t *testing.T) {
	orig := "127.0.0.1 localhost\n" + BeginMarker + "\n127.0.0.1\ta.test\n"
	m, path := newHosts(t, orig)
	if err := m.Add("b.test"); !errors.Is(err, ErrUnterminatedBlock) {
		t.Fatalf("want ErrUnterminatedBlock, got %v", err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != orig {
		t.Fatal("file must be left untouched")
	}
}

func TestMarkersOnlyMatchWholeLines(t *testing.T) {
	orig := "# note: " + BeginMarker + " was mentioned here\n127.0.0.1 localhost\n"
	m, path := newHosts(t, orig)
	if err := m.Add("a.test"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(b), orig) {
		t.Fatalf("user comment was treated as a marker:\n%s", b)
	}
}

func TestCRLFFileKeepsUserLines(t *testing.T) {
	m, path := newHosts(t, "127.0.0.1 localhost\r\n::1 localhost\r\n")
	if err := m.Add("a.test"); err != nil {
		t.Fatal(err)
	}
	if err := m.Remove("a.test"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "127.0.0.1 localhost") || !strings.Contains(string(b), "::1 localhost") {
		t.Fatalf("user lines lost:\n%q", b)
	}
	if strings.Contains(string(b), "a.test") {
		t.Fatalf("entry not removed:\n%q", b)
	}
}

func TestSetDoesNotMutateCallerSliceAndDedupes(t *testing.T) {
	m, _ := newHosts(t, "")
	in := []string{"b.test", "A.test", "b.test"}
	if err := m.Set(in); err != nil {
		t.Fatal(err)
	}
	if in[0] != "b.test" || in[1] != "A.test" {
		t.Fatalf("caller slice mutated: %v", in)
	}
	got, _ := m.Current()
	if len(got) != 2 || got[0] != "a.test" || got[1] != "b.test" {
		t.Fatalf("got %v", got)
	}
}

func TestRemovingLastEntryRemovesBlock(t *testing.T) {
	m, path := newHosts(t, "127.0.0.1 localhost\n")
	_ = m.Add("a.test")
	_ = m.Remove("a.test")
	b, _ := os.ReadFile(path)
	if strings.Contains(string(b), "PortFlow") {
		t.Fatalf("empty managed block should be removed:\n%s", b)
	}
}

func TestWritable(t *testing.T) {
	m, _ := newHosts(t, "")
	if err := m.Writable(); err != nil {
		t.Fatal(err)
	}
	if err := NewManagerAt(filepath.Join(t.TempDir(), "missing")).Writable(); err == nil {
		t.Fatal("missing file should not be writable")
	}
}
