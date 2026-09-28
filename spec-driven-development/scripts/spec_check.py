#!/usr/bin/env python3
"""Check a story's spec against itself and against the tests. Facts only.

    spec_check.py specs/card-move/            # every .md under a directory
    spec_check.py specs/card-move.md          # one file
    spec_check.py --trellis specs/card-move   # a Trellis vault directory

Reports, exit 1 if any:
  untested     a case whose ID appears in no test file
  unknown      a test that names a case ID the spec does not have
  duplicate    one case or decision ID defined twice
  collision    two active decisions govern the same thing and neither supersedes the other
  bad_ref      a decision supersedes, or a case covers, an ID that does not exist
  uncovered    an active decision no case covers
  bad_kind     a case whose kind is not expected or unexpected
  no_unexpected  a segment with cases but no unexpected-behaviour case

Spec format (see the skill):
  | PIN-C3 | PIN-D1, PIN-D2 | unexpected | Given ... when ... then ... |
  - **PIN-D2** Refs are stored. Why: ... Governs: cards.ref, api:/cards. Supersedes: PIN-D1.
Test files name a case by its ID, with - or _ as the separator.
"""

import argparse
import fnmatch
import json
import pathlib
import re
import subprocess
import sys

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))

from specs import trellis_segments  # noqa: E402

CASE_ROW = re.compile(r"^\|\s*([A-Z][A-Z0-9]*-C\d+)\s*\|\s*([^|]*?)\s*\|\s*([^|]*?)\s*\|")
DECISION = re.compile(r"^\s*[-*]\s*\**([A-Z][A-Z0-9]*-D\d+)\**\b(.*)$")
GOVERNS = re.compile(r"(?i)governs:\s*([^\n]+?)(?:\.\s|\.$|\s+supersedes:|$)")
SUPERSEDES = re.compile(r"(?i)supersedes:\s*([A-Z0-9_,\s-]+)")
CASE_IN_CODE = re.compile(r"(?<![A-Z0-9])([A-Z][A-Z0-9]*)[-_]C(\d+)(?!\d)")
TEST_FILES = ("*_test.go", "test_*.py", "*_test.py", "*.test.*", "*.spec.*",
              "tests/*", "test/*", "__tests__/*", "e2e/*")
HEADING = re.compile(r"^#+\s*(.*?)\s*$")
APPROVED = re.compile(r"^Status: cases approved \S+ at ([0-9a-f]{7,40})\b", re.MULTILINE)
KINDS = {"expected", "unexpected"}
DECISION_ID = re.compile(r"[A-Z][A-Z0-9]*-D\d+")


def segments_from_paths(paths):
    out = []
    for raw in paths:
        path = pathlib.Path(raw)
        files = sorted(path.rglob("*.md")) if path.is_dir() else [path]
        for f in files:
            out.append((str(f), f.read_text(encoding="utf-8")))
    return out


def segments_from_trellis(directory, cwd):
    found = trellis_segments(directory, cwd)
    if found is None:
        raise SystemExit(f"trellis vault ls {directory} failed: is this a Trellis project?")
    return found


def parse(segments):
    cases, decisions, problems = {}, {}, []
    per_segment = {}
    for name, text in segments:
        kinds = []
        lines = text.splitlines()
        section = ""
        for i, line in enumerate(lines):
            heading = HEADING.match(line)
            if heading:
                section = heading.group(1).lower()
                continue
            row = CASE_ROW.match(line)
            if row:
                cid, kind = row.group(1), row.group(3).strip().lower()
                if cid in cases:
                    problems.append(("duplicate", f"case {cid} is defined in {cases[cid]['in']} and {name}"))
                cases[cid] = {"in": name, "kind": kind, "covers": set(DECISION_ID.findall(row.group(2)))}
                kinds.append(kind)
                if kind not in KINDS:
                    problems.append(("bad_kind", f"{cid} has kind '{kind}'; use expected or unexpected"))
                continue
            # Open holds questions, which may name a decision without defining it.
            dec = DECISION.match(line) if section != "open" else None
            if dec:
                did = dec.group(1)
                # A decision's bullet continues on indented lines.
                body = dec.group(2)
                for more in lines[i + 1:]:
                    if more.startswith(("  ", "\t")) and not DECISION.match(more):
                        body += " " + more.strip()
                    else:
                        break
                if did in decisions:
                    problems.append(("duplicate", f"decision {did} is defined in {decisions[did]['in']} and {name}"))
                gov = GOVERNS.search(body)
                sup = SUPERSEDES.search(body)
                decisions[did] = {
                    "in": name,
                    "governs": {g.strip().lower() for g in gov.group(1).split(",") if g.strip()} if gov else set(),
                    "supersedes": {s.strip() for s in re.split(r"[,\s]+", sup.group(1)) if s.strip()} if sup else set(),
                }
        per_segment[name] = kinds
    for name, kinds in per_segment.items():
        if kinds and "unexpected" not in kinds:
            problems.append(("no_unexpected", f"{name} has {len(kinds)} case(s) and no unexpected-behaviour case"))
    return cases, decisions, problems


