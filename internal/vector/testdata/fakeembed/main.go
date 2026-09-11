// Command fakeembed is a deterministic stand-in for an embedding provider.
// The index execs the configured command directly, with no shell, so the
// fixture has to be a real executable: Windows cannot exec a .sh, and
// CreateProcess will not run a .bat either. Behaviour is driven by the
// environment, which the child inherits from the test process.
package main

import (
	"fmt"
	"os"
)

func main() {
	if path := os.Getenv("FAKEEMBED_FAIL_IF_EXISTS"); path != "" {
		if _, err := os.Stat(path); err == nil {
			os.Exit(1)
		}
	}
	if path := os.Getenv("FAKEEMBED_COUNT_FILE"); path != "" {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if _, err := f.WriteString("x"); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if err := f.Close(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	fmt.Print("[1, 0]")
}
