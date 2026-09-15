# Integrations

One directory per harness. Each holds the hook code that connects that harness
to Trellis — and nothing else.

## Why they are separate

Knowledge is portable; triggering is not. The CLI is the same everywhere, and
`skills/` is one copy of markdown that every harness reads. What differs is
only how a harness announces a turn and how it accepts injected context:

| Harness | Event | Input | Output |
|---|---|---|---|
| Claude Code | `UserPromptSubmit` | `prompt`, `cwd`, `scratchpad_dir` on stdin | `hookSpecificOutput.additionalContext` |
| Codex | see `codex/` | | |

That table is the whole adapter. An integration translates it and calls the
CLI; it never decides what to search for, how to rank, or what to show.

## The contract

An integration must:

1. Read the harness's event from stdin and pull out the free text.
2. Call one Trellis command. All judgement lives in the CLI, so two harnesses
   cannot drift into recalling different things.
3. Inject **identifiers, never bodies**. Opening an entry costs a turn and is
   the agent's call; not knowing it exists is not.
4. Bound what it injects, and not resend what this session already saw. A
   prompt hook runs every turn, and an injected token is paid for once on cache
   write and again on every later read.
5. Fail silently. A notice repeated once per prompt is worse than the miss it
   reports.

## Adding a harness

Copy the nearest existing directory, change the two envelope functions, and
register it the way that harness expects. If you find yourself moving logic out
of the CLI to make it fit, stop: that logic belongs in `trellis` so every other
harness gets it too.
