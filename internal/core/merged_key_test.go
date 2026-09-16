package core

import (
	"strings"
	"testing"
)

func retire(t *testing.T, c *Core, key string, into Project) {
	t.Helper()
	if _, err := c.db.Exec(`INSERT INTO merged_project (key, into_id, merged_at) VALUES (?, ?, 1)`, key, into.ID); err != nil {
		t.Fatal(err)
	}
}

func TestAMergedKeyIsReserved(t *testing.T) {
	c := testCore(t).WithKBRoot(t.TempDir())
	ctx := t.Context()
	mono, err := c.CreateProject(ctx, "MONO", false)
	if err != nil {
		t.Fatal(err)
	}
	retire(t, c, "API", mono)

	_, err = c.ProjectByKey(ctx, "api")
	if got := errCode(t, err); got != "project_merged" {
		t.Fatalf("ProjectByKey: code = %s", got)
	}
	te := err.(*Error)
	if te.Exit != 3 || !strings.Contains(te.Msg, "MONO") || te.Detail.(map[string]string)["into"] != "MONO" {
		t.Errorf("error = %+v", te)
	}

	_, err = c.CreateProject(ctx, "API", false)
	if got := errCode(t, err); got != "key_reserved" {
		t.Errorf("CreateProject: code = %s", got)
	}
	_, err = c.InitProject(ctx, InitRequest{Dir: t.TempDir(), Key: "API", Join: true})
	if got := errCode(t, err); got != "key_reserved" {
		t.Errorf("InitProject: code = %s", got)
	}

	if err := c.DeleteProject(ctx, "MONO"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateProject(ctx, "API", false); err != nil {
		t.Errorf("deleting the survivor must free the key: %v", err)
	}
}
