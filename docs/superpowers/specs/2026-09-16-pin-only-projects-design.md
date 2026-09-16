# Pin-only project resolution

Date: 2026-09-16
Status: design, planned in `docs/superpowers/plans/2026-09-16-pin-only-projects.md`
Revised: 2026-09-16, after an adversarial review. The migration's integrity
check now actually gates the commit. `init` became one transaction with a
no-clobber pin write, no longer joins on an inferred key, and no longer creates
boards. Old bindings are kept in the event log instead of relying on the user
to copy them before an automatic migration.
Layer 1 of 3: pin-only resolution (this spec), then virtual path addressing
(`2026-09-16-virtual-paths-design.md`), then `project merge`
(`2026-09-16-project-merge-design.md`). The order of the last two was swapped
when planning: merge rewrites references, and it should rewrite them once, in
their final syntax.

## Problem

A `project` row conflates two things: the project, and one way a local
directory reaches it. `identity_kind`, `identity_value` and `root_path UNIQUE`
live on the project itself (`internal/store/migrations/0001_init.sql:2`), so a
project has exactly one identity and at most one directory.

What that costs, verified against the code:

- **A renamed folder becomes a new, empty project.** A repository without a
  remote gets a `path` identity. After a rename, `Identify` returns a new path
  and a new `SuggestedKey`, neither matches, and `EnsureProject` creates a
  second project (`internal/core/project.go:72`). On the author's machine five of
  six projects have a `path` identity.
- **A moved folder with the same name fails.** The key is taken, so the create
  branch returns `key_collision` (`internal/core/project.go:67`).
- **A project cannot exist without a directory**, and a monorepo is one project
  unless someone writes a pin.
- **Any command in any git repository creates a project**, including the
  SessionStart hook's `board show --brief` (`internal/cli/root.go:115`).
- **Resolution needs `git` on `PATH`** and shells out on every invocation
  (`internal/resolve/git.go`), with an environment scrub to stop inherited
  `GIT_DIR` from answering for a different repository.

And one live bug. Outside a repository the pin walk's boundary is `$HOME`
(`internal/resolve/resolve.go:119`), which `findPin` checks inclusively
(`internal/resolve/resolve.go:102`), so it reads `$HOME/.trellis` — which
is the storage root, a directory — as a pin:

```
reading /home/<user>/.trellis: read /home/<user>/.trellis: is a directory
```

`board show` hides it because it silences any error whose text contains
`run: trellis init --pin` (`internal/cli/board_show.go:42`), so the hook stays
quiet; `trellis doctor` shows it.

## Model

A project is a virtual namespace identified by its key. It does not depend on
any directory. The only link from a local directory to a project is a
`.trellis` file. Trellis does not ask git who a repository is.

## The pin file

A pin is a file named `.trellis` whose `os.Stat` reports a regular file. `Stat`
follows symlinks, so a symlinked pin counts when its target is a regular file.
During the walk, a directory or any other non-regular entry with that name is
skipped. This is the fix for the bug above, and it applies on every platform,
even though the Windows storage root (`%LOCALAPPDATA%\trellis`) does not
collide. `init` treats the same entry as an error; see below.

Its content is one virtual path, with surrounding whitespace trimmed:

```
/TRELLIS
/MONO/boards/api
```

- The key is matched case-insensitively and always written upper-case.
- The board segment is a board **slug**, in the shape `slugify` gives one
  (`internal/core/board.go:38`): lower-case Unicode letters and digits joined
  by single hyphens. `SelectBoard` matches on name today
  (`internal/core/board.go:146`); the pin's board is matched on slug.
- Anything else is a hard usage error, `bad_pin`, that names the file and shows
  the accepted shapes. That covers an empty file, more than one line, extra
  segments, a collection other than `boards`, a malformed key or slug, and a
  bare `TRELLIS`.
- The bare `TRELLIS` form is the old format. There is no compatibility. The
  message for it says to replace the content with `/TRELLIS`, or to delete the
  file and run `trellis init --key TRELLIS`.

The parser lives in a new package, `internal/vpath`. Its `Path` type is general
from the start, `{Project, Collection, Name}`, and `ParsePin` accepts only these
two shapes. Path addressing adds a general parser beside it without reshaping
the type.

**Pins are meant to be committed.** A committed pin travels with every clone and
every worktree; `git worktree add` does not copy untracked files, so an
uncommitted pin leaves worktree sessions with no project. `init` says this in
its output. This repository removes `/.trellis` from `.gitignore` and commits a
pin containing `/TRELLIS`.

