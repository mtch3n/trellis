package cli

import (
	"bytes"
	"encoding/json/v2"
	"testing"

	"github.com/spf13/cobra"
)

// An agent never has a TTY, so JSON must be the default rather than a flag it
// has to remember.
func TestEmitDefaultsToJSONWithoutTTY(t *testing.T) {
	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	type payload struct {
		Seq    string   `json:"seq"`
		Labels []string `json:"labels"`
	}
	err := Emit(cmd, payload{Seq: "XPSCTL-12"}, func() string { return "table form" })
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}

	var got payload
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output was not JSON: %v (%q)", err, buf.String())
	}
	if got.Seq != "XPSCTL-12" {
		t.Errorf("seq = %q, want XPSCTL-12", got.Seq)
	}
	// encoding/json/v2 encodes a nil slice as [], not null, which matters when
	// the consumer is an agent.
	if !bytes.Contains(buf.Bytes(), []byte(`"labels":[]`)) {
		t.Errorf("nil slice should encode as [], got %s", buf.String())
	}
}
