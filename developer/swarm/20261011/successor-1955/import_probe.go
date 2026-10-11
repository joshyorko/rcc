// Non-timed diagnosis of RCC import lifecycle. This is not the old timed wave.
package main

import (
	"encoding/json"
	"os"
	_ "github.com/joshyorko/rcc/htfs"
)

func main() {
	p := os.Getenv("ROBOCORP_HOME")
	st, err := os.Stat(p)
	if err != nil { panic(err) }
	mode := st.Mode().Perm()
	json.NewEncoder(os.Stdout).Encode(map[string]any{"home": p, "mode": mode.String(), "mode_octal": uint32(mode), "guard_pass": mode == 0700})
	if mode != 0700 { os.Exit(1) }
}
