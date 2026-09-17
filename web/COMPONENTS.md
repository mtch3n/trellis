# Component Registry

This file documents all UI components used in trellis and enforces the component
policy defined in the design specification (§12.1 and §12.2).

## Policy

1. **shadcn/ui components are the default.** Before writing a custom component, search
   the registry with `pnpm dlx shadcn@latest search @shadcn -q "<term>"` and add it from
   shadcn rather than hand-rolling markup.

2. **This project uses the Base UI variant**, not Radix (`components.json` → `"style":
   "base-nova"`, `"base": "base"`). Custom triggers use the `render` prop; `asChild` is a
   Radix API and does not exist here. `<Button render={<Link to="…" />}>` is the correct
   way to make a button navigate — nesting `<Button>` inside `<Link>` produces a
   `<button>` inside an `<a>`, which is invalid HTML.

3. **Custom components require proof, recorded here before the component is written:**
   the registry search actually run and its result showing nothing suitable; why
   composition of existing primitives cannot produce the behaviour; and what the custom
   component owns that no primitive does.

4. **Third-party libraries are wrapped, not scattered.** `components/wrappers/` is the
   only place a third-party library may be imported: `react-markdown`, `@milkdown/kit`,
   `d3-*`. Each wrapper is listed below with what it wraps and why. Pages compose the
   registry and the wrappers, plus the behaviour libraries the audit sanctions by name
   (`@dnd-kit/*`), which supply sensors, collision detection and transforms rather than
   markup. Pure logic with no markup lives in `lib/`.

5. **Composition over custom markup.** Wrapping primitives to add behaviour is correct;
   reimplementing a primitive's markup to change appearance is not — restyle through
   Tailwind theme tokens instead.

6. **Two screens showing the same thing must show it with the same component.**
   Consistency is enforced by `make ui-audit`, not by instruction.

## Installed shadcn Components

| Component | Location | Purpose |
|-----------|----------|---------|
| Alert | `@/components/ui/alert` | Persistent inline failure state that must stay on the page |
| Breadcrumb | `@/components/ui/breadcrumb` | Where a full page sits: the card page reads Board, then the card's ref |
| Badge | `@/components/ui/badge` | Status chips: priority, claim state, verify state, connection state |
| Button | `@/components/ui/button` | Primary interactive element |
| Card | `@/components/ui/card` | Container (CardHeader, CardTitle, CardDescription, CardContent, CardFooter) |
| Collapsible | `@/components/ui/collapsible` | The graph dock's open and closed state; exposes `--collapsible-panel-height` so the panel animates |
| AlertDialog | `@/components/ui/alert-dialog` | Confirming a destructive action that needs no typed input, e.g. deleting a card |
| Dialog | `@/components/ui/dialog` | Modal card editor and the graph explorer; owns focus trap, Escape, scroll lock and `aria-modal` |
| DropdownMenu | `@/components/ui/dropdown-menu` | The project switcher. Switching project is navigation, so it is a menu, not a form select |
| Empty | `@/components/ui/empty` | Empty states and the catch-all 404 route. Never boxed: a title, a line of description, and an action when one exists. The audit rejects a border or card fill on it |
| Field | `@/components/ui/field` | Form layout (FieldGroup, Field, FieldLabel) |
| Input | `@/components/ui/input` | Single-line form control |
| InputGroup | `@/components/ui/input-group` | An input with an icon or control inside its border: the vault filter and the search field. Never an absolutely positioned icon over a plain Input |
| Kbd | `@/components/ui/kbd` | A keyboard shortcut shown beside the action it triggers, e.g. Ctrl Enter beside Create card |
| Label | `@/components/ui/label` | Control label primitive used by Field |
| Resizable | `@/components/ui/resizable` | The vault's draggable divider between the navigator and the entry, over `react-resizable-panels` |
| Select | `@/components/ui/select` | A pick from a fixed set: a card's status (the keyboard path for moving it) and priority, an entry's template and folder, a template's choice fields |
| Separator | `@/components/ui/separator` | Dividers, replacing `border-t` spacer divs |
| Skeleton | `@/components/ui/skeleton` | Loading placeholders |
| Combobox | `@/components/ui/combobox` | Choosing one item from many by typing, such as the card to relate another card to |
| Popover | `@/components/ui/popover` | The list behind a timeline mark, opened by hover or press, that stays open while the pointer moves into it |
| Spinner | `@/components/ui/spinner` | Inline pending indicator |
| Textarea | `@/components/ui/textarea` | Multiline Markdown source control |
| Toast | `@/components/ui/toast` | Transient action failures. Base UI ships `toast`; `sonner` is the Radix/React-Aria equivalent and is not used here |
| Tooltip | `@/components/ui/tooltip` | The label of every icon-only button, through `IconButton`. `TooltipProvider` is mounted once in `main.tsx` |
| Toggle | `@/components/ui/toggle` | Toggle primitive used by ToggleGroup |
| ToggleGroup | `@/components/ui/toggle-group` | Fixed option sets, e.g. card priority |
| Switch | `@/components/ui/switch` | A true-or-false setting. Square, like everything else: the registry's `rounded-full` is replaced |
| ScrollArea | `@/components/ui/scroll-area` | A fixed-height region that scrolls on its own, such as the daemon log. Its thumb is square |
| Table | `@/components/ui/table` | Rows of like things with columns: projects, search results, templates |

