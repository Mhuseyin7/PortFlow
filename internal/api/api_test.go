package api

import (
	"testing"
)

func TestValidateHostname(t *testing.T) {
	cases := []struct {
		in   string
		fail bool
	}{
		{"app.shop.test", false},
		{"a-b.test", false},
		{"", true},
		{"app.shop.test/../etc/passwd", true},
		{"UPPER.test", false}, // will be lowercased
		{"foo..test", true},
		{".foo.test", true},
		{"foo bar.test", true},
	}
	for _, c := range cases {
		err := validateHostname(c.in)
		if (err != nil) != c.fail {
			t.Errorf("validateHostname(%q) err=%v want fail=%v", c.in, err, c.fail)
		}
	}
}