### Key grammar

New keys must match `^[A-Z][A-Z0-9]*(-[A-Z0-9]+)*$`. `init` and `project new`
check it. `GLOBAL` (`internal/core/knowledge.go:24`) is reserved and refused.

A pin can only name a key that satisfies the grammar. Today's `sanitizeKey`
(`internal/resolve/resolve.go:68`) and `EnsureProject` accept any characters, so
an existing database may hold a key the grammar rejects. Such a project keeps
all its data. It stays reachable through `--project` and the web UI, `doctor`
lists it, and layer 2's `project merge` is how it gets folded into a key that
can be pinned. Nothing renames keys in place.

A default key derived from a folder name is sanitized: upper-case, every run of
characters outside `[A-Z0-9]` becomes `-`, leading and trailing `-` are trimmed,
and a result starting with a digit gets a `P` prefix (as `sanitizeKey` does
today). An empty result requires `--key`.

## Resolution

**Project:** `--project`, then `TRELLIS_PROJECT`, then the pin walk. Nothing
else.

**Board:** `--board`, then `TRELLIS_BOARD`, then the pin's board segment, then
the existing `SelectBoard` rules. The pin's board applies only when the project
also came from the pin; under `--project OTHER` the pin describes a different
context and is ignored for board selection.

`currentBoard` (`internal/cli/root.go:87`) and `currentProject`
(`internal/cli/config.go:28`) duplicate the project half of this today. Both call
one helper that returns the project and, when it came from a pin, the pin's path
and board.

### The pin walk

Canonicalize the working directory with `filepath.EvalSymlinks`. A failure is an
error. The walk does not fall back to the lexical cleaning that `normalizeDir`
uses for paths that may not exist (`internal/resolve/resolve.go:132`).

Then, at each directory `d`:

1. If `d` is the normalized `$HOME` or the filesystem root, stop. Neither is
   checked: a pin there would capture every directory beneath it, and
   `$HOME/.trellis` is the default storage root. If `os.UserHomeDir` fails,
   only the filesystem root is excluded.
2. `Stat` `d/.trellis`. If it is a regular file, that is the pin.
3. `Lstat` `d/.git`. If it exists — directory, file or symlink — stop.
4. Otherwise continue with the parent.

**Errors are never absence.** Any error from those `Stat` and `Lstat` calls
other than not-exist is a hard error. Reading the pin works the same way,
matching today's reader (`internal/resolve/resolve.go:99`). A permission error
must never let the walk continue to an ancestor's pin.

The pin check comes before the `.git` stop, so a pin at a repository root is
found. The nearest pin wins, so a pin in a monorepo subdirectory overrides the
one at its root. A pin above a repository root never applies to that
repository. `$HOME` is compared through `normalizeDir`, for the macOS
`/private/var` and Windows 8.3 reasons already in CLAUDE.md.

### Why the walk stops at `.git`

Common tools split into two camps. git stops at the first `.git` it finds, and a
repository never inherits anything from outside it; `GIT_CEILING_DIRECTORIES`
exists to bound the search further. Prettier, `go.mod` and `package.json`
lookups take the nearest file with no repository boundary, and EditorConfig
merges upward until a file says `root = true`.

Trellis takes git's side for two reasons:

- **The cost of a wrong match is different.** A misapplied formatter config
  produces visibly wrong formatting. A misapplied pin sends cards and knowledge
  to the wrong board without any error.
- **Pins are committed.** A pin inside the repository travels with it. A pin
  outside does not, so honoring it would make the same repository resolve
  differently depending on where it was cloned.

The check is an `Lstat`, not `git rev-parse`: no subprocess, no git binary, no
environment scrub. `.git` is only a stop sign; no identity is read from it.

### Outcomes

| Situation | Result |
|---|---|
| Pin found, project exists | Resolves |
| Pin found, project missing | Not-found error `project_not_found`; the message names the pin file; hint `trellis init` |
| Pin found, board slug missing | Not-found error `unknown_board`, naming the pin file and listing the project's board slugs |
| Pin malformed | Usage error `bad_pin` naming the file |
| No pin | Usage error `unresolved`; hint `trellis init --key <KEY>` |

`board show` stays silent only for `unresolved`, matched on the error code with
`errors.As`, not on message text (`internal/cli/board_show.go:42`). A missing
project or a malformed pin exits non-zero, so the hook reports "Trellis brief
unavailable": that directory opted in, and the failure is worth surfacing. With
no pin the hook injects nothing — no pin means Trellis is not used here, and a
hint in every such session would be noise. `doctor` carries the hint instead.

