"""The two lazy exits a probe can see: placeholder code, and asking to continue while work is left."""

import pathlib
import re
import sys

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))

import json  # noqa: E402

import state  # noqa: E402

PROSE = {".md", ".markdown", ".txt", ".rst", ".adoc"}
ALLOW = "sdd: allow-placeholder"
COMMENT = r"(?://|#|--|/\*|<!--)"
STRING = re.compile(r"\"(?:\\.|[^\"\\])*\"|'(?:\\.|[^'\\])*'")
# Matched against the line with its string literals emptied, so text inside a
# string is never taken for a comment.
IN_CODE = [
    re.compile(COMMENT + r"\s*\.{3}"),
    re.compile(r"(?i)" + COMMENT + r".*\b(?:rest of (?:the )?(?:code|file|function|method|implementation)"
               r"|existing code|remaining (?:code|implementation))\b"),
    re.compile(r"(?i)\b(?:TODO|FIXME)\b\W*(?:implement|fill in|add (?:the )?implementation)"),
    re.compile(r"(?i)\bpass\s*#\s*(?:todo|fixme|placeholder|implement)"),
]
# Matched against the whole line: the placeholder is the string.
WITH_STRING = [
    re.compile(r"(?i)\bpanic\(\s*\"(?:not implemented|unimplemented|todo)"),
    re.compile(r"(?i)\bthrow new Error\(\s*[\"'](?:not implemented|todo)"),
]
CLAIMS_DONE = re.compile(
    r"(?i)\b(?:all\s+)?(?:the\s+)?(?:tests?|suite)\s+(?:now\s+)?(?:pass(?:es|ed|ing)?|green)\b"
    r"|\ball (?:green|passing)\b|\b\d+\s+(?:tests?\s+)?passed\b"
    r"|\b(?:it(?:'s| is)|work is|everything is|all)\s+(?:done|finished|complete)\b|\b(?:is|are|now)\s+fixed\b"
    r"|(?:測試|测试)[^。\n]{0,12}?(?:通過|通过|過了|过了)|全(?:部)?(?:通過|通过|綠|绿)|(?:做完|完成)了")
NEGATION = re.compile(r"(?i)\bnot\b|n't\b|\bfail|未|沒|没|不")
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
            if ALLOW not in line and (any(p.search(STRING.sub('""', line)) for p in IN_CODE)
                                      or any(p.search(line) for p in WITH_STRING))]


def asks_to_continue(reply):
    """True when the reply's last paragraph asks whether to go on."""
    tail = reply.strip().split("\n\n")[-1] if reply.strip() else ""
    return bool(ASKS_TO_CONTINUE.search(tail[-400:]))


def claims_done(reply):
    return any(not NEGATION.search(m.group()) for m in CLAIMS_DONE.finditer(reply or ""))


def results(root):
    """Verify results, newest first."""
    found = []
    for path in sorted((state.folder(root) / "verify").glob("*.json"), key=lambda p: p.stat().st_mtime, reverse=True):
        try:
            found.append(json.loads(path.read_text(encoding="utf-8")))
        except (OSError, ValueError):
            continue
    return found


def work_left(root):
    """The blockers in the newest verify result: what the probes last said is unfinished."""
    newest = results(root)[:1]
    return [f"{p['probe']}: {p['detail'].splitlines()[0]}" for r in newest
            for p in r.get("problems", []) if p.get("severity") == "blocker"]


def verified(root, top):
    """True when a verify result for the current tree passed."""
    tree = state.tree_hash(top)
    return any(r.get("tree") == tree and r.get("exit") == 0 for r in results(root))
