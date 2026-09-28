#!/usr/bin/env python3
"""Say, from probes alone, whether the work in this tree is done.

    verify.py [--json]

Probes, in order: the configured test command, spec_check, approved-test
drift, leftover [DEBUG-xxxx] tags, and draft migrations awaiting release. Each problem is a blocker, decide or note
and is written to the open-items queue; any blocker exits 1. A result is cached
by the hash of the working tree, so a second call on the same tree, from any
agent, reruns nothing. With no blocker, an observe-only shadow is started.
"""

import argparse
import json
import os
import pathlib
import re
import shutil
import subprocess
import sys

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))

import open_items  # noqa: E402
import spec_check  # noqa: E402
import state  # noqa: E402
from migrations import drafts  # noqa: E402
from specs import all_segments  # noqa: E402

PROBES = ("test", "spec", "approved-tests", "debug-tags", "migrations")
TAIL_LINES = 30
DEBUG_TAG = r"\[DEBUG-[0-9a-f]{4,}\]"
APPROVED = re.compile(r"^Status: cases approved \S+ at ([0-9a-f]{7,40})\b", re.MULTILINE)
SEVERITY = {"collision": "decide", "uncovered": "decide", "no_unexpected": "note", "untested": "note"}


def worktree_top(cwd):
    top = state.git(["rev-parse", "--show-toplevel"], cwd)
    if not top:
        raise ValueError(f"{cwd} is not inside a git repository")
    return pathlib.Path(top.strip())


def problem(probe, severity, detail, evidence=""):
    return {"probe": probe, "severity": severity, "detail": detail, "evidence": evidence}


def probe_test(top, config):
    command = config.get("test")
    if not isinstance(command, str) or not command.strip():
        return [problem("test", "decide", "no test command: set \"test\" in .sdd/config.json")]
    timeout = config.get("test_timeout", 600)
    # The command is the repository's own, trusted as its Makefile is, and may
    # chain steps with && — hence a shell.
    try:
        result = subprocess.run(command, shell=True, cwd=top, capture_output=True,
                                text=True, timeout=timeout, check=False)
    except subprocess.TimeoutExpired:
        return [problem("test", "blocker", f"`{command}` did not finish in {timeout}s")]
    if result.returncode == 0:
        return []
    tail = "\n".join((result.stdout + result.stderr).rstrip().splitlines()[-TAIL_LINES:])
    return [problem("test", "blocker", f"`{command}` exited {result.returncode}\n{tail}", tail)]


def story_of(name):
    return str(pathlib.PurePosixPath(name).parent) if "/" in name else name


def approvals(segments):
    """{story: approval commit} for every story whose cases are approved."""
    found = {}
    for name, text in segments:
        match = APPROVED.search(text)
        if match:
            found[story_of(name)] = match.group(1)
    return found


def probe_spec(top, segments, approved):
    cases, decisions, problems = spec_check.parse(segments)
    problems += spec_check.check_decisions(decisions, cases)
    problems += spec_check.check_tests(cases, top)
    out = []
    for kind, detail in problems:
        severity = SEVERITY.get(kind, "blocker")
        subject = detail.split()[0]
        home = {"untested": cases.get(subject, {}).get("in", ""),
                "uncovered": decisions.get(subject, {}).get("in", ""),
                "no_unexpected": subject}.get(kind)
        # Once the user approved a story's cases, a gap in them stops the work.
        if home is not None and story_of(home) in approved:
            severity = "blocker"
        out.append(problem("spec", severity, f"{kind}: {detail}"))
    return out, cases


def case_regex(cid):
    prefix, number = cid.split("-C")
    return rf"(?<![A-Za-z0-9]){re.escape(prefix)}[-_]C{number}(?![0-9])"


def chunk(text, cid):
    """The test that names cid: its first naming line through the end of its body."""
    lines = text.splitlines()
    pattern = re.compile(case_regex(cid))
    for start, line in enumerate(lines):
        if pattern.search(line):
            break
    else:
        return None
    indent = len(line) - len(line.lstrip())
    end = start + 1
    while end < len(lines):
        current = lines[end]
        if current.strip() and len(current) - len(current.lstrip()) <= indent:
            if current.lstrip().startswith(("}", ")", "]", "end")):
                end += 1
            break
        end += 1
    return "\n".join(lines[start:end]).rstrip()


