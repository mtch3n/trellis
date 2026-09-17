package cli

import (
	"encoding/json/v2"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/mtch3n/trellis/internal/core"
)

func showEntry(t *testing.T, ref string) core.Knowledge {
	t.Helper()
	var got core.Knowledge
	if err := json.Unmarshal([]byte(runCmd(t, "knowledge", "show", ref, "--json")), &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestKnowledgeEditMetadataFlags(t *testing.T) {
	projectEnv(t)
	runCmd(t, "label", "new", "reviewed", "--description", "Reviewed")
	runCmd(t, "knowledge", "new", "--title", "Metadata")
	runCmd(t, "knowledge", "edit", "metadata", "--template", "research", "--private=true", "--tag", "one", "--tag", "two", "--label", "reviewed", "--if-version", "1")
	got := showEntry(t, "metadata")
	if got.Template != "research" || !got.Private || len(got.Tags) != 2 || len(got.Labels) != 1 {
		t.Fatalf("%+v", got)
	}
}

// --tag and --label replace their lists; given empty, they clear them.
func TestKnowledgeEditReplacesAndClearsTagsAndLabels(t *testing.T) {
	projectEnv(t)
	runCmd(t, "label", "new", "priority", "--description", "Priority")
	runCmd(t, "label", "new", "reviewed", "--description", "Reviewed")
	runCmd(t, "knowledge", "new", "--title", "Tagged")
	edit := func(args ...string) core.Knowledge {
		t.Helper()
		v := showEntry(t, "tagged").Version
		runCmd(t, append(append([]string{"knowledge", "edit", "tagged"}, args...), "--if-version", strconv.FormatInt(v, 10))...)
		return showEntry(t, "tagged")
	}

	got := edit("--tag", "a", "--tag", "b", "--label", "priority")
	if !slices.Equal(got.Tags, []string{"a", "b"}) || !slices.Equal(got.Labels, []string{"priority"}) {
		t.Fatalf("set: tags %v, labels %v", got.Tags, got.Labels)
	}
	got = edit("--tag", "c", "--label", "reviewed")
	if !slices.Equal(got.Tags, []string{"c"}) || !slices.Equal(got.Labels, []string{"reviewed"}) {
		t.Fatalf("replace: tags %v, labels %v", got.Tags, got.Labels)
	}
	got = edit("--tag=", "--label=")
	if len(got.Tags) != 0 || len(got.Labels) != 0 {
		t.Fatalf("clear: tags %v, labels %v", got.Tags, got.Labels)
	}
}

// knowledge edit --set writes a template field.
func TestKnowledgeEditSetsAField(t *testing.T) {
	projectEnv(t)
	runCmd(t, "knowledge", "new", "--title", "Owned")
	runCmd(t, "knowledge", "edit", "owned", "--set", "owner=alice", "--if-version", "1")
	got := showEntry(t, "owned")
	raw, err := os.ReadFile(got.Path)
	if err != nil || !strings.Contains(string(raw), "owner: alice") {
		t.Fatalf("file = %s, %v", raw, err)
	}
}
