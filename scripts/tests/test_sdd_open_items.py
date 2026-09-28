"""Open-items queue: specs/sdd/open-items, cases SDD-C12 to SDD-C24."""

from datetime import datetime, timedelta, timezone
import json
import subprocess
import sys
import tempfile
import unittest

from sdd_helpers import PLUGIN, Repo, load

HOOK = load("sdd_hook", "spec-driven-development/hooks/sdd_hook.py")
ITEMS = load("open_items", "spec-driven-development/scripts/open_items.py")
SPECS = load("specs", "spec-driven-development/scripts/specs.py")

REPLY = """Two things to settle.

Q1. Should drafts live under migrations/draft? — recommended: yes, one folder per sequence.
Q2. Keep internal/store/migrate_0014.go as a Go draft? — recommended: yes, it cannot be SQL.
"""


class QueueTest(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.repo = Repo(tmp.name)
        self.repo.write("internal/store/migrate_0014.go")
        self.repo.commit()
        self.head = self.repo.git("rev-parse", "HEAD").strip()

    def stop(self, reply):
        return HOOK.stop({"cwd": str(self.repo.root), "last_assistant_message": reply, "session_id": "s1"})

    def queue_lines(self):
        path = self.repo.root / ".sdd/queue.jsonl"
        return [json.loads(line) for line in path.read_text(encoding="utf-8").splitlines()] if path.exists() else []

    def open_items(self, **kw):
        return ITEMS.open_items(self.repo.root, **kw)


class CaptureTests(QueueTest):
    def test_SDD_C12_questions_in_a_reply_are_captured_with_time_and_anchors(self):
        self.stop(REPLY)
        events = self.queue_lines()
        self.assertEqual([e["event"] for e in events], ["opened", "opened"])
        first, second = events
        for event in events:
            self.assertEqual(event["kind"], "question")
            self.assertEqual(event["commit"], self.head)
            self.assertEqual(event["session"], "s1")
            datetime.fromisoformat(event["at"])
        self.assertEqual(first["text"], "Should drafts live under migrations/draft?")
        self.assertEqual(first["recommended"], "yes, one folder per sequence.")
        self.assertEqual(second["files"], ["internal/store/migrate_0014.go"])
        self.assertTrue((self.repo.root / ".sdd/.gitignore").is_file())

    def test_SDD_C13_asking_again_is_seen_not_a_second_item(self):
        self.stop(REPLY)
        self.stop("Still open:\nQ1. Should drafts live under migrations/draft? — recommended: yes.\n")
        self.assertEqual([e["event"] for e in self.queue_lines()], ["opened", "opened", "seen"])
        self.assertEqual(len(self.open_items()), 2)

    def test_SDD_C14_a_reply_without_questions_writes_nothing(self):
        self.assertIsNone(self.stop("I changed the parser and ran the suite."))
        self.assertFalse((self.repo.root / ".sdd").exists())

    def test_SDD_C15_a_malformed_line_is_skipped(self):
        self.stop(REPLY)
        with open(self.repo.root / ".sdd/queue.jsonl", "a", encoding="utf-8") as stream:
            stream.write("{not json\n")
        self.assertEqual(len(self.open_items()), 2)
        self.assertIsNone(self.stop("Nothing to ask."))

    def test_SDD_C24_ask_user_question_left_unanswered_is_captured(self):
        # The shape Claude Code sends when a question was answered and when the user was away.
        questions = [{"question": "Which store?", "header": "Store", "options": []},
                     {"question": "Which name?", "header": "Name", "options": []}]
        HOOK.post_tool({"cwd": str(self.repo.root), "tool_name": "AskUserQuestion",
                        "tool_input": {"questions": questions},
                        "tool_response": {"questions": questions, "answers": {"Which store?": "Trellis"},
                                          "annotations": {}}})
        self.assertEqual([i["text"] for i in self.open_items()], ["Which name?"])

    def test_SDD_C50_a_declined_question_is_found_in_the_transcript_once(self):
        # Esc on AskUserQuestion interrupts the turn and fires no hook; only the transcript has it.
        transcript = self.repo.root.parent / (self.repo.root.name + ".jsonl")
        use = {"type": "assistant", "message": {"content": [{"type": "tool_use", "id": "toolu_1",
               "name": "AskUserQuestion", "input": {"questions": [{"question": "Solo or team?", "options": []}]}}]}}
        result = {"type": "user", "toolUseResult": "User rejected tool use", "message": {"content": [
                  {"type": "tool_result", "tool_use_id": "toolu_1", "is_error": True, "content": "rejected"}]}}
        transcript.write_text(json.dumps(use) + "\n" + json.dumps(result) + "\n", encoding="utf-8")
        event = {"cwd": str(self.repo.root), "session_id": "s9", "transcript_path": str(transcript),
                 "last_assistant_message": "Stopped."}
        HOOK.stop(event)
        self.assertEqual([i["text"] for i in self.open_items()], ["Solo or team?"])
        ITEMS.resolve(self.repo.root, self.open_items()[0]["id"], "fix")
        HOOK.session_end(event)
        self.assertEqual(self.open_items(), [])

    def test_SDD_C24_a_fully_answered_question_captures_nothing(self):
        questions = [{"question": "Which store?", "options": []}]
        HOOK.post_tool({"cwd": str(self.repo.root), "tool_name": "AskUserQuestion",
                        "tool_input": {"questions": questions},
                        "tool_response": {"questions": questions, "answers": {"Which store?": "Trellis"}}})
        self.assertFalse((self.repo.root / ".sdd").exists())


class ToolErrorTests(QueueTest):
    def test_SDD_C74_a_failed_question_that_was_not_declined_is_not_captured(self):
        transcript = self.repo.root.parent / (self.repo.root.name + ".err.jsonl")
        use = {"type": "assistant", "message": {"content": [{"type": "tool_use", "id": "toolu_2",
               "name": "AskUserQuestion", "input": {"questions": [{"question": "Broken?", "options": []}]}}]}}
        result = {"type": "user", "toolUseResult": "Error: invalid options", "message": {"content": [
                  {"type": "tool_result", "tool_use_id": "toolu_2", "is_error": True, "content": "Error: invalid options"}]}}
        transcript.write_text(json.dumps(use) + "\n" + json.dumps(result) + "\n", encoding="utf-8")
        HOOK.stop({"cwd": str(self.repo.root), "session_id": "s8", "transcript_path": str(transcript),
                   "last_assistant_message": "Stopped."})
        self.assertEqual(self.open_items(), [])


class RobustnessTests(QueueTest):
    def test_SDD_C103_a_row_missing_fields_crashes_nothing(self):
        self.stop(REPLY)
        with open(self.repo.root / ".sdd/queue.jsonl", "a", encoding="utf-8") as stream:
            stream.write(json.dumps({"event": "opened", "id": "q-00000000"}) + "\n")
        self.stop("Nothing to ask.")
        out = HOOK.session_start({"cwd": str(self.repo.root), "source": "startup"})
        self.assertIn("2 open", out)

    def test_SDD_C104_hooks_never_call_trellis(self):
        self.stop(REPLY)
        import os, pathlib
        bin_dir = pathlib.Path(self.repo.root.parent) / (self.repo.root.name + "-bin")
        bin_dir.mkdir()
        marker = bin_dir / "called"
        (bin_dir / "trellis").write_text(f"#!/bin/sh\ntouch {marker}\nsleep 30\n", encoding="utf-8")
        (bin_dir / "trellis").chmod(0o755)
        path = os.environ["PATH"]
        os.environ["PATH"] = f"{bin_dir}{os.pathsep}{path}"
        try:
            self.stop("Should I continue?")
            HOOK.session_start({"cwd": str(self.repo.root), "source": "startup"})
        finally:
            os.environ["PATH"] = path
        self.assertFalse(marker.exists())


class ResolveTests(QueueTest):
    def test_SDD_C68_an_unknown_action_is_refused(self):
        self.stop(REPLY)
        item = self.open_items()[0]["id"]
        with self.assertRaises(ValueError):
            ITEMS.resolve(self.repo.root, item, "ignore")
        self.assertIn(item, [i["id"] for i in self.open_items()])


class StalenessTests(QueueTest):
    def test_SDD_C16_a_decision_that_answers_resolves_the_question(self):
        self.stop(REPLY)
        qid = self.queue_lines()[0]["id"]
        self.repo.write("specs/pins.md", f"## Decisions\n- **PIN-D1** Drafts under draft/. Governs: x. Answers: {qid}.\n")
        self.assertEqual(len(self.open_items(specs=SPECS.file_segments)), 1)
        resolved = [e for e in self.queue_lines() if e["event"] == "resolved"]
        self.assertEqual((resolved[0]["id"], resolved[0]["how"]), (qid, "decision"))

    def test_SDD_C17_a_question_whose_file_changed_is_drifted(self):
        self.stop(REPLY)
        self.repo.write("internal/store/migrate_0014.go", "package store\n")
        states = {i["text"]: i["state"] for i in self.open_items()}
        self.assertEqual(states["Keep internal/store/migrate_0014.go as a Go draft?"], "drifted")
        self.assertEqual(states["Should drafts live under migrations/draft?"], "open")

    def test_SDD_C18_an_old_question_with_unchanged_anchors_is_aged(self):
        self.stop(REPLY)
        later = datetime.now(timezone.utc) + timedelta(days=15)
        states = {i["text"]: i["state"] for i in self.open_items(now=later)}
        self.assertEqual(set(states.values()), {"aged"})


class SessionStartTests(QueueTest):
    def test_SDD_C19_open_items_give_one_line(self):
        self.stop(REPLY)
        out = HOOK.session_start({"cwd": str(self.repo.root), "source": "startup"})
        self.assertEqual(len(out.splitlines()), 1)
        self.assertIn("2 open", out)
        self.assertIn("/sdd:triaging-open-items", out)

    def test_SDD_C20_no_open_items_print_nothing(self):
        self.assertIsNone(HOOK.session_start({"cwd": str(self.repo.root), "source": "startup"}))
        self.stop(REPLY)
        for item in self.open_items():
            ITEMS.resolve(self.repo.root, item["id"], "dismiss")
        self.assertIsNone(HOOK.session_start({"cwd": str(self.repo.root), "source": "clear"}))


class CompactionTests(QueueTest):
    def test_SDD_C21_compaction_keeps_open_caps_archive_and_keeps_tombstones(self):
        root = self.repo.root
        for n in range(250):
            item = ITEMS.capture(root, "finding", f"problem {n}", severity="note")
            ITEMS.resolve(root, item, "fix")
        dismissed = ITEMS.capture(root, "question", "Rename the table?")
        ITEMS.resolve(root, dismissed, "dismiss")
        kept = ITEMS.capture(root, "question", "Keep the index?")
        ITEMS.capture(root, "question", "Keep the index?")
        ITEMS.compact(root)
        self.assertEqual([e["event"] for e in self.queue_lines()], ["opened", "seen"])
        archive = [json.loads(l) for l in (root / ".sdd/archive.jsonl").read_text(encoding="utf-8").splitlines()]
        self.assertEqual(len([a for a in archive if not a.get("tombstone")]), 200)
        self.assertEqual([a["id"] for a in archive if a.get("tombstone")], [dismissed])
        self.assertEqual(archive[-1]["text"], "problem 249")
        self.assertIsNone(ITEMS.capture(root, "question", "Rename the table?"))
        self.assertEqual([i["id"] for i in self.open_items()], [kept])

    def test_SDD_C22_concurrent_appends_all_land(self):
        code = ("import sys; sys.path.insert(0, sys.argv[1]); import open_items\n"
                "for n in range(100): open_items.capture(sys.argv[2], 'question', f'{sys.argv[3]} {n}?')\n")
        procs = [subprocess.Popen([sys.executable, "-c", code, str(PLUGIN / "scripts"), str(self.repo.root), tag])
                 for tag in ("a", "b")]
        for proc in procs:
            self.assertEqual(proc.wait(timeout=60), 0)
        lines = self.queue_lines()
        self.assertEqual(len(lines), 200)
        self.assertEqual(len({e["id"] for e in lines}), 200)

    def test_SDD_C23_a_linked_worktree_writes_to_the_main_queue(self):
        with tempfile.TemporaryDirectory() as other:
            linked = f"{other}/wt"
            self.repo.git("worktree", "add", "-q", "-b", "side", linked)
            HOOK.stop({"cwd": linked, "last_assistant_message": REPLY})
        self.assertEqual(len(self.queue_lines()), 2)


if __name__ == "__main__":
    unittest.main()
