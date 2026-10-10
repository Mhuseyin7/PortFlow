// Package hosts is PortFlow's OS hosts-file adapter. It writes a single
// managed block delimited by BeginMarker/EndMarker so we can add/remove
// PortFlow entries without disturbing user-authored lines.
//
// The hosts file is a shared OS resource; on most systems editing it needs
// admin/root. PortFlow keeps changes minimal, atomic, and always inside a
// clearly marked block so a bad state is trivial to inspect or revert.
package hosts

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/portflow/portflow/internal/hostname"
)

// Markers are ASCII-only on purpose: some tools read the hosts file in a
// legacy code page.
const (
	BeginMarker = "# BEGIN PortFlow (managed) - do not edit inside this block"
	EndMarker   = "# END PortFlow"
)

// ErrUnterminatedBlock means the begin marker exists without an end marker.
// PortFlow refuses to guess which lines belong to it.
var ErrUnterminatedBlock = errors.New("hosts file has a PortFlow begin marker without an end marker; fix it by hand")

// Manager coordinates edits to the hosts file.
type Manager struct {
	mu   sync.Mutex
	path string
}

// NewManager returns a Manager rooted at the platform default hosts file
// (see hosts_*.go for the OS-specific path).
func NewManager() *Manager {
	return &Manager{path: defaultHostsPath()}
}

// NewManagerAt returns a Manager rooted at a specific path (useful for tests).
func NewManagerAt(path string) *Manager {
	return &Manager{path: path}
}

// Path returns the hosts file path this manager writes.
func (m *Manager) Path() string { return m.path }

// Set replaces PortFlow's managed block with entries for the given hostnames,
// all pointing at 127.0.0.1 and ::1. Invalid names are rejected, never written.
func (m *Manager) Set(hostnames []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.set(hostnames)
}

// Add adds one hostname to the managed block (idempotent).
func (m *Manager) Add(host string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, err := m.current()
	if err != nil {
		return err
	}
	return m.set(append(cur, host))
}

// Remove drops one hostname from the managed block.
func (m *Manager) Remove(host string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, err := m.current()
	if err != nil {
		return err
	}
	host = hostname.Normalize(host)
	out := make([]string, 0, len(cur))
	for _, h := range cur {
		if h != host {
			out = append(out, h)
		}
	}
	return m.set(out)
}

// Current returns the hostnames PortFlow currently manages.
func (m *Manager) Current() ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.current()
}

// Writable reports whether the hosts file can be opened for writing, without
// modifying it. Used by `portflow doctor`.
func (m *Manager) Writable() error {
	f, err := os.OpenFile(m.path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	return f.Close()
}

func (m *Manager) current() ([]string, error) {
	raw, err := os.ReadFile(m.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	_, block, _, err := splitBlock(raw)
	if err != nil || block == nil {
		return nil, err
	}
	var out []string
	seen := map[string]bool{}
	scanner := bufio.NewScanner(bytes.NewReader(block))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		for _, name := range fields[1:] {
			if !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

func (m *Manager) set(hostnames []string) error {
	names, err := cleanNames(hostnames)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(m.path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	before, _, after, err := splitBlock(raw)
	if err != nil {
		return err
	}
	out := joinParts(before, renderBlock(names), after)
	if bytes.Equal(out, raw) {
		return nil
	}
	return atomicWriteHosts(m.path, out)
}

// cleanNames normalizes, validates, de-duplicates and sorts without touching
// the caller's slice.
func cleanNames(in []string) ([]string, error) {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, h := range in {
		h = hostname.Normalize(h)
		if err := hostname.Validate(h); err != nil {
			return nil, fmt.Errorf("refusing to write %q to the hosts file: %w", h, err)
		}
		if !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	sort.Strings(out)
	return out, nil
}

func renderBlock(names []string) []byte {
	if len(names) == 0 {
		return nil
	}
	var buf bytes.Buffer
	buf.WriteString(BeginMarker + "\n")
	for _, h := range names {
		fmt.Fprintf(&buf, "127.0.0.1\t%s\n", h)
		fmt.Fprintf(&buf, "::1\t%s\n", h)
	}
	buf.WriteString(EndMarker + "\n")
	return buf.Bytes()
}

// splitBlock returns (before, block, after); block includes both marker
// lines. Markers only count when they are a whole line.
func splitBlock(raw []byte) (before, block, after []byte, err error) {
	begin, end := -1, -1
	for off := 0; off < len(raw); {
		next := len(raw)
		lineEnd := len(raw)
		if nl := bytes.IndexByte(raw[off:], '\n'); nl >= 0 {
			lineEnd = off + nl
			next = lineEnd + 1
		}
		line := strings.TrimRight(string(raw[off:lineEnd]), " \t\r")
		switch {
		case begin < 0 && line == BeginMarker:
			begin = off
		case begin >= 0 && line == EndMarker:
			end = next
		}
		if end >= 0 {
			break
		}
		off = next
	}
	if begin < 0 {
		return raw, nil, nil, nil
	}
	if end < 0 {
		return nil, nil, nil, ErrUnterminatedBlock
	}
	return raw[:begin], raw[begin:end], raw[end:], nil
}

func joinParts(prefix, block, suffix []byte) []byte {
	var buf bytes.Buffer
	if len(prefix) > 0 {
		buf.Write(prefix)
		if !bytes.HasSuffix(prefix, []byte("\n")) {
			buf.WriteByte('\n')
		}
	}
	buf.Write(block)
	buf.Write(suffix)
	return buf.Bytes()
}
