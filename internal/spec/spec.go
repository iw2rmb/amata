package spec

import (
	"bytes"
	"fmt"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const Version = "amata/v1"

type Document struct {
	Version   string          `yaml:"version"`
	Name      string          `yaml:"name"`
	Entry     string          `yaml:"entry"`
	Workspace Workspace       `yaml:"workspace,omitempty"`
	Params    map[string]any  `yaml:"params,omitempty"`
	Defaults  map[string]any  `yaml:"defaults,omitempty"`
	Schemas   map[string]any  `yaml:"schemas,omitempty"`
	Flows     map[string]Flow `yaml:"flows"`
}

type Workspace struct {
	Root     string `yaml:"root,omitempty"`
	StateDir string `yaml:"state_dir,omitempty"`
}

type Flow struct {
	Steps []Step `yaml:"steps,omitempty"`
}

type Step struct {
	ID     string         `yaml:"id,omitempty"`
	Type   string         `yaml:"type,omitempty"`
	Fields map[string]any `yaml:",inline"`
}

func (s Step) ExecutorType() string {
	if s.Type != "" {
		return s.Type
	}
	if _, ok := s.Fields["command"]; ok {
		return "shell"
	}
	if _, ok := s.Fields["expr"]; ok {
		return "expr"
	}
	if _, ok := s.Fields["assert"]; ok {
		return "assert"
	}

	return ""
}

type Loaded struct {
	Path string
	Spec Document
}

func Load(path string) (Loaded, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return Loaded{}, fmt.Errorf("resolve spec path: %w", err)
	}

	data, err := compose(absPath)
	if err != nil {
		return Loaded{}, err
	}

	document, err := Decode(data)
	if err != nil {
		return Loaded{}, fmt.Errorf("decode spec %s: %w", absPath, err)
	}

	return Loaded{
		Path: absPath,
		Spec: document,
	}, nil
}

func Decode(data []byte) (Document, error) {
	var document Document

	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&document); err != nil {
		return Document{}, err
	}

	if err := validate(document); err != nil {
		return Document{}, err
	}

	return document, nil
}

func validate(document Document) error {
	if document.Version != Version {
		return fmt.Errorf("unsupported version %q", document.Version)
	}
	if document.Name == "" {
		return fmt.Errorf("name is required")
	}
	if document.Entry == "" {
		return fmt.Errorf("entry is required")
	}
	if len(document.Flows) == 0 {
		return fmt.Errorf("flows is required")
	}
	if _, ok := document.Flows[document.Entry]; !ok {
		return fmt.Errorf("entry flow %q is not defined", document.Entry)
	}
	if err := validateBuiltInSteps(document); err != nil {
		return err
	}

	return nil
}
