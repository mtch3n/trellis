package cli

import (
	"bytes"
	"errors"
	"testing"

	"github.com/mtch3n/trellis/internal/platform"
)

func runCmdErr(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	root := newRootCmd()
	root.SetArgs(args)
	root.SetOut(&out)
	root.SetErr(&out)
	err := root.Execute()
	return out.String(), err
}

func runCmd(t *testing.T, args ...string) string {
	t.Helper()
	out, err := runCmdErr(t, args...)
	if err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out)
	}
	return out
}

func cliErrCode(err error) string {
	if e, ok := errors.AsType[*platform.Error](err); ok {
		return e.Code
	}
	return ""
}
