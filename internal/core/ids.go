package core

import (
	"strconv"
	"strings"
	"uuid"

	"github.com/mtch3n/trellis/internal/address"
)

// NewID mints every id Trellis stores: a uuid v7, which is time-ordered so
// ids sort by creation. Card ranking relies on this property. The stdlib guarantees uuid v7 is
// monotonically increasing within a process via a 12-bit sub-millisecond
// fraction and a monotonic bump under mutex when timestamps repeat. This
// guarantee holds only within a process; code must not assume ordering across
// separate processes.
func NewID() string { return uuid.NewV7().String() }

// CardRef is a parsed card reference. Exactly one of UUID or Seq is set.
type CardRef struct {
	UUID       string
	Seq        int64
	ProjectKey string // the ref's prefix, set when the reference was qualified
	Project    string // the project an address names; "" for shorthand
}

// ParseCardRef accepts "12", "XPSCTL-12", "/XPSCTL/cards/XPSCTL-12", or a
// uuid. An address keeps the project it names in Project, apart from the
// ref's prefix: the two differ once projects are merged, and deciding what
// that means is checkCardProject's job, not the parser's.
func ParseCardRef(s string) CardRef {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "/") {
		p, err := address.Parse(s)
		if err != nil || p.Collection != address.CollectionCards {
			return CardRef{}
		}
		// address.Parse has checked the PREFIX-N shape; only the number can still
		// fail, by overflowing.
		key, num, _ := strings.CutLast(p.Name, "-")
		n, err := strconv.ParseInt(num, 10, 64)
		if err != nil {
			return CardRef{}
		}
		return CardRef{Seq: n, ProjectKey: key, Project: p.Project}
	}
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

// qualified is the PREFIX-N form of a qualified ref.
func (r CardRef) qualified() string { return r.ProjectKey + "-" + itoa(r.Seq) }

// String renders a CardRef for error messages. An address is shown as the
// address the caller typed.
func (r CardRef) String() string {
	switch {
	case r.UUID != "":
		return r.UUID
	case r.Project != "":
		return address.Card(r.Project, r.qualified()).String()
	case r.ProjectKey != "":
		return r.qualified()
	case r.Seq > 0:
		return itoa(r.Seq)
	}
	return "<none>"
}
