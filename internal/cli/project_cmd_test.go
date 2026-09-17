package cli

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
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

func TestProjectMergeEndToEnd(t *testing.T) {
	repo := pinEnv(t, "mono")
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	seedProject(t, "MONO")
	seedProject(t, "API")
	writePin(t, repo, "/MONO\n")
	api := filepath.Join(repo, "api")
	if err := os.Mkdir(api, 0o755); err != nil {
		t.Fatal(err)
	}
	writePin(t, api, "/API\n")
	t.Chdir(api)
	if out := runCmd(t, "card", "new", "--title", "from api", "--json"); !strings.Contains(out, `"ref":"API-1"`) {
		t.Fatalf("card new = %s", out)
	}

	_, err := execCmd("project", "merge", "api")
	if ce := coreErr(t, err); ce.Code != "missing_into" {
		t.Errorf("no --into: %+v", ce)
	}

	var plan struct {
		Ready bool `json:"ready"`
		Pins  struct {
			Rewrite []struct {
				Path string `json:"path"`
				To   string `json:"to"`
			} `json:"rewrite"`
		} `json:"pins"`
	}
	out := runCmd(t, "project", "merge", "api", "--into", "/mono", "--json")
	if err := json.Unmarshal([]byte(out), &plan); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !plan.Ready || len(plan.Pins.Rewrite) != 1 || plan.Pins.Rewrite[0].To != "/MONO/boards/api" {
		t.Errorf("plan = %+v", plan)
	}
	if got, _ := os.ReadFile(filepath.Join(api, ".trellis")); string(got) != "/API\n" {
		t.Errorf("the plan rewrote a pin: %q", got)
	}

	runCmd(t, "project", "merge", "api", "--into", "mono", "--apply")

	if got, _ := os.ReadFile(filepath.Join(api, ".trellis")); string(got) != "/MONO/boards/api\n" {
		t.Errorf("pin after the merge = %q", got)
	}
	if got := showBoard(t); got.Project != "MONO" || got.Slug != "api" {
		t.Errorf("this directory now opens %+v", got)
	}
	runCmd(t, "card", "show", "API-1")
	// An address names the project, which is gone; the ref above names a card.
	_, err = execCmd("card", "show", "/API/cards/API-1")
	if ce := coreErr(t, err); ce.Code != "project_merged" {
		t.Errorf("an address under /API: %+v", ce)
	}
	_, err = execCmd("--project", "API", "card", "ls")
	if ce := coreErr(t, err); ce.Code != "project_merged" {
		t.Errorf("--project API: %+v", ce)
	}
}
