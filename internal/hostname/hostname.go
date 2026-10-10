// Package hostname is PortFlow's single source of truth for what counts as an
// acceptable local development hostname, project name and loopback target.
package hostname

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

// DefaultSuffixes are the only top-level labels PortFlow will register by
// default. They are reserved for local/testing use (RFC 6761 / RFC 6762), so
// a registered name can never shadow a real public website in the hosts file
// or receive a certificate for one. ".local" collides with mDNS on some
// systems; see docs/SECURITY.md.
var DefaultSuffixes = []string{"test", "localhost", "local"}

// Normalize lowercases and trims whitespace and one trailing root dot.
func Normalize(h string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(h)), ".")
}

// Validate checks RFC 1123 syntax for an already-normalized hostname with at
// least two labels.
func Validate(h string) error {
	if h == "" {
		return errors.New("hostname required")
	}
	if len(h) > 253 {
		return errors.New("hostname too long")
	}
	labels := strings.Split(h, ".")
	if len(labels) < 2 {
		return errors.New("hostname needs at least two labels, e.g. app.shop.test")
	}
	for _, l := range labels {
		if err := validLabel(l); err != nil {
			return fmt.Errorf("invalid hostname %q: %w", h, err)
		}
	}
	return nil
}

// ValidateProject checks a project group name. The empty string is allowed
// and means "no project".
func ValidateProject(p string) error {
	if p == "" {
		return nil
	}
	if err := validLabel(p); err != nil {
		return fmt.Errorf("invalid project name %q: %w", p, err)
	}
	return nil
}

func validLabel(l string) error {
	if l == "" {
		return errors.New("empty label")
	}
	if len(l) > 63 {
		return errors.New("label longer than 63 characters")
	}
	if l[0] == '-' || l[len(l)-1] == '-' {
		return errors.New("label starts or ends with '-'")
	}
	for i := 0; i < len(l); i++ {
		c := l[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-':
		default:
			return fmt.Errorf("illegal character %q", c)
		}
	}
	return nil
}

// AllowedSuffix reports whether h ends in one of suffixes (DefaultSuffixes
// when suffixes is empty). h must be normalized.
func AllowedSuffix(h string, suffixes []string) bool {
	if len(suffixes) == 0 {
		suffixes = DefaultSuffixes
	}
	for _, s := range suffixes {
		s = strings.TrimPrefix(strings.ToLower(s), ".")
		if s != "" && strings.HasSuffix(h, "."+s) {
			return true
		}
	}
	return false
}

// IsLoopbackHost reports whether h is a literal loopback address or the name
// "localhost". It deliberately does not resolve DNS: a name that merely
// resolves to loopback today could resolve elsewhere tomorrow.
func IsLoopbackHost(h string) bool {
	h = strings.ToLower(strings.Trim(strings.TrimSpace(h), "[]"))
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// Slug converts arbitrary text (a directory name, a container name) into a
// single valid DNS label, or "" if nothing usable remains.
func Slug(s string) string {
	var b strings.Builder
	dash := true // swallow leading separators
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		default:
			if !dash {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	out := strings.TrimRight(b.String(), "-")
	if len(out) > 63 {
		out = strings.TrimRight(out[:63], "-")
	}
	return out
}
