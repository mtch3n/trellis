package mcpserver_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/mtch3n/trellis/internal/platform"
)

type cardOut struct {
	Card    platform.Card     `json:"card"`
	Comment *platform.Comment `json:"comment"`
}

type claimOut struct {
	Card *platform.Card `json:"card"`
}

type boardOut struct {
	Board platform.Board `json:"board"`
}

type sessionClient struct {
	cs    *mcp.ClientSession
	actor string
}

var toolNames = []string{"board_brief", "board_list", "board_write", "card_claim", "card_list", "card_show", "card_write", "project_list"}

var readTools = []string{"board_brief", "board_list", "card_list", "card_show", "project_list"}

func listTools(t *testing.T, cs *mcp.ClientSession) []*mcp.Tool {
	t.Helper()
	res, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return res.Tools
}

func names(tools []*mcp.Tool) []string {
	var out []string
	for _, tl := range tools {
		out = append(out, tl.Name)
	}
	slices.Sort(out)
	return out
}

func TestPlat_PLAT_C29_NewAndLegacyProtocolsListTheSameTools(t *testing.T) {
	e := newEnv(t)
	token := e.member("ming", "claude-laptop")
	modern := e.connect(token, nil, "2026-07-28")
	legacy := e.connect(token, nil, "2025-06-18")
	if v := modern.InitializeResult().ProtocolVersion; v != "2026-07-28" {
		t.Fatalf("modern client negotiated %q", v)
	}
	if v := legacy.InitializeResult().ProtocolVersion; v != "2025-06-18" {
		t.Fatalf("legacy client negotiated %q", v)
	}
	a, b := names(listTools(t, modern)), names(listTools(t, legacy))
	if !slices.Equal(a, b) || len(a) == 0 {
		t.Fatalf("modern tools %v, legacy tools %v", a, b)
	}
}

func TestPlat_PLAT_C31_ExactlyEightToolsWithSchemasAndReadOnlyHints(t *testing.T) {
	e := newEnv(t)
	cs := e.agent("ming", "claude-laptop", nil)
	tools := listTools(t, cs)
	if got := names(tools); !slices.Equal(got, toolNames) {
		t.Fatalf("tools = %v, want %v", got, toolNames)
	}
	for _, tl := range tools {
		if tl.InputSchema == nil || tl.OutputSchema == nil {
			t.Errorf("%s: input schema %v, output schema %v", tl.Name, tl.InputSchema != nil, tl.OutputSchema != nil)
		}
		hint := tl.Annotations != nil && tl.Annotations.ReadOnlyHint
		if want := slices.Contains(readTools, tl.Name); hint != want {
			t.Errorf("%s: readOnlyHint = %v, want %v", tl.Name, hint, want)
		}
	}
}

func TestPlat_PLAT_C32_UnknownActionOrMissingFieldIsUsageAndWritesNothing(t *testing.T) {
	e := newEnv(t)
	cs := e.agent("ming", "claude-laptop", nil)
	card := ok[cardOut](t, cs, "card_write", map[string]any{"action": "new", "title": "ship"}).Card
	cards, boards := e.count("card"), e.count("board")

	for _, c := range []struct {
		tool   string
		args   map[string]any
		action string
		field  string
	}{
		{"board_write", map[string]any{"action": "fly", "name": "x"}, "fly", "action"},
		{"card_write", map[string]any{"action": "teleport", "title": "x"}, "teleport", "action"},
		{"card_claim", map[string]any{"action": "grab", "ref": card.Ref}, "grab", "action"},
		{"board_write", map[string]any{"action": "new"}, "new", "name"},
		{"board_write", map[string]any{"action": "rename", "board": "ming"}, "rename", "name"},
		{"card_write", map[string]any{"action": "new"}, "new", "title"},
		{"card_write", map[string]any{"action": "move", "ref": card.Ref}, "move", "column"},
		{"card_write", map[string]any{"action": "comment", "ref": card.Ref}, "comment", "body"},
		{"card_claim", map[string]any{"action": "claim"}, "claim", "ref"},
	} {
		got := fails(t, cs, c.tool, c.args)
		if !strings.Contains(got.Message, c.action) || !strings.Contains(got.Message, c.field) {
			t.Errorf("%s %v: message %q does not name %s and %s", c.tool, c.args, got.Message, c.action, c.field)
		}
		if got.Code != "bad_action" && got.Code != "missing_field" {
			t.Errorf("%s %v: code %q, want a usage code", c.tool, c.args, got.Code)
		}
	}
	if e.count("card") != cards || e.count("board") != boards || e.count("comment") != 0 {
		t.Fatal("a refused call wrote something")
	}
	if shown := ok[platform.Card](t, cs, "card_show", map[string]any{"ref": card.Ref}); shown.ClaimedBy != nil || shown.Column != "backlog" {
		t.Fatalf("the card changed: %+v", shown)
	}
}

