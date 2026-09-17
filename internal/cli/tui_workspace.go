package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/jmoiron/sqlx"
	"github.com/mtch3n/trellis/internal/core"
	"github.com/rivo/tview"
)

var (
	tuiBG       = tcell.GetColor("#111820")
	tuiFG       = tcell.GetColor("#dbe5ed")
	tuiMuted    = tcell.GetColor("#9cabb9")
	tuiAccent   = tcell.GetColor("#7ddbc4")
	tuiSelected = tcell.GetColor("#284b52")
)

// terminalWorkspace owns widgets and data on the application's event loop.
type terminalWorkspace struct {
	ctx                         context.Context
	session                     tuiSession
	app                         *tview.Application
	pages                       *tview.Pages
	root, body, content, bottom *tview.Flex
	header, status, preview     *tview.TextView
	sidebar                     *tview.List
	input                       *tview.InputField
	tree                        *tview.TreeView
	columns                     []core.Column
	cards                       []core.Card
	boards                      []core.Board
	entries                     []core.Entry
	lists                       []*tview.List
	laneCards                   [][]core.Card
	view                        string
	lane                        int
	selected                    *core.Card
	entry                       *core.Entry
	filter                      string
	inputMode                   string
	modal                       bool
	hideSidebar, reading        bool
	width, height               int
}

