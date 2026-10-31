// notes.go implements PLAN §7's "notas LLM": a low-priority, per-agent
// prompt that turns one completed showdown hand into short, private notes
// about rivals. Session (internal/table) owns WHEN this runs (after every
// hand that reached showdown, one call per LLM seat, async, never blocking
// a decision); this file owns the prompt, the JSON Schema and the parse.
package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/whoAngeel/pokerhub/internal/engine"
	"github.com/whoAngeel/pokerhub/internal/llm"
)

// maxNotesPerRival / maxNoteChars are PLAN §7's hard limits: at most 3 notes
// per rival, each at most 80 characters (runes), replacing (never
// appending to) that rival's previous notes.
const (
	maxNotesPerRival = 3
	maxNoteChars     = 80
	notesMaxTokens   = 200 // small budget: this is a terse note, not a decision
)

// notesSchemaName is passed as llm.Request.SchemaName.
const notesSchemaName = "memory_notes"

// notesSchema is the JSON Schema a notes-update response must satisfy:
// {"notes": {"<seat>": ["...", ...]}}, keyed by the rival's seat number as
// a string (object keys are always strings in JSON).
var notesSchema = json.RawMessage(`{
  "type": "object",
  "required": ["notes"],
  "properties": {
    "notes": {
      "type": "object",
      "additionalProperties": {
        "type": "array",
        "maxItems": 3,
        "items": {"type": "string", "maxLength": 80}
      }
    }
  }
}`)

// AgentInfo is what Session needs to run an asynchronous notes update for
// one seat between hands. Provider is nil when the seat isn't an LLM seat
// or memory notes are disabled (table.memory_notes: false in config.yaml);
// Session must skip the seat entirely in that case. Provider is expected to
// already be wrapped at llm.PriorityLow on the SAME llm.Queue decisions use
// at llm.PriorityHigh (internal/config wires this), so a pending note
// update never delays a decision.
type AgentInfo struct {
	Seat        int
	PersonaName string
	Provider    llm.Provider
	Model       string
}

// rawNotes is the wire shape notesSchema describes.
type rawNotes struct {
	Notes map[string][]string `json:"notes"`
}

// systemPrompt builds the per-persona system prompt for a notes update, in
// Spanish, independent of the decision prompt in internal/agent (this
// package never imports internal/agent).
func systemPrompt(personaName string) string {
	return fmt.Sprintf(`Eres «%s», jugador/a de Texas Hold'em. Acaba de terminar una mano que llegó a showdown.
Tu tarea AHORA no es jugar: es actualizar tus notas privadas sobre tus rivales a partir de lo que acabas de observar.
Reglas:
- Responde SOLO en español.
- Te paso el resumen PÚBLICO de la mano (acciones + cartas reveladas en el showdown) y tus notas actuales por rival (asiento -> notas).
- Para cada rival sobre el que aprendiste algo nuevo o relevante, da hasta 3 notas cortas (máx. 80 caracteres cada una) que SUSTITUYEN (no se suman a) sus notas actuales.
- Si no aprendiste nada nuevo sobre un rival, no lo incluyas: sus notas actuales se mantienen igual.
- Nunca inventes cartas: usa solo lo que aparece en el resumen de la mano.
Responde SOLO con JSON válido según el esquema: {"notes": {"<asiento>": ["nota corta", ...]}}.`, personaName)
}

// userPrompt renders the hand summary and the observer's current notes as
// compact JSON, matching the agent package's own convention of prefixing a
// short untrusted-data warning before the payload (there is none needed
// here: handSummary is built exclusively from public table.Event data, see
// HandSummary).
func userPrompt(handSummary string, currentNotes map[int][]string) string {
	data, err := json.Marshal(struct {
		HandSummary  string              `json:"hand_summary"`
		CurrentNotes map[string][]string `json:"current_notes"`
	}{
		HandSummary:  handSummary,
		CurrentNotes: stringifyKeys(currentNotes),
	})
	if err != nil {
		data = []byte(`{"hand_summary":"","current_notes":{}}`)
	}
	return "Mano recién terminada, en JSON:\n" + string(data)
}

