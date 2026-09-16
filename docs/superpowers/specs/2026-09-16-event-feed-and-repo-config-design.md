# Event feed and repository config

Date: 2026-09-16
Status: approved 2026-09-16

## Problem

The user wants two things that together make Trellis a base for integrations:

1. When something is pushed into Trellis — a finding written, any knowledge
   entry created or edited, a card created or moved — something else can react:
   run an agent, call a script, notify someone.
2. A repository can carry its own Trellis settings, beyond the one-line
   `.trellis` pin.

The user's decisions:

- Reactions are **queued through the event log**, not run inline after a write.
- **Trellis core never runs a reaction.** A separate subsystem or extension
  does. Which kinds of reaction exist (command, agent, webhook) is decided later
  and is not part of this design.
- Repository settings live in **`.trellis.yaml`** (or `.trellis.yml`).
  `.trellis` stays the one-line pin (see
  `2026-09-16-pin-only-projects-design.md`).
- **Not every setting belongs to a repository.** The daemon, the web UI, storage
  and anything machine-wide are configured on the machine, never by a
  repository file.
- **No trust mechanism now.** Core exposes what an extension needs, and runs
  nothing from a repository file.

So this design has two parts: an event feed that an extension can consume
reliably, and a repository config file that core reads for settings and passes
through, uninterpreted, for extensions.

## Part 1 — the event feed

### What exists

Every write already records a row in `event`
(`internal/store/migrations/0001_init.sql`): a monotonic `seq`, `ts`, `actor`,
`entity_type`, `entity_id`, `action`, `field`, `old_value`, `new_value`. The log
is append-only; `maintenance prune --events --before` is the only thing that
removes rows. The web UI's timeline (another session's uncommitted work) reads it
through `GET /api/p/{key}/events?after=&limit=`.

The event log is already a durable, ordered queue. What is missing is a stable
way to read it from outside, and a place for a consumer to remember how far it
got.

### The feed

One core query serves the CLI, the web endpoint and any extension:

```go
type EventQuery struct {
	ProjectID string   // "" = every project
	After     int64    // exclusive
	Limit     int      // default 1000, max 5000
	Kinds     []string // card | knowledge | board | label | note; empty = all
	Actions   []string // created, edited, moved, ...; empty = all but read
	DocTypes  []string // knowledge only: finding, decision, ...
	NotActor  string   // skip events written by this actor
}

type FeedEvent struct {
	Seq    int64  `json:"seq"`
	TS     int64  `json:"ts"`
	Actor  string `json:"actor"`
	Kind   string `json:"kind"`   // the event's entity type
	Ref    string `json:"ref"`    // card and note: KEY-12; knowledge: KEY/slug or GLOBAL/slug; board and label: name
	Title  string `json:"title"`
	Type   string `json:"type,omitempty"` // knowledge doc_type
	Action string `json:"action"`
	Field  string `json:"field,omitempty"`
	Old    string `json:"old,omitempty"`
	New    string `json:"new,omitempty"`
}

func (c *Core) EventFeed(ctx context.Context, q EventQuery) (events []FeedEvent, next *int64, err error)
```

