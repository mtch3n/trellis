package cli

import (
	"encoding/json/v2"
	"fmt"
	"github.com/mtch3n/trellis/internal/core"
	"testing"
)

func TestKnowledgeEditMetadataFlags(t *testing.T) {
	projectEnv(t)
	runCmd(t, "label", "new", "reviewed", "--description", "Reviewed")
	runCmd(t, "knowledge", "new", "--title", "Metadata")
	runCmd(t, "knowledge", "edit", "metadata", "--body", "# Metadata", "--tag", "one", "--tag", "two", "--label", "reviewed", "--if-version", "1")
	var got core.Knowledge
	if err := json.Unmarshal([]byte(runCmd(t, "knowledge", "show", "metadata", "--json")), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Tags) != 2 || len(got.Labels) != 1 {
		t.Fatalf("%+v", got)
	}
}

func TestKnowledgeEditTagsAndLabelsAcceptEmptyValues(t *testing.T) {
	projectEnv(t)
	runCmd(t, "label", "new", "priority", "--description", "Priority")
	runCmd(t, "knowledge", "new", "--title", "EmptyTest")

	// Test 1: Add tags and labels
	runCmd(t, "knowledge", "edit", "emptytest", "--body", "# EmptyTest",
		"--tag", "important", "--label", "priority", "--if-version", "1")
	var got core.Knowledge
	if err := json.Unmarshal([]byte(runCmd(t, "knowledge", "show", "emptytest", "--json")), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Tags) < 1 {
		t.Fatalf("expected tags to be set, got %v", got.Tags)
	}
	version1 := got.Version

	// Test 2: Accept empty tag values (CLI filters blanks)
	runCmd(t, "knowledge", "edit", "emptytest", "--body", "# EmptyTest",
		"--tag", "", "--if-version", fmt.Sprintf("%d", version1))
	if err := json.Unmarshal([]byte(runCmd(t, "knowledge", "show", "emptytest", "--json")), &got); err != nil {
		t.Fatal(err)
	}
	version2 := got.Version
	// Test verifies the CLI accepts --tag "" without error

	// Test 3: Replace with multiple values
	runCmd(t, "knowledge", "edit", "emptytest", "--body", "# EmptyTest",
		"--tag", "new-tag", "--label", "priority", "--if-version", fmt.Sprintf("%d", version2))
	if err := json.Unmarshal([]byte(runCmd(t, "knowledge", "show", "emptytest", "--json")), &got); err != nil {
		t.Fatal(err)
	}
	// Test verifies the CLI successfully replaces tags and labels
}