func tuiText(s string) string { return tview.Escape(safeTerminalText(s)) }
func tuiBox(b *tview.Box, title string) {
	b.SetBackgroundColor(tuiBG).SetBorder(true).SetBorderColor(tuiMuted).SetTitle(" " + title + " ").SetTitleColor(tuiAccent)
	b.SetFocusFunc(func() { b.SetBorderColor(tuiAccent) }).SetBlurFunc(func() { b.SetBorderColor(tuiMuted) })
}
func tuiView() *tview.TextView {
	v := tview.NewTextView().SetDynamicColors(true).SetWordWrap(true).SetTextColor(tuiFG)
	v.SetBackgroundColor(tuiBG)
	return v
}
func tuiList() *tview.List {
	l := tview.NewList().ShowSecondaryText(true).SetMainTextColor(tuiFG).SetSecondaryTextColor(tuiMuted).SetSelectedTextColor(tuiFG).SetSelectedBackgroundColor(tuiSelected).SetHighlightFullLine(true)
	l.SetBackgroundColor(tuiBG)
	return l
}
func newTerminalWorkspace(ctx context.Context, app *appCtx) *terminalWorkspace {
	w := &terminalWorkspace{ctx: ctx, session: tuiSession{app: app, seen: make(map[string]int64)}, app: tview.NewApplication(), view: "board", width: 120, height: 36}
	w.pages = tview.NewPages()
	w.header = tuiView()
	w.status = tuiView()
	w.preview = tuiView()
	w.sidebar = tuiList().ShowSecondaryText(false)
	tuiBox(w.sidebar.Box, "Workspace")
	tuiBox(w.preview.Box, "Preview")
	w.content = tview.NewFlex()
	w.body = tview.NewFlex()
	w.bottom = tview.NewFlex().SetDirection(tview.FlexRow)
	w.root = tview.NewFlex().SetDirection(tview.FlexRow).AddItem(w.header, 2, 0, false).AddItem(w.body, 0, 1, true).AddItem(w.bottom, 2, 0, false)
	w.root.SetBackgroundColor(tuiBG)
	w.pages.AddPage("workspace", w.root, true, true)
	w.app.SetRoot(w.pages, true).EnableMouse(true).EnablePaste(true).SetInputCapture(w.key)
	w.app.SetBeforeDrawFunc(func(s tcell.Screen) bool {
		width, height := s.Size()
		if width != w.width || height != w.height {
			w.width, w.height = width, height
			w.layout()
		}
		return false
	})
	w.hints()
	return w
}
func (w *terminalWorkspace) run() error {
	ctx, stop := signal.NotifyContext(w.ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			w.app.Stop()
		case <-done:
		}
	}()
	return w.app.Run()
}
func (w *terminalWorkspace) reload() error {
	a := w.session.app
	boards, err := a.Core.ListBoards(w.ctx, a.Project.ID)
	if err != nil {
		return err
	}
	columns, err := a.Core.ListColumns(w.ctx, a.Board.ID)
	if err != nil {
		return err
	}
	cards, err := a.Core.ListCardsPage(w.ctx, core.CardScope{BoardID: a.Board.ID}, core.CardFilter{Limit: -1})
	if err != nil {
		return err
	}
	w.boards, w.columns, w.cards = boards, columns, cards.Cards
	w.header.SetText(fmt.Sprintf(" [::b]TRELLIS[::-]  /  %s  /  %s\n [#9cabb9]Local workspace · %d cards[-]", tuiText(a.Project.Key), tuiText(a.Board.Name), cards.Total))
	w.sidebar.Clear()
	w.sidebar.AddItem("Boards", "", '1', func() { w.switchView("board") }).AddItem("Knowledge", "", '2', func() { w.switchView("vault") }).AddItem("Activity", "", '3', func() { w.switchView("activity") })
	for _, board := range w.boards {
		label := "  " + board.Name
		if board.ID == a.Board.ID {
			label = "● " + board.Name
		}
		w.sidebar.AddItem(tuiText(label), "", 0, func() { a.Board = board; w.filter = ""; w.view = "board"; w.refresh() })
	}
	w.sidebar.AddItem("Help", "", '?', w.help)
	w.render()
	return nil
}
func (w *terminalWorkspace) refresh() {
	if err := w.reload(); err != nil {
		w.message("Refresh failed", err.Error())
	}
}
func (w *terminalWorkspace) switchView(view string) {
	w.view = view
	w.filter = ""
	w.reading = false
	w.render()
	w.focusContent()
}
func (w *terminalWorkspace) render() {
	w.selected = nil
	w.entry = nil
	w.preview.SetText("").ScrollToBeginning()
	switch w.view {
	case "vault":
		w.renderVault()
	case "activity":
		w.renderActivity()
	default:
		w.renderBoard()
	}
	w.layout()
	w.hints()
}
func (w *terminalWorkspace) layout() {
	w.body.Clear()
	w.content.Clear()
	if !w.hideSidebar && !w.reading && w.width >= 75 {
		w.body.AddItem(w.sidebar, 22, 0, false)
	}
	w.body.AddItem(w.content, 0, 1, true)
	if w.reading {
		w.content.AddItem(w.preview, 0, 1, true)
		return
	}
	switch w.view {
	case "vault":
		if w.width < 90 {
			w.content.AddItem(w.tree, 0, 1, true)
		} else {
			w.content.AddItem(w.tree, 30, 0, true).AddItem(w.preview, 0, 1, false)
		}
	case "activity":
		w.content.AddItem(w.preview, 0, 1, true)
	default:
		board := tview.NewFlex()
		available := w.width
		if !w.hideSidebar && w.width >= 75 {
			available -= 22
		}
		visible := max(1, available/26)
		start := 0
		if w.lane >= visible {
			start = w.lane - visible + 1
		}
		for i := start; i < min(len(w.lists), start+visible); i++ {
			board.AddItem(w.lists[i], 0, 1, i == w.lane)
		}
		if len(w.lists) == 0 {
			board.AddItem(tuiView().SetText("\n No columns in this board.\n Use trellis column new --help to add one."), 0, 1, false)
		}
		stack := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(board, 0, 1, true)
		if w.height >= 25 {
			stack.AddItem(w.preview, 8, 0, false)
		}
		w.content.AddItem(stack, 0, 1, true)
	}
}
func (w *terminalWorkspace) renderBoard() {
	w.lists = nil
	w.laneCards = nil
	w.lane = min(w.lane, max(0, len(w.columns)-1))
	for i, column := range w.columns {
		list := tuiList()
		var cards []core.Card
		for _, card := range w.cards {
			if card.ColumnID == column.ID && matchesFilter(w.filter, card.Ref, card.Title, card.BodyMD) {
				cards = append(cards, card)
			}
		}
		tuiBox(list.Box, fmt.Sprintf("%s · %d", tuiText(column.Name), len(cards)))
		for _, card := range cards {
			list.AddItem(tuiText(card.Title), tuiText(card.Ref+"  ·  "+card.PriorityName), 0, nil)
		}
		if len(cards) == 0 {
			list.AddItem("No cards", "n creates a card", 0, nil)
		}
		w.laneCards = append(w.laneCards, cards)
		w.lists = append(w.lists, list)
		list.SetChangedFunc(func(index int, _ string, _ string, _ rune) { w.lane = i; w.selectCard(index) })
		list.SetSelectedFunc(func(index int, _ string, _ string, _ rune) {
			w.lane = i
			w.selectCard(index)
			if w.selected != nil {
				w.reading = true
				w.layout()
				w.app.SetFocus(w.preview)
			}
		})
		list.SetFocusFunc(func() { list.SetBorderColor(tuiAccent); w.lane = i; w.selectCard(list.GetCurrentItem()) })
	}
	if len(w.lists) > 0 {
		w.selectCard(w.lists[w.lane].GetCurrentItem())
	}
}
func matchesFilter(query string, values ...string) bool {
	return strings.Contains(strings.ToLower(strings.Join(values, " ")), strings.ToLower(query))
}
func (w *terminalWorkspace) selectCard(index int) {
	w.selected = nil
	if w.lane >= len(w.laneCards) || index < 0 || index >= len(w.laneCards[w.lane]) {
		w.preview.SetText("\n No card selected. Press n to create one.")
		return
	}
	card := w.laneCards[w.lane][index]
	w.selected = &card
	w.session.seen[card.ID] = card.Version
	text := fmt.Sprintf("[::b]%s  %s[::-]\n[#7ddbc4]%s · %s[-]\n\n%s", tuiText(card.Ref), tuiText(card.Title), tuiText(card.ColumnName), tuiText(card.PriorityName), terminalMarkdown(card.BodyMD))
	notes, err := w.session.app.Core.GetCommentsByCard(w.ctx, card.ID)
	if err != nil {
		text += "\n\nNotes unavailable: " + tuiText(err.Error())
	} else {
		for _, note := range notes {
			text += "\n\n[#7ddbc4]" + tuiText(note.Actor) + "[-]\n" + terminalMarkdown(note.BodyMD)
		}
	}
	w.preview.SetTitle(" Card · Enter read · e edit · m move · a note ")
	w.preview.SetText(text).ScrollToBeginning()
}
func (w *terminalWorkspace) renderVault() {
	root := tview.NewTreeNode("Documents").SetColor(tuiAccent)
	w.tree = tview.NewTreeView().SetRoot(root).SetCurrentNode(root).SetGraphicsColor(tuiMuted)
	tuiBox(w.tree.Box, "Knowledge")
	entries, err := w.session.app.Core.ListEntries(w.ctx, w.session.app.Project.ID, core.EntryFilter{})
	if err != nil {
		w.preview.SetText(tuiText(err.Error()))
		return
	}
	w.entries = entries
	slices.SortFunc(entries, func(a, b core.Entry) int { return strings.Compare(a.Template+"/"+a.Title, b.Template+"/"+b.Title) })
	groups := map[string]*tview.TreeNode{}
	var first *tview.TreeNode
	for _, entry := range entries {
		if !matchesFilter(w.filter, entry.Title, entry.Slug, entry.BodyMD) {
			continue
		}
		group := groups[entry.Template]
		if group == nil {
			group = tview.NewTreeNode(tuiText(entry.Template)).SetColor(tuiAccent)
			groups[entry.Template] = group
			root.AddChild(group)
		}
		node := tview.NewTreeNode(tuiText(entry.Title)).SetColor(tuiFG).SetReference(entry)
		group.AddChild(node)
		if first == nil {
			first = node
		}
	}
	w.tree.SetChangedFunc(func(node *tview.TreeNode) {
		if entry, ok := node.GetReference().(core.Entry); ok {
			w.selectEntry(entry)
		}
	})
	w.tree.SetSelectedFunc(func(node *tview.TreeNode) {
		if entry, ok := node.GetReference().(core.Entry); ok {
			w.selectEntry(entry)
			w.reading = true
			w.layout()
			w.app.SetFocus(w.preview)
		} else {
			node.SetExpanded(!node.IsExpanded())
		}
	})
	if first != nil {
		w.tree.SetCurrentNode(first)
		w.selectEntry(first.GetReference().(core.Entry))
	} else {
		w.preview.SetText("\n No matching documents.\n Press n to create a knowledge entry.")
	}
}
func (w *terminalWorkspace) selectEntry(entry core.Entry) {
	w.entry = &entry
	text := "[::b]" + tuiText(entry.Title) + "[::-]\n[#9cabb9]" + tuiText(entry.Ref) + " · " + tuiText(entry.Template) + "[-]\n\n" + terminalMarkdown(entry.BodyMD)
	links, err := w.session.app.Core.Backlinks(w.ctx, entry.ID)
	if err != nil {
		text += "\n\nBacklinks unavailable: " + tuiText(err.Error())
	} else if len(links) > 0 {
		text += "\n\n[#7ddbc4::b]Linked from[::-]\n"
		for _, link := range links {
			text += tuiText(link.Ref+"  "+link.Title) + "\n"
		}
	}
	w.preview.SetTitle(" Document · Enter read · e edit ")
	w.preview.SetText(text).ScrollToBeginning()
}

