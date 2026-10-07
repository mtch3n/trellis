"""sdd hooks: facts about the work, checked by probes rather than by a model.

post-tool      After a write to a migration, compare its version with every
               local branch and the default branch; speaks only on a collision.
               After AskUserQuestion, queue every question left unanswered.
stop           Queue every "Qn." question in the last reply, and every question
               of an AskUserQuestion the user declined (read from the transcript).
session-end    The same transcript scan, for a session ending with /clear or exit.
session-start  One line when open items are waiting; nothing otherwise.

None of them runs the test suite. Requires Python 3 and git.
"""

import json
import os
import pathlib
import re
import sys

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[1] / "scripts"))

import follow_through  # noqa: E402
import open_items  # noqa: E402
from migrations import check_draft, check_migration  # noqa: E402
import state  # noqa: E402
from state import git, load_config  # noqa: E402

# A question asked in the plugin's format: "Q1. <question> — recommended: <answer>".
QUESTION = re.compile(r"^\s*(?:[-*]\s*)?\**Q\d+[.):]\**\s+(.*\S)")
RECOMMENDED = re.compile(r"\s*[—–-]*\s*(?:recommended|建議|建议)\s*[:：]\s*", re.IGNORECASE)
STORY = re.compile(r"\bspecs/([\w.-]+)")
# How Claude Code records a user declining a tool call (verified 2026-09-28);
# any other error is a failure, not a decision.
DECLINED = "User rejected tool use"


# --- migrations ---------------------------------------------------------

def migration_facts(event):
    tool_input = event.get("tool_input") or {}
    target = tool_input.get("file_path") or tool_input.get("path")
    cwd = event.get("cwd")
    if not isinstance(target, str) or not isinstance(cwd, str):
        return None
    top = git(["rev-parse", "--show-toplevel"], cwd)
    if not top:
        return None
    root = pathlib.Path(top.strip())
    try:
        rel = pathlib.Path(target).resolve().relative_to(root.resolve()).as_posix()
    except ValueError:
        return None
    patterns = load_config(root).get("migrations") or []
    release = (state.folder(state.repo_root(cwd)) / "release.json").is_file()
    problems = check_migration(root, rel, patterns, release) + check_draft(root, rel, patterns)
    if not problems:
        return None
    return {"decision": "block", "reason": "sdd (migration facts, from git):\n- " + "\n- ".join(problems)}


# --- open items ---------------------------------------------------------

def queue_questions(cwd, texts, session=""):
    texts = [t for t in texts if isinstance(t, str) and t.strip()]
    if not texts or not isinstance(cwd, str):
        return None
    root = state.repo_root(cwd)
    for text in texts:
        question, recommended = split_question(text)
        story = STORY.search(text)
        open_items.capture(root, "question", question, recommended=recommended, session=session,
                           story=f"specs/{story.group(1)}" if story else "")
    return None


def split_question(text):
    parts = RECOMMENDED.split(text, maxsplit=1)
    return parts[0].strip(), parts[1].strip() if len(parts) > 1 else ""


def asked(event):
    questions = (event.get("tool_input") or {}).get("questions") or []
    return [q.get("question") for q in questions if isinstance(q, dict)]


def written_text(event):
    """(path, [(first line in the file, text written)]) for Write, Edit and MultiEdit."""
    tool_input = event.get("tool_input") or {}
    path = tool_input.get("file_path")
    if not isinstance(path, str):
        return None, []
    if isinstance(tool_input.get("content"), str):
        return path, [(1, tool_input["content"])]
    pieces = [e.get("new_string") for e in tool_input.get("edits") or [] if isinstance(e, dict)]
    pieces.append(tool_input.get("new_string"))
    try:
        current = pathlib.Path(path).read_text(encoding="utf-8", errors="replace")
    except OSError:
        current = ""
    out = []
    for piece in pieces:
        if isinstance(piece, str) and piece:
            at = current.find(piece)
            out.append((current.count("\n", 0, at) + 1 if at >= 0 else 1, piece))
    return path, out


