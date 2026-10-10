package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/portflow/portflow/internal/hostname"
	"gopkg.in/yaml.v3"
)

// maxProjectFileSize bounds how much of a portflow.yml we are willing to parse.
const maxProjectFileSize = 1 << 20

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

// LoadProject reads and validates a portflow.yml file. Unknown keys are
// rejected so typos and smuggled settings fail loudly instead of being ignored.
func LoadProject(path string) (*Project, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxProjectFileSize+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxProjectFileSize {
		return nil, fmt.Errorf("project file is larger than %d bytes", maxProjectFileSize)
	}
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	var p Project
	if err := dec.Decode(&p); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("project file is empty")
		}
		return nil, err
	}
	if err := p.validate(); err != nil {
		return nil, err
	}
	return &p, nil
}

func (p *Project) validate() error {
	if p.Version != 1 {
		return fmt.Errorf("unsupported project file version: %d (want 1)", p.Version)
	}
	if p.Project == "" {
		return errors.New("project name is required")
	}
	if err := hostname.ValidateProject(p.Project); err != nil {
		return err
	}
	for name, s := range p.Services {
		if s.Domain == "" {
			return fmt.Errorf("service %q: domain is required", name)
		}
		if err := hostname.Validate(hostname.Normalize(s.Domain)); err != nil {
			return fmt.Errorf("service %q: %w", name, err)
		}
		if s.Port <= 0 || s.Port > 65535 {
			return fmt.Errorf("service %q: invalid port %d", name, s.Port)
		}
	}
	return nil
}

// WriteProject writes a Project to disk atomically as YAML.
func WriteProject(path string, p *Project) error {
	b, err := yaml.Marshal(p)
	if err != nil {
		return err
	}
	return AtomicWrite(path, b, 0o644)
}
