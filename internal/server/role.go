// Package server is the real-time WebSocket server: the Echo app, the Hub
// (seq/ring buffer/fan-out), and Project, the single visibility filter
// every outgoing Envelope passes through (docs/PROTOCOL.md §1).
package server

import "fmt"

// Role is who a connection is: either the spectator (god view) or one
// specific player seat. The zero value is NOT a valid Role (Spectator
// false, Seat 0 looks like "player seat 0" by accident) -- always build one
// with Spectate() or PlayerSeat(n).
type Role struct {
	spectator bool
	seat      int
}

// Spectate returns the spectator Role.
func Spectate() Role { return Role{spectator: true} }

// PlayerSeat returns the Role for seat's own connection.
func PlayerSeat(seat int) Role { return Role{seat: seat} }

// IsSpectator reports whether r is the spectator role.
func (r Role) IsSpectator() bool { return r.spectator }

// Seat returns the seat this Role is "player:<seat>" for. Meaningless
// (returns 0) when IsSpectator() is true.
func (r Role) Seat() int { return r.seat }

// Sees reports whether this Role may see data about seat (its own seat, or
// spectator seeing anyone).
func (r Role) Sees(seat int) bool { return r.spectator || r.seat == seat }

func (r Role) String() string {
	if r.spectator {
		return "spectator"
	}
	return fmt.Sprintf("player:%d", r.seat)
}
