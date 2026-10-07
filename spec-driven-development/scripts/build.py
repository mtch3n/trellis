#!/usr/bin/env python3
"""Start a story's build on this branch.

    build.py start <KEY>

Runs only in a linked worktree on a branch other than the default. The start is
one line in .sdd/builds.jsonl, which every worktree shares: red.py records a
case only where its story was started, and verify holds a story to account
there.
"""

import pathlib
import sys

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))

import spec_check  # noqa: E402
import state  # noqa: E402
from specs import StoreError, all_segments  # noqa: E402


def path(root):
    return state.folder(root) / "builds.jsonl"


def started(root):
    """({story key: the branches it is built on}, how many lines could not be read)."""
    rows, unread = state.read_jsonl(path(root))
    out = {}
    for row in rows:
        if isinstance(row.get("story"), str) and isinstance(row.get("branch"), str):
            out.setdefault(row["story"], set()).add(row["branch"])
        else:
            unread += 1
    return out, unread


def linked(top):
    """True in a linked worktree, False in the main checkout."""
    own, common = (state.git(["rev-parse", "--path-format=absolute", flag], top)
                   for flag in ("--git-dir", "--git-common-dir"))
    return bool(own and common) and pathlib.Path(own.strip()) != pathlib.Path(common.strip())


def start(cwd, key):
    top = state.git(["rev-parse", "--show-toplevel"], cwd)
    if not top:
        print(f"refused: {cwd} is not inside a git repository")
        return 1
    top = pathlib.Path(top.strip())
    branch = state.branch(top)
    where = ("the main checkout" if not linked(top) else "a detached HEAD" if not branch
             else "the default branch" if branch == state.default_branch(top) else "")
    if where:
        print(f"refused: {key} cannot be built in {where}. Enter a worktree on a branch of its own "
              "(EnterWorktree, or `git worktree add <path> -b <branch>`) and run this there.")
        return 1
    try:
        segments = all_segments(top)
    except StoreError as error:
        print(f"refused: the spec store could not be read: {error}")
        return 1
    cases, _, _ = spec_check.parse(segments)
    stories = {spec_check.story_of(case["in"]) for cid, case in cases.items() if cid.split("-C")[0] == key}
    if not stories:
        print(f"refused: no story uses the key {key}")
        return 1
    if not stories & set(spec_check.approvals(segments)):
        print(f"refused: {key}'s cases are not approved")
        return 1
    root = state.repo_root(cwd)
    with state.Lock(root):
        if branch not in started(root)[0].get(key, ()):
            state.append_jsonl(path(root), [{"story": key, "branch": branch, "at": state.now_iso()}])
    print(f"started {key} on {branch}")
    return 0


def main():
    if len(sys.argv) != 3 or sys.argv[1] != "start":
        print(__doc__, file=sys.stderr)
        return 2
    return start(pathlib.Path.cwd(), sys.argv[2])


if __name__ == "__main__":
    sys.exit(main())