## Commands

### `trellis init [--key KEY] [--no-preset]`

`init` makes the current directory resolvable by writing `./.trellis`; the
`--pin` flag is removed. The only inputs are `--key`, the global `--board`
flag, and `./.trellis` itself. The walk plays no part.

**Refusals, checked first:**

- `--project` is refused with a usage error that points to `--key`.
  `TRELLIS_PROJECT` and `TRELLIS_BOARD` are not consulted.
- If `TRELLIS_PROJECT` is set, the output warns that commands in this
  environment resolve to that project, not to the pin.
- `init` refuses to run in `$HOME` or the filesystem root, because the walk
  never reads a pin there.
- `./.trellis` that exists but is not a regular file is `bad_pin`.

**`./.trellis` exists.** Parse it.

- If `--key` differs from the pinned key, or `--board` names a board other than
  the pinned one, fail with `pin_exists`, naming the file.
- Otherwise create the project if it is missing — a fresh clone on a new
  machine.
- A pinned board that does not exist is `unknown_board`, with the hint
  `trellis board new`.

**No `./.trellis`.**

- **With `--key`,** join the project if it exists, and create it if it does
  not. The output says which happened. The key must pass the key grammar
  either way: `init` never writes a pin its own parser would reject, so a
  project with a legacy key is reached through `--project` or merged.
- **Without `--key`,** the key is the sanitized folder name, and `init` only
  ever creates with it. If a project with that key already exists, the result is
  `key_collision`. The hint suggests `trellis init --key <KEY>` to join that
  project deliberately, or `--key <OTHER>` for a new one. Two unrelated folders
  named `api` therefore never share a board by accident.
- **With `--board NAME`,** the board is matched by name, as the flag is
  everywhere else, and must already exist. The pin records that board's slug.
  Without `--board`, the pin is `/KEY`.

**`init` does not create boards** beyond the default board of a new project.
Board slugs are derived from names, with a suffix on collision
(`internal/core/board.go:60`), and names are unique separately
(`internal/store/migrations/0001_init.sql:22`). Creating a board to match an
exact slug is therefore not something the board code can promise.

**One transaction.** Core gains
`InitProject(ctx, dir, key string, join bool, board string, preset bool)`, whose
steps all run inside a single `Core.Tx`:

1. Look up the project, or create it with its default board.
2. Seed the default labels, unless `--no-preset` was given.
3. Resolve the board.

The existing tx-scoped `createBoard` covers the board. Label seeding gets a
tx-scoped variant, because each public `Core` method opens its own transaction
(`internal/core/core.go:72`).

**No-clobber pin write.** After the commit, `InitProject` publishes the pin
with `writeAtomic(path, data, false)` (`internal/core/file_store.go:13`): a
synced temporary file, then a hard link that fails if `.trellis` already exists.
`writeAtomic` creates files with mode 0600. git records only the executable
bit, so clones get the usual mode.

If another `init` wrote the pin first, the new pin is read back:

- **It names the same project and board:** success.
- **It names anything else:** `pin_exists`. The error says that any project
  this call created still exists, without a pin, and shows up in
  `project ls`. An unpinned project is harmless and can be pinned later with
  `init --key`.

If the output finds a pin above the current directory, it notes that the new pin
overrides it and names that pin's path. It also reminds the caller to commit
`.trellis`. JSON output includes `pin_path`, `project`, `board`, `columns`, and
whether the project was created or joined.

### `trellis project`

A new command group:

- `project new KEY [--no-preset]` creates a project and its default board, with
  no pin. It refuses an existing key and `GLOBAL`.
- `project ls` lists every project: key, name, board count, created time.

Layer 2 adds `project merge` here.

`EnsureProject` and `resolve.Identity` are removed. `InitProject` and
`project new` share a tx-scoped create, which is today's create branch of
`EnsureProject`. `internal/core` still imports `internal/resolve`, but only to
read a pin back when the no-clobber write finds one already there.
`internal/resolve` never imports core, so the import-cycle note at
`internal/resolve/resolve.go:26` goes away with `Identify`.

### `trellis doctor`

- **`project` check, where the project came from:** `/TRELLIS (pin <path>)`,
  `(from --project)` or `(from TRELLIS_PROJECT)`.
- **No pin:** a warning that suggests `trellis init --key <KEY>`.
- **Pin names a project this database does not have, or there is no database
  yet (a fresh clone):** a warning that suggests `trellis init`. A database
  that cannot be opened or read is a warning with the error, never `ok`.
