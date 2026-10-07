"""The shadow the agent must answer: specs/sdd-shadow, cases SDS-C1 to SDS-C27."""

from datetime import datetime, timedelta, timezone
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
ITEMS = load("open_items", "spec-driven-development/scripts/open_items.py")
VERIFY = load("verify", "spec-driven-development/scripts/verify.py")
HOOK = load("sdd_hook", "spec-driven-development/hooks/sdd_hook.py")

CHANGE = "def pin(card):\n    return card.id\n"


class ShadowTest(unittest.TestCase):
    """A repository with a change on branch feat/x, and a fake model for both passes."""

    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        (Path(tmp.name) / "repo").mkdir()
        self.repo = Repo(Path(tmp.name) / "repo")
        self.root = self.repo.root
        self.repo.write("base.txt")
        self.repo.commit("base")
        STATE.ensure(self.root)
        self.repo.git("checkout", "-q", "-b", "feat/x")
        self.repo.write("pin.py", CHANGE)
        self.prompts, self.commands = [], []

    def shadow(self, find="", check="", check_code=0, check_raises=None):
        """Run the shadow; the finder prints find, the second pass prints check."""
        real = subprocess.run

        def run(args, **kwargs):
            if args[0] != "claude":
                return real(args, **kwargs)
            prompt = args[2]
            self.prompts.append(prompt)
            self.commands.append(args)
            if "CONFIRMED <n>" not in prompt:
                return subprocess.CompletedProcess(args, 0, stdout=find, stderr="")
            if check_raises:
                raise check_raises
            return subprocess.CompletedProcess(args, check_code, stdout=check, stderr="second pass broke")

        with mock.patch.object(SHADOW.subprocess, "run", run):
            SHADOW.run(self.root, self.root, "tree-1", "claude")

    def log(self):
        path = self.root / ".sdd/shadow.jsonl"
        return [json.loads(line) for line in path.read_text(encoding="utf-8").splitlines()] if path.exists() else []

    def findings(self):
        return [i for i in ITEMS.open_items(self.root, resolve_answers=False) if i.get("probe") == "shadow"]

    def raise_finding(self, text, branch="feat/x", at=None):
        return ITEMS.capture(self.root, "finding", text, severity="decide", probe="shadow", branch=branch,
                             scenario=f"when {text}", tree="t", now=at)

    def stop(self, active=False):
        return HOOK.stop({"cwd": str(self.root), "last_assistant_message": "I changed pin.py.",
                          "stop_hook_active": active}) or {}

    def cli(self, script, *args):
        return subprocess.run([sys.executable, str(PLUGIN / "scripts" / script), *args], cwd=self.root,
                              capture_output=True, text=True, timeout=60, check=False)


class SecondPassTests(ShadowTest):
    def test_SDS_C1_only_a_confirmed_candidate_goes_further(self):
        self.shadow(find="- pin crashes on None — evidence: `card.id`\n- pin is slow — evidence: `pin`\n",
                    check="CONFIRMED 1: pin(None) raises AttributeError\nREJECTED 2: pin is one attribute read\n")
        self.assertEqual([f["text"] for f in self.findings()], ["pin crashes on None — evidence: `card.id`"])
        answers = [r["text"] for r in self.log() if r.get("pass") == "check"]
        self.assertEqual(len(answers), 2, self.log())

    def test_SDS_C2_a_failed_second_pass_queues_nothing(self):
        for kwargs in ({"check_code": 1}, {"check_raises": subprocess.TimeoutExpired("claude", 600)}):
            self.shadow(find="- pin crashes on None — evidence: `card.id`\n", **kwargs)
            self.assertEqual(self.findings(), [])
            self.assertTrue(self.log()[-1].get("skipped"), self.log())

    def test_SDS_C3_an_unanswered_candidate_is_not_confirmed(self):
        self.shadow(find="- one — evidence: `a`\n- two — evidence: `b`\n- three — evidence: `c`\n",
                    check="CONFIRMED 1: one breaks\nI think 2 is real too\n")
        self.assertEqual([f["text"] for f in self.findings()], ["one — evidence: `a`"])

    def test_SDS_C4_no_candidate_starts_no_second_pass(self):
        self.shadow(find="")
        self.assertEqual(len(self.prompts), 1)
        self.assertEqual(self.findings(), [])


class CheckPassTests(ShadowTest):
    """Shadow findings f-152765dd and f-5f22fa23, raised by the shadow on its own build."""

    def test_the_second_pass_can_only_read(self):
        self.shadow(find="- one — evidence: `a`\n", check="REJECTED 1: no\n")
        command = self.commands[1]
        self.assertEqual(command[command.index("--tools") + 1], "Read,Grep,Glob")
        self.assertIn("--strict-mcp-config", command)
        self.assertNotIn("--allowedTools", command)

    def test_a_claim_both_confirmed_and_rejected_is_not_confirmed(self):
        self.shadow(find="- one — evidence: `a`\n", check="CONFIRMED 1: it breaks\nREJECTED 1: it does not\n")
        self.assertEqual(self.findings(), [])


