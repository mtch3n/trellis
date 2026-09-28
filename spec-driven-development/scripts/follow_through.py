"""The two lazy exits a probe can see: placeholder code, and asking to continue while work is left."""

import pathlib
import re
import sys

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))

import open_items  # noqa: E402
import spec_check  # noqa: E402
from specs import all_segments  # noqa: E402

PROSE = {".md", ".markdown", ".txt", ".rst", ".adoc"}
ALLOW = "sdd: allow-placeholder"
COMMENT = r"(?://|#|--|/\*|<!--)"
PLACEHOLDERS = [
    re.compile(COMMENT + r"\s*\.{3}"),
    re.compile(r"(?i)" + COMMENT + r".*\b(?:rest of (?:the )?(?:code|file|function|method|implementation)"
               r"|existing code|remaining (?:code|implementation))\b"),
    re.compile(r"(?i)\b(?:TODO|FIXME)\b\W*(?:implement|fill in|add (?:the )?implementation)"),
    re.compile(r"(?i)\bpass\s*#\s*(?:todo|fixme|placeholder|implement)"),
    re.compile(r"(?i)\bpanic\(\s*\"(?:not implemented|unimplemented|todo)"),
    re.compile(r"(?i)\bthrow new Error\(\s*[\"'](?:not implemented|todo)"),
]
ASKS_TO_CONTINUE = re.compile(
    r"(?i)\b(?:should|shall)\s+(?:i|we)\s+(?:continue|proceed|keep going|go on|finish)"
    r"|\b(?:do you want|would you like|want)\s+me\s+to\s+(?:continue|proceed|keep going|go on|finish)"
    r"|\blet me know if you(?:'d| would) like me to (?:continue|proceed)"
    r"|要(?:我)?繼續|要不要繼續|是否(?:要)?繼續|要(?:我)?继续|要不要继续|是否(?:要)?继续")


def placeholders(rel, text):
    """[(line within text, line)] of every placeholder in text written to rel."""
    if pathlib.PurePosixPath(rel).suffix.lower() in PROSE:
        return []
    return [(n, line.strip()) for n, line in enumerate(text.splitlines(), 1)
            if ALLOW not in line and any(p.search(line) for p in PLACEHOLDERS)]


def asks_to_continue(reply):
    """True when the reply's last paragraph asks whether to go on."""
    tail = reply.strip().split("\n\n")[-1] if reply.strip() else ""
    return bool(ASKS_TO_CONTINUE.search(tail[-400:]))


def work_left(root, top):
    """What the probes say is unfinished: open blocker findings, untested approved cases."""
    left = [f"blocker: {i['text']}" for i in open_items.open_items(root)
            if i["kind"] == "finding" and i.get("severity") == "blocker"]
    segments = all_segments(top)
    approved = spec_check.approvals(segments)
    if approved:
        cases, _, _ = spec_check.parse(segments)
        for kind, detail in spec_check.check_tests(cases, top):
            cid = detail.split()[0]
            if kind == "untested" and spec_check.story_of(cases[cid]["in"]) in approved:
                left.append(f"{cid} has no test yet")
    return left

