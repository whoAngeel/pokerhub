package server

import (
	"fmt"

	"github.com/whoAngeel/pokerhub/internal/agent"
	"github.com/whoAngeel/pokerhub/internal/config"
	"github.com/whoAngeel/pokerhub/internal/protocol"
)

// PublicConfigSeat is one seats[] entry of GET /api/config (PROTOCOL.md §1):
// no secrets, no base_url, no api keys -- just enough for the frontend to
// paint the room before the first snapshot arrives.
type PublicConfigSeat struct {
	Seat      int     `json:"seat"`
	Name      string  `json:"name"`
	PersonaID *string `json:"persona_id"`
	Kind      string  `json:"kind"` // "llm" | "rulebot" | "human"
	Model     *string `json:"model"`
}

// PublicConfig is GET /api/config's JSON body: the table's public
// composition, fixed for the lifetime of the pokerd process.
type PublicConfig struct {
	SmallBlind int                `json:"small_blind"`
	BigBlind   int                `json:"big_blind"`
	BuyIn      int                `json:"buy_in"`
	Seats      []PublicConfigSeat `json:"seats"`
}

// HasHumanSeat reports whether any seat is kind "human" (PROTOCOL.md §1:
// new spectator connections are refused with close code 4003 while this is
// true, unless server.debug is set). Unreachable via configs/config.yaml
// today -- config.Validate only accepts seat types "rulebot"/"llm" -- but
// kept so Hub's refusal path (and its test) doesn't depend on that changing.
func (p PublicConfig) HasHumanSeat() bool {
	for _, s := range p.Seats {
		if s.Kind == "human" {
			return true
		}
	}
	return false
}

// TableStateSeats renders PublicConfig as the []protocol.Seat
// protocol.NewTableState wants to seed a Hub's god-view state: every seat
// starts at BuyIn chips, "active", no bet yet, no known hole cards (Apply
// fills those in as the live event stream arrives).
func (p PublicConfig) TableStateSeats() []protocol.Seat {
	out := make([]protocol.Seat, len(p.Seats))
	for i, s := range p.Seats {
		out[i] = protocol.Seat{
			Seat: s.Seat, Name: s.Name, PersonaID: s.PersonaID, Kind: s.Kind,
			Stack: p.BuyIn, Status: "active", Model: s.Model,
		}
	}
	return out
}

// BuildPublicConfig renders cfg's PUBLIC surface only: blinds, buy-in, and
// each seat's name/persona_id/kind/model -- never provider, base_url or
// api_key_env (PROTOCOL.md §1 "sin claves ni prompts"). persona_id comes
// from the persona YAML's own `id` field (agent.LoadPersona), not the
// seat's configured file path, since that path is a filesystem detail the
// frontend has no use for.
func BuildPublicConfig(cfg *config.Config) (PublicConfig, error) {
	pub := PublicConfig{SmallBlind: cfg.Table.SmallBlind, BigBlind: cfg.Table.BigBlind, BuyIn: cfg.Table.BuyIn}
	for i, s := range cfg.Seats {
		seat := PublicConfigSeat{Seat: i, Kind: s.Type, Name: s.Name}

		if s.Type == config.SeatLLM {
			persona, err := agent.LoadPersona(cfg.PersonaPath(s.Persona))
			if err != nil {
				return PublicConfig{}, fmt.Errorf("server: public config seats[%d]: %w", i, err)
			}
			if seat.Name == "" {
				seat.Name = persona.Name
			}
			seat.PersonaID = protocol.StringPtr(persona.ID)
			seat.Model = protocol.StringPtr(s.Model)
		}
		if seat.Name == "" {
			seat.Name = fmt.Sprintf("Seat %d", i)
		}
		pub.Seats = append(pub.Seats, seat)
	}
	return pub, nil
}
