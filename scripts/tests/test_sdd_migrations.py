"""Draft migrations and the merge at release: specs/sdd/migrations, SDD-C35 to SDD-C43."""

import json
import subprocess
import sys
import tempfile
import unittest

from sdd_helpers import PLUGIN, Repo, load

HOOK = load("sdd_hook", "spec-driven-development/hooks/sdd_hook.py")
RELEASE = PLUGIN / "scripts/release.py"
CHECK = PLUGIN / "scripts/release_check.py"
VERIFY = load("verify", "spec-driven-development/scripts/verify.py")

DIR = "db/migrations"


class MigrationFlowTest(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.repo = Repo(tmp.name)
        self.root = self.repo.root
        self.repo.write(f"{DIR}/0015_cards.sql", "CREATE TABLE cards (id INTEGER);\n")
        self.repo.write(f"{DIR}/0016_pins.sql", "CREATE TABLE pins (id INTEGER);\n")
        self.repo.commit("released")

    def hook(self, rel):
        return HOOK.post_tool(self.repo.edit_event(rel))

    def run_script(self, script, *args):
        return subprocess.run([sys.executable, str(script), *args], cwd=self.root,
                              capture_output=True, text=True, timeout=60, check=False)

    def listing(self):
        return sorted(p.relative_to(self.root).as_posix() for p in (self.root / DIR).rglob("*") if p.is_file())


class HookTests(MigrationFlowTest):
    def test_SDD_C35_writing_a_draft_is_silent(self):
        self.repo.write(f"{DIR}/draft/labels.sql", "ALTER TABLE cards ADD label TEXT;\n")
        self.assertIsNone(self.hook(f"{DIR}/draft/labels.sql"))

    def test_SDD_C36_a_new_numbered_migration_outside_release_is_blocked(self):
        self.repo.write(f"{DIR}/0017_labels.sql", "ALTER TABLE cards ADD label TEXT;\n")
        out = self.hook(f"{DIR}/0017_labels.sql")
        self.assertEqual(out["decision"], "block")
        self.assertIn(f"{DIR}/draft/", out["reason"])

    def test_SDD_C37_release_mode_allows_numbered_and_still_checks_collisions(self):
        self.repo.write(".sdd/.gitignore", "*\n")
        self.repo.write(".sdd/release.json", "{}")
        self.repo.write(f"{DIR}/0017_release.sql", "SELECT 1;\n")
        self.assertIsNone(self.hook(f"{DIR}/0017_release.sql"))
        self.repo.write(f"{DIR}/0016_again.sql", "SELECT 1;\n")
        self.assertIn("share version 16", self.hook(f"{DIR}/0016_again.sql")["reason"])

    def test_SDD_C38_the_same_draft_slug_on_another_branch_is_reported(self):
        self.repo.git("checkout", "-q", "-b", "wip/labels")
        self.repo.write(f"{DIR}/draft/labels.sql", "ALTER TABLE cards ADD label TEXT;\n")
        self.repo.commit("labels")
        self.repo.git("checkout", "-q", "-b", "wip/other", "main")
        self.repo.write(f"{DIR}/draft/labels.sql", "ALTER TABLE pins ADD label TEXT;\n")
        out = self.hook(f"{DIR}/draft/labels.sql")
        self.assertIn("wip/labels", out["reason"])


class ReleaseTests(MigrationFlowTest):
    def drafts(self, *names):
        for name in names:
            self.repo.write(f"{DIR}/draft/{name}", f"-- {name}\nSELECT '{name}';\n")
            self.repo.commit(name)

    def test_SDD_C39_drafts_merge_in_commit_order_into_the_next_number(self):
        self.drafts("b_second.sql", "a_first.sql", "c_third.sql")
        result = self.run_script(RELEASE, "apply", "--name", "labels")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.listing(), [f"{DIR}/0015_cards.sql", f"{DIR}/0016_pins.sql", f"{DIR}/0017_labels.sql"])
        merged = (self.root / DIR / "0017_labels.sql").read_text(encoding="utf-8")
        self.assertLess(merged.index("b_second"), merged.index("a_first"))
        self.assertLess(merged.index("a_first"), merged.index("c_third"))
        self.assertFalse((self.root / ".sdd/release.json").exists())

    def test_SDD_C40_a_failing_check_leaves_the_drafts_and_no_numbered_file(self):
        self.drafts("a.sql")
        self.repo.write(".sdd/config.json", json.dumps({"migration_checks": ["echo upgrade broke; exit 4"]}))
        result = self.run_script(RELEASE, "apply")
        self.assertEqual(result.returncode, 1)
        self.assertIn("upgrade broke", result.stdout + result.stderr)
        self.assertEqual(self.listing(), [f"{DIR}/0015_cards.sql", f"{DIR}/0016_pins.sql", f"{DIR}/draft/a.sql"])
        self.assertFalse((self.root / ".sdd/release.json").exists())

    def test_SDD_C71_a_check_that_times_out_also_rolls_back(self):
        self.drafts("a.sql")
        self.repo.write(".sdd/config.json", json.dumps({"migration_checks": ["sleep 5"], "migration_check_timeout": 1}))
        result = self.run_script(RELEASE, "apply")
        self.assertEqual(result.returncode, 1)
        self.assertIn("did not finish", result.stdout)
        self.assertEqual(self.listing(), [f"{DIR}/0015_cards.sql", f"{DIR}/0016_pins.sql", f"{DIR}/draft/a.sql"])
        self.assertFalse((self.root / ".sdd/release.json").exists())

    def test_SDD_C101_release_refuses_a_number_another_branch_took(self):
        self.repo.git("checkout", "-q", "-b", "wip/other")
        self.repo.write(f"{DIR}/0017_other.sql", "SELECT 2;\n")
        self.repo.commit("other took 17")
        self.repo.git("checkout", "-q", "main")
        self.drafts("a.sql")
        result = self.run_script(RELEASE, "apply")
        self.assertEqual(result.returncode, 1)
        self.assertIn("wip/other", result.stdout)
        self.assertEqual(self.listing(), [f"{DIR}/0015_cards.sql", f"{DIR}/0016_pins.sql", f"{DIR}/draft/a.sql"])

    def test_SDD_C117_a_prefixed_go_migration_does_not_break_numbering(self):
        self.repo.write(".sdd/config.json", json.dumps({"migrations": [f"{DIR}/*", "store/migrate_[0-9][0-9][0-9][0-9].go"]}))
        self.repo.write("store/migrate_0017.go", "package store\n")
        self.drafts("labels.sql")
        result = self.run_script(RELEASE, "plan", "--name", "labels")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn(f"{DIR}/0018_labels.sql", result.stdout)

    def test_SDD_C41_release_check_fails_while_a_draft_remains(self):
        self.assertEqual(self.run_script(CHECK).returncode, 0)
        self.drafts("labels.sql")
        result = self.run_script(CHECK)
        self.assertEqual(result.returncode, 1)
        self.assertIn(f"{DIR}/draft/labels.sql", result.stdout)
        self.repo.write(".sdd/config.json", json.dumps({"test": "true"}))
        notes = [p for p in VERIFY.verify(self.root)["problems"] if p["probe"] == "migrations"]
        self.assertEqual([p["severity"] for p in notes], ["note"])

    def test_SDD_C42_no_drafts_is_nothing_to_merge(self):
        result = self.run_script(RELEASE, "apply")
        self.assertEqual(result.returncode, 0)
        self.assertIn("nothing to merge", result.stdout)
        self.assertEqual(self.listing(), [f"{DIR}/0015_cards.sql", f"{DIR}/0016_pins.sql"])

    def test_SDD_C43_a_non_sql_draft_takes_its_own_number(self):
        self.drafts("labels.sql", "backfill.go")
        result = self.run_script(RELEASE, "apply", "--name", "labels")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.listing(), [f"{DIR}/0015_cards.sql", f"{DIR}/0016_pins.sql",
                                          f"{DIR}/0017_labels.sql", f"{DIR}/0018_backfill.go"])

    def test_SDD_C51_plan_marks_a_draft_that_changes_data(self):
        self.repo.write(f"{DIR}/draft/labels.sql", "-- UPDATE nothing here\nALTER TABLE cards ADD label TEXT;\n")
        self.repo.write(f"{DIR}/draft/backfill.sql", "UPDATE cards SET label = 'none';\n")
        self.repo.commit("drafts")
        out = self.run_script(RELEASE, "plan").stdout
        marked = [line for line in out.splitlines() if "changes data" in line]
        self.assertEqual(len(marked), 1, out)
        self.assertIn("backfill.sql", marked[0])


if __name__ == "__main__":
    unittest.main()
