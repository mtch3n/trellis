# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

One developer, working across roughly seven local repositories, whose actual work
runs through AI coding agents at the CLI. They are not the board's main author —
the agents are. They open the web UI in three confirmed situations:

1. **Coming back cold.** Returning after hours or days to a project whose last
   session they no longer remember. Measured: 217 of 237 resumes after >24h
   (92%) opened with no recap available.
2. **Watching agents work.** Live, during a session, to see cards move and leases
   get claimed as they happen.
3. **Reviewing after a session.** Checking what the agent actually did and what
   it wrote down.

## Product Purpose

A local-first kanban board and knowledge base for work that is executed by AI
agents. One Go binary ships the CLI, an optional daemon, and the embedded web UI.
Success is that a human returning to a project can re-establish what is happening
without re-reading transcripts, and that an agent can record progress and
findings that survive the end of its session.

## Positioning

The board's primary caller is an agent, not a human. Cards, leases, notes, and
knowledge are written by agents through the CLI; the web UI is the human's window
onto that activity rather than a place they maintain a backlog by hand. This
asymmetry is the product, and it is what a conventional kanban tool could not
truthfully copy: those are authored by the people who read them.

## Operating Context

- CLI-first, with agent identity carried in `TRELLIS_AGENT`.
- A systemd user daemon serves the UI on authenticated localhost. The token is
  exchanged for a session cookie at `/`; a deep link opened without that cookie
  fails to load its data.
- The board pushes live updates over SSE.
- `internal/resolve` maps a working directory to a project via env, a `.trellis`
  pin, git remote, then git root.
- Agent sessions are short and fragmented: median 4 prompts, 36% end within 2,
  only 5% reach 50.

## Capabilities and Constraints

- Boards, columns, and cards carrying priority, an optimistic-concurrency
  `version`, and an optional lease `owner`. Cards accumulate notes and activity.
- Leases can be stolen, with a reason recorded.
- Knowledge documents with wikilinks, labels, and a link graph. A dangling
  wikilink is a stub, not an error.
- Search across cards and knowledge: FTS5, vector, and hybrid.
- Markdown files are the source of truth for knowledge content. SQLite holds
  metadata, relationships, and derived indexes; anything derived must be
  rebuildable from the files.
- Cross-platform: Linux, macOS, and Windows all ship.
- `web/dist` is committed and embedded with `go:embed`; a directory with no
  embeddable files breaks the build.

## Evidence on Hand

- `TRELLIS/pain-point-analysis-sept-2026` — derived from 20,287 prompts and
  1,810 session transcripts, Feb–Sep 2026. Read it before re-analyzing; the doc
  states re-deriving is expensive.
- Usage is overwhelmingly agent-driven: 149 invocations on Sep 14 against
  **exactly one human write in the product's lifetime** (a smoke test). All 14
  registered agents were created by the hook itself.
- The same document records that scope creep within a session, not cross-session
  context loss, is the top user frustration — and that Trellis does not address
  it. Future work must not claim otherwise.
- That document also flags a fabricated "standing preferences" table produced by
  an earlier analysis, with roughly 50x inflation on every row. It must not be
  reused.
- No customer, benchmark, pricing, or adoption claims exist. The product has been
  live approximately one working day.

## Product Principles

1. **The agent writes; the human reads.** Layout weight follows that asymmetry
   rather than mirroring a tool whose users author their own cards.
2. **Orientation after absence is the primary job.** The most valuable thing the
   interface can do is answer "what is happening here" for someone who has
   forgotten.
3. **One project is the working scope.** Switching projects is a deliberate act,
   not an ambient condition.
4. **Files are the truth.** Derived state is disposable and must be rebuildable.
5. **Report what is real.** This project has already been burned once by
   fabricated analysis; unverified claims are worse than gaps.

## Accessibility & Inclusion

Keyboard parity is a product requirement, not a nicety: until recently the core
kanban interaction — moving a card between columns — was reachable only by mouse
drag-and-drop, with no keyboard path at all. Every action the interface offers
must be operable from the keyboard.
