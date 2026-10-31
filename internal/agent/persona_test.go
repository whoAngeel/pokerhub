package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPersona_AllConfigsValid(t *testing.T) {
	files := []string{"toro.yaml", "abuela.yaml", "profesor.yaml", "payaso.yaml"}
	for _, f := range files {
		t.Run(f, func(t *testing.T) {
			p, err := LoadPersona(filepath.Join("..", "..", "configs", "personas", f))
			if err != nil {
				t.Fatalf("LoadPersona(%s) error = %v", f, err)
			}
			if p.ID == "" || p.Name == "" || p.Voice == "" || p.Strategy == "" {
				t.Errorf("LoadPersona(%s) = %+v, missing required field", f, p)
			}
			if p.Reasoning != ReasoningRich && p.Reasoning != ReasoningShort {
				t.Errorf("LoadPersona(%s).Reasoning = %q, want rich|short", f, p.Reasoning)
			}
		})
	}
}

func TestLoadPersona_MissingFile(t *testing.T) {
	_, err := LoadPersona("does/not/exist.yaml")
	if err == nil {
		t.Fatal("LoadPersona on a missing file: want error, got nil")
	}
}

func TestLoadPersona_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	writeFile(t, path, "id: [this is not a scalar\n")

	_, err := LoadPersona(path)
	if err == nil {
		t.Fatal("LoadPersona on invalid YAML: want error, got nil")
	}
}

func TestLoadPersona_ValidationErrors(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{"missing id", "name: X\nreasoning: rich\ntemperature: 0.5\nchattiness: 0.5\nvoice: v\nstrategy: s\n"},
		{"missing name", "id: x\nreasoning: rich\ntemperature: 0.5\nchattiness: 0.5\nvoice: v\nstrategy: s\n"},
		{"bad reasoning", "id: x\nname: X\nreasoning: medium\ntemperature: 0.5\nchattiness: 0.5\nvoice: v\nstrategy: s\n"},
		{"temperature out of range", "id: x\nname: X\nreasoning: rich\ntemperature: 5\nchattiness: 0.5\nvoice: v\nstrategy: s\n"},
		{"chattiness out of range", "id: x\nname: X\nreasoning: rich\ntemperature: 0.5\nchattiness: 2\nvoice: v\nstrategy: s\n"},
		{"missing voice", "id: x\nname: X\nreasoning: rich\ntemperature: 0.5\nchattiness: 0.5\nstrategy: s\n"},
		{"missing strategy", "id: x\nname: X\nreasoning: rich\ntemperature: 0.5\nchattiness: 0.5\nvoice: v\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "p.yaml")
			writeFile(t, path, tt.yaml)
			_, err := LoadPersona(path)
			if err == nil {
				t.Fatalf("LoadPersona(%s): want error, got nil", tt.name)
			}
		})
	}
}

func TestPersona_MaxTokens(t *testing.T) {
	tests := []struct {
		reasoning string
		want      int
	}{
		{ReasoningRich, 450},
		{ReasoningShort, 180},
	}
	for _, tt := range tests {
		p := Persona{Reasoning: tt.reasoning}
		if got := p.MaxTokens(); got != tt.want {
			t.Errorf("MaxTokens() for reasoning=%q = %d, want %d", tt.reasoning, got, tt.want)
		}
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writeFile(%s): %v", path, err)
	}
}
