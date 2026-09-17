package cli

import (
	"encoding/json/v2"
	"github.com/mtch3n/trellis/internal/core"
	"testing"
)

func TestKnowledgeEditMetadataFlags(t *testing.T) {
	projectEnv(t)
	runCmd(t, "label", "new", "reviewed", "--description", "Reviewed")
	runCmd(t, "knowledge", "new", "--title", "Metadata")
	runCmd(t, "knowledge", "edit", "metadata", "--template", "decision", "--private=true", "--tag", "one", "--tag", "two", "--label", "reviewed", "--if-version", "1")
	var got core.Knowledge
	if err := json.Unmarshal([]byte(runCmd(t, "knowledge", "show", "metadata", "--json")), &got); err != nil {
		t.Fatal(err)
	}
	if got.Template != "decision" || !got.Private || len(got.Tags) != 2 || len(got.Labels) != 1 {
		t.Fatalf("%+v", got)
	}
}
