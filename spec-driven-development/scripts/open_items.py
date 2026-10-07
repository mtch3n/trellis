#!/usr/bin/env python3
"""The open-items queue: questions the user has not answered and findings not yet resolved.

.sdd/queue.jsonl is an append-only event log (opened, seen, resolved); the open
set is derived from it. Time never resolves an item; it only marks it aged.

    open_items.py list [--json]                 open items, stale ones marked
    open_items.py resolve <id> --how <how> [--note <text>]
    open_items.py compact                       fold resolved items into the archive
"""

import argparse
from datetime import datetime, timedelta, timezone
import hashlib
import json
import pathlib
import re
import sys

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))

import state  # noqa: E402
from specs import all_segments  # noqa: E402

KINDS = ("question", "finding")
HOWS = ("fix", "card", "decision", "accept", "dismiss", "gone", "replied")
TOMBSTONE_HOWS = ("accept", "dismiss")
AGED_AFTER = timedelta(days=14)
ARCHIVE_CAP = 200
COMPACT_AT = 1_000_000
ANSWERS = re.compile(r"(?i)\banswers:\s*([a-z]-[0-9a-f]{8}(?:[\s,]+[a-z]-[0-9a-f]{8})*)")
PATHLIKE = re.compile(r"[\w.-]+(?:/[\w.-]+)+|[\w-]+\.[A-Za-z]{1,5}\b")


def queue_path(root):
    return state.folder(root) / "queue.jsonl"


def archive_path(root):
    return state.folder(root) / "archive.jsonl"


def normalize(text):
    return re.sub(r"\s+", " ", text).strip().lower()


def item_id(kind, story, text):
    digest = hashlib.sha1(f"{kind}\0{story}\0{normalize(text)}".encode("utf-8")).hexdigest()
    return f"{kind[0]}-{digest[:8]}"


def mentioned_files(root, text):
    found = []
    for token in PATHLIKE.findall(text):
        token = token.rstrip(".")
        if token not in found and (pathlib.Path(root) / token).is_file():
            found.append(token)
    return found


def fold(events):
    """{id: item} from an event log, in first-opened order."""
    items = {}
    for event in events:
        iid, kind = event.get("id"), event.get("event")
        if not isinstance(iid, str):
            continue
        if kind == "opened":
            if not all(isinstance(event.get(k), str) for k in ("kind", "at", "text")):
                continue
            items[iid] = {**event, "last_seen": event.get("at"), "resolved": None}
        elif iid in items and kind == "seen":
            items[iid]["last_seen"] = event.get("at")
        elif iid in items and kind == "resolved":
            items[iid]["resolved"] = event
    return items


def tombstones(root, items):
    stones = {iid for iid, item in items.items()
              if item["resolved"] and item["resolved"].get("how") in TOMBSTONE_HOWS}
    archived, _ = state.read_jsonl(archive_path(root))
    stones.update(a["id"] for a in archived if a.get("tombstone"))
    return stones


def capture(root, kind, text, *, story="", recommended="", session="", now=None, **extra):
    """Open an item, or mark it seen. Returns its id, or None if it was dismissed or accepted."""
    if kind not in KINDS:
        raise ValueError(f"kind must be one of {KINDS}")
    root = pathlib.Path(root)
    iid = item_id(kind, story, text)
    at = state.now_iso(now)
    with state.Lock(root):
        events, _ = state.read_jsonl(queue_path(root))
        items = fold(events)
        if iid in tombstones(root, items):
            return None
        current = items.get(iid)
        if current and not current["resolved"]:
            event = {"at": at, "event": "seen", "id": iid, "commit": state.head(root)}
        else:
            event = {"at": at, "event": "opened", "id": iid, "kind": kind, "text": text,
                     "story": story, "files": mentioned_files(root, text), "session": session,
                     "commit": state.head(root)}
            if recommended:
                event["recommended"] = recommended
            event.update({k: v for k, v in extra.items() if v not in (None, "")})
        state.append_jsonl(queue_path(root), [event])
        big = queue_path(root).stat().st_size > COMPACT_AT
    if big:
        compact(root)
    return iid


def resolve(root, iid, how, note="", now=None):
    if how not in HOWS:
        raise ValueError(f"how must be one of {HOWS}")
    with state.Lock(root):
        items = fold(state.read_jsonl(queue_path(root))[0])
        if iid not in items or items[iid]["resolved"]:
            return False
        if items[iid].get("probe") == "shadow":
            if how not in ("fix", "dismiss"):
                raise ValueError("a shadow finding is answered with --how fix or --how dismiss")
            if how == "dismiss" and not note.strip():
                raise ValueError("a shadow finding is dismissed with --note saying why it is not a problem")
        event = {"at": state.now_iso(now), "event": "resolved", "id": iid, "how": how}
        if note:
            event["note"] = note
        state.append_jsonl(queue_path(root), [event])
    return True


