package cli

import (
	"slices"
	"testing"
)

func TestClient_CLIENT_C19_TheBinaryHasExactlyTheServerCommands(t *testing.T) {
	var got []string
	for _, c := range newRootCmd().Commands() {
		if c.Name() != "help" && c.Name() != "completion" {
			got = append(got, c.Name())
		}
	}
	want := []string{"admin", "call", "import", "init", "login", "serve", "update", "version"}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("commands = %v, want %v", got, want)
	}
}
