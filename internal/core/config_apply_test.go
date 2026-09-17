package core

import (
	"path/filepath"
	"testing"

	"github.com/mtch3n/trellis/internal/config"
	"github.com/mtch3n/trellis/internal/store"
)

// ApplyConfig is the one place internal/cli's openCore and applyRepoConfig,
// the daemon's startup, and the settings API's PATCH hook apply lease TTL,
// default columns, label/tag requirements and history retention -- the
// settings the daemon keeps live rather than only reading at startup.
func TestApplyConfigAppliesEveryLiveSetting(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	c := New(db, FixedClock{MS: 1_000_000}, "test", dir)

	cfg := config.Defaults()
	cfg.Lease.TTL = "45m"
	cfg.Board.DefaultColumns = []string{"todo", "doing", "done"}
	cfg.Labels.RequireOnCard = true
	cfg.Tags.RequireOnCard = true
	keep := 7
	cfg.History.Keep = &keep

	c.ApplyConfig(cfg)

	if want := int64(45 * 60 * 1000); c.leaseTTL != want {
		t.Errorf("leaseTTL = %d, want %d", c.leaseTTL, want)
	}
	if len(c.defaultColumns) != 3 || c.defaultColumns[0] != "todo" || c.defaultColumns[2] != "done" {
		t.Errorf("defaultColumns = %v", c.defaultColumns)
	}
	if !c.requireLabels || !c.requireTags {
		t.Errorf("requireLabels=%v requireTags=%v, want both true", c.requireLabels, c.requireTags)
	}
	if c.historyKeep != 7 {
		t.Errorf("historyKeep = %d, want 7", c.historyKeep)
	}
}

// An unparseable lease.ttl must leave the previous value in place rather
// than zeroing it out, the same defensive rule SetLeaseTTL already applies.
func TestApplyConfigIgnoresAnUnparseableLeaseTTL(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	c := New(db, FixedClock{MS: 1_000_000}, "test", dir)
	before := c.leaseTTL

	cfg := config.Defaults()
	cfg.Lease.TTL = "not-a-duration"
	c.ApplyConfig(cfg)

	if c.leaseTTL != before {
		t.Errorf("leaseTTL = %d, want unchanged %d", c.leaseTTL, before)
	}
}
