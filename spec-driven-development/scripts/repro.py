#!/usr/bin/env python3
"""Debugging loops: a command that goes red on the bug, kept and counted by script.

    repro.py record "<command>"    register the loop; refused unless it fails now
    repro.py run <id>              rerun it; three red runs on new trees means stop
    repro.py bisect <id> <good>    find the commit that broke it, from a green ref
    repro.py list                  open loops

A red run counts as a failed fix only on a tree not tried before, so rerunning
without a change costs nothing. Loops live in .sdd/repro/<id>.json.
"""

import argparse
import hashlib
import json
import pathlib
import subprocess
import sys

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))

import state  # noqa: E402

TAIL_LINES = 20
STRIKES = 3
TIMEOUT = 600


def run_loop(command, cwd):
    """(passed, output tail). The command is one the agent wrote to reproduce its bug."""
    try:
        result = subprocess.run(command, shell=True, cwd=cwd, capture_output=True,
                                text=True, timeout=TIMEOUT, check=False)
    except subprocess.TimeoutExpired:
        return False, f"did not finish in {TIMEOUT}s"
    tail = "\n".join((result.stdout + result.stderr).rstrip().splitlines()[-TAIL_LINES:])
    return result.returncode == 0, tail


def loop_path(root, loop_id):
    return state.folder(root) / "repro" / f"{loop_id}.json"


def load(root, loop_id):
    path = loop_path(root, loop_id)
    if not path.is_file():
        raise SystemExit(f"no loop {loop_id}; list them with repro.py list")
    return json.loads(path.read_text(encoding="utf-8"))


def save(root, loop):
    path = loop_path(root, loop["id"])
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(loop, indent=2, ensure_ascii=False), encoding="utf-8")


def record(top, root, command):
    passed, tail = run_loop(command, top)
    if passed:
        print("refused: the loop is not red. It must fail on this bug before it can prove a fix.")
        return 1
    state.ensure(root)
    loop_id = "r-" + hashlib.sha1(f"{command}\0{state.now_iso()}".encode("utf-8")).hexdigest()[:8]
    tree = state.tree_hash(top)
    save(root, {"id": loop_id, "command": command, "at": state.now_iso(), "commit": state.head(top),
                "tree": tree, "tail": tail, "tried": [], "closed": False})
    print(f"registered {loop_id} (red, as it should be)\n{tail}")
    return 0


def run(top, root, loop_id):
    loop = load(root, loop_id)
    passed, tail = run_loop(loop["command"], top)
    tree = state.tree_hash(top)
    if passed:
        loop.update(closed=True, closed_at=state.now_iso())
        save(root, loop)
        print(f"{loop_id} is green: the loop is closed. Keep its regression test, then remove "
              "every [DEBUG-...] line.")
        return 0
    if tree != loop["tree"] and tree not in loop["tried"]:
        loop["tried"].append(tree)
    save(root, loop)
    strikes = len(loop["tried"])
    print(f"{loop_id} is still red ({strikes} failed fix(es)).\n{tail}")
    if strikes >= STRIKES:
        print(f"STOP: {strikes} fixes have failed. Question the design rather than trying another fix, "
              "and ask the user before going on.")
    return 1


def bisect(top, root, loop_id, good):
    loop = load(root, loop_id)
    start = (state.git(["rev-parse", "--abbrev-ref", "HEAD"], top) or "HEAD").strip()
    dirty = state.git(["status", "--porcelain", "--", ".", ":!.sdd"], top)
    if dirty is None or dirty.strip():
        print("refused: commit, stash or remove these first; bisect checks out other commits "
              f"and they would change what it sees:\n{(dirty or '').rstrip()}")
        return 1
    if state.git(["checkout", "-q", good], top) is None:
        print(f"refused: cannot check out {good}")
        return 1
    try:
        passed, _ = run_loop(loop["command"], top)
    finally:
        state.git(["checkout", "-q", start], top)
    if not passed:
        print(f"refused: the loop is red at {good} too, so bisect has no good side.")
        return 1
    try:
        if state.git(["bisect", "start", "HEAD", good], top) is None:
            print("git bisect start failed")
            return 1
        result = subprocess.run(["git", "bisect", "run", "sh", "-c", loop["command"]], cwd=top,
                                capture_output=True, text=True, timeout=TIMEOUT * 20, check=False)
        # The verdict is read from the ref, not from bisect's wording, which varies by git version.
        bad = state.git(["rev-parse", "--verify", "--quiet", "refs/bisect/bad"], top) if result.returncode == 0 else None
    finally:
        state.git(["bisect", "reset"], top)
    if not bad:
        print(f"bisect found no first bad commit:\n{(result.stdout + result.stderr)[-2000:]}")
        return 1
    subject = (state.git(["log", "-1", "--format=%s", bad.strip()], top) or "").strip()
    print(f"first bad commit: {bad.strip()} {subject}")
    return 0


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = parser.add_subparsers(dest="command", required=True)
    sub.add_parser("record").add_argument("loop")
    sub.add_parser("run").add_argument("id")
    b = sub.add_parser("bisect")
    b.add_argument("id")
    b.add_argument("good")
    sub.add_parser("list")
    args = parser.parse_args()
    cwd = pathlib.Path.cwd()
    top = pathlib.Path((state.git(["rev-parse", "--show-toplevel"], cwd) or str(cwd)).strip())
    root = state.repo_root(cwd)
    if args.command == "record":
        return record(top, root, args.loop)
    if args.command == "run":
        return run(top, root, args.id)
    if args.command == "bisect":
        return bisect(top, root, args.id, args.good)
    for path in sorted((state.folder(root) / "repro").glob("*.json")):
        loop = json.loads(path.read_text(encoding="utf-8"))
        if not loop["closed"]:
            print(f"{loop['id']}  {loop['at'][:10]}  {len(loop['tried'])} failed fix(es)  {loop['command']}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