def placeholder_facts(event):
    path, pieces = written_text(event)
    found = []
    for first, text in pieces:
        found += [f"{pathlib.Path(path).name}:{first + n - 1}: {line}"
                  for n, line in follow_through.placeholders(path, text)]
    if not found:
        return None
    return {"decision": "block", "reason": "sdd: placeholders where the work should be:\n- " + "\n- ".join(found)
            + "\nWrite the real code. A deliberate one carries `sdd: allow-placeholder` on its line."}


def merge(*outputs):
    blocks = [o for o in outputs if o]
    if not blocks:
        return None
    return {"decision": "block", "reason": "\n\n".join(o["reason"] for o in blocks)}


def post_tool(event):
    if event.get("tool_name") in ("Write", "Edit", "MultiEdit"):
        return merge(migration_facts(event), placeholder_facts(event))
    if event.get("tool_name") == "AskUserQuestion":
        response = event.get("tool_response")
        answers = response.get("answers") if isinstance(response, dict) else None
        answers = answers if isinstance(answers, dict) else {}
        return queue_questions(event.get("cwd"), [q for q in asked(event) if q not in answers],
                               event.get("session_id") or "")
    return migration_facts(event)


def declined_questions(root, event):
    """Questions of AskUserQuestion calls the user declined since the last scan.

    Declining with Esc interrupts the turn and fires no hook, so the transcript
    is the only record. Each session's scanned length is kept in
    .sdd/transcripts.json, so a question is found once.
    """
    path, session = event.get("transcript_path"), event.get("session_id") or ""
    if not isinstance(path, str) or not pathlib.Path(path).is_file():
        return []
    marks_path = state.folder(root) / "transcripts.json"
    marks = json.loads(marks_path.read_text(encoding="utf-8")) if marks_path.is_file() else {}
    start = marks.get(session or path, 0)
    asked_by_id, declined = {}, []
    with open(path, "rb") as stream:
        stream.seek(start)
        data = stream.read()
    for line in data.decode("utf-8", errors="replace").splitlines():
        try:
            row = json.loads(line)
        except ValueError:
            continue
        parts = (row.get("message") or {}).get("content") if isinstance(row, dict) else None
        for part in parts if isinstance(parts, list) else []:
            if not isinstance(part, dict):
                continue
            if part.get("type") == "tool_use" and part.get("name") == "AskUserQuestion":
                asked_by_id[part.get("id")] = asked({"tool_input": part.get("input")})
            elif (part.get("type") == "tool_result" and part.get("tool_use_id") in asked_by_id
                  and row.get("toolUseResult") == DECLINED):
                declined += asked_by_id[part["tool_use_id"]]
    # A session with nothing to capture leaves no trace: the cursor is kept only
    # once .sdd/ exists for some other reason.
    if declined or (state.folder(root).is_dir() and marks.get(session or path) != start + len(data)):
        state.ensure(root)
        marks[session or path] = start + len(data)
        marks_path.write_text(json.dumps(marks), encoding="utf-8")
    return declined


def last_reply(event):
    text = event.get("last_assistant_message")
    if isinstance(text, str):
        return text
    # Older harnesses only give the transcript. Its schema is undocumented,
    # so a miss here means no capture, never a wrong one.
    path = event.get("transcript_path")
    if not isinstance(path, str) or not pathlib.Path(path).is_file():
        return ""
    reply = ""
    with open(path, encoding="utf-8") as stream:
        for line in stream:
            try:
                row = json.loads(line)
            except ValueError:
                continue
            message = row.get("message") if isinstance(row, dict) else None
            if row.get("type") == "assistant" and isinstance(message, dict):
                parts = message.get("content")
                if isinstance(parts, list):
                    texts = [p.get("text", "") for p in parts if isinstance(p, dict) and p.get("type") == "text"]
                    if any(texts):
                        reply = "\n".join(texts)
    return reply


