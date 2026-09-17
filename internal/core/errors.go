package core

import (
	"errors"
	"fmt"
)

// Error is a user-facing failure. Every message is at most three lines: what is
// wrong, then a runnable fix.
type Error struct {
	Code   string // machine-readable, e.g. "conflict"
	Msg    string // one sentence, no trailing period
	Fix    string // a command the caller can run, or ""
	Exit   int
	Detail any // JSON detail for --json output, or nil
}

func (e *Error) Error() string {
	if e.Fix == "" {
		return e.Msg
	}
	return fmt.Sprintf("%s\n  run: %s", e.Msg, e.Fix)
}

func ErrUsage(code, msg, fix string) error {
	return &Error{Code: code, Msg: msg, Fix: fix, Exit: 2}
}

func ErrNotFound(code, msg, fix string) error {
	return &Error{Code: code, Msg: msg, Fix: fix, Exit: 3}
}

func ErrConflict(code, msg, fix string) error {
	return &Error{Code: code, Msg: msg, Fix: fix, Exit: 4}
}

func ErrPolicy(code, msg, fix string) error {
	return &Error{Code: code, Msg: msg, Fix: fix, Exit: 5}
}

// isCode reports whether err is a core error with this code.
func isCode(err error, code string) bool {
	e, ok := errors.AsType[*Error](err)
	return ok && e.Code == code
}
