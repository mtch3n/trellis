package platform

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

type Kind string

const (
	KindUsage    Kind = "usage"
	KindNotFound Kind = "not_found"
	KindConflict Kind = "conflict"
	KindPolicy   Kind = "policy"
)

type Error struct {
	Kind   Kind   `json:"-"`
	Code   string `json:"code"`
	Msg    string `json:"message"`
	Fix    string `json:"fix"`
	Detail any    `json:"detail,omitempty"`
}

func (e *Error) Error() string {
	if e.Fix == "" {
		return e.Msg
	}
	return fmt.Sprintf("%s\n  fix: %s", e.Msg, e.Fix)
}

func ErrUsage(code, msg, fix string) *Error {
	return &Error{Kind: KindUsage, Code: code, Msg: msg, Fix: fix}
}

func ErrNotFound(code, msg, fix string) *Error {
	return &Error{Kind: KindNotFound, Code: code, Msg: msg, Fix: fix}
}

func ErrConflict(code, msg, fix string) *Error {
	return &Error{Kind: KindConflict, Code: code, Msg: msg, Fix: fix}
}

func ErrPolicy(code, msg, fix string) *Error {
	return &Error{Kind: KindPolicy, Code: code, Msg: msg, Fix: fix}
}

func IsCode(err error, code string) bool {
	e, ok := errors.AsType[*Error](err)
	return ok && e.Code == code
}

func isUniqueViolation(err error) bool {
	pe, ok := errors.AsType[*pgconn.PgError](err)
	return ok && pe.Code == "23505"
}
