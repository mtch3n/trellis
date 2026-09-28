"""Run with python3 -B -m unittest discover -s scripts/tests -v."""

import json
import subprocess
import sys
import tempfile
import unittest

from sdd_helpers import ROOT, Repo, load

HOOK = load("sdd_hook", "spec-driven-development/hooks/sdd_hook.py")
CHECK = ROOT / "spec-driven-development/scripts/spec_check.py"


class MigrationTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.repo = Repo(self.tmp.name)
        self.repo.write("internal/store/migrations/0012_private.sql")
        self.repo.commit()
        # These cases are about numbered migrations, which only a release writes.
        self.repo.write(".sdd/.gitignore", "*\n")
        self.repo.write(".sdd/release.json", "{}")

    def test_version_taken_on_another_branch_is_a_collision(self):
        # The 2026-09-16 shape: two branches each took 0013.
        self.repo.git("checkout", "-q", "-b", "wip/card-relations")
        self.repo.write("internal/store/migrations/0013_card_relations.sql")
        self.repo.commit()
        self.repo.git("checkout", "-q", "-b", "wip/fix-pins", "main")
        self.repo.write("internal/store/migrations/0013_pins.sql")
        out = HOOK.post_tool(self.repo.edit_event("internal/store/migrations/0013_pins.sql"))
        self.assertEqual(out["decision"], "block")
        self.assertIn("wip/card-relations", out["reason"])
        self.assertIn("0013_card_relations.sql", out["reason"])

    def test_unique_version_and_a_gap_are_silent(self):
        self.repo.write("internal/store/migrations/0016_description.sql")
        self.assertIsNone(HOOK.post_tool(self.repo.edit_event("internal/store/migrations/0016_description.sql")))

    def test_same_migration_on_another_branch_is_not_a_collision(self):
        self.repo.git("checkout", "-q", "-b", "feature")
        self.repo.write("internal/store/migrations/0013_x.sql")
        self.repo.commit()
        self.repo.git("checkout", "-q", "-b", "feature-2")
        self.repo.write("internal/store/migrations/0013_x.sql", "y\n")
        out = HOOK.post_tool(self.repo.edit_event("internal/store/migrations/0013_x.sql"))
        self.assertIsNone(out)

    def test_two_units_with_one_version_in_the_tree(self):
        self.repo.write("internal/store/migrations/0013_a.sql")
        self.repo.write("internal/store/migrations/0013_b.sql")
        out = HOOK.post_tool(self.repo.edit_event("internal/store/migrations/0013_b.sql"))
        self.assertIn("share version 13", out["reason"])

    def test_editing_a_migration_already_on_main(self):
        self.repo.git("checkout", "-q", "-b", "feature")
        self.repo.write("internal/store/migrations/0012_private.sql", "changed\n")
        out = HOOK.post_tool(self.repo.edit_event("internal/store/migrations/0012_private.sql"))
        self.assertIn("already on main", out["reason"])

    def test_prisma_directories_are_units(self):
        self.repo.git("checkout", "-q", "-b", "other")
        self.repo.write("prisma/migrations/20250101000000_a/migration.sql")
        self.repo.commit()
        self.repo.git("checkout", "-q", "main")
        self.repo.write("prisma/migrations/20250101000000_b/migration.sql")
        out = HOOK.post_tool(self.repo.edit_event("prisma/migrations/20250101000000_b/migration.sql"))
        self.assertIn("20250101000000_a", out["reason"])

    def test_configured_globs_share_one_namespace(self):
        self.repo.write(".sdd/config.json", json.dumps({"migrations": ["internal/store/migrations/*", "internal/store/migrate_*.go"]}))
        self.repo.write("internal/store/migrations/0014_a.sql")
        self.repo.write("internal/store/migrate_0014.go")
        out = HOOK.post_tool(self.repo.edit_event("internal/store/migrate_0014.go"))
        self.assertIn("0014_a.sql", out["reason"])

    def test_non_migration_files_are_silent(self):
        self.repo.write("internal/core/card.go")
        self.assertIsNone(HOOK.post_tool(self.repo.edit_event("internal/core/card.go")))

    def test_hash_ids_and_versions_directories_are_not_sequences(self):
        # Alembic revisions are hashes, and a versions/ directory often holds release notes.
        self.repo.write("alembic/versions/27c6a30d7c24_add_users.py")
        self.repo.write("alembic/versions/27f3b1e9a001_add_orders.py")
        self.repo.write("migrations/27c6a30d7c24_a.py")
        self.repo.write("migrations/27f3b1e9a001_b.py")
        self.repo.write("docs/versions/1.2.md")
        self.repo.write("docs/versions/1.3.md")
        for rel in ("alembic/versions/27f3b1e9a001_add_orders.py", "migrations/27f3b1e9a001_b.py",
                    "docs/versions/1.3.md"):
            self.assertIsNone(HOOK.post_tool(self.repo.edit_event(rel)), rel)

    def test_rails_migrate_directory_is_a_sequence(self):
        self.repo.write("db/migrate/20250101000000_a.rb")
        self.repo.write("db/migrate/20250101000000_b.rb")
        out = HOOK.post_tool(self.repo.edit_event("db/migrate/20250101000000_b.rb"))
        self.assertIn("share version", out["reason"])


