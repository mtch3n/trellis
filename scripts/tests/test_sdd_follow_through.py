"""Follow-through: specs/sdd/follow-through, cases SDD-C52 to SDD-C60."""

import tempfile
import unittest

import json

from sdd_helpers import Repo, load

HOOK = load("sdd_hook", "spec-driven-development/hooks/sdd_hook.py")
ITEMS = load("open_items", "spec-driven-development/scripts/open_items.py")
VERIFY = load("verify", "spec-driven-development/scripts/verify.py")

ELISION = "    // ... rest of the code unchanged\n"


class FollowThroughTest(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.repo = Repo(tmp.name)
        self.root = self.repo.root

    def write(self, rel, text):
        self.repo.write(rel, text)
        return HOOK.post_tool({"cwd": str(self.root), "tool_name": "Write",
                               "tool_input": {"file_path": str(self.root / rel), "content": text}})

    def edit(self, rel, before, after):
        path = self.root / rel
        path.write_text(path.read_text(encoding="utf-8").replace(before, after), encoding="utf-8")
        return HOOK.post_tool({"cwd": str(self.root), "tool_name": "Edit",
                               "tool_input": {"file_path": str(path), "old_string": before, "new_string": after}})

    def stop(self, reply, **extra):
        return HOOK.stop({"cwd": str(self.root), "last_assistant_message": reply, **extra})

    def blocker(self):
        """A blocker that the newest verify result still holds."""
        self.repo.write(".sdd/config.json", json.dumps({"test": "echo `go test ./...`; exit 1"}))
        self.assertEqual(VERIFY.verify(self.root)["exit"], 1)


class PlaceholderTests(FollowThroughTest):
    def test_SDD_C52_an_elision_comment_is_blocked_with_file_and_line(self):
        out = self.write("app.go", "package app\n\nfunc Run() {\n" + ELISION + "}\n")
        self.assertEqual(out["decision"], "block")
        self.assertIn("app.go:4", out["reason"])

    def test_SDD_C53_only_the_new_text_is_scanned(self):
        self.repo.write("app.go", "package app\n\nfunc Run() {\n" + ELISION + "\treturn\n}\n")
        self.assertIsNone(self.edit("app.go", "\treturn\n", "\treturn // done\n"))

    def test_SDD_C54_markdown_and_marked_lines_are_allowed(self):
        self.assertIsNone(self.write("notes.md", "Example:\n" + ELISION))
        marked = ELISION.rstrip("\n") + "  // sdd: allow-placeholder\n"
        self.assertIsNone(self.write("app.go", "package app\n\nfunc Run() {\n" + marked + "}\n"))

    def test_SDD_C55_unfinished_bodies_are_blocked_but_plain_pass_is_not(self):
        out = self.write("calc.py", "def add(a, b):\n    pass  # TODO implement\n")
        self.assertIn("calc.py:2", out["reason"])
        out = self.write("run.go", 'package run\n\nfunc Run() {\n\tpanic("not implemented")\n}\n')
        self.assertIn("run.go:4", out["reason"])
        self.assertIsNone(self.write("empty.py", "class Marker:\n    pass\n"))


class ContinueTests(FollowThroughTest):
    def test_SDD_C56_asking_to_continue_with_a_blocker_is_sent_back(self):
        self.blocker()
        out = self.stop("I fixed half of it. Should I continue?")
        self.assertEqual(out["decision"], "block")
        self.assertIn("go test ./...", out["reason"])

    def test_SDD_C57_asking_with_nothing_left_stands(self):
        self.assertIsNone(self.stop("Everything is in. Should I continue with the docs?"))

    def test_SDD_C58_real_questions_are_never_sent_back(self):
        self.blocker()
        out = self.stop("Two choices first.\nQ1. Keep the index? — recommended: yes\n"
                        "Q2. Rename it? — recommended: no\nShould I continue?")
        self.assertNotIn("decision", out or {})

    def test_SDD_C59_a_second_stop_is_not_blocked_again(self):
        self.blocker()
        out = self.stop("還差一點。要我繼續嗎？", stop_hook_active=True)
        self.assertNotIn("decision", out)
        self.assertIn("go test ./...", out["systemMessage"])

    def test_SDD_C60_an_untested_approved_case_is_named(self):
        self.repo.write("specs/pins.md", "# Pins\n\nStatus: cases approved 2026-09-28 at abc1234\n\n"
                        "## Decisions\n- **PIN-D1** Pins sort first. Why: x. Governs: order.\n\n"
                        "## Cases\n| ID | Covers | Kind | Case |\n|---|---|---|---|\n"
                        "| PIN-C1 | PIN-D1 | expected | pinned first |\n"
                        "| PIN-C2 | PIN-D1 | unexpected | missing card |\n")
        self.repo.write("tests/test_pins.py", "def test_PIN_C1():\n    pass\n")
        self.repo.write(".sdd/config.json", json.dumps({"test": "true"}))
        for case in ("PIN-C1", "PIN-C2"):
            self.repo.write(f".sdd/red/{case}.json", "{}")
        VERIFY.verify(self.root)  # SDD-D62: the newest verify result is the evidence
        out = self.stop("做了一半，要我繼續嗎？")
        self.assertEqual(out["decision"], "block")
        self.assertIn("PIN-C2", out["reason"])
        self.assertNotIn("PIN-C1", out["reason"])

    def test_SDD_C100_a_blocker_the_newest_verify_dropped_does_not_send_back(self):
        ITEMS.capture(self.root, "finding", "an old failure", severity="blocker", probe="test")
        self.repo.write(".sdd/config.json", json.dumps({"test": "true"}))
        self.assertEqual(VERIFY.verify(self.root)["exit"], 0)
        ITEMS.capture(self.root, "finding", "an old failure", severity="blocker", probe="test")
        self.assertIsNone(self.stop("Half done. Should I continue?"))


class StringLiteralTests(FollowThroughTest):
    def test_SDD_C102_placeholder_text_inside_a_string_is_not_a_placeholder(self):
        self.assertIsNone(self.write("show.py", 'print("# ...")\nHELP = "-- ... rest of the code"\n'))


class DoneClaimTests(FollowThroughTest):
    def claim(self, reply="All tests pass and it is done.", **extra):
        return self.stop(reply, **extra)

    def test_SDD_C105_a_claim_without_a_passing_verify_is_sent_back_once(self):
        self.repo.write(".sdd/config.json", json.dumps({"test": "true"}))
        out = self.claim()
        self.assertEqual(out["decision"], "block")
        self.assertIn("verify.py", out["reason"])

    def test_SDD_C106_a_claim_with_a_passing_verify_stands(self):
        self.repo.write(".sdd/config.json", json.dumps({"test": "true"}))
        self.assertEqual(VERIFY.verify(self.root)["exit"], 0)
        out = self.claim()
        self.assertNotIn("decision", out or {})

    def test_SDD_C107_a_second_stop_only_tells_the_user(self):
        self.repo.write(".sdd/config.json", json.dumps({"test": "true"}))
        out = self.claim(stop_hook_active=True)
        self.assertNotIn("decision", out)
        self.assertIn("verify", out["systemMessage"])

    def test_SDD_C108_no_claim_computes_nothing(self):
        self.repo.write(".sdd/config.json", json.dumps({"test": "true"}))
        original = HOOK.state.tree_hash
        HOOK.state.tree_hash = lambda top: self.fail("tree hash computed without a claim")
        try:
            self.assertIsNone(self.stop("I renamed the parser."))
        finally:
            HOOK.state.tree_hash = original

    def test_SDD_C116_a_newer_blocked_verify_outweighs_an_older_pass(self):
        self.repo.write(".sdd/config.json", json.dumps({"test": "true"}))
        self.repo.write("specs/pins.md", "# Pins\n")
        self.repo.commit("spec")
        approval = self.repo.git("rev-parse", "--short", "HEAD").strip()
        self.repo.write("specs/pins.md", f"# Pins\n\nStatus: cases approved 2026-09-28 at {approval}\n\n"
                        "## Decisions\n- **PIN-D1** d. Governs: g. Unexpected: n/a — a sketch.\n\n"
                        "## Cases\n| ID | Covers | Kind | Case |\n|---|---|---|---|\n| PIN-C1 | PIN-D1 | expected | x |\n")
        self.repo.write("tests/test_pins.py", "def test_PIN_C1():\n    pass\n")
        self.repo.commit("tests")
        self.repo.write(".sdd/red/PIN-C1.json", "{}")
        self.assertEqual(VERIFY.verify(self.root)["exit"], 0)
        (self.root / ".sdd/red/PIN-C1.json").unlink()  # same tree, different inputs
        self.assertEqual(VERIFY.verify(self.root)["exit"], 1)
        self.assertEqual((self.claim() or {}).get("decision"), "block")

    def test_SDD_C119_negated_conditional_and_quoted_claims_are_not_claims(self):
        self.repo.write(".sdd/config.json", json.dumps({"test": "true"}))
        for reply in ("Not all tests pass.", "If tests pass, commit the change.", 'I wrote "tests pass" in the log.'):
            self.assertNotIn("decision", self.claim(reply) or {}, reply)

    def test_SDD_C120_plain_claims_are_caught(self):
        self.repo.write(".sdd/config.json", json.dumps({"test": "true"}))
        for reply in ("Done.", "All tests are passing."):
            self.assertEqual((self.claim(reply) or {}).get("decision"), "block", reply)

    def test_SDD_C129_single_quotes_hide_a_claim_but_apostrophes_do_not(self):
        self.repo.write(".sdd/config.json", json.dumps({"test": "true"}))
        self.assertNotIn("decision", self.claim("I wrote 'tests pass' in the log.") or {})
        self.assertEqual((self.claim("Everything's in and it's done.") or {}).get("decision"), "block")


class ContinueEdgeTests(FollowThroughTest):
    def test_SDD_C130_chinese_and_conditional_offers_ask_to_continue(self):
        self.blocker()
        for reply in ("做了一半。要不要繼續？", "Half of it is in. If you want, I can continue."):
            self.assertEqual((self.stop(reply) or {}).get("decision"), "block", reply)

    def test_SDD_C121_only_a_real_closing_question_asks_to_continue(self):
        self.blocker()
        self.assertIsNone(self.stop('I will not ask "Should I continue?" again; the fix is in.'))
        self.assertNotIn("decision", self.stop("Should I continue?\r\n\r\nNo: everything is committed.") or {})
        self.assertEqual(self.stop("Half of it is in.\r\n\r\nShould I continue?")["decision"], "block")

    def test_SDD_C122_returning_to_a_cached_clear_tree_clears_the_blocker(self):
        self.repo.write(".sdd/config.json", json.dumps({"test": "test ! -f broken"}))
        self.repo.write("app.py", "x = 1\n")
        self.assertEqual(VERIFY.verify(self.root)["exit"], 0)
        self.repo.write("broken", "")
        self.assertEqual(VERIFY.verify(self.root)["exit"], 1)
        (self.root / "broken").unlink()
        self.assertTrue(VERIFY.verify(self.root)["cached"])
        self.assertIsNone(self.stop("Half done. Should I continue?"))


if __name__ == "__main__":
    unittest.main()