def stop(event):
    cwd = event.get("cwd")
    if not isinstance(cwd, str):
        return None
    root = state.repo_root(cwd)
    reply = last_reply(event)
    found = [m.group(1) for m in map(QUESTION.match, reply.splitlines()) if m]
    queue_questions(cwd, found + declined_questions(root, event), event.get("session_id") or "")
    top = git(["rev-parse", "--show-toplevel"], cwd)
    top = pathlib.Path(top.strip()) if top else root
    if not found and follow_through.asks_to_continue(reply):
        left = follow_through.work_left(root, top)
        if left:
            listing = "\n- ".join(left)
            if event.get("stop_hook_active"):
                return {"systemMessage": f"sdd: the agent asked to continue while probes show work left:\n- {listing}"}
            return {"decision": "block", "reason": "sdd: do not ask; the probes say the work is not done:\n- "
                    + listing + "\nContinue. If something blocks you, say what it is."}
    if not found and follow_through.claims_done(reply) and load_config(root).get("test"):
        if not follow_through.verified(root, top):
            ask = "run verify.py before saying the work is done or the tests pass; report what it prints."
            if event.get("stop_hook_active"):
                return {"systemMessage": f"sdd: the agent claimed done with no passing verify for this tree; {ask}"}
            return {"decision": "block", "reason": f"sdd: no passing verify result for this tree. Please {ask}"}
    return shadow_turn(root, top, event.get("stop_hook_active"))


def shadow_turn(root, top, held):
    """Ask the agent about new shadow findings of this branch, and tell the user how earlier ones ended.

    The agent is asked once per finding, never while a stop hook already holds the turn.
    """
    if not (open_items.queue_path(root).is_file() or open_items.archive_path(root).is_file()):
        return None
    # Two sessions may stop at once; the lock keeps "asked once" true for both.
    with state.Lock(root, "shadow-hook.lock"):
        return shadow_turn_locked(root, top, held)


def adopted(root, branch, raised_on):
    """Whether the default branch, checked out here, answers for a finding raised
    on another branch: one merged into it or deleted. Without this such a
    finding was never asked about again and stayed open for good."""
    default = state.default_branch(root)
    if not raised_on or not default or branch != default or raised_on == default:
        return False
    if state.git(["rev-parse", "--verify", "--quiet", f"refs/heads/{raised_on}"], root) is None:
        return True  # deleted
    return state.git(["merge-base", "--is-ancestor", f"refs/heads/{raised_on}", default], root) is not None


