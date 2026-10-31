package llm

import (
	"strings"
	"testing"
)

func TestRegistry_BuiltinsRegistered(t *testing.T) {
	types := Types()
	for _, want := range []string{"openai", "mock"} {
		found := false
		for _, got := range types {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Types() = %v, want it to include %q", types, want)
		}
	}
}

func TestRegistry_New_Mock(t *testing.T) {
	p, err := New(ProviderConfig{Type: "mock"})
	if err != nil {
		t.Fatalf("New(mock) error = %v", err)
	}
	if p.Name() != "mock" {
		t.Errorf("Name() = %q, want mock", p.Name())
	}
}

func TestRegistry_New_UnknownType(t *testing.T) {
	_, err := New(ProviderConfig{Type: "definitely-not-a-provider"})
	if err == nil {
		t.Fatal("New() error = nil, want an error for an unknown provider type")
	}
	msg := err.Error()
	if !strings.Contains(msg, "definitely-not-a-provider") {
		t.Errorf("error does not mention the unknown type: %v", err)
	}
	for _, known := range []string{"openai", "mock"} {
		if !strings.Contains(msg, known) {
			t.Errorf("error does not list known type %q: %v", known, err)
		}
	}
}

func TestRegistry_Types_Sorted(t *testing.T) {
	types := Types()
	for i := 1; i < len(types); i++ {
		if types[i-1] > types[i] {
			t.Fatalf("Types() = %v is not sorted", types)
		}
	}
}

func TestRegistry_Register_DuplicatePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("Register() did not panic on a duplicate type")
		}
	}()
	Register("mock", func(cfg ProviderConfig) (Provider, error) { return NewMock(), nil })
}