def shadow_items(root):
    """Every shadow finding, open or resolved, archived ones included, oldest first."""
    items = [{"text": i["text"], "at": i["at"], "how": (i["resolved"] or {}).get("how")}
             for i in fold(state.read_jsonl(queue_path(root))[0]).values() if i.get("probe") == "shadow"]
    archived, _ = state.read_jsonl(archive_path(root))
    items += [{"text": a.get("text", ""), "at": a.get("opened_at", ""), "how": a.get("how")}
              for a in archived if a.get("probe") == "shadow"]
    return sorted(items, key=lambda i: i["at"])


def answered_ids(segments):
    found = set()
    for _, text in segments:
        for group in ANSWERS.findall(text):
            found.update(re.split(r"[\s,]+", group.strip()))
    return found


def drifted(root, item):
    files, commit = item.get("files") or [], item.get("commit")
    if not files or not commit:
        return False
    changed = state.git(["diff", "--name-only", commit, "--", *files], root)
    return bool(changed and changed.strip())


def open_items(root, now=None, specs=None, resolve_answers=True):
    """Open items with a state: open, drifted or aged.

    With resolve_answers, questions a spec decision answers are resolved first;
    that reads every spec, so hooks, which must stay fast, pass False.
    """
    root = pathlib.Path(root)
    items = fold(state.read_jsonl(queue_path(root))[0])
    pending = [i for i in items.values() if not i["resolved"]]
    if resolve_answers and any(i["kind"] == "question" for i in pending):
        answers = answered_ids((specs or all_segments)(root))
        for item in pending:
            if item["kind"] == "question" and item["id"] in answers:
                resolve(root, item["id"], "decision", note="a spec decision answers it", now=now)
                item["resolved"] = True
    now = now or datetime.now(timezone.utc)
    out = []
    for item in pending:
        if item["resolved"]:
            continue
        if drifted(root, item):
            item["state"] = "drifted"
        elif now - datetime.fromisoformat(item["at"]) > AGED_AFTER:
            item["state"] = "aged"
        else:
            item["state"] = "open"
        out.append(item)
    return out


def compact(root):
    """Keep open items' events; fold resolved items into a capped archive plus tombstones."""
    root = pathlib.Path(root)
    with state.Lock(root):
        events, _ = state.read_jsonl(queue_path(root))
        items = fold(events)
        archived, _ = state.read_jsonl(archive_path(root))
        for item in items.values():
            done = item["resolved"]
            if not done:
                continue
            archived.append({"id": item["id"], "kind": item["kind"], "text": item["text"],
                             "opened_at": item["at"], "resolved_at": done.get("at"), "how": done.get("how"),
                             **({"probe": item["probe"]} if item.get("probe") else {}),
                             **({"tombstone": True} if done.get("how") in TOMBSTONE_HOWS else {})})
        stones = [a for a in archived if a.get("tombstone")]
        rest = [a for a in archived if not a.get("tombstone")][-ARCHIVE_CAP:]
        keep = {iid for iid, item in items.items() if not item["resolved"]}
        state.replace_jsonl(archive_path(root), stones + rest)
        state.replace_jsonl(queue_path(root), [e for e in events if e.get("id") in keep])


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = parser.add_subparsers(dest="command", required=True)
    listing = sub.add_parser("list")
    listing.add_argument("--json", action="store_true")
    done = sub.add_parser("resolve")
    done.add_argument("id")
    done.add_argument("--how", required=True, choices=HOWS)
    done.add_argument("--note", default="")
    sub.add_parser("compact")
    args = parser.parse_args()

    root = state.repo_root(pathlib.Path.cwd())
    if args.command == "resolve":
        try:
            done = resolve(root, args.id, args.how, args.note)
        except ValueError as error:
            print(f"refused: {error}", file=sys.stderr)
            return 1
        if not done:
            print(f"{args.id} is not open", file=sys.stderr)
            return 1
        return 0
    if args.command == "compact":
        compact(root)
        return 0
    items = open_items(root)
    if args.json:
        print(json.dumps(items, ensure_ascii=False, indent=2))
        return 0
    for item in items:
        severity = item.get("severity", "")
        print(f"{item['id']}  {item['kind']:<8} {severity:<7} {item['state']:<7} {item['at'][:10]}  {item['text']}")
    if not items:
        print("no open items")
    return 0


if __name__ == "__main__":
    sys.exit(main())
