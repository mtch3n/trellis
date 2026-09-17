package core

import "testing"

func TestNewCardIDIsSortableByTime(t *testing.T) {
	const count = 1000
	ids := make([]string, count)
	seen := make(map[string]bool)

	for i := range count {
		ids[i] = NewCardID()
		if seen[ids[i]] {
			t.Fatalf("NewCardID returned duplicate id at index %d: %q", i, ids[i])
		}
		seen[ids[i]] = true
	}

	for i := 1; i < count; i++ {
		if ids[i-1] >= ids[i] {
			t.Errorf("uuid v7 not monotonic at index %d: %q >= %q", i, ids[i-1], ids[i])
		}
	}
}

func TestParseCardRef(t *testing.T) {
	tests := []struct {
		in      string
		wantSeq int64
		wantKey string
		wantID  bool
	}{
		{in: "12", wantSeq: 12},
		{in: "XPSCTL-12", wantSeq: 12, wantKey: "XPSCTL"},
		{in: "xpsctl-12", wantSeq: 12, wantKey: "XPSCTL"},
		{in: "0199c3a1-7b2e-7000-8000-000000000000", wantID: true},
	}
	for _, tc := range tests {
		got := ParseCardRef(tc.in)
		if tc.wantID {
			if got.UUID != tc.in {
				t.Errorf("ParseCardRef(%q).UUID = %q, want %q", tc.in, got.UUID, tc.in)
			}
			continue
		}
		if got.Seq != tc.wantSeq || got.ProjectKey != tc.wantKey {
			t.Errorf("ParseCardRef(%q) = %+v, want seq=%d key=%q", tc.in, got, tc.wantSeq, tc.wantKey)
		}
	}
}

func TestCardRefString(t *testing.T) {
	tests := []struct {
		ref  CardRef
		want string
	}{
		{ref: CardRef{UUID: "0199c3a1-7b2e-7000-8000-000000000000"}, want: "0199c3a1-7b2e-7000-8000-000000000000"},
		{ref: CardRef{ProjectKey: "XPSCTL", Seq: 12}, want: "XPSCTL-12"},
		{ref: CardRef{Seq: 12}, want: "12"},
		{ref: CardRef{}, want: "<none>"},
	}
	for _, tc := range tests {
		got := tc.ref.String()
		if got != tc.want {
			t.Errorf("CardRef%+v.String() = %q, want %q", tc.ref, got, tc.want)
		}
	}
}

func TestParseCardRefReadsACardAddress(t *testing.T) {
	cases := map[string]CardRef{
		"/xpsctl/cards/xpsctl-12": {Seq: 12, ProjectKey: "XPSCTL", Project: "XPSCTL"},
		// The address's project and the ref's prefix are kept apart: after a
		// merge they legitimately differ, and core decides what that means.
		"/MONO/cards/MY_APP-3": {Seq: 3, ProjectKey: "MY_APP", Project: "MONO"},
		"XPSCTL-12":            {Seq: 12, ProjectKey: "XPSCTL"},
	}
	for in, want := range cases {
		if got := ParseCardRef(in); got != want {
			t.Errorf("ParseCardRef(%q) = %+v, want %+v", in, got, want)
		}
	}
	for _, s := range []string{"/XPSCTL/vault/design", "/XPSCTL/cards/12", "/XPSCTL", "/XPSCTL/cards/XPSCTL-99999999999999999999"} {
		if got := ParseCardRef(s); got != (CardRef{}) {
			t.Errorf("ParseCardRef(%q) = %+v, want the empty ref", s, got)
		}
	}
	if got := ParseCardRef("/MONO/cards/MY_APP-3").String(); got != "/MONO/cards/MY_APP-3" {
		t.Errorf("String() of an address = %q", got)
	}
}
