# Integrations

One directory per harness. Each holds the hook code that connects that harness
to Trellis — and nothing else.

## Why they are separate

Knowledge is portable; triggering is not. The Trellis server is the same for
every harness, and `skills/` is one copy of markdown that every harness reads.
What differs is only how a harness announces a turn and how it accepts injected
context:

| Harness | Event | Input | Output |
|---|---|---|---|
| Claude Code | `UserPromptSubmit` | `prompt`, `cwd`, `session_id` on stdin | `hookSpecificOutput.additionalContext` |

That table is the whole adapter. An integration translates it and calls the
server; it never decides what to search for or how to rank.

## The contract

An integration must:

1. Read the harness's event from stdin and pull out the free text.
2. Call one tool through `trellis call recall '{"text": ..., "limit": 5}'`,
   with `TRELLIS_SESSION` set to the first 32 hex digits of the SHA-256 of the
   session id, the way the SessionStart hook sets it. All judgement lives in
   the server, so two harnesses cannot drift into recalling different things.
   The one check before the call is cheap: skip it when no `.trellis` marker
   sits in the directory or above it, and `TRELLIS_PROJECT` is unset.
3. Inject **slugs and recaps, never bodies**. Opening an entry costs a turn and
   is the agent's call; not knowing it exists is not.
4. Bound what it injects, and not resend what this session already saw. A
   prompt hook runs every turn, and an injected token is paid for once on cache
   write and again on every later read. Inject each hit's `line` as given, stop
   at the byte budget, and remember only the slugs it injected.
5. Fail silently. A notice repeated once per prompt is worse than the miss it
   reports.

## Adding a harness

Copy the nearest existing directory, change the two envelope functions, and
register it the way that harness expects. If you find yourself moving logic out
of the server to make it fit, stop: that logic belongs in the server so every
other harness gets it too.
