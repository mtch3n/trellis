"""Plugin shape and flow: specs/sdd (SDD-C1 to SDD-C5) and specs/sdd/flow (SDD-C6 to SDD-C11).

SDD-C9 to SDD-C11 run a real Haiku session and cost money, so they run only
with SDD_EVAL=1.
"""

import json
import os
import pathlib
import re
import shutil
import stat
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

from sdd_helpers import PLUGIN, ROOT, Repo, load

HOOK = load("sdd_hook", "spec-driven-development/hooks/sdd_hook.py")
STORE = PLUGIN / "scripts/store.py"
SKILLS = PLUGIN / "skills"
FLOW = {
    "writing-spec": ["/sdd:solo-building-from-cases", "/sdd:team-workflow"],
    "solo-building-from-cases": ["/sdd:verifying-before-done"],
    "team-workflow": ["/sdd:verifying-before-done"],
    "systematic-debugging": ["/sdd:verifying-before-done"],
    "verifying-before-done": ["/sdd:triaging-open-items"],
    "triaging-open-items": [],
    "releasing-migrations": [],
}
USER_ONLY = {"triaging-open-items", "releasing-migrations"}


def frontmatter(skill):
    text = (SKILLS / skill / "SKILL.md").read_text(encoding="utf-8")
    head = text.split("---", 2)[1]
    return dict(line.split(":", 1) for line in head.strip().splitlines() if ":" in line), text


class PluginTests(unittest.TestCase):
    @unittest.skipUnless(shutil.which("claude"), "needs the claude CLI")
    def test_SDD_C1_plugin_and_marketplace_validate(self):
        for target in (PLUGIN, ROOT):
            result = subprocess.run(["claude", "plugin", "validate", str(target)], capture_output=True,
                                    text=True, timeout=60, check=False)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        market = json.loads((ROOT / ".claude-plugin/marketplace.json").read_text(encoding="utf-8"))
        self.assertEqual([p["name"] for p in market["plugins"]], ["trellis", "sdd"])

    def test_SDD_C2_hooks_with_nothing_to_say_are_silent(self):
        with tempfile.TemporaryDirectory() as tmp:
            repo = Repo(tmp)
            repo.write("src/app.py")
            cwd = str(repo.root)
            self.assertIsNone(HOOK.post_tool(repo.edit_event("src/app.py")))
            self.assertIsNone(HOOK.session_end({"cwd": cwd, "session_id": "s"}))
            self.assertIsNone(HOOK.stop({"cwd": cwd, "last_assistant_message": "Done."}))
            self.assertIsNone(HOOK.session_start({"cwd": cwd, "source": "startup"}))
            self.assertFalse((repo.root / ".sdd").exists())


    def test_SDD_C64_no_hook_runs_the_test_command(self):
        with tempfile.TemporaryDirectory() as tmp:
            repo = Repo(tmp)
            marker = pathlib.Path(tmp).parent / (pathlib.Path(tmp).name + ".ran")
            repo.write(".sdd/config.json", json.dumps({"test": f"touch {marker}"}))
            repo.write("db/migrations/draft/a.sql", "SELECT 1;\n")
            cwd = str(repo.root)
            HOOK.post_tool(repo.edit_event("db/migrations/draft/a.sql"))
            HOOK.stop({"cwd": cwd, "last_assistant_message": "All tests pass. Q1. Next? — recommended: stop"})
            HOOK.session_end({"cwd": cwd, "session_id": "s"})
            HOOK.session_start({"cwd": cwd, "source": "startup"})
            self.assertFalse(marker.exists())


