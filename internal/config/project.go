package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Project mirrors portflow.yml.
type Project struct {
	Version  int                       `yaml:"version"`
	Project  string                    `yaml:"project"`
	Services map[string]ProjectService `yaml:"services"`
}

// ProjectService is one entry under `services:` in portflow.yml.
type ProjectService struct {
	Domain string `yaml:"domain"`
	Port   int    `yaml:"port"`
	Host   string `yaml:"host,omitempty"`
	NoTLS  bool   `yaml:"no_tls,omitempty"`
}

// LoadProject reads and validates a portflow.yml file.
func LoadProject(path string) (*Project, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p Project
	if err := yaml.Unmarshal(b, &p); err != nil {
		return nil, err
	}
	if p.Version != 1 {
		return nil, fmt.Errorf("unsupported project file version: %d (want 1)", p.Version)
	}
	if p.Project == "" {
		return nil, fmt.Errorf("project name is required")
	}
	for name, s := range p.Services {
		if s.Domain == "" {
			return nil, fmt.Errorf("service %q: domain is required", name)
		}
		if s.Port <= 0 || s.Port > 65535 {
			return nil, fmt.Errorf("service %q: invalid port %d", name, s.Port)
		}
	}
	return &p, nil
}

// WriteProject writes a Project to disk atomically as YAML.
func WriteProject(path string, p *Project) error {
	b, err := yaml.Marshal(p)
	if err != nil {
		return err
	}
	return AtomicWrite(path, b, 0o644)
}
