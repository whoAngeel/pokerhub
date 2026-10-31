package llm

import (
	"fmt"
	"sort"
	"sync"
)

var (
	registryMu sync.RWMutex
	registry   = map[string]Factory{}
)

// Register adds a provider factory under typ, so that it can later be built
// with New(ProviderConfig{Type: typ, ...}). Built-in providers register
// themselves from init(); community providers do the same from their own
// package (see docs/PLAN.md §6.1, Nivel 3).
//
// Register panics on a duplicate type name: that is a programming error that
// should surface at init time, not at runtime when a config happens to pick
// that type.
func Register(typ string, f Factory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[typ]; exists {
		panic(fmt.Sprintf("llm: provider type %q already registered", typ))
	}
	registry[typ] = f
}

// New builds a Provider from cfg, dispatching on cfg.Type to the factory
// registered for it. Unknown types produce an error that lists the known
// types, so misconfigured YAML fails loudly instead of silently.
func New(cfg ProviderConfig) (Provider, error) {
	registryMu.RLock()
	f, ok := registry[cfg.Type]
	registryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("llm: unknown provider type %q (known types: %s)", cfg.Type, joinTypes())
	}
	return f(cfg)
}

// Types returns the registered provider type names, sorted for deterministic
// error messages and listings.
func Types() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	types := make([]string, 0, len(registry))
	for t := range registry {
		types = append(types, t)
	}
	sort.Strings(types)
	return types
}

func joinTypes() string {
	types := Types()
	if len(types) == 0 {
		return "(none registered)"
	}
	out := types[0]
	for _, t := range types[1:] {
		out += ", " + t
	}
	return out
}