- **`project keys` check:** a warning that lists every key failing the key
  grammar. Neither check creates a database.

## Schema

Migration `0013` drops `identity_kind`, `identity_value` and `root_path`, and
the indexes `project_identity` and `project_root`. `Project` loses the three
fields, and its JSON drops them; nothing in `web/src` reads them.

`root_path` is `UNIQUE`, and SQLite refuses `ALTER TABLE DROP COLUMN` on a
column with a `UNIQUE` constraint, so the table is rebuilt.

**The rebuild must not cascade.** Every connection opens with
`foreign_keys(ON)` (`internal/store/db.go:66`). With enforcement on,
`DROP TABLE project` performs an implicit `DELETE`, and the `ON DELETE CASCADE`
on `board`, `card`, `knowledge` and the rest would empty the database.
`PRAGMA foreign_keys` is a no-op inside a transaction, and goose wraps each
migration in one by default.

**The integrity check must gate the commit.** A no-transaction SQL migration
cannot do that. goose runs each statement with `ExecContext` and never reads
result rows (`pressly/goose/v3@v3.28.0/migration_sql.go:70`), so the rows
returned by `PRAGMA foreign_key_check` would be discarded. Any check placed
after `COMMIT` would also be too late to roll back.

`0013` is therefore a Go migration, registered with
`goose.AddNamedMigrationNoTxContext`. goose merges registered Go migrations
with the embedded SQL ones (`migrate.go:303`). It follows SQLite's documented
table-rebuild procedure:

1. Pin a connection with `db.Conn`, and run `PRAGMA foreign_keys = OFF` on it.
   The store's pool has exactly one connection (`internal/store/db.go:76`).
   Every statement goes through that pinned connection and its transaction; a
   statement issued through the pool would wait forever for the only
   connection.
2. Begin a transaction.
3. For each project, insert an `event` row with `entity_type = 'project'`,
   `action = 'unbound'` and `actor = 'migration'`. Record `root_path`, and the
   `identity_kind:identity_value` pair, as `old_value`, one row per non-empty
   field.
4. Create the new table:

   ```sql
   CREATE TABLE project_new (
       id         TEXT PRIMARY KEY,
       key        TEXT NOT NULL UNIQUE,
       name       TEXT NOT NULL,
       created_at INTEGER NOT NULL
   );
   ```

5. `INSERT INTO project_new SELECT id, key, name, created_at FROM project`.
6. `DROP TABLE project`, then `ALTER TABLE project_new RENAME TO project`.
7. Query `PRAGMA foreign_key_check`. If it returns any row, roll back and fail
   the migration, naming the violating tables.
8. Commit. Whether the migration succeeded or failed, finish with
   `PRAGMA foreign_keys = ON`.

**The migration is safe to rerun.** goose records the version after the
function returns, outside its transaction. A process that dies in between
leaves the rebuild committed and the version unrecorded, so the migration
first checks whether `root_path` is already gone, and succeeds if so.

The Down migration re-adds the three columns empty.

## Removed

- `internal/resolve/git.go`, `internal/resolve/giturl.go` and their tests.
  `resolve.go` keeps only the pin walk and `normalizeDir`.
- `EnsureProject`, its rebind branch, and the `project rebound` event.
- `init --pin`.
- The message-text match in `internal/cli/board_show.go:42`.
- The `trellis init --pin` hints at `internal/cli/root.go:113`,
  `internal/cli/config.go:50` and `internal/cli/doctor.go:293`.

## Documentation

