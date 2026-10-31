package protocol

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/invopop/jsonschema"
)

// schemaTargets names every payload struct that gets its own
// docs/schema/<event>.json, keyed by the wire type name (PROTOCOL.md §4/§5).
var schemaTargets = map[string]any{
	TypeSnapshot:     SnapshotPayload{},
	TypeHandStart:    HandStartPayload{},
	TypeBlind:        BlindPayload{},
	TypeHoleCards:    HoleCardsPayload{},
	TypeStreet:       StreetPayload{},
	TypeTurn:         TurnPayload{},
	TypeThinking:     ThinkingPayload{},
	TypeAction:       ActionPayload{},
	TypeChat:         ChatPayload{},
	TypeReasoning:    ReasoningPayload{},
	TypeShowdown:     ShowdownPayload{},
	TypePotAwarded:   PotAwardedPayload{},
	TypeHandEnd:      HandEndPayload{},
	TypeRebuy:        RebuyPayload{},
	TypeMemoryUpdate: MemoryUpdatePayload{},
	TypeAgentError:   AgentErrorPayload{},

	"hello":         HelloPayload{},
	"control":       ControlPayload{},
	"client_action": ClientActionPayload{},
	TypeError:       ErrorPayload{},
	"envelope":      Envelope{},
}

// GenerateSchemas writes one docs/schema/<event>.json per entry in
// schemaTargets, via invopop/jsonschema reflection over the payload structs
// defined in payloads.go (PLAN §5 / `make schema`). dir is created if it
// doesn't exist.
func GenerateSchemas(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("protocol: mkdir %s: %w", dir, err)
	}

	r := &jsonschema.Reflector{
		DoNotReference: true,
		ExpandedStruct: true,
	}

	for name, target := range schemaTargets {
		schema := r.Reflect(target)
		out, err := json.MarshalIndent(schema, "", "  ")
		if err != nil {
			return fmt.Errorf("protocol: marshal schema for %s: %w", name, err)
		}
		path := filepath.Join(dir, name+".json")
		if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
			return fmt.Errorf("protocol: write %s: %w", path, err)
		}
	}
	return nil
}
