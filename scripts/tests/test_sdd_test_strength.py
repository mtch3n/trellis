"""Test strength: specs/sdd/test-strength, cases SDD-C76 to SDD-C89."""

import json
import subprocess
import sys
import tempfile
import unittest

from sdd_helpers import PLUGIN, Repo, load

VERIFY = load("verify", "spec-driven-development/scripts/verify.py")
RED = PLUGIN / "scripts/red.py"
CHECK = PLUGIN / "scripts/spec_check.py"

SPEC = """# Pins

Status: cases approved 2026-09-28 at {approval}

## Decisions
- **PIN-D1** Pins sort first. Why: x. Governs: order.

## Cases
| ID | Covers | Kind | Case |
|---|---|---|---|
| PIN-C1 | PIN-D1 | expected | pinned first |
| PIN-C2 | PIN-D1 | unexpected | missing card |
"""

TESTS = "def test_PIN_C1():\n    assert True\n\n\ndef test_PIN_C2():\n    assert True\n"


class StrengthTest(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.repo = Repo(tmp.name)
        self.root = self.repo.root

    def run_script(self, script, *args):
        return subprocess.run([sys.executable, str(script), *args], cwd=self.root,
                              capture_output=True, text=True, timeout=60, check=False)

    def red(self, case, command):
        return self.run_script(RED, case, command)

    def approved(self):
        self.repo.write(".sdd/config.json", json.dumps({"test": "true"}))
        self.repo.write("specs/pins.md", "# Pins\n")
        self.repo.commit("start")
        approval = self.repo.git("rev-parse", "--short", "HEAD").strip()
        self.repo.write("specs/pins.md", SPEC.format(approval=approval))
        self.repo.write("tests/test_pins.py", TESTS)
        self.repo.commit("tests")

    def blockers(self):
        return [p["detail"] for p in VERIFY.verify(self.root)["problems"] if p["severity"] == "blocker"]

    def check(self, spec):
        self.repo.write("specs/pins.md", spec)
        self.repo.write("tests/test_all.py", " ".join(f"PIN_C{n}" for n in range(1, 20)))
        result = self.run_script(CHECK, "specs", "--json")
        return [(p["kind"], p["detail"]) for p in json.loads(result.stdout)["problems"]]


class RedTests(StrengthTest):
    def test_SDD_C76_a_failing_run_naming_the_case_is_recorded(self):
        result = self.red("PIN-C2", "echo PIN_C2 broke; exit 1")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        saved = json.loads((self.root / ".sdd/red/PIN-C2.json").read_text(encoding="utf-8"))
        self.assertEqual(saved["command"], "echo PIN_C2 broke; exit 1")
        self.assertIn("PIN_C2 broke", saved["tail"])
        for key in ("tree", "at"):
            self.assertIn(key, saved)

    def test_SDD_C77_a_passing_run_is_refused(self):
        result = self.red("PIN-C2", "echo PIN_C2; true")
        self.assertEqual(result.returncode, 1)
        self.assertIn("not red", result.stdout)
        self.assertFalse((self.root / ".sdd/red/PIN-C2.json").exists())

    def test_SDD_C78_a_command_that_does_not_name_the_case_is_refused(self):
        result = self.red("PIN-C2", "exit 1")
        self.assertEqual(result.returncode, 1)
        self.assertIn("must name PIN-C2", result.stdout)

    def test_SDD_C79_an_approved_case_without_a_red_run_is_a_blocker(self):
        self.approved()
        blockers = self.blockers()
        self.assertTrue(any("PIN-C1" in b and "red" in b for b in blockers), blockers)
        self.assertTrue(any("PIN-C2" in b and "red" in b for b in blockers), blockers)

    def test_SDD_C80_a_recorded_red_run_clears_the_case(self):
        self.approved()
        self.assertEqual(self.red("PIN-C1", "echo PIN_C1; exit 1").returncode, 0)
        blockers = self.blockers()
        self.assertFalse(any("PIN-C1" in b for b in blockers), blockers)
        self.assertTrue(any("PIN-C2" in b for b in blockers), blockers)

    def test_SDD_C89_tests_that_landed_before_red_since_are_exempt(self):
        self.approved()
        since = self.repo.git("rev-parse", "HEAD").strip()
        self.repo.write(".sdd/config.json", json.dumps({"test": "true", "red_since": since}))
        self.assertEqual([b for b in self.blockers() if "red" in b], [])


class SizeAndCoverageTests(StrengthTest):
    def spec(self, decisions, rows):
        return "# S\n\n## Decisions\n" + decisions + "\n## Cases\n| ID | Covers | Kind | Case |\n|---|---|---|---|\n" + rows

    def test_SDD_C81_a_crowded_story_is_a_note(self):
        decisions = "".join(f"- **PIN-D{n}** d{n}. Governs: g{n}.\n" for n in range(1, 4))
        rows = "".join(f"| PIN-C{n} | PIN-D{n % 3 + 1} | {'unexpected' if n <= 3 else 'expected'} | c |\n"
                       for n in range(1, 14))
        problems = self.check(self.spec(decisions, rows))
        self.assertIn("crowded", [k for k, _ in problems])

    def test_SDD_C90_solo_building_records_guard_cases_by_breaking_the_code(self):
        text = (PLUGIN / "skills/solo-building-from-cases/SKILL.md").read_text(encoding="utf-8")
        self.assertIn("red.py", text)
        self.assertIn("break the code it guards", text)

    def test_SDD_C82_the_approval_message_lists_cases_per_decision(self):
        text = (PLUGIN / "skills/writing-cases/SKILL.md").read_text(encoding="utf-8")
        self.assertIn("per decision", text[text.index("## 4."):])

    def test_SDD_C83_a_decision_without_an_unexpected_case_is_named(self):
        decisions = "- **PIN-D1** a. Governs: a.\n- **PIN-D2** b. Governs: b.\n"
        rows = "| PIN-C1 | PIN-D1 | expected | x |\n| PIN-C2 | PIN-D1 | unexpected | y |\n| PIN-C3 | PIN-D2 | expected | z |\n"
        problems = self.check(self.spec(decisions, rows))
        self.assertEqual([d.split()[0] for k, d in problems if k == "no_unexpected"], ["PIN-D2"])

    def test_SDD_C84_a_reasoned_not_applicable_line_is_accepted(self):
        decisions = "- **PIN-D1** a. Governs: a. Unexpected: n/a — naming only.\n"
        problems = self.check(self.spec(decisions, "| PIN-C1 | PIN-D1 | expected | x |\n"))
        self.assertNotIn("no_unexpected", [k for k, _ in problems])

    def test_SDD_C85_not_applicable_without_a_reason_is_not_enough(self):
        decisions = "- **PIN-D1** a. Governs: a. Unexpected: n/a\n"
        problems = self.check(self.spec(decisions, "| PIN-C1 | PIN-D1 | expected | x |\n"))
        self.assertIn("no_unexpected", [k for k, _ in problems])


class DriftTests(StrengthTest):
    def drift(self):
        return [p["detail"] for p in VERIFY.verify(self.root)["problems"] if p["probe"] == "drift"]

    def governed(self, governs):
        self.repo.write(".sdd/config.json", json.dumps({"test": "true"}))
        self.repo.write("app.go", "package app\n")
        self.repo.write("specs/app.md", "# App\n\n## Decisions\n"
                        f"- **APP-D1** Run once. Governs: {governs}. Unexpected: n/a — a sketch.\n\n"
                        "## Cases\n| ID | Covers | Kind | Case |\n|---|---|---|---|\n| APP-C1 | APP-D1 | expected | x |\n")
        self.repo.write("tests/test_app.py", "APP_C1\n")
        self.repo.commit("spec and code")

    def test_SDD_C86_a_governed_file_changed_after_its_spec_is_drift(self):
        self.governed("app.go")
        self.repo.write("app.go", "package app\n\nfunc Run() {}\n")
        self.repo.commit("code only")
        drift = self.drift()
        self.assertEqual(len(drift), 1, drift)
        self.assertIn("APP-D1", drift[0])
        self.assertIn("app.go", drift[0])

    def test_SDD_C87_a_governs_that_is_not_a_path_is_not_checked(self):
        self.governed("cards.ref")
        self.repo.write("app.go", "package app\n\nfunc Run() {}\n")
        self.repo.commit("code only")
        self.assertEqual(self.drift(), [])

    def test_SDD_C88_a_spec_newer_than_its_file_is_not_drift(self):
        self.governed("app.go")
        self.repo.write("app.go", "package app\n\nfunc Run() {}\n")
        self.repo.commit("code")
        spec = (self.root / "specs/app.md").read_text(encoding="utf-8")
        self.repo.write("specs/app.md", spec + "\n")
        self.repo.commit("spec after")
        self.assertEqual(self.drift(), [])


if __name__ == "__main__":
    unittest.main()