def probe_approved_tests(top, cases, approved):
    out = []
    for cid, case in sorted(cases.items()):
        since = approved.get(story_of(case["in"]))
        if not since:
            continue
        regex = case_regex(cid).replace("(?<![A-Za-z0-9])", "").replace("(?![0-9])", "")
        first = state.git(["log", "--reverse", "--format=%H", "-G", regex, f"{since}..HEAD"], top)
        if first is None:
            out.append(problem("approved-tests", "decide", f"approval commit {since} is not in this history"))
            continue
        first = first.split()
        if not first:
            continue
        listed = state.git(["grep", "-l", "-P", case_regex(cid), first[0]], top) or ""
        for entry in listed.splitlines():
            rel = entry.split(":", 1)[1]
            if not spec_check.is_test_file(rel):
                continue
            landed = chunk(state.git(["show", f"{first[0]}:{rel}"], top) or "", cid)
            path = top / rel
            now = chunk(path.read_text(encoding="utf-8", errors="replace"), cid) if path.is_file() else None
            if landed and now != landed:
                change = "was removed" if now is None else "changed"
                out.append(problem("approved-tests", "blocker",
                                   f"the test for approved case {cid} in {rel} {change} after it landed in "
                                   f"{first[0][:7]}; an approved case changes only with the user"))
    return out


def probe_debug_tags(top):
    result = subprocess.run(["git", "grep", "-n", "-I", "--untracked", "-E", DEBUG_TAG, "--", ".", ":!.sdd"],
                            cwd=top, capture_output=True, text=True, timeout=60, check=False)
    return [problem("debug-tags", "blocker", f"leftover debug log at {':'.join(line.split(':', 2)[:2])}")
            for line in result.stdout.splitlines()]


def probe_migrations(top, config):
    return [problem("migrations", "note", f"draft migration waiting for release: {rel}")
            for rel in drafts(top, config.get("migrations") or [])]


def record(root, problems):
    """Write this run's problems to the queue and resolve verify findings that are gone."""
    current = set()
    for p in problems:
        iid = open_items.capture(root, "finding", p["detail"].splitlines()[0], severity=p["severity"],
                                 probe=p["probe"], evidence=p["evidence"])
        if iid:
            current.add(iid)
    for item in open_items.open_items(root):
        if item["kind"] == "finding" and item.get("probe") in PROBES and item["id"] not in current:
            open_items.resolve(root, item["id"], "gone", note="its probe no longer reproduces it")


def claude_command():
    return os.environ.get("SDD_CLAUDE") or shutil.which("claude")


def start_shadow(root, top, tree):
    claude = claude_command()
    if not claude:
        state.append_jsonl(state.folder(root) / "shadow.jsonl",
                           [{"at": state.now_iso(), "tree": tree, "skipped": "claude is not on PATH"}])
        return "skipped"
    script = pathlib.Path(__file__).resolve().parent / "shadow.py"
    subprocess.Popen([sys.executable, str(script), "run", str(root), str(top), tree, claude],
                     stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                     start_new_session=True)
    return "started"


def verify(cwd):
    top = worktree_top(cwd)
    root = state.repo_root(cwd)
    state.ensure(root)
    tree = state.tree_hash(top)
    cache = state.folder(root) / "verify" / f"{tree}.json"
    if cache.is_file():
        return {**json.loads(cache.read_text(encoding="utf-8")), "cached": True}

    config = state.load_config(top)
    segments = all_segments(top)
    approved = approvals(segments)
    problems = probe_test(top, config)
    spec_problems, cases = probe_spec(top, segments, approved)
    problems += spec_problems
    problems += probe_approved_tests(top, cases, approved)
    problems += probe_debug_tags(top)
    problems += probe_migrations(top, config)
    record(root, problems)

    blocked = any(p["severity"] == "blocker" for p in problems)
    result = {"tree": tree, "at": state.now_iso(), "exit": 1 if blocked else 0, "problems": problems,
              "shadow": "not started: a blocker stands" if blocked else start_shadow(root, top, tree)}
    cache.parent.mkdir(exist_ok=True)
    cache.write_text(json.dumps(result, ensure_ascii=False, indent=2), encoding="utf-8")
    return {**result, "cached": False}


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--json", action="store_true")
    args = parser.parse_args()
    result = verify(pathlib.Path.cwd())
    if args.json:
        print(json.dumps(result, ensure_ascii=False, indent=2))
        return result["exit"]
    origin = " (cached: this tree was verified before)" if result["cached"] else ""
    print(f"verify {result['tree'][:12]}{origin}: {'blocked' if result['exit'] else 'clear'}")
    for p in result["problems"]:
        print(f"  {p['severity']:<7} {p['probe']:<14} {p['detail']}")
    return result["exit"]


if __name__ == "__main__":
    sys.exit(main())