class StoreTests(unittest.TestCase):
    """store.py against a stand-in trellis, so no test touches a real Trellis home."""

    def run_store(self, answer):
        with tempfile.TemporaryDirectory() as tmp:
            bin_dir = pathlib.Path(tmp) / "bin"
            bin_dir.mkdir()
            fake = bin_dir / "trellis"
            fake.write_text(answer, encoding="utf-8")
            fake.chmod(fake.stat().st_mode | stat.S_IEXEC)
            (pathlib.Path(tmp) / "repo").mkdir()
            repo = Repo(pathlib.Path(tmp) / "repo")
            with mock.patch.dict(os.environ, {"PATH": f"{bin_dir}{os.pathsep}{os.environ['PATH']}"}):
                return subprocess.run([sys.executable, str(STORE)], cwd=repo.root, capture_output=True,
                                      text=True, timeout=30, check=False).stdout

    def test_SDD_C3_no_marker_means_files_even_with_trellis_installed(self):
        out = self.run_store("#!/bin/sh\necho 'no .trellis marker' >&2\nexit 3\n")
        self.assertIn("store: files", out)

    def test_SDD_C4_a_trellis_project_with_no_specs(self):
        out = self.run_store("#!/bin/sh\necho '{\"entries\":[]}'\n")
        self.assertEqual(out.splitlines(), ["store: trellis", "specs: none"])


class VocabularyTests(unittest.TestCase):
    @unittest.skipUnless(shutil.which("go"), "needs go")
    def test_SDD_C5_no_sdd_file_trips_the_vocabulary_test(self):
        result = subprocess.run(["go", "test", "./internal/vocabulary"], cwd=ROOT, capture_output=True,
                                text=True, timeout=300, check=False)
        hits = [line for line in result.stdout.splitlines()
                if re.search(r"spec-driven-development/|scripts/tests/(test_sdd|sdd_helpers)", line)]
        self.assertEqual(hits, [])


class SkillTests(unittest.TestCase):
    def test_SDD_C6_exactly_the_seven_skills(self):
        present = sorted(p.name for p in SKILLS.iterdir() if p.is_dir())
        self.assertEqual(present, sorted(FLOW))
        for skill in FLOW:
            self.assertEqual(frontmatter(skill)[0]["name"].strip(), skill)

    def test_SDD_C7_each_skill_ends_by_naming_its_successor(self):
        for skill, successors in FLOW.items():
            text = frontmatter(skill)[1]
            ending = text[text.rindex("\n## "):]
            for successor in successors:
                self.assertIn(successor, ending, f"{skill} does not hand off to {successor}")

    def skill_text(self, skill):
        return frontmatter(skill)[1]

    def test_SDD_C65_team_workflow_needs_trellis_and_scopes_workers(self):
        text = self.skill_text("team-workflow")
        self.assertIn("store: trellis", text)
        self.assertIn("migration", text.lower())
        self.assertRegex(text, r"Run only the card's tests")

    def test_SDD_C66_writing_spec_routes_first(self):
        text = self.skill_text("writing-spec")
        route = text[text.index("## 0. Route the request"):text.index("## 1.")]
        for word in ("No spec needed", "Extend", "New", "Split"):
            self.assertIn(word, route)

    def test_SDD_C67_writing_cases_walks_the_unexpected_list(self):
        text = self.skill_text("writing-spec")
        for item in ("bad input", "missing data", "permissions", "concurrency", "interruption and retry",
                     "upgrade from the current schema", "| ID | Covers | Kind | Case |"):
            self.assertIn(item, text)

    def test_SDD_C69_verifying_maps_claims_to_probes(self):
        text = self.skill_text("verifying-before-done")
        for claim in ("| Tests pass |", "| The bug is fixed |", "| The migration is safe |"):
            self.assertIn(claim, text)

    def test_SDD_C70_debugging_registers_a_loop_before_hypotheses(self):
        text = self.skill_text("systematic-debugging")
        self.assertLess(text.index("repro.py record"), text.index("## 3. Hypotheses"))
        self.assertIn("three to five", text)
        self.assertIn("[DEBUG-", text)
        self.assertIn("new unexpected case", text)

    def test_SDD_C8_only_triage_and_release_are_user_only(self):
        for skill in FLOW:
            flag = frontmatter(skill)[0].get("disable-model-invocation", "false").strip()
            self.assertEqual(flag == "true", skill in USER_ONLY, skill)


