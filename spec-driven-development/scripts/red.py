#!/usr/bin/env python3
"""Record that a case's test failed before its code existed.

    red.py <case> "<command>"

The command must name the case ID (PIN_C2 or PIN-C2), so it runs that test, and
must fail now. The record lands in .sdd/red/<case>.json; verify requires one
for every approved case whose test landed after `red_since` in .sdd/config.json.
"""

import json
import pathlib
import re
import subprocess
import sys

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))

import state  # noqa: E402

TAIL_LINES = 20
TIMEOUT = 600
CASE = re.compile(r"^[A-Z][A-Z0-9]*-C\d+$")


def names_case(command, case):
    prefix, number = case.split("-C")
    return re.search(rf"(?<![A-Za-z0-9]){re.escape(prefix)}[-_]C{number}(?![0-9])", command) is not None


def record_path(root, case):
    return state.folder(root) / "red" / f"{case}.json"


def record(cwd, case, command):
    if not CASE.match(case):
        print(f"refused: {case} is not a case ID like PIN-C2")
        return 1
    if not names_case(command, case):
        print(f"refused: the command must name {case} (as {case} or {case.replace('-', '_')}), "
              "so that it runs that case's test")
        return 1
    top = pathlib.Path((state.git(["rev-parse", "--show-toplevel"], cwd) or str(cwd)).strip())
    try:
        # The command is the agent's own test invocation, run as it would type it.
        result = subprocess.run(command, shell=True, cwd=top, capture_output=True,
                                text=True, timeout=TIMEOUT, check=False)
    except subprocess.TimeoutExpired:
        print(f"refused: the command did not finish in {TIMEOUT}s")
        return 1
    tail = "\n".join((result.stdout + result.stderr).rstrip().splitlines()[-TAIL_LINES:])
    if result.returncode == 0:
        print(f"refused: {case}'s test is not red. Write the test before the code and watch it fail.\n{tail}")
        return 1
    root = state.repo_root(cwd)
    state.ensure(root)
    path = record_path(root, case)
    path.parent.mkdir(exist_ok=True)
    path.write_text(json.dumps({"case": case, "command": command, "at": state.now_iso(),
                                "tree": state.tree_hash(top), "tail": tail}, indent=2, ensure_ascii=False),
                    encoding="utf-8")
    print(f"recorded {case} red.\n{tail}")
    return 0


def main():
    if len(sys.argv) != 3:
        print(__doc__, file=sys.stderr)
        return 2
    return record(pathlib.Path.cwd(), sys.argv[1], sys.argv[2])


if __name__ == "__main__":
    sys.exit(main())
