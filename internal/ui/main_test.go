package ui

import (
	"testing"

	"github.com/mtch3n/trellis/internal/testhome"
)

// TestMain makes this package's test binary hermetic: see internal/testhome.
// Tests here build every *core.Core with an explicit root (t.TempDir()), so
// this is a second line, not the mechanism: a test that forgot the root
// argument would fail to compile before it ever reached TRELLIS_HOME.
func TestMain(m *testing.M) { testhome.Main(m) }
