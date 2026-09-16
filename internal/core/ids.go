package core

import (
	"strconv"
	"strings"
	"uuid"
)

// NewCardID returns a uuid v7, which is time-ordered so card ids sort by creation.
// Card ranking relies on this property. The stdlib guarantees uuid v7 is
// monotonically increasing within a process via a 12-bit sub-millisecond
// fraction and a monotonic bump under mutex when timestamps repeat. This
// guarantee holds only within a process; code must not assume ordering across
// separate processes.
func NewCardID() string { return uuid.NewV7().String() }

// CardRef is a parsed card reference. Exactly one of UUID or Seq is set.
type CardRef struct {
	UUID       string
	Seq        int64
	ProjectKey string // set only when the reference was fully qualified
}

// ParseCardRef accepts "12", "XPSCTL-12", or a uuid.
func ParseCardRef(s string) CardRef {
	s = strings.TrimSpace(s)
	if _, err := uuid.Parse(s); err == nil {
		return CardRef{UUID: s}
	}
	if key, num, ok := strings.CutLast(s, "-"); ok {
		if n, err := strconv.ParseInt(num, 10, 64); err == nil {
			return CardRef{Seq: n, ProjectKey: strings.ToUpper(key)}
		}
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return CardRef{Seq: n}
	}
	return CardRef{}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// String renders a CardRef for error messages.
func (r CardRef) String() string {
	switch {
	case r.UUID != "":
		return r.UUID
	case r.ProjectKey != "":
		return r.ProjectKey + "-" + itoa(r.Seq)
	case r.Seq > 0:
		return itoa(r.Seq)
	}
	return "<none>"
}

// Project returns the ProjectKey if set, else "".
func (r CardRef) Project() string {
	return r.ProjectKey
}

// qualified returns the ProjectKey-Seq form when ProjectKey is set, else "".
func (r CardRef) qualified() string {
	if r.ProjectKey != "" {
		return r.ProjectKey + "-" + itoa(r.Seq)
	}
	return ""
}
