package cli

import (
	"testing"

	"github.com/mtch3n/trellis/internal/testhome"
)

// TestMain makes this package's test binary hermetic: see internal/testhome.
func TestMain(m *testing.M) { testhome.Main(m) }
