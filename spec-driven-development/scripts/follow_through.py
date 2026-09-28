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
# Each pattern must match a whole affirmative sentence's claim; see claims_done.
CLAIMS_DONE = re.compile(
    r"(?i)\b(?:all\s+)?(?:the\s+)?(?:tests?|suite)\s+(?:now\s+)?(?:pass(?:es|ed)?|(?:are|is)\s+(?:now\s+)?(?:passing|green))\b"
    r"|\ball (?:tests )?(?:green|passing)\b|\b\d+\s+(?:tests?\s+)?passed\b"
    r"|^\s*(?:all\s+)?done\s*$|\b(?:it(?:'s| is)|work is|everything is|all)\s+(?:done|finished|complete)\b"
    r"|\b(?:is|are|now)\s+fixed\b"
    r"|全部通過|全部通过|全過|全过|全綠|全绿|(?:測試|测试)(?:都)?(?:通過|通过)|做完了|全部完成")
CONDITIONAL = re.compile(r"(?i)\b(?:if|unless|once|when|whether)\b|如果|若|假如|等到")
QUOTED = re.compile(r"```.*?```|`[^`]*`|\"[^\"]*\"|“[^”]*”|「[^」]*」|(?<![\w])'[^'\n]+'(?![\w])", re.DOTALL)
# Question forms that contain a negation character without negating: 要不要, 可不可以.
NOT_NEGATION = re.compile(r"要不要|可不可以|能不能|是不是|好不好")
SENTENCE = re.compile(r"[.!?。！？\n]+")
NEGATION = re.compile(r"(?i)\bnot\b|n't\b|\bfail|未|沒|没|不")
ASKS_TO_CONTINUE = re.compile(
    r"(?i)\b(?:should|shall)\s+(?:i|we)\s+(?:continue|proceed|keep going|go on|finish)"
    r"|\b(?:do you want|would you like|want)\s+me\s+to\s+(?:continue|proceed|keep going|go on|finish)"
    r"|\blet me know if you(?:'d| would) like me to (?:continue|proceed)"
    r"|\bi can (?:continue|keep going|go on|finish the rest)"
    r"|要(?:我)?繼續|要不要繼續|是否(?:要)?繼續|要(?:我)?继续|要不要继续|是否(?:要)?继续")


def placeholders(rel, text):
    """[(line within text, line)] of every placeholder in text written to rel."""
    if pathlib.PurePosixPath(rel).suffix.lower() in PROSE:
        return []
    return [(n, line.strip()) for n, line in enumerate(text.splitlines(), 1)
            if ALLOW not in line and (any(p.search(STRING.sub('""', line)) for p in IN_CODE)
                                      or any(p.search(line) for p in WITH_STRING))]


def sentences(text):
    """Sentences outside quotes and code, with CRLF read as LF."""
    plain = QUOTED.sub(" ", (text or "").replace("\r\n", "\n").replace("\r", "\n"))
    return [part.strip() for part in SENTENCE.split(plain) if part.strip()]


def negated(sentence):
    return bool(NEGATION.search(NOT_NEGATION.sub("", sentence)))


def affirmed(text, pattern, conditions=True):
    """A sentence matches pattern and carries no negation, nor, if asked, a condition."""
    return any(pattern.search(s) and not negated(s) and not (conditions and CONDITIONAL.search(s))
               for s in sentences(text))


def asks_to_continue(reply):
    """True when the reply's last paragraph really asks or offers to go on.

    An offer is often conditional ("If you want, I can continue"), so conditions
    do not cancel it; a negation still does.
    """
    paragraphs = [p for p in (reply or "").replace("\r\n", "\n").split("\n\n") if p.strip()]
    return bool(paragraphs) and affirmed(paragraphs[-1][-400:], ASKS_TO_CONTINUE, conditions=False)


def claims_done(reply):
    """True on an affirmative claim that the work is done or the tests pass (SDD-D63)."""
    return affirmed(reply, CLAIMS_DONE)


def latest(root, top):
    """This worktree's latest verify result, cache hits included, or None."""
    import verify
    try:
        return json.loads(verify.latest_path(root, top).read_text(encoding="utf-8"))
    except (OSError, ValueError):
        return None


def work_left(root, top):
    """The blockers in this worktree's latest verify result."""
    result = latest(root, top) or {}
    return [f"{p['probe']}: {p['detail'].splitlines()[0]}" for p in result.get("problems", [])
            if p.get("severity") == "blocker"]


def verified(root, top):
    """True when this worktree's latest verify result passed on the current tree."""
    result = latest(root, top)
    return bool(result) and result.get("exit") == 0 and result.get("tree") == state.tree_hash(top)