// BuildRequest renders the full llm.Request for a notes update.
func BuildRequest(model, personaName, handSummary string, currentNotes map[int][]string) llm.Request {
	return llm.Request{
		Model:       model,
		System:      systemPrompt(personaName),
		User:        userPrompt(handSummary, currentNotes),
		Schema:      notesSchema,
		SchemaName:  notesSchemaName,
		MaxTokens:   notesMaxTokens,
		Temperature: 0,
	}
}

// Update asks info.Provider for a fresh notes update and returns the
// parsed per-rival-seat notes to merge in (seats not present in the result
// keep whatever notes they already had -- PLAN §7: "sustitución, no
// acumulación" is per rival, not a wholesale replace of the whole map).
//
// Any error (bad JSON, provider error/timeout) is returned as-is; the
// caller (table.Session) must treat it exactly like an empty result: keep
// the old notes and move on. A notes update NEVER blocks or breaks the
// game.
func Update(ctx context.Context, info AgentInfo, handSummary string, currentNotes map[int][]string) (map[int][]string, error) {
	if info.Provider == nil {
		return nil, fmt.Errorf("memory: seat %d has no notes provider", info.Seat)
	}
	req := BuildRequest(info.Model, info.PersonaName, handSummary, currentNotes)
	resp, err := info.Provider.Complete(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("memory: complete: %w", err)
	}
	return ParseNotes(resp.Content)
}

// ParseNotes parses a model response into per-rival-seat notes, clamped to
// maxNotesPerRival notes of at most maxNoteChars runes each. A seat key
// that doesn't parse as an int is skipped rather than failing the whole
// update (one bad key shouldn't discard every other rival's valid notes).
func ParseNotes(content string) (map[int][]string, error) {
	var raw rawNotes
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		return nil, fmt.Errorf("memory: invalid JSON output: %w", err)
	}
	out := make(map[int][]string, len(raw.Notes))
	for seatStr, notes := range raw.Notes {
		seat, err := strconv.Atoi(strings.TrimSpace(seatStr))
		if err != nil {
			continue
		}
		if len(notes) > maxNotesPerRival {
			notes = notes[:maxNotesPerRival]
		}
		clamped := make([]string, len(notes))
		for i, n := range notes {
			clamped[i] = clampRunes(strings.TrimSpace(n), maxNoteChars)
		}
		out[seat] = clamped
	}
	return out, nil
}

func clampRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func stringifyKeys(m map[int][]string) map[string][]string {
	out := make(map[string][]string, len(m))
	for seat, notes := range m {
		out[strconv.Itoa(seat)] = notes
	}
	return out
}

// HandSummary renders a compact PUBLIC summary of one completed hand: the
// table's action log plus every showdown reveal, by name when known. It
// never includes a hole card that wasn't actually revealed -- reveals is
// expected to be exactly what an EvShowdown engine event carried (engine
// only reveals non-folded seats at a genuine showdown; a hand that ended by
// fold emits no EvShowdown and, per table.Session's wiring, never reaches
// this function at all).
func HandSummary(actionLog string, reveals []engine.Reveal, names []string) string {
	if len(reveals) == 0 {
		return actionLog
	}
	sorted := append([]engine.Reveal{}, reveals...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Seat < sorted[j].Seat })

	parts := make([]string, 0, len(sorted))
	for _, r := range sorted {
		parts = append(parts, fmt.Sprintf("%s mostró %s (%s)", seatName(names, r.Seat), cardsString(r.Cards), r.Rank))
	}
	summary := "showdown: " + strings.Join(parts, ", ")
	if actionLog == "" {
		return summary
	}
	return actionLog + " | " + summary
}

func seatName(names []string, seat int) string {
	if seat >= 0 && seat < len(names) && names[seat] != "" {
		return names[seat]
	}
	return fmt.Sprintf("asiento %d", seat)
}

func cardsString(cards []engine.Card) string {
	parts := make([]string, len(cards))
	for i, c := range cards {
		parts[i] = string(c)
	}
	return strings.Join(parts, " ")
}
