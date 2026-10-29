package mcpserver_test

import (
	"encoding/json/v2"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/mtch3n/trellis/internal/platform"
)

type coordOut struct {
	Notices []platform.Notice `json:"notices"`
}

func (e *env) coordRun() {
	e.t.Helper()
	if _, err := e.p.Coordinate(e.t.Context()); err != nil {
		e.t.Fatal(err)
	}
}

func coordPair(t *testing.T, e *env) (ann, bob *mcp.ClientSession, annCard, bobCard platform.Card) {
	t.Helper()
	ann, bob = e.agent("ann", "laptop", nil), e.agent("bob", "laptop", nil)
	annCard = ok[cardOut](t, ann, "card_write", map[string]any{"action": "new", "title": "Rotate the postgres backup credentials"}).Card
	bobCard = ok[cardOut](t, bob, "card_write", map[string]any{"action": "new", "title": "postgres backup credentials rotation script", "body": "uses vaultwarden"}).Card
	e.coordRun()
	return ann, bob, annCard, bobCard
}

func TestCoord_COORD_C4_AnnSeesNoWordOnlyInBobsCardAndNotItsRef(t *testing.T) {
	e := newEnv(t)
	ann, _, _, bobCard := coordPair(t, e)

	notices := ok[coordOut](t, ann, "coordination", map[string]any{"action": "list"}).Notices
	if len(notices) != 1 || notices[0].With != "bob" {
		t.Fatalf("notices = %+v", notices)
	}
	brief := ok[platform.Brief](t, ann, "board_brief", nil)
	if len(brief.Notices) != 1 || brief.Notices[0].ID != notices[0].ID {
		t.Fatalf("brief notices = %+v", brief.Notices)
	}
	answers := map[string]any{"notices": notices, "brief": brief,
		"platform": e.coordMust(e.p.Notices(t.Context(), coordPrincipal(t, e, "ann"), "APP"))}
	for name, answer := range answers {
		raw, err := json.Marshal(answer)
		if err != nil {
			t.Fatal(err)
		}
		for _, word := range []string{"rotation", "script", "vaultwarden", bobCard.Ref, bobCard.ID.String()} {
			if strings.Contains(strings.ToLower(string(raw)), strings.ToLower(word)) {
				t.Errorf("%s answer holds %q: %s", name, word, raw)
			}
		}
	}
}

func (e *env) coordMust(v any, err error) any {
	e.t.Helper()
	if err != nil {
		e.t.Fatal(err)
	}
	return v
}

func coordPrincipal(t *testing.T, e *env, handle string) platform.Principal {
	t.Helper()
	m, err := e.p.Member(t.Context(), "acme", handle)
	if err != nil {
		t.Fatal(err)
	}
	return platform.MemberAgent(m.OrgID, m.ID, handle, "laptop", "")
}

func TestCoord_COORD_C6_BriefListsANoticeUntilItIsDismissedForGood(t *testing.T) {
	e := newEnv(t)
	ann, _, annCard, _ := coordPair(t, e)

	brief := ok[platform.Brief](t, ann, "board_brief", nil)
	if len(brief.Notices) != 1 || brief.Notices[0].Card != annCard.Ref {
		t.Fatalf("brief notices = %+v", brief.Notices)
	}
	left := ok[coordOut](t, ann, "coordination", map[string]any{"action": "dismiss", "id": brief.Notices[0].ID.String()}).Notices
	if len(left) != 0 {
		t.Fatalf("after dismiss = %+v", left)
	}
	if brief := ok[platform.Brief](t, ann, "board_brief", nil); len(brief.Notices) != 0 {
		t.Fatalf("brief after dismiss = %+v", brief.Notices)
	}
	e.coordRun()
	e.coordRun()
	if brief := ok[platform.Brief](t, ann, "board_brief", nil); len(brief.Notices) != 0 {
		t.Fatalf("brief after later runs = %+v", brief.Notices)
	}
}

func TestCoord_COORD_C7_DismissingAnotherMembersNoticeIsNotFound(t *testing.T) {
	e := newEnv(t)
	ann, bob, _, _ := coordPair(t, e)
	bobNotice := ok[coordOut](t, bob, "coordination", map[string]any{"action": "list"}).Notices[0]

	got := fails(t, ann, "coordination", map[string]any{"action": "dismiss", "id": bobNotice.ID.String()})
	missing := fails(t, ann, "coordination", map[string]any{"action": "dismiss", "id": "01a18867-6984-7e14-a048-252908d33801"})
	if got.Code != "notice_not_found" || got != missing {
		t.Fatalf("foreign dismiss = %+v, missing dismiss = %+v", got, missing)
	}
	still := ok[coordOut](t, bob, "coordination", map[string]any{"action": "list"}).Notices
	if len(still) != 1 || still[0].ID != bobNotice.ID {
		t.Fatalf("bob notices = %+v", still)
	}
}