// activityRow is one line of the workspace's activity view.
type activityRow struct {
	TS     int64  `db:"ts"`
	Actor  string `db:"actor"`
	Action string `db:"action"`
	Kind   string `db:"entity_type"`
	Title  string `db:"title"`
}

// recentActivity is a project's latest 100 events, newest first, reads
// excluded. A comment's title is its card's.
func recentActivity(ctx context.Context, db *sqlx.DB, projectID string) ([]activityRow, error) {
	var rows []activityRow
	err := db.SelectContext(ctx, &rows, `
		SELECT e.ts, e.actor, e.action, e.entity_type,
		       COALESCE(c.title, k.title, cc.title, b.name, '') AS title
		FROM event e
		LEFT JOIN card c ON e.entity_type = 'card' AND e.entity_id = c.id
		LEFT JOIN entry k ON e.entity_type = 'entry' AND e.entity_id = k.id
		LEFT JOIN comment cm ON e.entity_type = 'comment' AND e.entity_id = cm.id
		LEFT JOIN card cc ON cc.id = cm.card_id
		LEFT JOIN board b ON e.entity_type = 'board' AND e.entity_id = b.id
		WHERE e.project_id = ? AND e.action <> 'read'
		ORDER BY e.seq DESC LIMIT 100`, projectID)
	return rows, err
}