- `CLAUDE.md`: the `internal/resolve` line ("env, `.trellis` pin, git remote,
  git root") becomes "flag, env, `.trellis` pin". The invariant "The pin walk
  has a boundary" is rewritten: the walk stops after a directory containing
  `.git`, never checks `$HOME` or the filesystem root, and never treats an
  error as absence.
- `README.md:18` ("Projects are identified by Git repository URL…") and
  `README.md:54` ("Enable trellis in the current git repository").
- `PRODUCT.md:47`.
- `scripts/tests/test_plugin_cli.py:45` calls `init --pin --key HOOKTEST`.
- The doc comment on `DeleteProject` (`internal/core/project.go:137`) says
  running Trellis in the repository again creates a fresh project. Now the pin
  names a missing project and `trellis init` recreates it.
- The core `trellis` skill lists `init`, `project new` and `project ls`.

## Existing data

No binding is migrated. Every project keeps its row, boards, cards and
knowledge. Any command opens the database and runs `0013` automatically,
including the SessionStart hook. The old directories are therefore preserved
by the migration itself, in the event log, rather than by asking anyone to copy
them beforehand:

```bash
sqlite3 ~/.trellis/trellis.db "SELECT p.key, e.old_value FROM event e
  JOIN project p ON p.id = e.entity_id
  WHERE e.entity_type = 'project' AND e.action = 'unbound'"
```

To reconnect, run `trellis init --key <KEY>` in each directory. A directory
that still holds an old bare-key `.trellis` gets `bad_pin`, whose message says
to replace the content or delete the file first.

## Non-goals

- Any git detection beyond the `.git` stop.
- Matching a project by folder name. The name only suggests a key for `init`.
- A stored binding table. The pin is the binding.
- Creating a project anywhere except `init` and `project new`.
- Creating boards from `init`.
- A hook hint for directories with no pin.
- `--project` and `TRELLIS_PROJECT` accepting `/KEY` paths (layer 3).
- Renaming a project key.

## Follow-up layers

Each has its own spec:

- **Layer 2, virtual path addressing:**
  `docs/superpowers/specs/2026-09-16-virtual-paths-design.md`.
- **Layer 3, `project merge`:**
  `docs/superpowers/specs/2026-09-16-project-merge-design.md`. A key that fails
  the key grammar is folded into a valid one here.

## Testing

- **Pin parsing:**
  - Accepted: `/KEY`, lower-case `/key` (normalized), and `/KEY/boards/api`.
  - Rejected: empty, two lines, `/KEY/boards`, `/KEY/cards/1`, trailing
    segments, bad key characters, and a slug that `Slugify` would change.
  - A bare `KEY` gets the old-format message.
- **Pin walk:**
  - The nearest pin wins, and a subdirectory pin overrides the repository-root
    pin.
  - The walk stops after a `.git` directory and after a `.git` file (worktree).
    A pin above a repository root is ignored.
  - A symlinked pin to a regular file counts.
  - A regular `.trellis` file planted in a temporary `$HOME` is not read. A
    `.trellis` directory is skipped, which is the regression test for the bug.
    The filesystem root is not checked.
  - An unreadable `.trellis`, or an `Lstat` failure on `.git`, is an error and
    never falls through to an ancestor's pin. These cases are skipped on
    Windows, where permission bits do not deny reads.
  - With `os.UserHomeDir` failing, the walk still stops at the filesystem root.
  - `/var` versus `/private/var` on macOS, and 8.3 names on Windows CI.
  - No test needs `git` on `PATH`; walk fixtures `mkdir .git`.
- **Precedence:** `--project` beats `TRELLIS_PROJECT` beats the pin. `--board`
  beats `TRELLIS_BOARD` beats the pin's board. The pin's board is ignored under
  `--project`.
- **Errors:**
  - No pin gives `unresolved`, and `board show --brief` prints nothing and exits
    0.
  - A missing project, a missing pinned board and a malformed pin each exit
    non-zero, with the pin path in the message.
- **`init`:**
  - With `--key`: creates, and joins (output says joined).
  - Without `--key`: creates from the folder name, and an existing key gives
    `key_collision`.
  - An existing pin materializes its project on an empty database. A missing
    pinned board gives `unknown_board`, and no board is created.
  - Conflicting `--key` or `--board` against an existing pin gives
    `pin_exists`.
  - `--board NAME` records the board's slug, and a missing board fails.
  - Refused: `--project`, running in `$HOME` or the filesystem root, and a
    `.trellis` directory. A set `TRELLIS_PROJECT` produces the warning.
  - Folder-name sanitization: spaces, a leading digit, and an all-punctuation
    name that requires `--key`. `GLOBAL` is refused.
  - A pin that appears between the commit and the write: identical content
    succeeds, and different content gives `pin_exists` and leaves the file
    untouched.
  - A failure inside the transaction leaves no project, board or labels behind.
  - A parent pin is reported as overridden, and JSON carries `pin_path`.
- **`project new` / `project ls`:** `new` refuses an existing key and `GLOBAL`.
- **Migration `0013`:**
  - A populated database keeps every board, card, note, label and knowledge
    row, and the three columns are gone.
  - Each project's old `root_path` and identity are in the event log.
  - A seeded foreign-key violation (inserted with enforcement off) makes the
    migration fail and leaves the `project` table and its columns as they were.
- **Nonconforming keys:** a project whose key fails the grammar is still
  reachable with `--project` and is listed by `doctor`.
- **Hook** (`scripts/tests`): no pin produces no context. A pin naming a
  missing project produces "unavailable".
