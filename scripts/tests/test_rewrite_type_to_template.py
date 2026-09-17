"""Tests for scripts/rewrite_type_to_template.py."""

import contextlib
import io
import os
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from rewrite_type_to_template import main, rewrite_content  # noqa: E402


class RewriteContentTest(unittest.TestCase):
    def check(self, before, after, old, new):
        got, got_old, got_new = rewrite_content(before)
        self.assertEqual(got, after)
        self.assertEqual((got_old, got_new), (old, new))

    def test_note_is_dropped(self):
        self.check("---\ntitle: A\ntype: note\n---\n# A\n", "---\ntitle: A\n---\n# A\n", "note", None)

    def test_quoted_note_is_dropped(self):
        self.check("---\ntype: 'note'\n---\nbody\n", "---\n---\nbody\n", "note", None)

    def test_other_type_becomes_template(self):
        self.check("---\ntitle: A\ntype: decision\nprivate: true\n---\n\n# A\n",
                   "---\ntitle: A\ntemplate: decision\nprivate: true\n---\n\n# A\n", "decision", "decision")

    def test_existing_template_wins(self):
        self.check("---\ntemplate: finding\ntype: decision\n---\nx\n", "---\ntemplate: finding\n---\nx\n",
                   "decision", None)

    def test_crlf_is_kept(self):
        self.check("---\r\ntitle: A\r\ntype: finding\r\n---\r\nbody\r\n",
                   "---\r\ntitle: A\r\ntemplate: finding\r\n---\r\nbody\r\n", "finding", "finding")

    def test_only_the_top_level_key_changes(self):
        text = "---\ntitle: A\nsteps:\n  type: shell\n---\ntype: prose in the body\n"
        self.check(text, text, None, None)

    def test_no_frontmatter_or_no_type_is_left_alone(self):
        for text in ["", "# Just a body\ntype: x\n", "---\ntitle: A\n---\nbody\n", "---\ntype: x\nno closing line\n"]:
            self.check(text, text, None, None)


class MainTest(unittest.TestCase):
    def setUp(self):
        self.dir = Path(tempfile.mkdtemp())
        # A single project's own directory: knowledge/ beside artifacts/,
        # the layout under ~/.trellis/projects/<KEY> -- the shape that let
        # this script wander into user-uploaded files before it was scoped
        # to knowledge subtrees.
        (self.dir / "knowledge" / "ops").mkdir(parents=True)
        (self.dir / "knowledge" / "ops" / ".rollback.md").mkdir()
        (self.dir / "artifacts").mkdir()
        self.files = {
            self.dir / "knowledge" / "notes.md": "---\ntitle: N\ntype: note\n---\nn\n",
            self.dir / "knowledge" / "ops" / "rollback.md": "---\ntitle: R\ntype: runbook\n---\nr\n",
            self.dir / "knowledge" / "ops" / ".rollback.md" / "1.md": "---\ntitle: R\ntype: runbook\n---\nold\n",
            self.dir / "knowledge" / "plain.md": "---\ntitle: P\n---\np\n",
        }
        # A user-uploaded text artifact that happens to have the same
        # frontmatter shape as a knowledge entry. Trellis never wrote it,
        # and this script must never rewrite it either.
        self.artifact = self.dir / "artifacts" / "notes.md"
        self.artifact_text = "---\ntitle: uploaded\ntype: note\n---\nnot a knowledge entry\n"
        for path, text in self.files.items():
            path.write_text(text)
        self.artifact.write_text(self.artifact_text)

    def run_main(self, *args):
        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            code = main([str(self.dir), *args])
        return code, out.getvalue()

    def test_dry_run_reports_and_writes_nothing(self):
        code, out = self.run_main()
        self.assertEqual(code, 0)
        self.assertIn("type: note -> no template", out)
        self.assertIn("type: runbook -> template: runbook", out)
        self.assertIn("would rewrite 3 file(s)", out)
        self.assertNotIn(str(self.artifact), out)
        for path, text in self.files.items():
            self.assertEqual(path.read_text(), text)
        self.assertEqual(self.artifact.read_text(), self.artifact_text)

    def test_apply_rewrites_revisions_too(self):
        code, _ = self.run_main("--apply")
        self.assertEqual(code, 0)
        self.assertEqual((self.dir / "knowledge" / "notes.md").read_text(), "---\ntitle: N\n---\nn\n")
        self.assertEqual((self.dir / "knowledge" / "ops" / "rollback.md").read_text(),
                         "---\ntitle: R\ntemplate: runbook\n---\nr\n")
        self.assertEqual((self.dir / "knowledge" / "ops" / ".rollback.md" / "1.md").read_text(),
                         "---\ntitle: R\ntemplate: runbook\n---\nold\n")
        self.assertEqual((self.dir / "knowledge" / "plain.md").read_text(), "---\ntitle: P\n---\np\n")
        _, out = self.run_main("--apply")
        self.assertIn("rewrote 0 file(s)", out)

    def test_artifacts_directory_is_never_touched(self):
        code, out = self.run_main("--apply")
        self.assertEqual(code, 0)
        self.assertNotIn(str(self.artifact), out)
        self.assertEqual(self.artifact.read_text(), self.artifact_text)


if __name__ == "__main__":
    unittest.main()