## Wrapper Components

Components in `web/src/components/wrappers/` compose primitives and are listed here.

| Component | Wraps | Why |
|-----------|-------|-----|
| AppShell | `ProjectSwitcher`, `BoardSwitcher`, `Lamp`, `ThemeToggle`, `IconButton`, `GuardedLink` | The persistent chrome: the mark, the project scope control, the board switcher (when multiple boards exist), the three sections (Overview, Board, Vault), connection status, and Settings as the right-most control, a gear on a muted surface while settings are open. Every way out of a screen goes through the navigation guard. No rule under it at rest; a hairline appears once content scrolls beneath it. Project is a scope control rather than a destination, per PRODUCT.md principle 3: one project is the working scope and switching is a deliberate act. The current section is marked by a foreground rule that grows from the centre, never by the claimed accent |
| ProjectSwitcher | `DropdownMenu`, `Button`, `Lamp` | The project scope control. Opens below its trigger, wide enough that no key is cut, and gives each project one deciding fact: its expired claims, or its card count. The full table is a small action on the heading's row, not a pretend project in the list, so it costs no row of its own |
| BoardSwitcher | `DropdownMenu`, `Button` | The board scope control, shown only when a project has multiple boards. Opens below its trigger next to the project switcher and lists all boards, the current one selected, choosing one navigates to that board while keeping the project in scope |
| Lamp | none (registry search below) | The single state vocabulary: idle, claimed, alarm, live. Every row, tile, column and the SSE connection report themselves through it, so a quiet board reads as quiet. Idle is an outline and a lit lamp a flat fill; none glows |
| CardView | `MarkdownContent`, `MarkdownEditor`, `EditForm`, `InPlaceText`, `CardTimeline`, `MetaPanel`, `Select`, `Lamp` | One card in two columns: what it says on the left, what is true about it on the right. Backs both the dialog and the card page, so a card reads identically wherever it is opened. The title comes first; the ref is a fact, and the the claim (who has it, and stealing it) is its own group in the right column, which ends with when the card was created and last updated. Editing happens in place and covers the words only: reading, the title and body show a faint `edit-hint` wash when pointed at and a click on either starts editing it (not on a link, not at the end of a selection); editing, they become fields where they stand, in the same type, and the Timeline (comments and events together, with the comment box on top) stays put. The facts column ignores Edit: column, priority, labels and tags all apply the moment they change, like a tracker's side panel, and only a card being created drafts them. Save, Cancel and the markdown toggle belong to the parent's action row |
| MetaPanel | none (layout) | The right column. One shape across the card dialog, the card page and the vault entry, so moving between them costs no re-reading |
| MetaGroup | `Collapsible` when `collapsible` | One labelled section inside a `MetaPanel`. On the vault page each group collapses and is remembered per browser |
| CollapsibleGroup | MetaGroup | The collapsible form of a group: a chevron trigger over an animated panel |
| MetaFacts | none (layout) | A run of label/value rows, aligned by whitespace rather than a rule under each. Data values set in `text-meta`, words in `text-sm`; a value too long for one line stacks under its label |
| CardDialog | `Dialog`, `Button`, `EditActions`, `CardMenu`, `IconButton`, `CardView` | The quick look at a card, without leaving the board. One row of actions sits above the card and never scrolls: the ref, then Edit (or `EditActions` in its place, so starting an edit moves nothing), `CardMenu`, the card's own page, and close. A new card keeps Create at its bottom right, because a new card is a form to finish. The box hangs from near the top, not the centre, so a card that grows while being edited grows downward. Reading, focus lands on the scrolling body, which moves nothing and lets the arrow keys scroll |
| ForceGraph | `d3-force`, `d3-zoom`, `d3-drag`, `d3-selection`, `d3-transition` | The vault graph as something you can pan, zoom and pull apart. d3 owns physics, zoom and drag; React owns the elements. Positions and hover state are written to the DOM, so neither re-renders React. The layout is seeded and pre-settled, so the same vault draws the same picture. Names fade in with zoom, and pointing at a node lights it and its neighbours |
| ArtifactList | `Button`, `ArtifactPreview`, lucide | An entry's artifacts in the facts column. A row opens the preview; an archive row downloads; a name with no file behind it reads as a broken reference, not an error. Where the caller passes `onRemove`, each row gains a remove button beside it, never inside it |
| ArtifactsEditor | `ArtifactList`, `Input`, `DropdownMenu`, `Button`, `Spinner` | A card's files. The rows are `ArtifactList`, so a card's files read exactly like an entry's; what it adds is putting one there — a file chosen from this machine, or one the project already holds, since an artifact belongs to the project and several cards may cite it. Removing a row unlinks the file and leaves it stored, which is why the label says Remove, not Delete |
| EntryLinksEditor | `Combobox`, `Button`, `IconButton`, `react-router-dom` | The entries a card cites, in its facts column. A row opens the entry and keeps its anchor, because the anchor says which part the card leans on; a citation whose entry has been deleted reads as a stub, muted, the way an unresolved wikilink does. Adding picks an entry by typing, and every row removes itself. Changes apply at once, like the facts beside them |
| HistoryDialog | `Dialog`, `Button`, `Empty`, `Skeleton`, `IconButton` | The revisions of a card or an entry, and what one save changed: the list on the left, newest first, and the server's unified diff for the chosen version as text. History means revisions, so a card's comments and events stay in its Timeline. Version numbers have gaps, because a move bumps a card's version without keeping a revision, so the list is what the server kept rather than a count |
| ImportCardsDialog | `Dialog`, `Field`, `Textarea`, `Button`, `RefusalAlert`, `Spinner` | A plan as cards, the way `card import` takes it: a JSON array whose `id` is a handle the other cards in the same import can wait on. The text is checked for being an array of titled objects before it is sent, and the server's refusal is shown in the form rather than as a toast that outlives it. The cards land together or not at all |
| ArtifactPreview | `Dialog`, `Button`, `IconButton` | One artifact, previewed without running it: images only through `<img>` (an SVG there runs no script), audio and video with `preload="metadata"`, PDFs in a frame only when both `kind` and `mime` say PDF (no sandbox, which would disable the browser's PDF viewer), text fetched on open and shown as text, archives as a download. Every preview offers the download |
| PreviewBody | ArtifactPreview | Picks the element for an artifact's kind |
| NoPreview | ArtifactPreview | The message for a file with no safe preview; the header's Download stays available |
| TextPreview | ArtifactPreview | Fetches a text artifact when the preview opens, capped at 256 KB through a Range request, and renders it in a `<pre>` as text, never as markup |
| DiffView | `diff` (jsdiff) | What changed between two texts: line diff for bodies, word diff for titles and summaries. Additions sit on the accent surface, removals are struck through; long unchanged stretches fold |
| CardTimeline | `Collapsible`, `Button`, `DiffView`, `MarkdownContent` | A card's Timeline, newest first: its comments and its events merged by time. A plain marker per item on one rail down the left, filled for a comment or the card's creation and hollow otherwise; no icon repeats what the sentence beside it says. An event reads as a plain sentence ("Status changed from Backlog to Review", "Blocked by TRELLIS-3", "Resolved by REL-2"); a comment shows who commented and its markdown under the byline, with no panel around it. Who and at what time follow, under a heading for each day. A text edit opens into a diff when the log kept both sides, and says plainly when it did not |
| Marker | CardTimeline | An item's place on the rail: a small square, filled or hollow, that covers the line behind it |
| Byline | CardTimeline | Who, optionally what they did ("commented"), and at what time |
| TextChange | CardTimeline | The diff under one text edit, or the note that the log did not keep the text |
| CardMenu | `DropdownMenu`, `Tooltip`, `ConfirmDialog`, `Button`, `toast` | A card's secondary actions behind one button in its action row, the way a tracker keeps them: copy a link to the card, open its History, archive it or restore it, and delete it. Delete is confirmed and names the card; archiving and deleting are disabled with the reason while another actor's claim is live, and the confirmation closes only once the card is gone |
| DeleteProjectDialog | `Dialog`, `Field`, `Input`, `Alert`, `Button` | Deleting a project with its key retyped. Says what goes (boards, cards, entries and their files), what stays (the event log), and that running trellis in the repo starts a new project. The server asks for the same retyped key; a refusal (a live claim, entries in the global vault) is shown inside the dialog |
| IconButton | `Button`, `Tooltip` | An icon-only button whose label is both its accessible name and its tooltip. A `title` attribute is kept only for explaining why a control is disabled, since a disabled button shows no tooltip |
| VaultLayout | `Resizable`, `react-resizable-panels` | The vault's frame: navigator and entry side by side with a draggable divider (14rem to 36rem, 18rem by default), remembered per browser. The navigator keeps its pixel width when the window changes. Below lg the two stack |
| GraphDock | `Collapsible`, `ForceGraph`, `Button` | The graph docked at the foot of the navigator, open by default and remembered per browser. The whole vault with the open entry marked; the view glides to the entry when it leaves the middle of the frame |
| GraphExplorer | `Dialog`, `ForceGraph`, `Button`, `react-router-dom` | The graph at full size, with zoom controls and a rail naming what is unlinked and which links are still stubs. Its open state is the `view=graph` query, so following any link out of it closes it |
| VaultNav | `InputGroup`, `Collapsible`, `DropdownMenu`, `Tooltip`, `IconButton`, `NavLink`, lucide, `@/lib/vault-tree` | The vault as a file explorer. Entries are markdown files and the scopes are directories, so the tree shows that structure: the global vault and the project as top folders, subfolders wherever a slug has slashes, one line per file with a lock on private ones. A toolbar starts a new entry, filters (which opens every folder), sorts files within their folders (folders always first), and collapses or expands everything. A project folder's row offers a new entry inside it, over its count. Folders remember whether they were open, and the folders around the open entry open themselves when it changes. The arrow keys walk the rows. Nested contents hang from a faint guide under the folder's chevron. The selection is one surface that slides between rows. Takes the graph dock as a slot |
| Folder | VaultNav | One folder row and, when open, what it holds: its subfolders, then its files. In the project, the row carries a new-entry button beside it, since a button cannot hold another |
| SortMenu | VaultNav | How files are ordered within each folder: by title either way, recently edited, or recently created, remembered per browser |
| EntryView | `MarkdownContent`, `MarkdownEditor`, `EditForm`, `InPlaceText` | One vault entry, read or edited in place. Content only: its facts live in the meta column and its actions in the page's action row. Title, summary, rule and prose share one right edge, the reading measure. Reading, each part shows an `edit-hint` wash when pointed at and a click starts editing it. A body that opens with the title as an H1, as a template skeleton does, is shown and edited without it, since the title is already set above; saving puts the heading back under the title as saved, so the file keeps a heading that matches |
| MarkdownEditor | `MarkdownEditorImpl` (lazy) | Loads the editor only when someone edits. The dashboard is read-first, so opening a card should not parse an editor the reader never opens. While it loads, the empty `edit-surface` holds its place |
| MarkdownEditorImpl | `@milkdown/kit` (commonmark, gfm, history, listener), `Textarea` | Markdown edited in place: the reader's typeset on an `edit-surface`, so the text does not move when editing starts. Rich text or source is the caller's `source` prop, toggled from `EditActions`, so no toolbar row pushes the text down; `autoFocus` puts the caret at the end when an edit started from a click on the body. An empty body shows its placeholder. The serializer escapes square brackets, so wikilinks are restored before the text is handed back; without that, every rich-text save would turn `[[links]]` into plain text. Built from the kit rather than Crepe: Crepe eagerly imports a CodeMirror language mode per syntax, which was 3.5 MB of dist for syntax colour in an editor. The source view exists because a ProseMirror round-trip normalises markdown, and markdown files are the source of truth here |
| EditForm | `form` | The form around fields edited in place; Ctrl or Cmd with Enter submits from anywhere in it, the body editor included |
| InPlaceText | `Textarea` | A title or summary edited where it is read. It takes the read view's type, wraps and grows like the text it replaces, and sits on an `edit-surface`. One paragraph, so Enter submits |
| ProjectTimeline | `Popover`, `Tooltip`, `Toggle`, `IconButton`, `react-router-dom`, `@/lib/time-axis`, `@/lib/timeline-marks` | What happened in a project, at the top of the Overview, on four lanes that never grow however many cards there are: created, started (first claim or first move out of the queue), done, and entries written. Each event is a mark at its moment in the lane's own symbol; marks closer than 16px merge into one with a count, and zooming in pulls them apart. Pointing at a mark opens a list of what it stands for, with links, that stays open while the pointer moves into it. A start by an agent still holding the card is amber. Time is proportional while anything happens; silences over two hours become narrow labelled skips, and a toggle shows true time. It opens fitted to the width, or on now when there is more than fits, with zoom and fit controls |
| Ruler | ProjectTimeline | Day and hour marks, each skip's length, and now |
| Lane | ProjectTimeline | One lane: its name and count pinned at the left, and its track |
| Glyph | ProjectTimeline | A lane's symbol: plus for created, play for started, check for done, triangle for an entry (hollow when edited) |
| MarkCluster | ProjectTimeline | One mark, or several merged with a count; a hover or a press opens the list of cards or entries it stands for |
| CommentBox | `Textarea`, `Button`, `Kbd`, `Spinner` | Saying something on a card, at the top of its Timeline where the newest item lands. Markdown is rendered in the timeline; Ctrl or Cmd with Enter posts; what was typed is kept if posting fails |
| ChipEditor | `Button`, `DropdownMenu`, `Input` | The short words on a card: its labels, or its tags. Each carries its own remove, and adding is a pick from the project's labels or free text for tags, which is the difference between the two. Changes apply as they are made, like the facts beside them, rather than waiting for a save; on a card being created they go into the draft instead |
| RelationsEditor | `Combobox`, `Select`, `Button`, `IconButton`, `react-router-dom` | The cards this one stands in relation to (blocked by, blocks, resolved by, resolves, duplicate of, duplicated by, related to), grouped by how, each linking to its card with where it is now: done with a check, or its column. An unfinished blocker reads in red, because it is what makes the card wait. Adding picks the relation and then the card by typing; each row removes itself. Changes apply at once, like the facts beside them |
| NewEntryDialog | `Dialog`, `Field`, `Input`, `Select`, `TemplateSelect`, `TemplateFields`, `RefusalAlert`, `@/lib/templates` | Starting a vault entry. It asks for the title, the folder, the template, and only what that template cannot do without, so an entry that follows none is a title and a button. The chosen template's rules are described before they are met. The body is not written here: the new entry opens for editing with the cursor in its body, where the template's sections already wait. A refusal, such as a source that does not resolve, is listed inside the dialog |
| FolderField | NewEntryDialog | Where the new file goes: the project's top level, one of its folders, or a new folder typed in. Opened from a folder's row, it starts there. The server refuses a name close to an existing folder |
| TemplateSelect | `Select` | Which template an entry follows. No template comes first, because most entries follow none; a strict template says so in the list, and a template the entry names but Trellis no longer has is still shown, marked missing |
| TemplateFields | `Field`, `Select`, `Input`, `@/lib/templates` | What a template needs beyond the title: its sources, a select for each field with fixed choices, and a line for any other required field. Shared by the create and switch dialogs so both ask the same way. A required choice left empty is marked once a submit was tried |
| SourcesField | TemplateFields | The sources a template asks for, one per line, the first required, more added and removed. It says which forms are checked (a card, an `[[entry]]`) and which are kept as written |
| TemplateSwitchDialog | `Dialog`, `Alert`, `TemplateFields`, `RefusalAlert`, `@/lib/templates` | Moving an entry onto a strict template, which refuses an entry that falls short. It asks only for what the entry lacks, its sources, summary and fields read from the entry, and says what it will add: missing sections go at the end of the body, empty, in the same save, so the entry is checked once as it will be. Adding sections waits while the body is open in the editor. An advisory template never opens it; it switches at once and warns |
| SourcesEditor | `Input`, `Button`, `IconButton`, `react-router-dom`, `@/lib/sources` | What an entry's claims rest on, in its facts column. A card or entry source opens it, a URL opens in a new tab, anything else is shown as written. Adding and removing apply at once, and a bare card ref of the project is stored as its address so it is checked. A refused source stays in the box to be corrected |
| FieldsEditor | `Select`, `Input`, `Label`, `IconButton`, `@/lib/templates` | An entry's own fields in its facts column: every field its template names, set or not, then any other its frontmatter carries. A field with fixed choices is a select, any other a line of text; a list is shown, not edited, because a line would flatten it. A field the template does not name can be removed. Changes apply at once through PATCH `set`, and a refused value stays in its box |
| ChoiceField | FieldsEditor | A field with fixed choices. Required and set, it cannot be unset; a value the template no longer allows is shown in red, marked, so it can be replaced |
| TextField | FieldsEditor | A field of free text, saved when left or on Enter; Escape puts the saved value back. A new saved value replaces the draft while rendering, so the box keeps its focus |
| RefusalAlert | none (text) | Why the server said no, inside the form that asked: the message, then each problem it named on its own line, such as a template rule. Plain destructive text with `role="alert"`, not a bordered Alert, because it usually sits inside a dialog |
| ConfirmDialog | `AlertDialog`, `Spinner` | Asking before something that cannot be taken back: the question as the title, what goes and what stays, and a confirming button that names the act. While the act runs, nothing can be pressed or dismissed |
| NavigationGuardProvider | `ConfirmDialog`, `@/lib/navigation-guard` | Holds whether the screen has unsaved changes and asks before it is left. The app is not on a data router, so the links and menus that navigate ask through `guard`; a reload or a closed tab gets the browser's own prompt |
| GuardedLink | `react-router-dom` `Link`, `@/lib/navigation-guard` | A link that asks before leaving unsaved changes. A click meant for a new tab or window is let through |
| SettingsGeneral | `Field`, `Switch`, `Input`, `Select`, `PageHeader`, `@/lib/settings` | The machine-wide settings in `config.yaml`, grouped by the first segment of their keys, each group a legend and a separator rather than a card; a group the web cannot change at all comes last. Validation is the server's, so the form skips the browser's own bubbles. Nothing is written until Save, which is disabled until something changed; a refused save writes nothing and names each problem beside its setting, and a save that needs a restart says which keys wait for it. Keys the web may not change (`ui.*`, `search.vector.*`) are shown locked with "Set in config.yaml" |
| SettingRow | SettingsGeneral | One setting: its label, what it does, its key in mono and where its value comes from on the left, the control on the right. A value from `config.yaml` has a quiet Reset, which applies on save and can be undone |
| SettingControl | SettingsGeneral | The control a setting's type calls for: a switch, a number, a duration with a `30m` hint, a select of choices, or a comma-separated list |
| SettingsTemplates | `Table`, `Badge`, `Dialog`, `PageHeader` | The vault templates on this machine: name, strict or advisory, section count, and a Built-in badge. A row opens the file; New template names one and opens it |
| NewTemplateDialog | SettingsTemplates | Naming a new template. A name the server refuses, or one that exists, is said inside the dialog |
| TemplateEditor | `Textarea`, `DropdownMenu`, `ConfirmDialog`, `PageHeader` | One template's whole file as text, rules then skeleton. Save at the top right, disabled until something changed; Ctrl or Cmd with S or Enter saves from the text. A file that does not parse is refused whole, with the reason above the text. Leaving with unsaved changes asks first |
| TemplateMenu | TemplateEditor | The template's secondary actions behind one button with a tooltip: Reinstall for a shipped template, and Delete, both confirmed |
| SettingsMaintenance | `Select`, `Separator`, `ConfirmDialog`, `PageHeader` | What the database weighs, as one line, and the ways to trim it as a plain list: prune events and invocations older than a chosen age, trim revisions to `history.keep`, remove leftover revisions, compact. Every deletion asks first and reports how much went |
| MaintenanceRow | SettingsMaintenance | One task: what it does on the left, how to run it on the right |
| RetentionSelect | SettingsMaintenance | How old something must be to be pruned: 30, 90 or 180 days, or a year |
| SettingsLogs | `ScrollArea`, `Empty`, `PageHeader` | The end of the daemon's log in a fixed-height pane, scrolled to the newest line on every load, with its path under the title and Refresh at the top right. Without a log file it says where a supervised daemon logs instead |
| ActionRow | none (layout) | The row of actions over a card page or a vault entry: what it is on the left, what can be done to it on the right. Sticks under the app bar so Save stays in reach down a long body, and owns the stacking that pages may not, so the positioned editor never paints over it. The dialog draws its own row outside its scrolling body instead |
| EditActions | `Toggle`, `Tooltip`, `Button`, `Kbd`, `Spinner` | What an edit adds, as one compact cluster that takes Edit's place in the action row: the markdown source toggle, Cancel, and Save (or Create). The Ctrl+Enter shortcut is in Save's tooltip and works from the cluster as well as from the form |
| LiveStatusProvider | React context (`@/lib/live-status`) | Carries connection state from whichever screen owns the SSE stream up to the shell's lamp, so it is reported once rather than repeated as a banner. The context and `useLiveStatus` live in `lib`, so this file exports only the component |
| PageHeader | none (layout) | One compact row per page: what you are looking at, what is exceptional, what you can do. A count a column already prints does not belong here, and a fact worth nothing at zero is hidden at zero. A page that needs saying what it is for, such as a settings section, gets one description line under the title, and its actions stay level with the title |
| MarkdownContent | `react-markdown`, `remark-gfm`, `rehype-sanitize` | Safely renders stored Markdown with the shared typeset stylesheet; sanitization keeps raw HTML from becoming an injection surface. The prose measure lives in `.typeset-notes`, not in a className |
| ThemeToggle | `IconButton` | Flips the `dark` class on `<html>` and persists the choice to `localStorage`. Dark is the default; `index.html` sets the class before first paint so there is no flash |

## Custom component proof

Policy 3 requires the registry search and the reason, recorded before the component exists.

**ForceGraph.** `pnpm dlx shadcn@latest search @shadcn -q "graph"` returned *No items
found*; `-q "chart"` returned `@shadcn/chart`, which wraps Recharts for series data on
axes and has no notion of nodes, links, a force layout, or dragging a node. Composition
cannot produce the behaviour: the component is a physics simulation, a zoom behaviour
and per-node drag, which is d3's job, not markup's. ForceGraph owns the binding between
d3 and React (d3 writes positions and hover state to elements React rendered) and the
graph's visual states, which live in `index.css` under `.force-graph` because they are
data attributes changed on every pointer move.

**Lamp.** `pnpm dlx shadcn@latest search @shadcn -q "indicator status dot"` returned
*No items found*; `-q "badge"` returned only `@shadcn/badge`, which is a text chip with
padding and a label. Composition cannot produce the behaviour: a lamp has no text, is a
fixed 8px, and switches between four states where two carry their own keyframes. Forcing
Badge to render an empty fixed-size span would be reimplementing its markup to change its
appearance, which policy 4 forbids. Lamp owns the state vocabulary itself — the mapping
from claimed/blocked/idle/connected onto one visual language — which no primitive holds.

## Application Shell

| Component | Purpose |
|-----------|---------|
| App | Router root. Mounts `Toaster` once, and owns the catch-all 404 route |
| AppShell | The chrome every screen sits inside. Four slots, and it stays four slots |
| Shelled | Reads `projectKey` from the route and hands it to `AppShell`, so a route declares its section and nothing else |

## Page Components

| Component | Purpose |
|-----------|---------|
| RootRedirect | Resolves `/` to the busiest project's overview. One project is the working scope, so the root is that project, not a list |
| OverviewPage | The project at a glance and where the app lands, in a wider container than the document pages because it is a dashboard. The project timeline runs across the top, built from the project's whole event log paged from `/api/p/{key}/events`; below it, urgent cards, each column of work in motion with who holds what, the latest of this project's event log, and a rail with the board's counts, pinned entries and what was written lately |
| ProjectsPage | The full project table, reached from the shell's scope control rather than as a landing page, and the one place a project is deleted |
| BoardPage | Board view showing cards organized by column. Three tabs: the board, the same cards as rows, and the archived ones (`?archived=1`), which are a shelf rather than work in motion. Import takes a plan as JSON. A fixed frame under the shell: the header stays, the column strip scrolls sideways and each column scrolls its own cards, both with shadcn's `scroll-fade` edges. The page never scrolls |
| VaultPage | The vault as a workspace: the navigator with the graph docked beneath it, and the entry with its facts centred in the rest. Above the entry, one sticky action row: a breadcrumb, then Edit (or `EditActions` in its place) and the facts toggle, so an edit never rewraps the title. Lists carry no bodies, so the open entry is fetched on its own (`GET /api/p/{key}/vault/{slug}`) when chosen and after every save or conflict; until it arrives the title shows over a skeleton. Builds the graph once and hands it to the dock and the explorer. Serves both `/p/:key/vault` and `/p/:key/vault/:slug`, because with the tree always present a separate index page has nothing left to do |
| SearchPage | Search across both halves of the product, cards and vault entries, in every project |
| EventLogPage | The event log: every action across every project, newest first, at `/event-log` |
| SettingsPage | The machine's settings at `/settings/:section`, not a project's, so the shell carries no project. A section menu on the left and the open section on the right, within a 5xl container; on a narrow screen the menu becomes a row above. The menu is plain words, the current one on a muted surface, and its links go through the navigation guard. Sections route inside the page, so the shell stays mounted |
| CardPage | A card at its own URL. A sticky action row under the app bar (breadcrumb, then Edit or `EditActions`, and `CardMenu`) keeps Save in reach down a long card |

## Page-local Helpers

Presentational helpers scoped to one page. They compose registry components and own no
styling of their own; anything reused by a second page must graduate to `wrappers/`.

| Component | Page | Purpose |
|-----------|------|---------|
| LoadingBoard | BoardPage | `Skeleton` arrangement matching the column grid |
| BoardColumn | BoardPage | One kanban column. The whole column is the drop target, so a drop below the last card or into an empty column lands. While a card is in the hand every column outlines itself, the one under the pointer fills, and an empty column shows a dashed landing slot that exists only for the length of the drag |
| SortableCard | BoardPage | A card that can be picked up. Its place becomes a dashed landing slot while it is in the hand. A claimed card does not drag, and trying says why with a toast and a short refusal shake |
| CardTile | BoardPage | The face of a card, shared by the column, the landing slot and the card in the hand. Title first, then the words it is filed under — labels on a surface, tags in outline, because a label is the project's own vocabulary and a tag is anyone's — then one footer line: lamp, ref, priority when it is not normal, and the claimant in amber. No outline: it sits on its column by a `shadow-tile` that rises on hover, and in the hand it takes `shadow-lift`. Selected, it gains a foreground outline; the landing slot is dashed and flat |
| CardRows | BoardPage | Cards as rows grouped by column: the shape the List tab and the Archived tab share. A row opens the card, and nothing here is dragged, so the rows carry in columns what a tile shows in its footer |
| BoardFilters | BoardPage | What the board is showing, narrowed: one of the project's labels and one priority, read over the cards already loaded, so they answer without a round trip. "All" is the absence of a filter, not a value; the label filter is hidden until the project defines a label. Archived is not here — it is its own tab, because it asks the server for the other set of cards |
| Group | ProjectsPage | One project grouping (live, initialised-never-used) with its table and empty state |
| ProjectActions | ProjectsPage | A row's menu: open the overview, open the board, delete the project |
| CardSection | OverviewPage | One column of work in motion as rows: lamp, title, priority, claimant, ref. Each row opens the card's page |
| LoadingOverview | OverviewPage | `Skeleton` arrangement matching the overview's two columns |

## Type scale

One scale, defined in `index.css`. Four sizes, seven roles, and nothing outside them.
`make ui-audit` rejects any other size utility, any case or tracking utility, bare
`font-mono`, and a `cn` imported from anywhere but `@/lib/utils` (which is configured
with these roles; the bare package reads `text-meta` as a colour and drops it when
merging).

| Role | Size | Weight | Used for |
|---|---|---|---|
| `text-title` | 24px | 600 | The one h1 on a screen: a board, an entry, a card |
| `text-heading` | 16px | 600 | An h2 inside a page: Notes, Results, a list-view column |
| `text-body` | 15px | 400 | Reading: an entry's summary, and the base the prose scales from |
| `text-sm` | 14px | 400 | The interface: rows, tiles, the navigator, controls, table cells |
| `text-xs` | 12px | 400 | Secondary words: an entry's kind, a fact's label, captions, counts in words |
| `text-label` | 12px | 500 | Names of structure: a column, a scope, a meta group, a table head |
| `text-meta` | 12px mono | 400 | Identifiers: refs, slugs, agent ids, paths |

Prose lives in `.typeset-notes` at 15px / 1.65 inside the reading measure
(`--container-measure`, 42rem). Its headings are pulled onto the scale: h1 20px, h2 18px,
h3 16px, so a body that opens with `# Title` never rivals the title above it.

Hierarchy comes from weight and colour before size. `text-muted-foreground` is the only
secondary colour.

### When monospace is earned

Geist Mono is for **identifiers**, where the character grid does real work: card refs,
entry and stub slugs, agent ids, file paths, and markdown source. It arrives only
through `text-meta`, which sets the family and the size together.

Everything else is sans, so a row reads in one face: project keys (they are names
here), counts, times, versions, status words, category names, prose and the wordmark.
Numbers stay aligned without mono because the body sets `tabular-nums`.

### Case, and no eyebrows

Everything is sentence case, and nothing is tracked out. Words the backend stores in
lower case (column names, priorities, entry kinds, event actions) pass through
`sentence()` in `lib/format.ts` for display only.

Nothing sits above a title as a label. A title comes first; the identifier it belongs
to lives in the facts (a card's ref, an entry's slug) or in a footer line (a board
tile's ref, lamp and claimant).

## Colour

Three colours carry meaning and nothing else does.

| Token | Means |
|---|---|
| `claimed` (amber) | A card is claimed. The only accent in the interface |
| `danger` (red) | An alarm: urgent, blocked, destructive |
| `live` (green) | The SSE connection is up. Nowhere else |

Everything else is achromatic so a quiet board reads as quiet. A colour applied for
emphasis is a bug. In particular: selection is a surface (`bg-accent`) or a foreground
outline, the current section is a foreground rule, and a stub is dashed and muted,
because a stub wikilink is not an alarm.

No side stripes. A coloured `border-l` on a card, row, alert or empty state repeats what
the lamp already says; the audit rejects it.

## Shape

`--radius` is `0`. Every radius token resolves to it, so the whole interface is
square, graph nodes included. The one exception is `Lamp`'s `live` state, which is round
because it is a pilot lamp rather than a work state.

Elevation is a shadow only where something sits on a surface: a board tile on its
column, and the card in the hand. Nothing glows, no surface is a gradient, and no box
sits inside another box: a message inside a dialog is text, not an Alert. Shadow colours come from `--shade`,
`--shade-strong` and `--shade-deep`: thinned ink on the light theme, black on the
dark one, where the ink is light and would glow. A shadow barely reads on a dark
column, so there a tile's surface (`--tile`) is also a step lighter than it, with a
faint light along its top edge.

### Vocabulary

The vault is the collection; one item in it is an **Entry**. A card is **claimed**: its facts read "Claimed by", one past its time is an **expired claim**, and taking one from another actor is **stealing the claim**. A card's column reads **Column**, not Status. An entry's files are **Artifacts**. Files and components for the vault follow the same words: `VaultPage`, `VaultNav`, `Entry`.

A card's merged list of comments and events is its **Timeline**. The project-wide event stream is the **Event log**. **History** is reserved for revisions: the revision history and diff views of cards and entries. An entry's template reads as its name, or "No template" when it has none; the word "type" is retired for entries. "Activity" is retired as a word for the stream; do not use it in labels or identifiers. Wire names (`/api/activity`, the card detail's `activity`) stay until the backend renames them.

### Editing in place

An edit changes what can be typed into, not where anything is. `edit-surface` (in
`index.css`) is the one field treatment for in-place editing: a muted wash that
reaches 12px past the text sideways and 8px vertically, paid for by an equal negative
margin, so the words stay exactly where the reader saw them. Hover deepens the wash;
focus adds a 1px inset ring. `edit-hint` is the same geometry while reading: nothing
at rest, and the resting wash when pointed at, so the reader sees what a click will
edit and the click moves nothing. Both own their margin and padding, so never put a
spacing utility on the same element; wrap it instead.

Edit is about the words. It sits in the action row at the top of the card or entry,
and while editing `EditActions` takes its place, so Save is where the pointer already
is. Controls that are already controls (status, priority) are not part of an edit at
all: they apply as soon as they change, so they look and behave the same whether or
not an edit is open. Destructive actions live in the action row's menu, never at the
foot of the facts.

## Motion

Motion only ever shows a state change that is already true: something arrived, moved,
or was refused. Two curves live in `index.css`: `ease-arrive` for things entering, and
`ease-settle` for things settling in place. Nothing bounces.

| Where | What moves | Why |
|---|---|---|
| Entry | `animate-enter`: 4px rise and fade on each new entry | The document changed |
| Navigator | The selection surface slides to the open row | The selection moved |
| Graph dock | Panel height, chevron rotation | Open or closed |
| Graph | Nodes fade in, the layout eases to rest, fit and follow glide | The picture arrived; the reader moved |
| Board | The card in the hand lifts and tilts, siblings make room, the drop settles into the slot | A card is moving |
| Board | A claimed card shakes once | The move was refused |
| Shell | The section rule grows from the centre | The section changed |

`prefers-reduced-motion: reduce` removes all of it: CSS durations collapse in
`index.css`, the graph settles before it paints, and the board drops without an
animation.

## Notes

- Failed requests are read with `readError` (`@/lib/api`), which turns the server's `{"error": ...}` into the sentence inside it; never show a response body as it arrives. A refusal that carries more, such as who claims a contended card, is read with `readRefusal` and `whoClaims`.

- Every write a card supports lives in `cardActions` (`@/lib/card-actions`), which the board and the card page share: the fetch, the toast on failure, and the caller's own refresh afterwards. A new card action belongs there, not in a page.

- All imports come from `@/components/ui` (registry) or `@/components/wrappers` (composed).
- Raw HTML primitives are forbidden where a component exists. This includes markup that
  merely imitates one: a `fixed inset-0` overlay is a Dialog, an `animate-pulse` div is a
  Skeleton, a `<label>` is a Field, an `<hr>` is a Separator.
- Ad-hoc colors, spacing, and radius values are forbidden — use theme tokens. Raw palette
  colors (`text-amber-600`) are banned along with hex values, and manual `dark:` color
  overrides are banned because the tokens already carry both themes.
- `space-x-*` / `space-y-*` are banned; use `flex` with `gap-*`.
- Icons inside a `Button` carry `data-icon="inline-start"` or `"inline-end"` **on the icon
  element**, never on the Button, and carry no sizing class. Button sizes its own icons via
  `[&_svg:not([class*='size-'])]` and pads via `has-data-[icon=…]`, a `:has()` selector that
  only matches descendants. Icon-only buttons (`size="icon"`) have no label to pad from and
  are exempt.
- Pages do not set `z-index`; overlay components manage their own stacking.
