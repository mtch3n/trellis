#!/usr/bin/env python3
"""The observe-only shadow: a separate model run whose lines only the user sees.

    shadow.py run <root> <top> <tree> <claude>   (started by verify.py, detached)
    shadow.py mark <n> useful|not                 record whether line n helped

It never writes to the queue, never blocks, and never reaches the working
agent's context. Its lines wait in .sdd/shadow.jsonl until a hook shows them.
"""

import os
import pathlib
import subprocess
import sys

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))

import state  # noqa: E402
from migrations import default_branch  # noqa: E402
from specs import all_segments  # noqa: E402

MAX_LINES = 20
DIFF_CHARS = 60_000
SPEC_CHARS = 30_000
PROMPT = """You are observing a change. You are not reviewing it and nobody will act on
your words automatically. List at most five possible problems that a script
could not catch: a contradiction with a spec decision, an unexpected case the
change ignores, a risky schema or data change. One line each, in the form
`<problem> — evidence: <exact quote from the diff or spec>`. A line without an
exact quote is worthless. Output nothing else; if you see nothing, output nothing.

## Spec
{spec}

## Diff against {base}
{diff}
"""


def path(root):
    return state.folder(root) / "shadow.jsonl"


def run(root, top, tree, claude):
    base = default_branch(top) or "HEAD"
    diff = state.git(["diff", base], top, timeout=60) or ""
    spec = "\n\n".join(text for _, text in all_segments(top))
    prompt = PROMPT.format(spec=spec[:SPEC_CHARS], base=base, diff=diff[:DIFF_CHARS])
    rows = []
    try:
        result = subprocess.run([claude, "-p", prompt, "--model", "sonnet"], cwd=top, capture_output=True,
                                text=True, timeout=600, check=False, env={**os.environ, "SDD_SHADOW": "1"})
        if result.returncode:
            rows.append({"skipped": f"claude exited {result.returncode}: {result.stderr.strip()[:200]}"})
        else:
            rows += [{"text": line.strip()} for line in result.stdout.splitlines() if line.strip()][:MAX_LINES]
    except (OSError, subprocess.TimeoutExpired) as error:
        rows.append({"skipped": str(error)})
    if not rows:
        rows.append({"text": ""})
    at = state.now_iso()
    state.append_jsonl(path(root), [{"at": at, "tree": tree, **row} for row in rows])


def unshown(root):
    """Shadow lines the user has not seen yet; marks them seen."""
    rows, _ = state.read_jsonl(path(root))
    marker = state.folder(root) / "shadow.shown"
    seen = int(marker.read_text(encoding="utf-8") or 0) if marker.is_file() else 0
    fresh = [r["text"] for r in rows[seen:] if r.get("text")]
    if len(rows) > seen:
        marker.write_text(str(len(rows)), encoding="utf-8")
    return fresh


def mark(root, n, verdict):
    rows, _ = state.read_jsonl(path(root))
    lines = [r for r in rows if r.get("text")]
    if not 1 <= n <= len(lines):
        raise SystemExit(f"no shadow line {n}; there are {len(lines)}")
    state.append_jsonl(state.folder(root) / "shadow-marks.jsonl",
                       [{"at": state.now_iso(), "line": lines[n - 1]["text"], "useful": verdict == "useful"}])


def main():
    if len(sys.argv) == 6 and sys.argv[1] == "run":
        run(pathlib.Path(sys.argv[2]), pathlib.Path(sys.argv[3]), sys.argv[4], sys.argv[5])
        return 0
    if len(sys.argv) == 4 and sys.argv[1] == "mark" and sys.argv[3] in ("useful", "not"):
        mark(state.repo_root(pathlib.Path.cwd()), int(sys.argv[2]), sys.argv[3])
        return 0
    print(__doc__, file=sys.stderr)
    return 2


if __name__ == "__main__":
    sys.exit(main())
