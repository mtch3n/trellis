#!/usr/bin/env python3
"""Merge draft migrations into numbered ones, once, at release.

    release.py plan                 what apply would write, changing nothing
    release.py apply [--name NAME]  write it, run the checks, delete the drafts

Drafts are taken in the order they were first committed (uncommitted ones last).
In each migrations directory, the SQL drafts become one file with the next
number; every other draft takes a number of its own. The configured
`migration_checks` then run in order; if one fails, the numbered files are
removed and the drafts stay. Release mode (.sdd/release.json) is on only while
apply runs.
"""

import argparse
import os
import pathlib
import re
import subprocess
import sys

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))

import state  # noqa: E402
from migrations import check_migration, draft_of, drafts, migration_unit, repo_files  # noqa: E402


DATA_CHANGE = re.compile(r"^\s*(?:UPDATE|INSERT|DELETE|MERGE)\b", re.IGNORECASE | re.MULTILINE)


def changes_data(text):
    """True when SQL outside comments starts an UPDATE, INSERT, DELETE or MERGE."""
    code = re.sub(r"--[^\n]*|/\*.*?\*/", "", text, flags=re.DOTALL)
    return bool(DATA_CHANGE.search(code))


def first_commit_order(top, rel):
    """How deep in history the commit that added rel sits; uncommitted is last."""
    added = (state.git(["log", "--diff-filter=A", "--format=%H", "--", rel], top) or "").split()
    if not added:
        return float("inf")
    return int(state.git(["rev-list", "--count", added[-1]], top) or 0)


def numbering(top, home, patterns):
    """(next version, digit width) for the sequence the drafts in home join."""
    namespace = "configured" if patterns else home
    best, width = 0, 4
    for rel in repo_files(top):
        hit = migration_unit(rel, patterns)
        digits = re.search(r"\d+", hit[1]) if hit and hit[0] == namespace else None
        if digits and int(digits.group()) >= best:
            best, width = int(digits.group()), len(digits.group())
    return best + 1, width


def plan(top, patterns, name):
    """[(path to write, content, [drafts it comes from])] in writing order."""
    found = drafts(top, patterns)
    homes = {}
    for rel in sorted(found, key=lambda r: (first_commit_order(top, r), r)):
        homes.setdefault(draft_of(rel, patterns)[0], []).append(rel)
    writes = []
    for home, group in homes.items():
        number, width = numbering(top, home, patterns)
        sql = [r for r in group if r.endswith(".sql")]
        if sql:
            text = "\n".join((top / r).read_text(encoding="utf-8").rstrip() + "\n" for r in sql)
            writes.append((f"{home}/{number:0{width}d}_{name}.sql", text, sql))
            number += 1
        for rel in (r for r in group if not r.endswith(".sql")):
            draft = pathlib.PurePosixPath(rel)
            writes.append((f"{home}/{number:0{width}d}_{draft.stem}{draft.suffix}",
                           (top / rel).read_text(encoding="utf-8"), [rel]))
            number += 1
    return writes


def run_checks(top, commands, written, timeout):
    env = {**os.environ, "SDD_RELEASE_FILES": " ".join(written)}
    for command in commands:
        # The commands are the repository's own, like its Makefile; a shell lets them chain.
        try:
            result = subprocess.run(command, shell=True, cwd=top, env=env, capture_output=True,
                                    text=True, timeout=timeout, check=False)
        except subprocess.TimeoutExpired:
            return f"`{command}` did not finish in {timeout}s"
        except OSError as error:
            return f"`{command}` could not run: {error}"
        if result.returncode:
            return f"`{command}` exited {result.returncode}\n{(result.stdout + result.stderr).rstrip()}"
    return None


def apply(top, root, patterns, name, commands, timeout=1800):
    writes = plan(top, patterns, name)
    if not writes:
        print("nothing to merge: no draft migrations")
        return 0
    clashes = [problem for rel, _, _ in writes for problem in check_migration(top, rel, patterns, release=True)]
    if clashes:
        print("release refused; nothing was changed:\n- " + "\n- ".join(clashes))
        return 1
    taken = [rel for rel, _, _ in writes if (top / rel).exists()]
    if taken:
        print(f"{', '.join(taken)} already exist; nothing was changed")
        return 1
    marker = state.ensure(root) / "release.json"
    marker.write_text('{"at": "%s"}\n' % state.now_iso(), encoding="utf-8")
    written = []
    try:
        for rel, text, _ in writes:
            (top / rel).write_text(text, encoding="utf-8")
            written.append(rel)
        failure = run_checks(top, commands, written, timeout)
        if failure:
            for rel in written:
                (top / rel).unlink()
            print(f"release stopped; the drafts are unchanged.\n{failure}")
            return 1
        for _, _, sources in writes:
            for rel in sources:
                (top / rel).unlink()
                folder = (top / rel).parent
                if not any(folder.iterdir()):
                    folder.rmdir()
        for rel, _, sources in writes:
            print(f"wrote {rel} from {', '.join(sources)}")
        return 0
    finally:
        marker.unlink(missing_ok=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("command", choices=("plan", "apply"))
    parser.add_argument("--name", default="release", help="name of the merged SQL migration (default: release)")
    args = parser.parse_args()
    cwd = pathlib.Path.cwd()
    top = pathlib.Path(state.git(["rev-parse", "--show-toplevel"], cwd).strip())
    config = state.load_config(top)
    patterns = config.get("migrations") or []
    if args.command == "plan":
        writes = plan(top, patterns, args.name)
        for rel, _, sources in writes:
            print(f"{rel} <- {', '.join(sources)}")
            for source in sources:
                if source.endswith(".sql") and changes_data((top / source).read_text(encoding="utf-8")):
                    print(f"  {source} changes data: ask whether it stays its own numbered migration")
        if not writes:
            print("nothing to merge: no draft migrations")
        return 0
    return apply(top, state.repo_root(cwd), patterns, args.name, config.get("migration_checks") or [],
                 config.get("migration_check_timeout", 1800))


if __name__ == "__main__":
    sys.exit(main())