func (w *terminalWorkspace) renderActivity() {
	rows, err := recentActivity(w.ctx, w.session.app.db, w.session.app.Project.ID)
	w.preview.SetTitle(" Activity · latest 100 events ")
	if err != nil {
		w.preview.SetText(tuiText(err.Error()))
		return
	}
	var out strings.Builder
	for _, row := range rows {
		if !matchesFilter(w.filter, row.Title, row.Actor, row.Action) {
			continue
		}
		fmt.Fprintf(&out, "[#9cabb9]%s[-]  [#7ddbc4]%s[-] %s\n  %s · %s\n\n", time.UnixMilli(row.TS).Format("Jan 02 15:04"), tuiText(row.Kind), tuiText(row.Action), tuiText(row.Title), tuiText(row.Actor))
	}
	if out.Len() == 0 {
		out.WriteString("\n No activity to show.")
	}
	w.preview.SetText(out.String())
}
func terminalMarkdown(body string) string {
	var out strings.Builder
	code := false
	for line := range strings.SplitSeq(safeTerminalText(body), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			code = !code
			out.WriteString("[#9cabb9]────────────────────[-]\n")
			continue
		}
		switch {
		case code:
			out.WriteString("[#e9ca8b]" + tuiText(line) + "[-]\n")
		case strings.HasPrefix(line, "#"):
			out.WriteString("\n[#7ddbc4::b]" + tuiText(strings.TrimLeft(line, "# ")) + "[::-]\n")
		case strings.HasPrefix(line, "> "):
			out.WriteString("[#9cabb9]│ " + tuiText(strings.TrimPrefix(line, "> ")) + "[-]\n")
		default:
			out.WriteString(tuiText(line) + "\n")
		}
	}
	return out.String()
}
func (w *terminalWorkspace) hints() {
	text := " Tab panels  / filter  : commands  n new  e edit  m move  ? help  q quit"
	if w.view == "vault" {
		text = " Tab panels  / filter  Enter read  e edit  n new  b sidebar  ? help"
	}
	if w.reading {
		text = " Esc back  ↑↓ scroll  e edit  b sidebar  : commands  ? help"
	}
	if w.filter != "" {
		text += "  Filter: " + tuiText(w.filter)
	}
	w.status.SetText(text)
	if w.inputMode == "" {
		w.bottom.Clear().AddItem(w.status, 2, 0, false)
	}
}
func (w *terminalWorkspace) focusContent() {
	if w.reading || w.view == "activity" {
		w.app.SetFocus(w.preview)
	} else if w.view == "vault" {
		w.app.SetFocus(w.tree)
	} else if len(w.lists) > 0 {
		w.app.SetFocus(w.lists[w.lane])
	} else {
		w.app.SetFocus(w.sidebar)
	}
}
func (w *terminalWorkspace) key(event *tcell.EventKey) *tcell.EventKey {
	if event.Key() == tcell.KeyCtrlC {
		w.app.Stop()
		return nil
	}
	if w.modal {
		return event
	}
	if w.inputMode != "" {
		return event
	}
	if event.Key() == tcell.KeyEscape {
		if w.reading {
			w.reading = false
			w.layout()
		} else if w.filter != "" {
			w.filter = ""
			w.render()
		}
		w.focusContent()
		w.hints()
		return nil
	}
	if event.Key() == tcell.KeyTab || event.Key() == tcell.KeyBacktab {
		targets := []tview.Primitive{}
		if !w.hideSidebar && !w.reading && w.width >= 75 {
			targets = append(targets, w.sidebar)
		}
		if w.reading || w.view == "activity" {
			targets = append(targets, w.preview)
		} else if w.view == "vault" {
			targets = append(targets, w.tree)
			if w.width >= 90 {
				targets = append(targets, w.preview)
			}
		} else {
			for _, l := range w.lists {
				targets = append(targets, l)
			}
		}
		if len(targets) > 0 {
			index := slices.Index(targets, w.app.GetFocus())
			step := 1
			if event.Key() == tcell.KeyBacktab {
				step = -1
			}
			index = (index + step + len(targets)) % len(targets)
			w.app.SetFocus(targets[index])
			w.layout()
		}
		return nil
	}
	if !w.reading && w.view == "board" && w.app.GetFocus() != w.sidebar && (event.Key() == tcell.KeyLeft || event.Key() == tcell.KeyRight || event.Rune() == 'h' || event.Rune() == 'l') {
		delta := 1
		if event.Key() == tcell.KeyLeft || event.Rune() == 'h' {
			delta = -1
		}
		w.lane = max(0, min(len(w.lists)-1, w.lane+delta))
		w.layout()
		w.focusContent()
		return nil
	}
	switch event.Rune() {
	case 'q':
		w.app.Stop()
		return nil
	case '1':
		w.switchView("board")
		return nil
	case '2':
		w.switchView("vault")
		return nil
	case '3':
		w.switchView("activity")
		return nil
	case 'b':
		w.hideSidebar = !w.hideSidebar
		w.layout()
		w.focusContent()
		return nil
	case '/':
		w.openInput("filter")
		return nil
	case ':':
		w.palette()
		return nil
	case '?':
		w.help()
		return nil
	case 'r':
		w.refresh()
		w.focusContent()
		return nil
	case 'n':
		w.edit(true)
		return nil
	case 'e':
		w.edit(false)
		return nil
	case 'm':
		w.move()
		return nil
	case 'a':
		w.note()
		return nil
	case 'j':
		return tcell.NewEventKey(tcell.KeyDown, 0, event.Modifiers())
	case 'k':
		return tcell.NewEventKey(tcell.KeyUp, 0, event.Modifiers())
	}
	return event
}
func (w *terminalWorkspace) openInput(mode string) {
	w.inputMode = mode
	w.input = tview.NewInputField().SetLabel(" / Filter  ").SetFieldBackgroundColor(tuiSelected).SetFieldTextColor(tuiFG).SetLabelColor(tuiAccent)
	w.input.SetBackgroundColor(tuiBG)
	if mode == "command" {
		w.input.SetLabel(" : Command  ").SetPlaceholder("/help · Tab completes commands")
	}
	original := w.filter
	if mode == "filter" {
		w.input.SetText(w.filter).SetChangedFunc(func(text string) { w.filter = text; w.render() })
	}
	w.input.SetInputCapture(func(e *tcell.EventKey) *tcell.EventKey {
		if e.Key() == tcell.KeyTab && mode == "command" {
			s := w.input.GetText()
			if result, _, ok := completeTUI(s, len(s), '\t'); ok {
				w.input.SetText(result)
			}
			return nil
		}
		return e
	})
	w.input.SetDoneFunc(func(key tcell.Key) {
		if key != tcell.KeyEnter && key != tcell.KeyEscape {
			return
		}
		text := w.input.GetText()
		w.inputMode = ""
		if key == tcell.KeyEscape && mode == "filter" {
			w.filter = original
			w.render()
		}
		w.hints()
		w.focusContent()
		if key == tcell.KeyEnter && mode == "command" {
			if text == "/quit" {
				w.app.Stop()
				return
			}
			out, err := w.session.execute(w.ctx, text)
			if err != nil {
				w.message("Command failed", err.Error())
				return
			}
			w.refresh()
			w.message("Command result", out)
		}
	})
	w.bottom.Clear().AddItem(w.input, 1, 0, true).AddItem(tuiView().SetText(" Enter apply · Esc cancel"), 1, 0, false)
	w.app.SetFocus(w.input)
}
func (w *terminalWorkspace) showModal(title string, p tview.Primitive, width, height int) {
	w.modal = true
	frame := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(nil, 0, 1, false).AddItem(p, min(height, max(5, w.height-2)), 0, true).AddItem(nil, 0, 1, false)
	outer := tview.NewFlex().AddItem(nil, 0, 1, false).AddItem(frame, min(width, max(20, w.width-2)), 0, true).AddItem(nil, 0, 1, false)
	outer.SetInputCapture(func(e *tcell.EventKey) *tcell.EventKey {
		if e.Key() == tcell.KeyEscape {
			w.closeModal()
			return nil
		}
		return e
	})
	w.pages.AddPage("dialog", outer, true, true)
	w.app.SetFocus(p)
}
func (w *terminalWorkspace) closeModal() {
	w.pages.RemovePage("dialog")
	w.modal = false
	w.focusContent()
	w.hints()
}
func (w *terminalWorkspace) message(title, body string) {
	v := tuiView().SetText(tuiText(body))
	tuiBox(v.Box, tuiText(title)+" · Esc close")
	w.showModal(title, v, 85, 24)
}
func (w *terminalWorkspace) help() {
	w.message("Keyboard guide", `WORKSPACE
1 Boards    2 Knowledge    3 Activity
Tab / Shift-Tab switch panels · b collapse sidebar
Arrow keys or hjkl navigate · Enter open · Esc back
/ filter current view · : action menu · r refresh · q quit

CARDS
n create · e edit title and body · m move · a append note

KNOWLEDGE
Enter on a group collapses it; Enter on a document expands reading.
n create a document · e edit its body
Markdown headings, fenced code and quotes are styled.

FORMS
Tab advances fields · Enter activates buttons · Esc cancels
Edits use the version you read; conflicts keep the form open.

Mouse: click navigation/cards and scroll panels.
The command menu also provides the original slash commands.`)
}
func (w *terminalWorkspace) palette() {
	list := tuiList()
	tuiBox(list.Box, "Actions · Esc close")
	actions := []struct {
		name, detail string
		run          func()
	}{
		{"Boards", "Switch to the kanban board", func() { w.switchView("board") }},
		{"Knowledge", "Browse project documents", func() { w.switchView("vault") }},
		{"Activity", "Recent project events", func() { w.switchView("activity") }},
		{"New item", "Create a card or document", func() { w.edit(true) }},
		{"Edit selected", "Edit the selected card or document", func() { w.edit(false) }},
		{"Move card", "Choose a destination column", w.move},
		{"Add note", "Append a note to the selected card", w.note},
		{"Filter", "Find text in this view", func() { w.openInput("filter") }},
		{"Run slash command", "Existing commands and project-wide search", func() { w.openInput("command") }},
		{"Refresh", "Reload local data", w.refresh},
		{"Help", "Keyboard reference", w.help},
	}
	for _, a := range actions {
		list.AddItem(a.name, a.detail, 0, func() { w.closeModal(); a.run() })
	}
	w.showModal("Actions", list, 65, 26)
}
func (w *terminalWorkspace) edit(create bool) {
	if w.view == "activity" {
		return
	}
	a := w.session.app
	vault := w.view == "vault"
	if !create && ((vault && w.entry == nil) || (!vault && w.selected == nil)) {
		return
	}
	title, body := "", ""
	var card core.Card
	var entry core.Entry
	if !create {
		if vault {
			entry = *w.entry
			title, body = entry.Title, entry.BodyMD
		} else {
			card = *w.selected
			title, body = card.Title, card.BodyMD
		}
	}
	form := tview.NewForm().SetLabelColor(tuiAccent).SetFieldBackgroundColor(tuiSelected).SetFieldTextColor(tuiFG).SetButtonBackgroundColor(tuiSelected).SetButtonTextColor(tuiFG)
	tuiBox(form.Box, "Edit · Tab fields · Esc cancel")
	if create || !vault {
		form.AddInputField("Title", title, 0, nil, func(v string) { title = v })
	}
	form.AddTextArea("Body", body, 0, max(3, min(10, w.height-15)), 0, func(v string) { body = v })
	errorView := tuiView()
	form.AddButton("Save", func() {
		var err error
		if vault {
			if create {
				_, err = a.Core.CreateEntry(w.ctx, a.Project.ID, core.NewEntry{Title: title, Body: body})
			} else {
				_, err = a.Core.EditEntry(w.ctx, a.Project.ID, entry.Slug, body, new(entry.Version))
			}
		} else {
			if create {
				column := ""
				if w.lane < len(w.columns) {
					column = w.columns[w.lane].Name
				}
				_, err = a.Core.CreateCard(w.ctx, a.Project.ID, a.Board.ID, core.NewCard{Title: title, Body: body, Column: column})
			} else {
				_, err = a.Core.EditCard(w.ctx, a.Project.ID, core.ParseCardRef(card.Ref), core.CardEdit{Title: new(title), Body: new(body), IfVersion: new(card.Version)})
			}
		}
		if err != nil {
			errorView.SetText("[#ffb4ab]" + tuiText(err.Error()) + "[-]")
			return
		}
		w.closeModal()
		w.refresh()
		w.focusContent()
	}).AddButton("Cancel", w.closeModal).SetCancelFunc(w.closeModal)
	panel := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(form, 0, 1, true).AddItem(errorView, 3, 0, false)
	w.showModal("Edit", panel, 84, 24)
	w.app.SetFocus(form)
}
func (w *terminalWorkspace) move() {
	if w.view != "board" || w.selected == nil {
		return
	}
	card := *w.selected
	list := tuiList().ShowSecondaryText(false)
	tuiBox(list.Box, "Move "+tuiText(card.Ref))
	for _, column := range w.columns {
		list.AddItem(tuiText(column.Name), "", 0, func() {
			_, err := w.session.app.Core.MoveCard(w.ctx, w.session.app.Project.ID, w.session.app.Board.ID, core.ParseCardRef(card.Ref), column.Name)
			w.closeModal()
			if err != nil {
				w.message("Move failed", err.Error())
				return
			}
			w.refresh()
			w.focusContent()
		})
	}
	w.showModal("Move", list, 50, len(w.columns)+4)
}
func (w *terminalWorkspace) note() {
	if w.view != "board" || w.selected == nil {
		return
	}
	card := *w.selected
	body := ""
	form := tview.NewForm().SetLabelColor(tuiAccent).SetFieldBackgroundColor(tuiSelected).SetFieldTextColor(tuiFG)
	tuiBox(form.Box, "Note · "+tuiText(card.Ref))
	form.AddTextArea("Note", "", 0, 5, 0, func(s string) { body = s }).AddButton("Append", func() {
		if strings.TrimSpace(body) == "" {
			return
		}
		_, err := w.session.app.Core.CreateComment(w.ctx, card.ID, body)
		if err != nil {
			form.SetTitle(" " + tuiText(err.Error()) + " ")
			return
		}
		w.closeModal()
		w.refresh()
		w.focusContent()
	}).AddButton("Cancel", w.closeModal).SetCancelFunc(w.closeModal)
	w.showModal("Note", form, 75, 14)
}