func TestPlat_PLAT_C33_AgentRunsACardFromBoardToDone(t *testing.T) {
	e := newEnv(t)
	cs := e.agent("ming", "claude-laptop", nil)
	board := ok[boardOut](t, cs, "board_write", map[string]any{"action": "new", "name": "work"}).Board
	card := ok[cardOut](t, cs, "card_write", map[string]any{"action": "new", "board": board.Name, "title": "ship"}).Card
	ok[claimOut](t, cs, "card_claim", map[string]any{"action": "claim", "ref": card.Ref})
	ok[cardOut](t, cs, "card_write", map[string]any{"action": "comment", "ref": card.Ref, "body": "tested"})
	ok[cardOut](t, cs, "card_write", map[string]any{"action": "move", "ref": card.Ref, "column": "done"})

	got := ok[platform.Card](t, cs, "card_show", map[string]any{"ref": card.Ref})
	if got.Board != "work" || got.Column != "done" || !got.Done {
		t.Fatalf("card on %q in %q done=%v", got.Board, got.Column, got.Done)
	}
	if got.ClaimedBy != nil {
		t.Fatalf("card still claimed by %s", *got.ClaimedBy)
	}
	if len(got.Comments) != 1 || got.Comments[0].Body != "tested" {
		t.Fatalf("comments = %+v", got.Comments)
	}
}

func TestPlat_PLAT_C34_TheProjectHeaderAndNoBoardGiveThePersonalBoard(t *testing.T) {
	e := newEnv(t)
	cs := e.connect(e.member("ming", "claude-laptop"), headers{"X-Trellis-Project": "APP"}, "")
	card := ok[cardOut](t, cs, "card_write", map[string]any{"action": "new", "title": "ship"}).Card
	if !strings.HasPrefix(card.Ref, "APP-") || card.Board != "ming" {
		t.Fatalf("card %s on board %q, want APP-n on ming", card.Ref, card.Board)
	}
	boards := ok[struct {
		Boards []platform.Board `json:"boards"`
	}](t, cs, "board_list", nil).Boards
	i := slices.IndexFunc(boards, func(b platform.Board) bool { return b.ID == card.BoardID })
	if i < 0 || boards[i].Owner != "ming" || boards[i].Scope != platform.ScopePersonal {
		t.Fatalf("the card's board is not ming's personal board: %+v", boards)
	}
}

func TestPlat_PLAT_C35_NoProjectArgumentAndNoHeaderIsUnresolved(t *testing.T) {
	e := newEnv(t)
	cs := e.connect(e.member("ming", "claude-laptop"), nil, "")
	for _, tool := range []string{"board_list", "board_brief", "card_list"} {
		got := fails(t, cs, tool, nil)
		if got.Code != "unresolved" || !strings.Contains(got.Fix, "project") || !strings.Contains(got.Fix, "X-Trellis-Project") {
			t.Fatalf("%s: %+v, want unresolved with a fix that names project and X-Trellis-Project", tool, got)
		}
	}
	got := fails(t, cs, "card_write", map[string]any{"action": "new", "title": "ship"})
	if got.Code != "unresolved" {
		t.Fatalf("card_write: %+v", got)
	}
}

func TestPlat_PLAT_C36_MissingAndForeignProjectsAnswerTheSameNotFound(t *testing.T) {
	e := newEnv(t)
	e.must(e.p.CreateProject(t.Context(), "acme", "OTHER", "Other"))
	cs := e.agent("ming", "claude-laptop", nil)
	missing := fails(t, cs, "card_list", map[string]any{"project": "NOPE"})
	foreign := fails(t, cs, "card_list", map[string]any{"project": "OTHER"})
	if missing.Code != "project_not_found" || missing != foreign {
		t.Fatalf("missing %+v, foreign %+v", missing, foreign)
	}
}

func TestPlat_PLAT_C37_AMissingCardIsAToolErrorWithCodeMessageAndFix(t *testing.T) {
	e := newEnv(t)
	cs := e.agent("ming", "claude-laptop", nil)
	got := fails(t, cs, "card_show", map[string]any{"ref": "APP-999"})
	if got.Code != "card_not_found" || got.Message == "" || got.Fix == "" {
		t.Fatalf("error = %+v", got)
	}
}

type invocationRow struct {
	Tool     string `db:"tool"`
	Action   string `db:"action"`
	ArgNames string `db:"arg_names"`
	MemberID string `db:"member_id"`
	Actor    string `db:"actor"`
	Code     string `db:"code"`
	Row      string `db:"row"`
}

func (e *env) invocations() []invocationRow {
	e.t.Helper()
	var rows []invocationRow
	if err := e.db.Select(&rows, `SELECT tool, action, array_to_string(arg_names, ',') AS arg_names,
		member_id::text AS member_id, actor, code, i::text AS row FROM invocation i ORDER BY id`); err != nil {
		e.t.Fatal(err)
	}
	return rows
}

