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


if __name__ == "__main__":
    unittest.main()
