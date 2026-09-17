package ui

import (
	"fmt"
	"os"
	"testing"
)

// Tests here build a Core without WithKBRoot, which writes knowledge under
// the Trellis home. Point that home at a temporary directory so a test run
// never writes into the user's real ~/.trellis.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "trellis-ui-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Setenv("TRELLIS_HOME", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