func TestPlat_PLAT_C38_ACardWriteLogsOneInvocationRow(t *testing.T) {
	e := newEnv(t)
	cs := e.agent("ming", "claude-laptop", headers{"X-Trellis-Session": "s1"})
	m, err := e.p.Member(t.Context(), "acme", "ming")
	if err != nil {
		t.Fatal(err)
	}
	ok[cardOut](t, cs, "card_write", map[string]any{"action": "new", "title": "ship", "body": "now"})

	rows := e.invocations()
	if len(rows) != 1 {
		t.Fatalf("%d invocation rows, want 1: %+v", len(rows), rows)
	}
	r := rows[0]
	want := invocationRow{Tool: "card_write", Action: "new", ArgNames: "action,body,title", MemberID: m.ID.String(),
		Actor: "agent:ming/claude-laptop/s1", Code: "0", Row: r.Row}
	if r != want {
		t.Fatalf("row = %+v, want %+v", r, want)
	}
}

func TestPlat_PLAT_C39_TheLogHoldsNoArgumentTextAndTheRefusedCode(t *testing.T) {
	e := newEnv(t)
	cs := e.agent("ming", "claude-laptop", nil)
	ok[cardOut](t, cs, "card_write", map[string]any{"action": "new", "title": "zebrafish lantern", "body": "quokka marmalade"})
	fails(t, cs, "card_show", map[string]any{"ref": "APP-999"})
	fails(t, cs, "card_write", map[string]any{"action": "walrus", "title": "narwhal"})

	rows := e.invocations()
	if len(rows) != 3 {
		t.Fatalf("%d rows, want 3", len(rows))
	}
	for _, r := range rows {
		for _, word := range []string{"zebrafish", "lantern", "quokka", "marmalade", "APP-999", "walrus", "narwhal"} {
			if strings.Contains(r.Row, word) {
				t.Errorf("row %q holds %q", r.Row, word)
			}
		}
	}
	if rows[1].Tool != "card_show" || rows[1].Code != "card_not_found" {
		t.Fatalf("refused row = %+v", rows[1])
	}
	if rows[2].Code != "bad_action" {
		t.Fatalf("bad action row = %+v", rows[2])
	}
}

func TestPlat_PLAT_C40_BriefCountsVisibleBoardsClaimsAndTheNextCard(t *testing.T) {
	e := newEnv(t)
	a := e.agent("ann", "laptop", nil)
	b := e.agent("bob", "laptop", nil)
	team := ok[boardOut](t, a, "board_write", map[string]any{"action": "new", "name": "team"}).Board
	ok[boardOut](t, a, "board_write", map[string]any{"action": "share", "board": team.Name})
	ok[cardOut](t, a, "card_write", map[string]any{"action": "new", "board": "team", "title": "shared"})
	claimed := ok[cardOut](t, a, "card_write", map[string]any{"action": "new", "title": "mine"}).Card
	next := ok[cardOut](t, a, "card_write", map[string]any{"action": "new", "title": "after"}).Card
	ok[claimOut](t, a, "card_claim", map[string]any{"action": "claim", "ref": claimed.Ref})
	ok[cardOut](t, b, "card_write", map[string]any{"action": "new", "title": "bob secret"})
	ok[boardOut](t, b, "board_write", map[string]any{"action": "new", "name": "bobs"})

	brief := ok[platform.Brief](t, a, "board_brief", nil)
	got := map[string]int{}
	for _, bc := range brief.Boards {
		for _, c := range bc.Columns {
			got[bc.Owner+"/"+bc.Board+"/"+c.Column] = c.Count
		}
	}
	if got["ann/ann/backlog"] != 2 || got["ann/team/backlog"] != 1 {
		t.Fatalf("counts = %v", got)
	}
	for _, bc := range brief.Boards {
		if bc.Owner == "bob" {
			t.Fatalf("brief holds bob's board %s", bc.Board)
		}
	}
	if len(brief.Claimed) != 1 || brief.Claimed[0].Ref != claimed.Ref {
		t.Fatalf("claimed = %+v", brief.Claimed)
	}
	if brief.Next == nil || brief.Next.Ref != next.Ref {
		t.Fatalf("next = %+v, want %s", brief.Next, next.Ref)
	}
}

func TestPlat_PLAT_C41_BriefOfAnEmptyProjectIsZeroNotAnError(t *testing.T) {
	e := newEnv(t)
	cs := e.agent("ming", "claude-laptop", nil)
	brief := ok[platform.Brief](t, cs, "board_brief", nil)
	if len(brief.Boards) != 1 || brief.Boards[0].Board != "ming" {
		t.Fatalf("boards = %+v", brief.Boards)
	}
	for _, c := range brief.Boards[0].Columns {
		if c.Count != 0 {
			t.Fatalf("column %s has %d cards", c.Column, c.Count)
		}
	}
	if len(brief.Claimed) != 0 || brief.Next != nil {
		t.Fatalf("claimed %+v, next %+v", brief.Claimed, brief.Next)
	}
}
