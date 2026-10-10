package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "portflow.yml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

const goodProject = `version: 1
project: shop
services:
  app:
    domain: app.shop.test
    port: 3000
  api:
    domain: API.shop.test
    port: 8000
    no_tls: true
`

func TestLoadProjectOK(t *testing.T) {
	p, err := LoadProject(writeTemp(t, goodProject))
	if err != nil {
		t.Fatal(err)
	}
	if p.Project != "shop" || len(p.Services) != 2 || !p.Services["api"].NoTLS {
		t.Fatalf("unexpected project: %+v", p)
	}
}

func TestLoadProjectRejects(t *testing.T) {
	cases := map[string]string{
		"empty file":      "",
		"bad version":     "version: 2\nproject: shop\nservices: {}\n",
		"no project":      "version: 1\nservices: {}\n",
		"bad project":     "version: 1\nproject: \"<script>\"\nservices: {}\n",
		"unknown key":     "version: 1\nproject: shop\nservices: {}\nevil: true\n",
		"unknown svc key": "version: 1\nproject: shop\nservices:\n  a:\n    domain: a.shop.test\n    port: 1\n    command: rm -rf /\n",
		"bad domain":      "version: 1\nproject: shop\nservices:\n  a:\n    domain: \"a.shop.test\\n127.0.0.1 evil.com\"\n    port: 1\n",
		"bad port":        "version: 1\nproject: shop\nservices:\n  a:\n    domain: a.shop.test\n    port: 70000\n",
		"missing domain":  "version: 1\nproject: shop\nservices:\n  a:\n    port: 80\n",
	}
	for name, content := range cases {
		if _, err := LoadProject(writeTemp(t, content)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestLoadProjectTooLarge(t *testing.T) {
	big := "version: 1\nproject: shop\nservices: {}\n# " + strings.Repeat("x", maxProjectFileSize)
	if _, err := LoadProject(writeTemp(t, big)); err == nil {
		t.Fatal("expected size error")
	}
}

func TestWriteProjectRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "portflow.yml")
	in := &Project{Version: 1, Project: "shop", Services: map[string]ProjectService{
		"app": {Domain: "app.shop.test", Port: 3000},
	}}
	if err := WriteProject(path, in); err != nil {
		t.Fatal(err)
	}
	out, err := LoadProject(path)
	if err != nil {
		t.Fatal(err)
	}
	if out.Services["app"].Port != 3000 {
		t.Fatalf("round trip lost data: %+v", out)
	}
}
