package hostname

import (
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	long := strings.Repeat("a", 64) + ".test"
	cases := []struct {
		in   string
		fail bool
	}{
		{"app.shop.test", false},
		{"a-b.test", false},
		{"xn--e1afmkfd.test", false},
		{"", true},
		{"test", true},
		{"foo..test", true},
		{".foo.test", true},
		{"foo.test.", true}, // callers must Normalize first
		{"-a.test", true},
		{"a-.test", true},
		{"foo bar.test", true},
		{"under_score.test", true},
		{"app.shop.test/../etc/passwd", true},
		{"app.shop.test\n127.0.0.1 evil.com", true},
		{"ünicode.test", true},
		{long, true},
		{strings.Repeat("a.", 130) + "test", true},
	}
	for _, c := range cases {
		if err := Validate(c.in); (err != nil) != c.fail {
			t.Errorf("Validate(%q) err=%v want fail=%v", c.in, err, c.fail)
		}
	}
}

func TestNormalize(t *testing.T) {
	if got := Normalize("  API.Shop.TEST. "); got != "api.shop.test" {
		t.Fatalf("got %q", got)
	}
}

func TestAllowedSuffix(t *testing.T) {
	cases := map[string]bool{
		"app.shop.test":      true,
		"app.localhost":      true,
		"printer.local":      true,
		"google.com":         false,
		"test":               false,
		"evil.test.com":      false,
		"notest":             false,
		"app.shop.testing":   false,
		"github.com.test.io": false,
	}
	for h, want := range cases {
		if got := AllowedSuffix(h, nil); got != want {
			t.Errorf("AllowedSuffix(%q) = %v want %v", h, got, want)
		}
	}
	if !AllowedSuffix("app.dev", []string{".dev"}) {
		t.Error("custom suffix with leading dot should work")
	}
}

func TestIsLoopbackHost(t *testing.T) {
	yes := []string{"127.0.0.1", "127.9.9.9", "::1", "[::1]", "localhost", "LOCALHOST", "::ffff:127.0.0.1"}
	no := []string{"", "0.0.0.0", "10.0.0.1", "192.168.1.5", "169.254.169.254", "example.com", "127.0.0.1.evil.com", "localhost.evil.com"}
	for _, h := range yes {
		if !IsLoopbackHost(h) {
			t.Errorf("%q should be loopback", h)
		}
	}
	for _, h := range no {
		if IsLoopbackHost(h) {
			t.Errorf("%q must not be loopback", h)
		}
	}
}

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"Active Projects": "active-projects",
		"my_db":           "my-db",
		"--weird--":       "weird",
		"UPPER":           "upper",
		"!!!":             "",
		"a/b\\c":          "a-b-c",
	}
	for in, want := range cases {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q want %q", in, got, want)
		}
	}
	if got := Slug(strings.Repeat("x", 100)); len(got) != 63 {
		t.Errorf("slug length %d, want 63", len(got))
	}
}

func TestValidateProject(t *testing.T) {
	for _, ok := range []string{"", "shop", "my-app", "a1"} {
		if err := ValidateProject(ok); err != nil {
			t.Errorf("ValidateProject(%q): %v", ok, err)
		}
	}
	for _, bad := range []string{"<img src=x onerror=alert(1)>", "a b", "UPPER", "a.b", "-x"} {
		if err := ValidateProject(bad); err == nil {
			t.Errorf("ValidateProject(%q) should fail", bad)
		}
	}
}
