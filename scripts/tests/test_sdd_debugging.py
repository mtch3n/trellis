"""repro.py: specs/sdd/debugging, cases SDD-C44 to SDD-C49."""

import json
import subprocess
import sys
import tempfile
import unittest

from sdd_helpers import PLUGIN, Repo

REPRO = PLUGIN / "scripts/repro.py"


class ReproTest(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.repo = Repo(tmp.name)
        self.root = self.repo.root
        self.repo.write("calc.py", "def add(a, b):\n    return a + b\n")
        self.repo.commit("good")

    def repro(self, *args):
        return subprocess.run([sys.executable, str(REPRO), *args], cwd=self.root,
                              capture_output=True, text=True, timeout=120, check=False)

    def loop(self):
        return f"{sys.executable} -B -c \"import calc; assert calc.add(2, 2) == 4, 'add is wrong'\""

    def break_it(self, op="-"):
        self.repo.write("calc.py", f"def add(a, b):\n    return a {op} b\n")

    def record(self):
        result = self.repro("record", self.loop())
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        return result.stdout.split()[1]


class RecordTests(ReproTest):
    def test_SDD_C44_a_red_command_is_registered(self):
        self.break_it()
        loop_id = self.record()
        saved = json.loads((self.root / ".sdd/repro" / f"{loop_id}.json").read_text(encoding="utf-8"))
        self.assertEqual(saved["command"], self.loop())
        self.assertEqual(saved["commit"], self.repo.git("rev-parse", "HEAD").strip())
        self.assertIn("add is wrong", saved["tail"])
        self.assertIn("at", saved)

    def test_SDD_C45_a_green_command_is_refused(self):
        result = self.repro("record", self.loop())
        self.assertEqual(result.returncode, 1)
        self.assertIn("not red", result.stdout + result.stderr)
        self.assertFalse((self.root / ".sdd/repro").exists())

    def test_SDD_C46_a_green_run_closes_the_loop(self):
        self.break_it()
        loop_id = self.record()
        self.break_it("+")
        result = self.repro("run", loop_id)
        self.assertEqual(result.returncode, 0)
        self.assertIn("green", result.stdout)
        saved = json.loads((self.root / ".sdd/repro" / f"{loop_id}.json").read_text(encoding="utf-8"))
        self.assertTrue(saved["closed"])

    def test_SDD_C47_three_red_trees_stop_but_reruns_do_not_count(self):
        self.break_it()
        loop_id = self.record()
        outputs = []
        for op in ("%", "%", "/", "//"):
            self.break_it(op)
            outputs.append(self.repro("run", loop_id).stdout)
        self.assertNotIn("STOP", outputs[1])
        self.assertIn("STOP", outputs[3])
        self.assertIn("ask the user", outputs[3])


class BisectTests(ReproTest):
    def history(self):
        for n in range(3):
            self.repo.write(f"note{n}.txt")
            self.repo.commit(f"fine {n}")
        self.break_it()
        self.repo.commit("the bad one")
        bad = self.repo.git("rev-parse", "HEAD").strip()
        for n in range(2):
            self.repo.write(f"later{n}.txt")
            self.repo.commit(f"later {n}")
        return bad

    def test_SDD_C48_bisect_names_the_bad_commit(self):
        good = self.repo.git("rev-parse", "HEAD").strip()
        bad = self.history()
        loop_id = self.record()
        result = self.repro("bisect", loop_id, good)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn(bad, result.stdout)
        self.assertEqual(self.repo.git("rev-parse", "HEAD").strip(), self.repo.git("rev-parse", "main").strip())

    def test_SDD_C49_a_red_good_ref_is_refused(self):
        self.history()
        loop_id = self.record()
        result = self.repro("bisect", loop_id, "HEAD~1")
        self.assertEqual(result.returncode, 1)
        self.assertIn("red at HEAD~1", result.stdout + result.stderr)

    def test_SDD_C75_bisect_refuses_untracked_files(self):
        good = self.repo.git("rev-parse", "HEAD").strip()
        self.history()
        loop_id = self.record()
        self.repo.write("fixture.json", "{}")
        result = self.repro("bisect", loop_id, good)
        self.assertEqual(result.returncode, 1)
        self.assertIn("fixture.json", result.stdout)


if __name__ == "__main__":
    unittest.main()
