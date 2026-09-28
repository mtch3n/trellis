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
# What test runners print when nothing ran: a failure then proves nothing.
# How runners mark a failed test on the line that names it: unittest "FAIL: test_x",
# pytest "FAILED path::test_x", go "--- FAIL: TestX", jest "✕ name".
FAILED = re.compile(r"(?i)\b(?:fail(?:ed|ure)?|error)\b|✕|✗|×")
NO_TEST = re.compile(r"(?i)Ran 0 tests|no tests ran|no tests to run|collected 0 items|\[build failed\]|\[setup failed\]")


def names_case(text, case):
    prefix, number = case.split("-C")
    return re.search(rf"(?<![A-Za-z0-9]){re.escape(prefix)}[-_]C{number}(?![0-9])", text) is not None


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
    output = result.stdout + result.stderr
    tail = "\n".join(output.rstrip().splitlines()[-TAIL_LINES:])
    if result.returncode == 0:
        print(f"refused: {case}'s test is not red. Write the test before the code and watch it fail.\n{tail}")
        return 1
    # A runner's verdict is in its closing lines; a failure message quoting
    # these words further up is not the runner saying nothing ran.
    closing = "\n".join([line for line in output.splitlines() if line.strip()][-3:])
    if NO_TEST.search(closing):
        print(f"refused: no test ran, so the failure proves nothing about {case}.\n{tail}")
        return 1
    if not names_case(output, case):
        print(f"refused: the output does not name {case}, so the runner did not run its test.\n{tail}")
        return 1
    if not any(names_case(line, case) and FAILED.search(line) for line in output.splitlines()):
        print(f"refused: {case} did not fail; the output names it only beside a pass or without a failure.\n{tail}")
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