class QueueTests(ShadowTest):
    CONFIRM = {"find": "- pin crashes on None — evidence: `card.id`\n",
               "check": "CONFIRMED 1: pin(None) raises AttributeError\n"}

    def test_SDS_C5_a_confirmed_candidate_is_a_decide_finding(self):
        self.shadow(**self.CONFIRM)
        [finding] = self.findings()
        self.assertEqual((finding["kind"], finding["severity"], finding["branch"], finding["tree"]),
                         ("finding", "decide", "feat/x", "tree-1"))
        self.assertIn("pin(None) raises AttributeError", finding["scenario"])

    def test_SDS_C6_verify_neither_fails_on_nor_resolves_a_shadow_finding(self):
        self.repo.write(".sdd/config.json", json.dumps({"test": "true"}))
        self.repo.commit("config")
        self.raise_finding("pin crashes on None")
        result = VERIFY.verify(self.root)
        self.assertEqual(result["exit"], 0, result["problems"])
        self.assertEqual(len(self.findings()), 1)

    def test_SDS_C7_the_same_confirmed_text_stays_one_item(self):
        self.shadow(**self.CONFIRM)
        self.shadow(**self.CONFIRM)
        self.assertEqual(len(self.findings()), 1)

    def test_SDS_C8_a_dismissed_finding_is_not_opened_again(self):
        self.shadow(**self.CONFIRM)
        ITEMS.resolve(self.root, self.findings()[0]["id"], "dismiss", note="pin is never given None")
        self.shadow(**self.CONFIRM)
        self.assertEqual(self.findings(), [])


class AskTests(ShadowTest):
    def test_SDS_C9_an_open_finding_holds_the_stop_once_with_how_to_resolve_it(self):
        iid = self.raise_finding("pin crashes on None")
        out = self.stop()
        self.assertEqual(out.get("decision"), "block", out)
        for text in (iid, "pin crashes on None", "--how fix", "--how dismiss", "--note"):
            self.assertIn(text, out["reason"])

    def test_SDS_C10_a_finding_already_asked_about_does_not_hold_again(self):
        self.raise_finding("pin crashes on None")
        self.assertEqual(self.stop().get("decision"), "block")
        self.assertNotIn("decision", self.stop())

    def test_SDS_C11_another_branchs_finding_waits_for_its_own_branch(self):
        self.raise_finding("pin crashes on None", branch="feat/other")
        self.assertNotIn("decision", self.stop())
        self.repo.git("checkout", "-q", "-b", "feat/other")
        self.assertEqual(self.stop().get("decision"), "block")

    def test_SDS_C12_a_stop_already_held_is_not_asked_and_the_next_one_is(self):
        self.raise_finding("pin crashes on None")
        self.assertNotIn("decision", self.stop(active=True))
        self.assertEqual(self.stop().get("decision"), "block")

    def test_SDS_C13_dismissing_without_a_note_is_refused(self):
        iid = self.raise_finding("pin crashes on None")
        result = self.cli("open_items.py", "resolve", iid, "--how", "dismiss")
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn("note", result.stderr)
        self.assertEqual(len(self.findings()), 1)

    def test_SDS_C14_dismissing_with_a_note_resolves_and_keeps_it(self):
        iid = self.raise_finding("pin crashes on None")
        result = self.cli("open_items.py", "resolve", iid, "--how", "dismiss", "--note", "never given None")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(self.findings(), [])
        events = [json.loads(l) for l in (self.root / ".sdd/queue.jsonl").read_text(encoding="utf-8").splitlines()]
        self.assertEqual([e.get("note") for e in events if e["event"] == "resolved"], ["never given None"])


class RatingTests(ShadowTest):
    def test_SDS_C15_each_resolution_is_shown_to_the_user_once(self):
        fixed, dismissed = self.raise_finding("pin crashes on None"), self.raise_finding("pin is slow")
        ITEMS.resolve(self.root, fixed, "fix")
        ITEMS.resolve(self.root, dismissed, "dismiss", note="one attribute read")
        message = self.stop().get("systemMessage", "")
        for text in ("pin crashes on None", "fixed", "pin is slow", "dismissed", "one attribute read"):
            self.assertIn(text, message)
        self.assertNotIn("systemMessage", self.stop())

    def test_SDS_C16_score_counts_fixed_dismissed_and_open_across_the_archive(self):
        ITEMS.resolve(self.root, self.raise_finding("pin crashes on None"), "fix")
        ITEMS.resolve(self.root, self.raise_finding("pin is slow"), "dismiss", note="one read")
        ITEMS.compact(self.root)
        self.raise_finding("pin forgets the board")
        result = self.cli("shadow.py", "score")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout.split(), ["fixed", "1", "dismissed", "1", "open", "1"])

    def test_SDS_C17_mark_is_gone(self):
        result = self.cli("shadow.py", "mark", "1", "useful")
        self.assertEqual(result.returncode, 2)
        self.assertIn("shadow.py", result.stderr)
        self.assertFalse((self.root / ".sdd/shadow-marks.jsonl").exists())


