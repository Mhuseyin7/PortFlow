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
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
)

const (
	BeginMarker = "# BEGIN PortFlow (managed) — do not edit inside this block"
	EndMarker   = "# END PortFlow"
)

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
// all pointing at 127.0.0.1 and ::1.
func (m *Manager) Set(hostnames []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	raw, err := os.ReadFile(m.path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	prefix, suffix := splitAroundBlock(raw)
	block := renderBlock(hostnames)
	out := joinParts(prefix, block, suffix)
	return atomicWriteHosts(m.path, out)
}

// Add is a convenience that reads current entries and adds one.
func (m *Manager) Add(hostname string) error {
	current, err := m.Current()
	if err != nil {
		return err
	}
	for _, h := range current {
		if strings.EqualFold(h, hostname) {
			return nil
		}
	}
	return m.Set(append(current, hostname))
}

// Remove is a convenience that reads current entries and drops one.
func (m *Manager) Remove(hostname string) error {
	current, err := m.Current()
	if err != nil {
		return err
	}
	out := make([]string, 0, len(current))
	for _, h := range current {
		if !strings.EqualFold(h, hostname) {
			out = append(out, h)
		}
	}
	return m.Set(out)
}

// Current returns the hostnames PortFlow currently manages.
func (m *Manager) Current() ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	raw, err := os.ReadFile(m.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	_, block, _ := splitBlock(raw)
	if block == nil {
		return nil, nil
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
		if len(fields) < 2 {
			continue
		}
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

func renderBlock(hostnames []string) []byte {
	if len(hostnames) == 0 {
		return nil
	}
	sort.Strings(hostnames)
	var buf bytes.Buffer
	buf.WriteString(BeginMarker + "\n")
	for _, h := range hostnames {
		fmt.Fprintf(&buf, "127.0.0.1\t%s\n", h)
		fmt.Fprintf(&buf, "::1\t%s\n", h)
	}
	buf.WriteString(EndMarker + "\n")
	return buf.Bytes()
}

// splitBlock returns (before, block, after) — block includes markers.
func splitBlock(raw []byte) (before, block, after []byte) {
	beginIdx := bytes.Index(raw, []byte(BeginMarker))
	if beginIdx < 0 {
		return raw, nil, nil
	}
	endIdx := bytes.Index(raw[beginIdx:], []byte(EndMarker))
	if endIdx < 0 {
		return raw, nil, nil
	}
	endIdx += beginIdx + len(EndMarker)
	if endIdx < len(raw) && raw[endIdx] == '\n' {
		endIdx++
	}
	return raw[:beginIdx], raw[beginIdx:endIdx], raw[endIdx:]
}

// splitAroundBlock returns (before, after) with any existing block removed.
func splitAroundBlock(raw []byte) (before, after []byte) {
	b, _, a := splitBlock(raw)
	return b, a
}

func joinParts(prefix, block, suffix []byte) []byte {
	var buf bytes.Buffer
	if len(prefix) > 0 {
		buf.Write(prefix)
		if !bytes.HasSuffix(prefix, []byte("\n")) {
			buf.WriteByte('\n')
		}
	}
	if len(block) > 0 {
		buf.Write(block)
	}
	if len(suffix) > 0 {
		buf.Write(suffix)
	}
	return buf.Bytes()
}
