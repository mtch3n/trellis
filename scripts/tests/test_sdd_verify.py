"""verify.py: specs/sdd/verify, cases SDD-C25 to SDD-C34."""

import json
import os
import shutil
import stat
import tempfile
import time
import unittest
from unittest import mock

from sdd_helpers import Repo, load

HOOK = load("sdd_hook", "spec-driven-development/hooks/sdd_hook.py")
VERIFY = load("verify", "spec-driven-development/scripts/verify.py")
ITEMS = load("open_items", "spec-driven-development/scripts/open_items.py")

TAG = "[DEBUG-" + "a4f2]"  # split so this file does not trip the probe it tests

SPEC = """# Pins

{status}

## Decisions
- **PIN-D1** Pins live in SQLite. Why: queries. Governs: pin.storage.

## Cases
| ID | Covers | Kind | Case |
|---|---|---|---|
| PIN-C1 | PIN-D1 | expected | pin an entry |
| PIN-C2 | PIN-D1 | unexpected | pin a missing entry |
"""

TESTS = """def test_PIN_C1_pins():
    assert pin("a") == "a"


def test_PIN_C2_missing():
    assert pin(None) is None
"""


class VerifyTest(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.repo = Repo(tmp.name)
        self.root = self.repo.root
        self.calls = self.root.parent / (self.root.name + ".shadow-calls")
        fake = self.root.parent / (self.root.name + ".claude")
        fake.write_text(f"#!/bin/sh\necho call >> {self.calls}\necho 'Q: is PIN-D1 still true? evidence: x'\n",
                        encoding="utf-8")
        fake.chmod(fake.stat().st_mode | stat.S_IEXEC)
        patcher = mock.patch.dict(os.environ, {"SDD_CLAUDE": str(fake)})
        patcher.start()
        self.addCleanup(patcher.stop)

    def config(self, **values):
        self.repo.write(".sdd/config.json", json.dumps(values))

    def verify(self, wait_shadow=True):
        result = VERIFY.verify(self.root)
        if wait_shadow and result.get("shadow") == "started":
            deadline = time.monotonic() + 10
            while time.monotonic() < deadline and not self.shadow_done():
                time.sleep(0.05)
        return result

    def shadow_done(self):
        rows = self.shadow_rows()
        return any("text" in r or "skipped" in r for r in rows)

    def shadow_rows(self):
        path = self.root / ".sdd/shadow.jsonl"
        return [json.loads(l) for l in path.read_text(encoding="utf-8").splitlines()] if path.exists() else []

    def blockers(self, result):
        return [p["detail"] for p in result["problems"] if p["severity"] == "blocker"]

    def approved_story(self):
        self.repo.write("specs/pins.md", SPEC.format(status="Status: cases pending approval"))
        self.repo.commit("spec")
        approval = self.repo.git("rev-parse", "--short", "HEAD").strip()
        self.repo.write("specs/pins.md", SPEC.format(status=f"Status: cases approved 2026-09-28 at {approval}"))
        self.repo.write("tests/test_pins.py", TESTS)
        self.repo.commit("tests")
        for case in ("PIN-C1", "PIN-C2"):  # SDD-D53: each approved case was seen red
            self.repo.write(f".sdd/red/{case}.json", json.dumps({"case": case}))


class VerifyTests(VerifyTest):
    def test_SDD_C25_all_probes_pass(self):
        self.config(test="true")
        self.approved_story()
        result = self.verify()
        self.assertEqual((result["exit"], self.blockers(result)), (0, []))
        self.assertTrue((self.root / ".sdd/verify" / f"{result['key']}.json").is_file())
        self.assertEqual(self.calls.read_text(encoding="utf-8").count("call"), 1)

    def test_SDD_C26_a_cached_tree_does_not_rerun_the_suite(self):
        counter = self.root.parent / (self.root.name + ".runs")
        self.config(test=f"echo run >> {counter}")
        first = self.verify()
        second = self.verify()
        self.assertEqual(counter.read_text(encoding="utf-8").count("run"), 1)
        self.assertTrue(second["cached"])
        self.assertEqual(first["tree"], second["tree"])
        self.assertEqual(self.calls.read_text(encoding="utf-8").count("call"), 1)

    def test_SDD_C27_a_failing_suite_is_a_blocker_and_no_shadow(self):
        self.config(test="echo 'FAIL: TestPin'; exit 3")
        result = self.verify()
        self.assertEqual(result["exit"], 1)
        self.assertTrue(any("exited 3" in b and "FAIL: TestPin" in b for b in self.blockers(result)))
        self.assertFalse(self.calls.exists())
        findings = [i for i in ITEMS.open_items(self.root) if i["kind"] == "finding"]
        self.assertEqual([f["severity"] for f in findings], ["blocker"])

    def test_SDD_C28_no_test_command_is_a_decide_and_the_rest_still_runs(self):
        self.repo.write("app.py", f"print('x')  # {TAG}\n")
        result = self.verify()
        severities = {p["probe"]: p["severity"] for p in result["problems"]}
        self.assertEqual(severities["test"], "decide")
        self.assertEqual(severities["debug-tags"], "blocker")

    def test_SDD_C29_an_approved_test_changed_after_it_landed_is_a_blocker(self):
        self.config(test="true")
        self.approved_story()
        self.repo.write("tests/test_pins.py", TESTS.replace('pin(None) is None', 'True'))
        result = self.verify()
        self.assertEqual(result["exit"], 1)
        self.assertTrue(any("PIN-C2" in b and "tests/test_pins.py" in b for b in self.blockers(result)))
        self.assertFalse(any("PIN-C1" in b for b in self.blockers(result)))

    def test_SDD_C30_a_new_test_for_an_unapproved_case_is_fine(self):
        self.config(test="true")
        self.approved_story()
        spec = (self.root / "specs/pins.md").read_text(encoding="utf-8")
        self.repo.write("specs/pins.md", spec + "| PIN-C3 | PIN-D1 | expected | pin twice |\n")
        self.repo.write("tests/test_pins.py", TESTS + "\n\ndef test_PIN_C3_new():\n    assert True\n")
        result = self.verify()
        # The locked-test probe is silent; SDD-D53's red record is a separate probe.
        self.assertEqual([p for p in result["problems"] if p["probe"] == "approved-tests"], [])

    def test_SDD_C31_a_leftover_debug_tag_is_a_blocker(self):
        self.config(test="true")
        self.repo.write("pkg/store.py", "x = 1\nprint(x)  # " + TAG + "\n")
        result = self.verify()
        self.assertTrue(any("pkg/store.py:2" in b for b in self.blockers(result)))

    def test_SDD_C32_a_slow_suite_is_a_blocker(self):
        self.config(test="sleep 5", test_timeout=1)
        result = self.verify()
        self.assertTrue(any("did not finish in 1s" in b for b in self.blockers(result)))

    def test_SDD_C33_shadow_lines_reach_the_user_not_the_queue(self):
        self.config(test="true")
        self.verify()
        out = HOOK.stop({"cwd": str(self.root), "last_assistant_message": "Done."})
        self.assertIn("is PIN-D1 still true?", out["systemMessage"])
        self.assertNotIn("decision", out)
        self.assertEqual(ITEMS.open_items(self.root), [])
        self.assertIsNone(HOOK.stop({"cwd": str(self.root), "last_assistant_message": "Done."}))

    def test_SDD_C34_no_claude_skips_the_shadow_and_keeps_the_result(self):
        self.config(test="true")
        with mock.patch.dict(os.environ, {"SDD_CLAUDE": "", "PATH": os.path.dirname(shutil.which("git"))}):
            result = self.verify()
        self.assertEqual((result["exit"], result["shadow"]), (0, "skipped"))
        self.assertIn("claude", self.shadow_rows()[0]["skipped"])

    def test_SDD_C72_verify_in_a_linked_worktree_keeps_state_in_the_main_one(self):
        self.config(test="true")
        self.repo.commit("config")
        linked = self.root.parent / (self.root.name + "-wt")
        self.repo.git("worktree", "add", "-q", "-b", "side", str(linked))
        self.addCleanup(lambda: self.repo.git("worktree", "remove", "--force", str(linked)))
        result = VERIFY.verify(linked)
        self.assertEqual(result["exit"], 0)
        self.assertFalse((linked / ".sdd/.gitignore").exists())
        self.assertFalse((linked / ".sdd/verify").exists())
        self.assertTrue((self.root / ".sdd/verify" / f"{result['key']}.json").is_file())

    def test_SDD_C73_an_uncovered_decision_in_an_approved_story_is_a_blocker(self):
        self.config(test="true")
        self.approved_story()
        spec = (self.root / "specs/pins.md").read_text(encoding="utf-8")
        self.repo.write("specs/pins.md", spec.replace("## Cases", "- **PIN-D2** Pins expire. Why: x. Governs: pin.ttl.\n\n## Cases"))
        result = self.verify()
        self.assertTrue(any("PIN-D2" in b for b in self.blockers(result)))

    def test_SDD_C91_state_outside_the_tree_is_part_of_the_cache_key(self):
        self.config(test="true")
        self.approved_story()
        (self.root / ".sdd/red/PIN-C2.json").unlink()
        self.assertTrue(any("PIN-C2" in b for b in self.blockers(self.verify())))
        self.repo.write(".sdd/red/PIN-C2.json", json.dumps({"case": "PIN-C2"}))
        second = self.verify()
        self.assertFalse(second["cached"])
        self.assertEqual(self.blockers(second), [])

    def test_SDD_C92_a_spec_only_commit_does_not_hide_the_test_it_names(self):
        self.config(test="true")
        self.repo.write("specs/pins.md", SPEC.format(status="Status: cases pending approval"))
        self.repo.commit("spec")
        approval = self.repo.git("rev-parse", "--short", "HEAD").strip()
        self.repo.write("specs/pins.md", SPEC.format(status=f"Status: cases approved 2026-09-28 at {approval}")
                        + "\nNote on PIN-C2 and PIN_C1.\n")
        self.repo.commit("spec mentions the cases first")
        self.repo.write("tests/test_pins.py", TESTS)
        self.repo.commit("tests")
        for case in ("PIN-C1", "PIN-C2"):
            self.repo.write(f".sdd/red/{case}.json", json.dumps({"case": case}))
        self.repo.write("tests/test_pins.py", TESTS.replace('pin(None) is None', 'True'))
        self.assertTrue(any("PIN-C2" in b and "tests/test_pins.py" in b for b in self.blockers(self.verify())))


if __name__ == "__main__":
    unittest.main()
