// Command gen-schema writes docs/schema/<event>.json from the payload
// structs in internal/protocol (invoked by `make schema`; PLAN §5).
package main

import (
	"fmt"
	"os"

	"github.com/whoAngeel/pokerhub/internal/protocol"
)

func main() {
	dir := "docs/schema"
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}
	if err := protocol.GenerateSchemas(dir); err != nil {
		fmt.Fprintln(os.Stderr, "gen-schema:", err)
		os.Exit(1)
	}
	fmt.Println("wrote", dir)
}