SPEC = """# Pins

## Decisions
- **PIN-D1** Pins live in a file. Why: simple. Governs: pin.storage.
- **PIN-D2** Pins live in SQLite. Why: queries. Governs: pin.storage. Supersedes: PIN-D1.
- **PIN-D3** Refs are stored. Why: moves. Governs: cards.ref, pin.storage.

## Cases
| ID | Covers | Kind | Case |
|---|---|---|---|
| PIN-C1 | PIN-D2 | expected | pin an entry |
| PIN-C2 | PIN-D2, PIN-D3 | unexpected | pin a missing entry |
"""


class SpecCheckTests(unittest.TestCase):
    def run_check(self, spec, tests):
        with tempfile.TemporaryDirectory() as tmp:
            repo = Repo(tmp)
            repo.write("specs/pins.md", spec)
            for rel, text in tests.items():
                repo.write(rel, text)
            result = subprocess.run([sys.executable, str(CHECK), "specs", "--json"], cwd=tmp,
                                    capture_output=True, text=True, check=False)
            return result.returncode, json.loads(result.stdout)

    def kinds(self, report):
        return sorted(p["kind"] for p in report["problems"])

    def test_reports_facts(self):
        code, report = self.run_check(SPEC, {"pin_test.go": "func TestPin_PIN_C1(t *testing.T) {}\nfunc TestPin_PIN_C9(t *testing.T) {}\n"})
        self.assertEqual(code, 1)
        self.assertEqual(report["cases"], 2)
        # D1 is superseded, so only D2 and D3 are active, and they collide.
        self.assertEqual(self.kinds(report), ["collision", "unknown", "untested"])
        collision = next(p for p in report["problems"] if p["kind"] == "collision")
        self.assertIn("PIN-D2 and PIN-D3", collision["detail"])

    def test_clean_spec_passes(self):
        spec = SPEC.replace(", pin.storage.", ".")
        code, report = self.run_check(spec, {"pin_test.go": "func TestPin_PIN_C1(t *testing.T) {}\nfunc TestPin_PIN_C2(t *testing.T) {}\n"})
        self.assertEqual((code, report["problems"]), (0, []))

    def test_an_open_question_naming_a_decision_is_not_a_definition(self):
        spec = SPEC.replace(", pin.storage.", ".") + "\n## Open\n- PIN-D2 may need an index. Governs: pin.storage.\n"
        code, report = self.run_check(spec, {"pin_test.go": "func TestPin_PIN_C1(t *testing.T) {}\nfunc TestPin_PIN_C2(t *testing.T) {}\n"})
        self.assertEqual((code, report["problems"]), (0, []))

    def test_SDD_C61_an_active_decision_no_case_covers_is_uncovered(self):
        spec = SPEC.replace(", pin.storage.", ".").replace("PIN-D2, PIN-D3", "PIN-D2")
        _, report = self.run_check(spec, {"pin_test.go": "func TestPin_PIN_C1(t *testing.T) {}\nfunc TestPin_PIN_C2(t *testing.T) {}\n"})
        # Under SDD-D55 the uncovered decision also lacks an unexpected case.
        self.assertEqual(self.kinds(report), ["no_unexpected", "uncovered"])
        self.assertTrue(all(p["detail"].startswith("PIN-D3") for p in report["problems"]))

    def test_SDD_C62_covering_an_unknown_decision_is_a_bad_ref(self):
        spec = ("- **X-D1** a. Governs: a. Supersedes: X-D9.\n"
                "| X-C1 | X-D1 | expected | ok |\n"
                "| X-C2 | X-D7 | unexpected | bad input |\n")
        _, report = self.run_check(spec, {"x_test.py": "def test_X_C1():\n    pass\n\ndef test_X_C2():\n    pass\n"})
        self.assertEqual(self.kinds(report), ["bad_ref", "bad_ref", "no_unexpected"])

    def test_SDD_C63_a_segment_without_an_unexpected_case_and_an_unknown_kind(self):
        spec = ("- **X-D1** a. Governs: a.\n"
                "| X-C1 | X-D1 | expected | ok |\n"
                "| X-C2 | X-D1 | happy | old word |\n")
        _, report = self.run_check(spec, {"x_test.py": "def test_X_C1():\n    pass\n\ndef test_X_C2():\n    pass\n"})
        self.assertEqual(self.kinds(report), ["bad_kind", "no_unexpected"])


if __name__ == "__main__":
    unittest.main()