EVAL_SPEC = """# Pins

Intent: pin a card to the top of its column.

## Decisions
- **PIN-D1** A pin is a boolean on the card. Why: one pin per card. Governs: cards.pinned.
- **PIN-D2** Pinned cards sort first, then by position. Why: stable order. Governs: board.order.
- **PIN-D3** Pinning a card that does not exist returns not-found. Why: exit codes. Governs: pin.errors.
"""


@unittest.skipUnless(os.environ.get("SDD_EVAL") == "1" and shutil.which("claude"), "a paid Haiku run; set SDD_EVAL=1")
class HaikuEvals(unittest.TestCase):
    def session(self, root, prompt, resume=None):
        args = ["claude", "-p", prompt, "--model", "haiku", "--output-format", "json",
                "--plugin-dir", str(PLUGIN), "--permission-mode", "acceptEdits",
                "--allowedTools", "Bash,Read,Write,Edit,Skill,Glob,Grep"]
        if resume:
            args += ["--resume", resume]
        env = {k: v for k, v in os.environ.items() if k != "SDD_CLAUDE"}
        result = subprocess.run(args, cwd=root, capture_output=True, text=True, timeout=900, check=False, env=env)
        data = json.loads(result.stdout)
        return data["result"], data["session_id"]

    def repo(self, tmp):
        repo = Repo(tmp)
        repo.write("specs/pins.md", EVAL_SPEC)
        repo.commit("spec")
        return repo

    def test_SDD_C9_writing_cases_asks_approval_and_solo_or_team(self):
        with tempfile.TemporaryDirectory() as tmp:
            repo = self.repo(tmp)
            repo.write(".trellis", "/PINS\n")
            fake = pathlib.Path(tmp) / "bin"
            fake.mkdir()
            entry = {"slug": "specs/pins", "ref": "/PINS/vault/specs/pins", "path": str(repo.root / "specs/pins.md")}
            (fake / "trellis").write_text("#!/bin/sh\necho '%s'\n" % json.dumps({"entries": [entry]}), encoding="utf-8")
            (fake / "trellis").chmod(0o755)
            with mock.patch.dict(os.environ, {"PATH": f"{fake}{os.pathsep}{os.environ['PATH']}"}):
                reply, _ = self.session(repo.root, "/sdd:writing-spec the story in specs/pins.md. "
                                                   "Its decisions are settled; write the cases.")
        self.assertRegex(reply, r"PIN-C\d")
        self.assertRegex(reply.lower(), r"approv")
        self.assertRegex(reply.lower(), r"solo")
        self.assertRegex(reply.lower(), r"team")

    def test_SDD_C10_without_trellis_team_is_not_offered(self):
        with tempfile.TemporaryDirectory() as tmp:
            repo = self.repo(tmp)
            reply, _ = self.session(repo.root, "/sdd:writing-spec the story in specs/pins.md. "
                                               "Its decisions are settled; write the cases.")
        self.assertRegex(reply, r"PIN-C\d")
        self.assertIn("trellis", reply.lower())
        self.assertNotRegex(reply.lower(), r"team:\s|choose team|or team")

    def test_SDD_C11_all_as_recommended_writes_every_decision(self):
        with tempfile.TemporaryDirectory() as tmp:
            repo = Repo(tmp)
            repo.write("app.py", "def pin(card):\n    pass\n")
            repo.commit("app")
            reply, session = self.session(repo.root, "/sdd:writing-spec I want to archive cards "
                                                     "instead of deleting them. Small mode.")
            asked = re.findall(r"(?m)^\s*\**Q\d+[.)]", reply)
            self.assertTrue(asked, reply)
            self.session(repo.root, "All as recommended.", resume=session)
            specs = list((repo.root / "specs").rglob("*.md"))
            self.assertTrue(specs)
            decisions = re.findall(r"-\s*\**[A-Z]+-D\d+", "".join(p.read_text(encoding="utf-8") for p in specs))
            self.assertGreaterEqual(len(decisions), len(asked))


if __name__ == "__main__":
    unittest.main()