def check_decisions(decisions, cases):
    problems = []
    covered = set()
    for cid, case in sorted(cases.items()):
        for did in sorted(case["covers"]):
            if did not in decisions:
                problems.append(("bad_ref", f"{cid} covers {did}, which does not exist"))
            covered.add(did)
    superseded = set()
    for did, d in decisions.items():
        for old in d["supersedes"]:
            if old not in decisions:
                problems.append(("bad_ref", f"{did} supersedes {old}, which does not exist"))
            superseded.add(old)
    active = sorted(did for did in decisions if did not in superseded)
    problems += [("uncovered", f"{did} is covered by no case") for did in active if did not in covered]
    for i, a in enumerate(active):
        for b in active[i + 1:]:
            shared = decisions[a]["governs"] & decisions[b]["governs"]
            if shared:
                problems.append(("collision",
                                 f"{a} and {b} both govern {', '.join(sorted(shared))}; "
                                 "one must supersede the other, or they must govern different things"))
    return problems


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


def is_test_file(rel):
    name = pathlib.PurePosixPath(rel).name
    return any(fnmatch.fnmatch(name, p) or fnmatch.fnmatch(rel, "*" + p) or fnmatch.fnmatch(rel, p)
               for p in TEST_FILES)


def test_files(root):
    listed = subprocess.run(["git", "ls-files", "--cached", "--others", "--exclude-standard"],
                            cwd=root, capture_output=True, text=True, timeout=20, check=False)
    for rel in listed.stdout.splitlines():
        if is_test_file(rel):
            yield root / rel


def check_tests(cases, root):
    named = {}
    for path in test_files(root):
        try:
            text = path.read_text(encoding="utf-8")
        except (OSError, UnicodeDecodeError):
            continue
        for prefix, number in CASE_IN_CODE.findall(text):
            named.setdefault(f"{prefix}-C{number}", str(path.relative_to(root)))
    prefixes = {cid.split("-C")[0] for cid in cases}
    problems = [("untested", f"{cid} has no test naming it") for cid in sorted(cases) if cid not in named]
    problems += [("unknown", f"{where} names {cid}, which the spec does not define")
                 for cid, where in sorted(named.items())
                 if cid not in cases and cid.split("-C")[0] in prefixes]
    return problems


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("paths", nargs="*", help="spec files or directories")
    parser.add_argument("--trellis", metavar="DIR", help="read the spec from this Trellis vault directory")
    parser.add_argument("--root", default=".", help="repository whose tests to scan (default: .)")
    parser.add_argument("--json", action="store_true")
    args = parser.parse_args()
    if not args.paths and not args.trellis:
        parser.error("give spec paths or --trellis DIR")

    root = pathlib.Path(args.root).resolve()
    segments = segments_from_trellis(args.trellis, root) if args.trellis else segments_from_paths(args.paths)
    cases, decisions, problems = parse(segments)
    problems += check_decisions(decisions, cases)
    problems += check_tests(cases, root)

    if args.json:
        print(json.dumps({"cases": len(cases), "decisions": len(decisions),
                          "problems": [{"kind": k, "detail": d} for k, d in problems]}, indent=2))
    else:
        print(f"{len(cases)} case(s), {len(decisions)} decision(s), {len(problems)} problem(s)")
        for kind, detail in problems:
            print(f"  {kind:<11} {detail}")
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main())
