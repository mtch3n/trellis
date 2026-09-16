package cli

import (
	"encoding/json/v2"
	"testing"
)

func TestProjectNewCreatesAnUnpinnedProject(t *testing.T) {
	pinEnv(t, "anywhere")
	out := runCmd(t, "project", "new", "research", "--json")
	var p struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal([]byte(out), &p); err != nil || p.Key != "RESEARCH" {
		t.Fatalf("project new = %q, %v", out, err)
	}
	// Reachable by name, although no pin names it.
	if got := showBoard(t, "--project", "RESEARCH").Slug; got != "research" {
		t.Errorf("default board slug = %s", got)
	}
}

func TestProjectNewRefusesReservedAndTakenKeys(t *testing.T) {
	pinEnv(t, "anywhere")
	runCmd(t, "project", "new", "TAKEN")
	for key, want := range map[string]string{"GLOBAL": "reserved_key", "taken": "key_collision", "my_app": "bad_key"} {
		_, err := execCmd("project", "new", key)
		if ce := coreErr(t, err); ce.Code != want {
			t.Errorf("project new %s: code = %s, want %s", key, ce.Code, want)
		}
	}
}

func TestProjectLsListsKeysAndBoardCounts(t *testing.T) {
	pinEnv(t, "anywhere")
	seedProject(t, "ALPHA", "Web")
	seedProject(t, "BETA")
	out := runCmd(t, "project", "ls", "--json")
	var v struct {
		Projects []struct {
			Key    string `json:"key"`
			Boards int    `json:"boards"`
		} `json:"projects"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, p := range v.Projects {
		got[p.Key] = p.Boards
	}
	if got["ALPHA"] != 2 || got["BETA"] != 1 || len(got) != 2 {
		t.Errorf("projects = %v", got)
	}
}