- **The shape is the web timeline's** (`seq, ts, actor, kind, ref, action,
  field, old, new`) plus `title` and `type`, so a consumer can react to "a
  finding was created" without a second read. The web handler is changed to call
  `EventFeed`, so the two cannot drift.
- **Content never ships.** `old` and `new` are filled only for a card's column
  move — values that are column names. Bodies, summaries and titles in
  `new_value` are never copied into the feed. An extension that wants a body
  reads it with `knowledge show`, an explicit read.
- **Private entries** appear as ref and title only, which is what session
  injection already discloses for them.
- **Kinds are the entity types the log records today**: `card`, `knowledge`,
  `board`, `label` and `note` (a card note, whose `ref` is its card's).
  Artifacts have no events of their own; linking one to an entry is a
  `knowledge` event, `artifact_linked` or `artifact_unlinked`.
- **`read` is left out unless asked for.** `knowledge show` records one per
  read, and a feed that carried them would be mostly reads.
- **An entity that no longer exists** keeps its events with an empty `ref` and
  `title`, so an extension still sees that something happened to it. The
  `deleted` event itself carries the title the log recorded at deletion.
  A project-scoped read reaches them through the `project_id` column on `event`
  (filled at write time).
- `next` is the last `seq` returned, or null for an empty page.

### Consumers

A consumer is a named cursor, so an extension can stop and resume without
missing or repeating work:

```sql
CREATE TABLE event_consumer (
    name       TEXT PRIMARY KEY,
    cursor     INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
```

- Reading does not advance the cursor. The consumer calls `ack` after it has
  handled an event. That makes delivery **at least once**: a crash between
  handling and `ack` repeats the event, and never skips it. Extensions must
  tolerate a repeat.
- `ack` never moves a cursor backwards, and an `ack` beyond the newest event is
  refused.
- **A gap is reported, not hidden.** If `maintenance prune --events` removed
  events a consumer had not reached, the next read returns `gap: true` and the
  oldest retained `seq`. The consumer decides what to do. Pruning never waits for
  consumers.
- A consumer's own writes carry its actor (`TRELLIS_AGENT`). `NotActor` lets it
  skip them, which is how an extension avoids reacting to itself.

### CLI

```
trellis events [--after N] [--limit N] [--kind K]... [--action A]... [--type T]...
               [--not-actor A] [--all-projects] [--follow]
trellis events --consumer NAME [filters...] [--follow]
trellis events ack NAME SEQ
trellis events consumers              # name, cursor, lag, gap
trellis events consumers rm NAME
```

- Output is JSON lines, one `FeedEvent` per line, so a shell pipeline or a
  long-running extension can read it incrementally.
- `--consumer NAME` starts after that consumer's cursor and creates the consumer
  on first use.
- `--follow` keeps reading. It polls every second. It does not need the daemon.
  The daemon can wake followers faster later, without changing the output.
- `--all-projects` is required to read across projects. The default is the
  current project, as every other command.

### Web

`GET /api/p/{key}/events?after=&limit=` keeps its path and its response,
`{"events": [...], "next": ...}`. It calls `EventFeed`, and gains the
`title` and `type` fields. Consumers are a CLI and extension concern; the web
API does not manage them.

### What the feed does not do

- **It runs nothing.** No command, agent or webhook is started by Trellis.
- **It does not see an edit until Trellis does.** A file saved in an editor
  becomes a `reloaded` event when Trellis next reads that entry, not when it is
  saved. An extension that needs prompt notice of hand edits has to prompt a
  read, for example with `knowledge ls`.
- **It is not a transaction log for replication.** It carries what changed and
  who did it, not the new state.

## Part 2 — `.trellis.yaml`

### Where it is found

The file is `.trellis.yaml` or `.trellis.yml`; both in one directory is an
error naming both. Below, `.trellis.yaml` means either.

It sits in the directory that resolved the project: the directory
of the `.trellis` pin. Until the pin-only design ships, it can also sit in the
repository root, which is how a project without a pin is resolved today. A
`.trellis.yaml` anywhere else is not read. It is meant to be committed, like the
pin.

A project resolved through `TRELLIS_PROJECT` or `--project` reads no
`.trellis.yaml`: there is no directory to read it from.

### What it holds

```yaml
config:
  card.ls_limit: 50
  lease.ttl: 45m
  labels.require_on_card: true

extensions:
  actions:
    - on: knowledge.created
      type: finding
      run: ./scripts/review-finding.sh
```

- **`config`** takes the dotted keys `trellis config ls` lists, but only those a
  repository may set. A repository configures how work in it is done. The
  daemon, the web UI, storage and anything else that belongs to the machine are
  not its business — and the file is committed and arrives with every clone, so
  it must not be able to redirect storage, open a port, or run a program.

  | Allowed | Refused |
  |---|---|
  | `card.*`, `lease.ttl`, `board.default_columns`, `labels.*`, `tags.*`, `search.limit`, `search.method` | `ui.*` (the daemon and web UI), `db.*`, `git.*`, `search.vector.*` (it includes `embed_command`, which runs a program), and every key added later until it is marked repository-safe |

  The allowed set is declared next to each key in `internal/config`, so a new
  key is refused until someone decides it is safe.
- **`extensions`** is a map from an extension's name to anything. Core parses it
  only as YAML, never interprets it, and never runs anything from it.
  `trellis extension config <name>` prints that subtree as JSON, which is how an
  extension reads its settings. **An extension that runs commands from this
  section must decide for itself whether to trust the file.** Core's
  documentation says so plainly.
- Any other top-level key, an unknown or refused `config` key, or a value that
  does not parse for its key is an error naming the file and the key. A broken
  file is never silently ignored.

### Precedence

From lowest to highest:

1. built-in defaults
2. the global config file
3. `.trellis.yaml`
4. project overrides in the database (`trellis config set`)

Environment variables and flags keep overriding everything.

The repository file is the team's shared default. A person's `trellis config
set` on their own machine still wins.

`trellis config get` reports the source as `default`, `config`, `repo` or
`project`. It also stops reporting `default` for a value that came from the
global file, which is the bug in `EffectiveValue` today.

`trellis config set --repo <key> <value>` and `unset --repo` write the file,
preserving the other keys and the `extensions` section.

### Scope of its effect

`.trellis.yaml` applies to commands run from that repository. The daemon and the
web UI do not run inside a repository, so they do not read it. A setting that
must hold however a write arrives belongs in a project override instead.
`trellis config get` says which layer answered. This is why the allowed keys
are the ones that shape what a caller does — listing limits, lease length, what
a card needs — and not storage policy.

## Non-goals

- Running anything: commands, agents, webhooks. That is an extension's job, and
  its kinds are decided later.
- A trust mechanism for repository files. Core runs nothing from them, so it
  needs none. An executing extension needs one.
- Push delivery over a socket or HTTP. Polling `--follow` is enough for a local
  tool, and the daemon can add a wake-up later without changing the output.
- Replacing project overrides in the database.

## Order

After revision history and templates. The feed's web handler replaces another
session's query, so that session's commit lands first.

## Testing

- `EventFeed` returns events in `seq` order, pages with `After` and `Limit`, and
  filters by kind, action, doc type, actor and project; `read` events appear
  only when asked for.
- It never returns a body, summary or edited title in `old`/`new`; a column move
  carries both column names.
- A private entry's events carry ref and title and nothing else.
- A consumer resumes after its cursor; `ack` never moves backwards and refuses a
  `seq` past the newest; a read after pruning past the cursor reports `gap`.
- `--follow` prints an event written after it started.
- `NotActor` skips a consumer's own writes.
- `.trellis.yaml` and `.trellis.yml` are both read; both present is an error.
- Allowed keys apply; refused keys (`ui.port`, `db.busy_timeout_ms`,
  `search.vector.embed_command`) and unknown keys fail with the file path; and
  `extensions` round-trips through `trellis extension config`.
- Precedence: each layer overrides the one below it, and `config get` names the
  right source.
- `config set --repo` preserves the rest of the file.
- A project resolved by `TRELLIS_PROJECT` reads no repository file.
- The web events endpoint returns the documented shape through `EventFeed`.
