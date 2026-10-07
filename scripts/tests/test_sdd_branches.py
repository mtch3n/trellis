"""A story belongs to a branch: specs/sdd-branches, cases SDB-C1 to SDB-C19."""

import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from sdd_helpers import PLUGIN, Repo, load

STATE = load("state", "spec-driven-development/scripts/state.py")
SHADOW = load("shadow", "spec-driven-development/scripts/shadow.py")
VERIFY = load("verify", "spec-driven-development/scripts/verify.py")
BUILD = PLUGIN / "scripts/build.py"
RED = PLUGIN / "scripts/red.py"

SPEC = """# Pins

Status: {status}

## Decisions
- **PIN-D1** Pins sort first. Why: x. Governs: order.

## Cases
| ID | Covers | Kind | Case |
|---|---|---|---|
| PIN-C1 | PIN-D1 | expected | pinned first |
| PIN-C2 | PIN-D1 | unexpected | missing card |
"""

TEST_C1 = "def test_PIN_C1():\n    assert True\n"
TEST_C2 = "def test_PIN_C2():\n    assert True\n"


class BranchTest(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.tmp = Path(tmp.name)
        (self.tmp / "repo").mkdir()
        self.repo = Repo(self.tmp / "repo")
        self.root = self.repo.root
        self.repo.write("base.txt")
        self.repo.commit("base")

    def approved(self):
        """An approved story PIN on main, with no test yet."""
        self.repo.write(".sdd/config.json", json.dumps({"test": "true"}))
        approval = self.repo.git("rev-parse", "--short", "HEAD").strip()
        self.repo.write("specs/pins.md", SPEC.format(status=f"cases approved 2026-10-07 at {approval}"))
        self.repo.commit("approve pins")

    def worktree(self, branch, *args):
        path = self.tmp / branch.replace("/", "-")
        self.repo.git("worktree", "add", "-q", str(path), *(args or ("-b", branch)))
        return path

    def script(self, script, cwd, *args):
        return subprocess.run([sys.executable, str(script), *args], cwd=cwd,
                              capture_output=True, text=True, timeout=60, check=False)

    def start(self, cwd, key="PIN"):
        return self.script(BUILD, cwd, "start", key)

    def red(self, cwd, case="PIN-C1"):
        return self.script(RED, cwd, case, f"echo FAIL: test_{case.replace('-', '_')}; exit 1")

    def builds(self):
        path = self.root / ".sdd/builds.jsonl"
        if not path.exists():
            return []
        return [(row["story"], row["branch"]) for row in map(json.loads, path.read_text(encoding="utf-8").splitlines())]

    def verify(self, cwd):
        result = VERIFY.verify(cwd)
        by = {severity: [p["detail"] for p in result["problems"] if p["severity"] == severity]
              for severity in ("blocker", "note")}
        return by["blocker"], by["note"]

    def shadow_prompt(self):
        """The prompt a shadow run would send, with no model started."""
        STATE.ensure(self.root)
        real, prompts = subprocess.run, []

        def run(args, **kwargs):
            if args[0] != "claude":
                return real(args, **kwargs)
            prompts.append(args[2])
            return subprocess.CompletedProcess(args, 0, stdout="", stderr="")

        with mock.patch.object(SHADOW.subprocess, "run", run):
            SHADOW.run(self.root, self.root, "tree", "claude")
        return prompts[0]

    def upstream(self):
        remote = self.tmp / "remote.git"
        subprocess.run(["git", "init", "-q", "--bare", str(remote)], check=True)
        self.repo.git("remote", "add", "origin", str(remote))
        self.repo.git("push", "-q", "-u", "origin", "main")


class ChangedTests(BranchTest):
    def test_SDB_C1_a_branch_changes_everything_since_it_left_the_default_branch(self):
        self.repo.git("checkout", "-q", "-b", "feat/pin")
        self.repo.git("checkout", "-q", "main")
        self.repo.write("later-on-main.txt")
        self.repo.commit("main moves on")
        self.repo.git("checkout", "-q", "feat/pin")
        self.repo.write("committed.txt")
        self.repo.commit("work")
        self.repo.write("uncommitted.txt")
        self.assertEqual(STATE.changed_files(self.root), ["committed.txt", "uncommitted.txt"])

    def test_SDB_C2_the_default_branch_changes_what_is_not_pushed(self):
        self.upstream()
        for name in ("one.txt", "two.txt"):
            self.repo.write(name)
            self.repo.commit(name)
        self.repo.write("base.txt", "edited\n")
        self.assertEqual(STATE.changed_files(self.root), ["base.txt", "one.txt", "two.txt"])

    def test_SDB_C3_no_upstream_or_no_merge_base_leaves_uncommitted_work_only(self):
        self.repo.write("committed.txt")
        self.repo.commit("never pushed, and no upstream to compare with")
        self.repo.write("uncommitted.txt")
        self.assertEqual(STATE.changed_files(self.root), ["uncommitted.txt"])

        self.repo.git("add", "-A")
        self.repo.git("commit", "-q", "-m", "clean")
        self.repo.git("checkout", "-q", "--orphan", "lone")
        self.repo.commit("unrelated history")
        self.repo.write("after.txt")
        self.assertEqual(STATE.changed_files(self.root), ["after.txt"])

    def test_SDB_C4_the_shadow_sees_a_commit_made_on_the_default_branch(self):
        self.upstream()
        self.repo.write("pin.py", "def pin():\n    return 'pinned-on-main'\n")
        self.repo.commit("straight to main")
        STATE.ensure(self.root)
        real, prompts = subprocess.run, []

        def run(args, **kwargs):
            if args[0] != "claude":
                return real(args, **kwargs)
            prompts.append(args[2])
            return subprocess.CompletedProcess(args, 0, stdout="", stderr="")

        with mock.patch.object(SHADOW.subprocess, "run", run):
            SHADOW.run(self.root, self.root, "tree", "claude")
        self.assertIn("pinned-on-main", prompts[0])

    def test_SDB_C21_the_shadow_sees_a_new_file_that_is_not_committed(self):
        self.repo.git("checkout", "-q", "-b", "feat/pin")
        self.repo.write("pin.py", "def pin():\n    return 'new-and-untracked'\n")
        self.assertIn("new-and-untracked", self.shadow_prompt())

    def test_a_stale_local_default_branch_does_not_widen_the_change(self):
        self.upstream()
        self.repo.write("pushed.txt")
        self.repo.commit("on main, pushed")
        self.repo.git("push", "-q")
        self.repo.git("checkout", "-q", "-b", "feat/pin")
        self.repo.git("branch", "-f", "main", "HEAD~1")
        self.repo.write("work.txt")
        self.repo.commit("work")
        self.assertEqual(STATE.changed_files(self.root), ["work.txt"])


class StartTests(BranchTest):
    def test_SDB_C5_a_worktree_on_its_own_branch_starts_the_story(self):
        self.approved()
        result = self.start(self.worktree("feat/pin"))
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(self.builds(), [("PIN", "feat/pin")])

    def test_SDB_C6_the_main_checkout_is_refused(self):
        self.approved()
        self.repo.git("checkout", "-q", "-b", "feat/pin")
        result = self.start(self.root)
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn("worktree", result.stdout)
        self.assertEqual(self.builds(), [])

    def test_SDB_C7_the_default_branch_or_a_detached_head_is_refused(self):
        self.approved()
        self.repo.git("checkout", "-q", "-b", "parked")
        for path in (self.worktree("main", "main"), self.worktree("detached", "--detach")):
            result = self.start(path)
            self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
            self.assertIn("refused", result.stdout)
        self.assertEqual(self.builds(), [])

    def test_SDB_C8_an_unknown_or_unapproved_story_is_refused(self):
        self.approved()
        self.repo.write("specs/draft.md", SPEC.format(status="cases pending approval").replace("PIN-", "DRAFT-"))
        self.repo.commit("a draft")
        path = self.worktree("feat/pin")
        unknown, pending = self.start(path, "NOPE"), self.start(path, "DRAFT")
        self.assertEqual((unknown.returncode, pending.returncode), (1, 1))
        self.assertIn("no story", unknown.stdout)
        self.assertIn("not approved", pending.stdout)
        self.assertEqual(self.builds(), [])

    def test_SDB_C9_starting_twice_records_once(self):
        self.approved()
        path = self.worktree("feat/pin")
        self.assertEqual((self.start(path).returncode, self.start(path).returncode), (0, 0))
        self.assertEqual(self.builds(), [("PIN", "feat/pin")])

    def test_SDB_C10_a_second_branch_builds_the_same_story(self):
        self.approved()
        first, second = self.worktree("feat/a"), self.worktree("feat/b")
        self.assertEqual((self.start(first).returncode, self.start(second).returncode), (0, 0))
        self.assertEqual(self.builds(), [("PIN", "feat/a"), ("PIN", "feat/b")])
        self.assertEqual((self.red(first, "PIN-C1").returncode, self.red(second, "PIN-C2").returncode), (0, 0))


class RedTests(BranchTest):
    def record(self, case="PIN-C1"):
        return self.root / f".sdd/red/{case}.json"

    def test_SDB_C11_a_started_story_records_its_red_run(self):
        self.approved()
        path = self.worktree("feat/pin")
        self.assertEqual(self.start(path).returncode, 0)
        result = self.red(path)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(json.loads(self.record().read_text(encoding="utf-8"))["case"], "PIN-C1")

    def test_SDB_C12_a_story_never_started_is_refused(self):
        self.approved()
        result = self.red(self.worktree("feat/pin"))
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn("build.py start PIN", result.stdout)
        self.assertFalse(self.record().exists())

    def test_SDB_C13_a_story_started_on_another_branch_is_refused(self):
        self.approved()
        first, second = self.worktree("feat/a"), self.worktree("feat/b")
        self.assertEqual(self.start(first).returncode, 0)
        result = self.red(second)
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertFalse(self.record().exists())


class VerifyTests(BranchTest):
    def test_SDB_C14_a_story_built_elsewhere_adds_one_note_on_the_default_branch(self):
        self.approved()
        path = self.worktree("feat/pin")
        self.assertEqual(self.start(path).returncode, 0)
        (path / "tests").mkdir()
        (path / "tests/test_pins.py").write_text(TEST_C1 + TEST_C2, encoding="utf-8")
        blockers, notes = self.verify(self.root)
        self.assertEqual(blockers, [])
        self.assertEqual(len([n for n in notes if "specs/pins" in n]), 1, notes)

    def test_SDB_C15_an_untested_case_blocks_on_the_build_branch(self):
        self.approved()
        path = self.worktree("feat/pin")
        self.assertEqual(self.start(path).returncode, 0)
        blockers, _ = self.verify(path)
        self.assertTrue(any("untested: PIN-C2" in b for b in blockers), blockers)

    def test_SDB_C16_a_merged_story_blocks_when_its_test_is_removed(self):
        self.approved()
        self.repo.write("tests/test_pins.py", TEST_C1 + TEST_C2)
        self.repo.commit("the story, merged")
        self.repo.write("tests/test_pins.py", TEST_C1)
        blockers, _ = self.verify(self.root)
        self.assertTrue(any("PIN-C2" in b for b in blockers), blockers)

    def test_SDB_C20_deleting_every_test_of_a_merged_story_still_blocks(self):
        self.approved()
        self.repo.write("tests/test_pins.py", TEST_C1 + TEST_C2)
        self.repo.commit("the story, merged")
        (self.root / "tests/test_pins.py").unlink()
        blockers, _ = self.verify(self.root)
        self.assertTrue(any("PIN-C1" in b for b in blockers), blockers)
        self.assertTrue(any("PIN-C2" in b for b in blockers), blockers)

    def test_SDB_C17_a_story_never_started_adds_a_note_and_no_blocker(self):
        self.approved()
        blockers, notes = self.verify(self.root)
        self.assertEqual(blockers, [])
        self.assertEqual(len([n for n in notes if "specs/pins" in n]), 1, notes)

    def test_SDB_C18_a_damaged_build_record_falls_back_to_the_tests(self):
        self.approved()
        self.repo.write("tests/test_pins.py", TEST_C1)
        self.repo.commit("one test")
        self.repo.write(".sdd/builds.jsonl", "this line is not JSON\n")
        blockers, notes = self.verify(self.root)
        self.assertTrue(any("untested: PIN-C2" in b for b in blockers), blockers)
        self.assertTrue(any("builds.jsonl" in n for n in notes), notes)


class SkillTests(unittest.TestCase):
    def test_SDB_C19_both_build_skills_start_in_a_worktree_before_the_first_red_run(self):
        solo = (PLUGIN / "skills/solo-building-from-cases/SKILL.md").read_text(encoding="utf-8")
        worker = (PLUGIN / "skills/team-workflow/SKILL.md").read_text(encoding="utf-8").split("## Worker", 1)[1]
        for text in (solo, worker):
            self.assertIn("worktree", text)
            self.assertIn("build.py start", text)
            self.assertLess(text.index("worktree"), text.index("build.py start"))
            if "red.py" in text:
                self.assertLess(text.index("build.py start"), text.index("red.py"))


if __name__ == "__main__":
    unittest.main()