def shadow_turn_locked(root, top, held):
    seen_path = state.folder(root) / "shadow-hook.json"
    try:
        seen = json.loads(seen_path.read_text(encoding="utf-8")) if seen_path.is_file() else {}
    except ValueError:
        # Writes are atomic, so only something outside sdd can tear the record.
        # Read as empty, it asks about open findings and reports resolutions one
        # more time each, which beats a hook that fails on every stop.
        seen = {}
    if not isinstance(seen, dict):
        seen = {}
    asked, reported = set(seen.get("asked", [])), set(seen.get("reported", []))
    items = [i for i in open_items.fold(state.read_jsonl(open_items.queue_path(root))[0]).values()
             if i.get("probe") == "shadow"]
    # A finding compacted into the archive since the last stop is reported from there.
    archived = [{"id": a["id"], "text": a.get("text", ""), "resolved": {"how": a.get("how"), "note": a.get("note")}}
                for a in state.read_jsonl(open_items.archive_path(root))[0]
                if a.get("probe") == "shadow" and isinstance(a.get("id"), str)]
    ended = [i for i in items + archived if i["resolved"] and i["id"] not in reported]
    branch = state.branch(top)
    new = [] if held else [i for i in items if not i["resolved"] and i["id"] not in asked
                           and (i.get("branch") == branch or adopted(root, branch, i.get("branch")))]
    out = {}
    if ended:
        verbs = {"fix": "fixed", "dismiss": "dismissed"}
        lines = [f"- {verbs.get(i['resolved'].get('how'), i['resolved'].get('how'))}: {i['text']}"
                 + (f" (why: {i['resolved']['note']})" if i["resolved"].get("note") else "") for i in ended]
        out["systemMessage"] = "sdd shadow, how its findings ended:\n" + "\n".join(lines)
    if new:
        resolve = f"python3 {pathlib.Path(open_items.__file__).resolve()} resolve"
        listing = "\n".join(f"- {i['id']}: {i['text']}" + (f" (when: {i['scenario']})" if i.get("scenario") else "")
                            for i in new)
        out["decision"] = "block"
        out["reason"] = ("sdd shadow: a second look at your change confirmed these. Check each against the code, "
                         f"then either fix it and run `{resolve} <id> --how fix --note \"<what the fix changed>\"`, or run "
                         f"`{resolve} <id> --how dismiss --note \"<why it is not a problem>\"`.\n{listing}")
    if ended or new:
        # Written beside the record and renamed over it, so a stop killed
        # mid-write leaves the old record, never a torn one.
        tmp = seen_path.with_suffix(".json.tmp")
        tmp.write_text(json.dumps({"asked": sorted(asked | {i["id"] for i in new}),
                                   "reported": sorted(reported | {i["id"] for i in ended})}),
                       encoding="utf-8")
        os.replace(tmp, seen_path)
    return out or None


def user_prompt(event):
    """The user's next message in a session answers the questions that session queued."""
    cwd, session = event.get("cwd"), event.get("session_id")
    if not isinstance(cwd, str) or not session:
        return None
    root = state.repo_root(cwd)
    if not open_items.queue_path(root).is_file():
        return None
    for item in open_items.fold(state.read_jsonl(open_items.queue_path(root))[0]).values():
        if item["kind"] == "question" and not item["resolved"] and item.get("session") == session:
            open_items.resolve(root, item["id"], "replied", note="the user sent their next message")
    return None


def session_end(event):
    cwd = event.get("cwd")
    if isinstance(cwd, str):
        queue_questions(cwd, declined_questions(state.repo_root(cwd), event), event.get("session_id") or "")
    return None


def session_start(event):
    cwd = event.get("cwd")
    if not isinstance(cwd, str):
        return None
    root = state.repo_root(cwd)
    if not open_items.queue_path(root).is_file():
        return None
    items = open_items.open_items(root, resolve_answers=False)
    if not items:
        return None
    questions = sum(1 for i in items if i["kind"] == "question")
    stale = sum(1 for i in items if i["state"] != "open")
    detail = f"{questions} question(s), {len(items) - questions} finding(s)" + (f", {stale} stale" if stale else "")
    return f"sdd: {len(items)} open item(s) waiting for the user ({detail}). The user resumes them with /sdd:triaging-open-items."


HANDLERS = {"post-tool": post_tool, "stop": stop, "session-end": session_end, "session-start": session_start,
            "user-prompt": user_prompt}


def main():
    if os.environ.get("SDD_SHADOW"):
        return
    try:
        if len(sys.argv) != 2 or sys.argv[1] not in HANDLERS:
            raise ValueError("usage: sdd_hook.py " + "|".join(HANDLERS))
        event = json.load(sys.stdin)
        if not isinstance(event, dict):
            raise ValueError("hook input must be an object")
        output = HANDLERS[sys.argv[1]](event)
    except Exception as error:  # a hook must never fail the user's turn
        output = {"systemMessage": f"sdd hook skipped: {type(error).__name__}: {error}"}
    if isinstance(output, str):
        print(output)
    elif output:
        print(json.dumps(output))


if __name__ == "__main__":
    main()
