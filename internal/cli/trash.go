package cli

import (
	"errors"

	"github.com/mtch3n/trellis/internal/core"
)

// isNotFound reports a lookup that found nothing live, which --trashed
// answers from the trash instead.
func isNotFound(err error) bool {
	e, ok := errors.AsType[*core.Error](err)
	return ok && e.Exit == 3
}
