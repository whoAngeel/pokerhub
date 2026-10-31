// Package replay records a live session's god-view event stream to a
// .jsonl file and plays one back into a fresh server.Hub, so the frontend
// (or anyone) can develop and demo against the exact same WebSocket
// contract without a GPU or an LLM running (docs/PROTOCOL.md §7).
package replay

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/whoAngeel/pokerhub/internal/protocol"
	"github.com/whoAngeel/pokerhub/internal/table"
)

// Recorder is a table.Sink: wire it into a live table.Session (usually
// alongside a server.Hub via table.MultiSink{hub, recorder}) and it
// appends one god-view protocol.Envelope per table.Event to path, as
// JSON Lines, flushing after every line so a crash loses at most the
// in-flight write.
//
// Recorder keeps its OWN protocol.Converter and seq counter rather than
// subscribing to the Hub: table.MultiSink hands every table.Event to each
// sink in the same order, synchronously, so Recorder's seq ends up
// numerically identical to the Hub's -- the replayed file's seq values are
// exactly what live clients saw, not a second independent numbering.
type Recorder struct {
	mu   sync.Mutex
	f    *os.File
	w    *bufio.Writer
	conv *protocol.Converter
	seq  int64
}

// NewRecorder creates (or truncates) path and returns a Recorder ready to
// use as a table.Sink. deadlineMS feeds every recorded "turn" event's
// deadline_ms, matching whatever the live Hub was built with.
func NewRecorder(path string, deadlineMS int) (*Recorder, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, fmt.Errorf("replay: create %s: %w", path, err)
	}
	return &Recorder{f: f, w: bufio.NewWriter(f), conv: protocol.NewConverter(deadlineMS)}, nil
}

// Emit implements table.Sink.
func (r *Recorder) Emit(ev table.Event) {
	env, err := r.conv.Convert(ev)
	if err != nil {
		// A conversion error here is a programming error (Convert is
		// exhaustive over every table.EventType), not a runtime condition
		// the recorder should crash the game over -- drop the line and
		// keep going, same philosophy as the rest of the stack ("a bad
		// model never blocks the table").
		fmt.Fprintf(os.Stderr, "replay: recorder: convert %s: %v\n", ev.Type, err)
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	env = env.WithSeq(r.seq)
	line, err := json.Marshal(env)
	if err != nil {
		fmt.Fprintf(os.Stderr, "replay: recorder: marshal %s: %v\n", env.Type, err)
		return
	}
	if _, err := r.w.Write(line); err != nil {
		fmt.Fprintf(os.Stderr, "replay: recorder: write: %v\n", err)
		return
	}
	if _, err := r.w.WriteString("\n"); err != nil {
		fmt.Fprintf(os.Stderr, "replay: recorder: write: %v\n", err)
		return
	}
	if err := r.w.Flush(); err != nil {
		fmt.Fprintf(os.Stderr, "replay: recorder: flush: %v\n", err)
	}
}

// Close flushes and closes the underlying file. Safe to call once, after
// the session that was recording into it has stopped.
func (r *Recorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.w.Flush(); err != nil {
		_ = r.f.Close()
		return fmt.Errorf("replay: recorder: flush on close: %w", err)
	}
	return r.f.Close()
}
