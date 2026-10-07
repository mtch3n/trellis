#!/usr/bin/env python3
"""The shadow: a second look at a change, whose confirmed findings the agent must answer.

    shadow.py run <root> <top> <tree> <claude>   (started by verify.py, detached)
    shadow.py score                               fixed, dismissed and open shadow findings

A finder proposes at most five problems a script could not catch. A second,
separate run reads the code and confirms or rejects each one. A confirmed one
is queued as a `decide` finding with probe `shadow`; the Stop hook asks the
agent to fix or dismiss it, and how it was resolved is its rating. Both passes
are logged in .sdd/shadow.jsonl. A shadow finding never changes verify's verdict.
"""

import os
import pathlib
import re
import subprocess
import sys

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))

import open_items  # noqa: E402
import state  # noqa: E402
from specs import StoreError, all_segments  # noqa: E402

MAX_CANDIDATES = 5
EARLIER = 40
DIFF_CHARS = 60_000
SPEC_CHARS = 30_000
TIMEOUT = 600
FIND = """You are looking at a change for problems that a script could not catch: a
contradiction with a spec decision, an unexpected case the change ignores, a
risky schema or data change. List at most five, one line each, in the form
`<problem> — evidence: <exact quote from the diff or spec>`. A line without an
exact quote is worthless. Output nothing else; if you see nothing, output nothing.

## Already raised (do not repeat these, or anything that says the same)
{earlier}

## Spec
{spec}

## Diff against {base}
{diff}
"""
CHECK = """You are checking claims about a code change against the code itself. The
repository is your working directory: read whatever you need, and change nothing.

For each numbered claim, answer on a line of its own, exactly one of:
CONFIRMED <n>: <what goes wrong, and with which input or state>
REJECTED <n>: <why the claim does not hold>
Confirm a claim only when the code shows it happening. Output nothing else.

## Claims
{claims}

## Diff against {base}
{diff}
"""
ANSWER = re.compile(r"^\s*(CONFIRMED|REJECTED)\s+(\d+)\s*:\s*(.*)$")


def path(root):
    return state.folder(root) / "shadow.jsonl"


def ask(claude, prompt, top, *options):
    """The model's stdout, or raises RuntimeError saying why there is none."""
    try:
        result = subprocess.run([claude, "-p", prompt, "--model", "sonnet", *options], cwd=top,
                                capture_output=True, text=True, timeout=TIMEOUT, check=False,
                                env={**os.environ, "SDD_SHADOW": "1"})
    except (OSError, subprocess.TimeoutExpired) as error:
        raise RuntimeError(str(error)) from error
    if result.returncode:
        raise RuntimeError(f"claude exited {result.returncode}: {result.stderr.strip()[:200]}")
    return result.stdout


def run(root, top, tree, claude):
    rows = []

    def log(**row):
        rows.append({"at": state.now_iso(), "tree": tree, **row})

    try:
        base = state.diff_base(top)
        diff = state.diff(top)[:DIFF_CHARS]
        try:
            spec = "\n\n".join(text for _, text in all_segments(top))[:SPEC_CHARS]
        except StoreError:
            spec = ""
        earlier = [i["text"] for i in open_items.shadow_items(root)][-EARLIER:]
        found = ask(claude, FIND.format(earlier="\n".join(f"- {t}" for t in earlier) or "(none)",
                                        spec=spec, base=base, diff=diff), top)
        candidates = [line.strip().removeprefix("- ").strip() for line in found.splitlines()
                      if line.strip()][:MAX_CANDIDATES]
        for text in candidates:
            log(**{"pass": "find", "text": text})
        if not candidates:
            log(text="")
        else:
            claims = "\n".join(f"{n}. {text}" for n, text in enumerate(candidates, 1))
            # Only these tools exist for the check, and no MCP server: it reads, it never writes.
            answers = ask(claude, CHECK.format(claims=claims, base=base, diff=diff), top,
                          "--tools", "Read,Grep,Glob", "--strict-mcp-config")
            confirmed, rejected = {}, set()
            for line in filter(str.strip, answers.splitlines()):
                log(**{"pass": "check", "text": line.strip()})
                match = ANSWER.match(line)
                if match:
                    n = int(match.group(2))
                    if match.group(1) == "REJECTED":
                        rejected.add(n)
                    elif 1 <= n <= len(candidates):
                        confirmed[n] = match.group(3).strip()
            confirmed = {n: scenario for n, scenario in confirmed.items() if n not in rejected}
            branch = state.branch(top)
            for n, scenario in sorted(confirmed.items()):
                open_items.capture(root, "finding", candidates[n - 1], severity="decide", probe="shadow",
                                   scenario=scenario, branch=branch, tree=tree)
    except (ValueError, RuntimeError, OSError) as error:
        log(skipped=str(error))
    finally:
        state.append_jsonl(path(root), rows)


def score(root):
    items = open_items.shadow_items(root)
    fixed = sum(1 for i in items if i["how"] == "fix")
    dismissed = sum(1 for i in items if i["how"] == "dismiss")
    print(f"fixed {fixed}\ndismissed {dismissed}\nopen {sum(1 for i in items if not i['how'])}")


def main():
    if len(sys.argv) == 6 and sys.argv[1] == "run":
        run(pathlib.Path(sys.argv[2]), pathlib.Path(sys.argv[3]), sys.argv[4], sys.argv[5])
        return 0
    if len(sys.argv) == 2 and sys.argv[1] == "score":
        score(state.repo_root(pathlib.Path.cwd()))
        return 0
    print(__doc__, file=sys.stderr)
    return 2


if __name__ == "__main__":
    sys.exit(main())
