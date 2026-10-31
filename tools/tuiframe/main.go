// Throwaway tool (not part of the module's real deliverables, deleted
// right after use) to render one tui.Model frame against the live
// `pokerd serve` on 127.0.0.1:8090 for the T2 manual-check report, without
// needing a real TTY/AltScreen session. It connects over the WebSocket
// directly (same contract tui.Client uses) and feeds Envelopes into
// Model.Update exactly like model_test.go does, then switches to the
// Razonamiento tab and prints View().Content.
//
// Deliberately never logs the dial/fetch endpoint or any error wrapping
// it: both embed ?token=... (PROTOCOL.md §1), and this tool's whole job is
// to produce a plain-text frame, not to print secrets.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/whoAngeel/pokerhub/internal/protocol"
	"github.com/whoAngeel/pokerhub/internal/tui"
)

func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key, val = strings.TrimSpace(key), strings.TrimSpace(val)
		if len(val) >= 2 && (val[0] == '"' || val[0] == '\'') && val[len(val)-1] == val[0] {
			val = val[1 : len(val)-1]
		}
		if _, set := os.LookupEnv(key); !set {
			os.Setenv(key, val)
		}
	}
}

func main() {
	loadDotEnv(".env")
	token := os.Getenv("POKERHUB_TOKEN")
	base := "ws://127.0.0.1:8090"

	fetchCtx, cancelFetch := context.WithTimeout(context.Background(), 3*time.Second)
	seeds, _ := tui.FetchConfig(fetchCtx, base, token)
	cancelFetch()
	fmt.Fprintf(os.Stderr, "seeded %d seats from /api/config\n", len(seeds))

	endpoint, err := tui.NormalizeURL(base, token)
	if err != nil {
		fmt.Fprintln(os.Stderr, "normalize url failed")
		os.Exit(1)
	}

	var mdl tea.Model = tui.NewModel(false, seeds) // unicode glyphs for the report
	mdl, _ = mdl.Update(tea.WindowSizeMsg{Width: 170, Height: 46})

	n := 0
	haveReasoning := false

	if os.Getenv("TUI_LOCAL") != "" {
		// Local fallback (no live server needed): replay
		// recordings/sample.jsonl the same way model_test.go does, for
		// layout debugging independent of the live server's availability.
		f, ferr := os.Open("recordings/sample.jsonl")
		if ferr != nil {
			fmt.Fprintln(os.Stderr, "open sample.jsonl failed:", ferr)
			os.Exit(1)
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			var env protocol.Envelope
			if err := json.Unmarshal([]byte(line), &env); err != nil {
				continue
			}
			mdl, _ = mdl.Update(tui.EnvMsg{Env: env})
			n++
			if env.Type == protocol.TypeReasoning {
				haveReasoning = true
			}
		}
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()

		conn, _, derr := websocket.Dial(ctx, endpoint, nil)
		if derr != nil {
			fmt.Fprintln(os.Stderr, "dial failed (is the server up / token correct?)")
			os.Exit(1)
		}
		defer conn.Close(websocket.StatusNormalClosure, "")

		for {
			var env protocol.Envelope
			if err := wsjson.Read(ctx, conn, &env); err != nil {
				break
			}
			mdl, _ = mdl.Update(tui.EnvMsg{Env: env})
			n++
			if env.Type == protocol.TypeReasoning {
				haveReasoning = true
			}
			// Stop once we have at least one reasoning event and a decent
			// amount of table activity, so the frame has something in every
			// tab without waiting the full 8s budget.
			if haveReasoning && n > 25 {
				break
			}
		}
	}
	fmt.Fprintf(os.Stderr, "read %d envelopes (reasoning seen: %v)\n", n, haveReasoning)

	// Switch to the Razonamiento tab (key "2", keys.go's Tab2 binding) for
	// the report frame.
	mdl, _ = mdl.Update(tea.KeyPressMsg{Text: "2", Code: '2'})

	view := mdl.View()
	fmt.Println(view.Content)
}