class RepeatAndEmptyTests(ShadowTest):
    def test_SDS_C18_earlier_findings_open_or_resolved_are_listed_not_to_repeat(self):
        ITEMS.resolve(self.root, self.raise_finding("pin is slow"), "dismiss", note="one read")
        self.raise_finding("pin crashes on None")
        self.shadow(find="")
        prompt = self.prompts[0]
        self.assertIn("do not repeat", prompt.lower())
        self.assertIn("pin is slow", prompt)
        self.assertIn("pin crashes on None", prompt)

    def test_SDS_C19_only_the_newest_40_earlier_findings_are_listed(self):
        start = datetime(2026, 10, 1, tzinfo=timezone.utc)
        for n in range(45):
            self.raise_finding(f"earlier finding [{n:02d}]", at=start + timedelta(minutes=n))
        self.shadow(find="")
        prompt = self.prompts[0]
        self.assertIn("earlier finding [44]", prompt)
        self.assertIn("earlier finding [05]", prompt)
        self.assertNotIn("earlier finding [04]", prompt)
        self.assertNotIn("earlier finding [00]", prompt)

    def test_SDS_C20_nothing_changed_starts_no_model(self):
        (self.root / "pin.py").unlink()
        self.repo.write(".sdd/config.json", json.dumps({"test": "true"}))
        self.repo.commit("config")
        self.repo.git("checkout", "-q", "main")
        self.repo.git("merge", "-q", "feat/x")
        result = VERIFY.verify(self.root)
        self.assertEqual((result["exit"], result["shadow"]), (0, "nothing changed"))
        self.assertEqual(self.log(), [])

    def test_SDS_C21_a_failing_git_is_reported_not_taken_as_no_change(self):
        real = SHADOW.state.git

        def git(args, cwd, timeout=10):
            return None if args[0] == "diff" else real(args, cwd, timeout)

        with mock.patch.object(SHADOW.state, "git", git):
            self.shadow(find="- pin crashes — evidence: `x`\n")
        self.assertEqual(self.prompts, [])
        self.assertIn("diff", self.log()[-1].get("skipped", ""), self.log())
        self.assertEqual(self.findings(), [])

    def test_SDS_C22_a_repository_with_no_commit_lists_its_new_file(self):
        empty = self.root.parent / "empty"
        empty.mkdir()
        fresh = Repo(empty)
        fresh.write("first.txt")
        self.assertEqual(STATE.changed_files(empty), ["first.txt"])


class RepliedTests(ShadowTest):
    def prompt(self, session, root=None):
        return HOOK.user_prompt({"cwd": str(root or self.root), "session_id": session, "prompt": "yes"})

    def questions(self):
        return [i for i in ITEMS.open_items(self.root, resolve_answers=False) if i["kind"] == "question"]

    def test_SDS_C23_the_next_message_in_the_session_resolves_its_questions(self):
        iid = ITEMS.capture(self.root, "question", "Which board?", session="s1")
        self.assertIsNone(self.prompt("s1"))
        self.assertEqual(self.questions(), [])
        events = [json.loads(l) for l in (self.root / ".sdd/queue.jsonl").read_text(encoding="utf-8").splitlines()]
        self.assertIn({"id": iid, "how": "replied"}, [{"id": e["id"], "how": e.get("how")} for e in events
                                                      if e["event"] == "resolved"])

    def test_SDS_C24_a_message_in_another_session_leaves_it_open(self):
        ITEMS.capture(self.root, "question", "Which board?", session="s1")
        self.prompt("s2")
        self.assertEqual(len(self.questions()), 1)

    def test_SDS_C25_a_replied_question_asked_again_opens_again(self):
        ITEMS.capture(self.root, "question", "Which board?", session="s1")
        self.prompt("s1")
        ITEMS.capture(self.root, "question", "Which board?", session="s3")
        self.assertEqual(len(self.questions()), 1)

    def test_SDS_C26_a_repository_without_sdd_is_left_alone(self):
        bare = self.root.parent / "bare"
        bare.mkdir()
        Repo(bare)
        self.assertIsNone(self.prompt("s1", bare))
        self.assertFalse((bare / ".sdd").exists())


class PluginTests(unittest.TestCase):
    def test_SDS_C27_the_hook_is_registered_and_the_skill_says_how_to_answer(self):
        hooks = json.loads((PLUGIN / "hooks/hooks.json").read_text(encoding="utf-8"))["hooks"]
        commands = [h["command"] for entry in hooks.get("UserPromptSubmit", []) for h in entry["hooks"]]
        self.assertTrue(any("sdd_hook.py" in c and "user-prompt" in c for c in commands), commands)
        skill = (PLUGIN / "skills/verifying-before-done/SKILL.md").read_text(encoding="utf-8")
        for text in ("open_items.py resolve", "--how fix", "--how dismiss"):
            self.assertIn(text, skill)


if __name__ == "__main__":
    unittest.main()
