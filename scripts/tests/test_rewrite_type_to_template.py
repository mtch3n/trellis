#!/usr/bin/env python3
"""Unit tests for rewrite_type_to_template.py"""

import os
import sys
import unittest
import tempfile
from pathlib import Path

# Add scripts directory to path so we can import the module
sys.path.insert(0, os.path.join(os.path.dirname(__file__), '..'))

from rewrite_type_to_template import (
    rewrite_file_content,
    rewrite_frontmatter,
    find_markdown_files,
)


class TestRewriteFrontmatter(unittest.TestCase):
    """Test frontmatter rewriting logic."""

    def test_type_note_removed(self):
        """type: note should be removed."""
        fm = 'title: Test\ntype: note\n'
        result, changed = rewrite_frontmatter(fm)
        self.assertTrue(changed)
        self.assertNotIn('type:', result)
        self.assertNotIn('template:', result)
        self.assertIn('title:', result)

    def test_type_other_becomes_template(self):
        """type: other should become template: other."""
        fm = 'title: Test\ntype: decision\n'
        result, changed = rewrite_frontmatter(fm)
        self.assertTrue(changed)
        self.assertNotIn('type:', result)
        self.assertIn('template: decision', result)

    def test_template_preserved(self):
        """Existing template: should be preserved, type: dropped."""
        fm = 'title: Test\ntemplate: decision\ntype: decision\n'
        result, changed = rewrite_frontmatter(fm)
        self.assertTrue(changed)
        self.assertIn('template: decision', result)
        self.assertNotIn('type:', result)

    def test_no_type_no_change(self):
        """Frontmatter without type: should not change."""
        fm = 'title: Test\ntemplate: decision\n'
        result, changed = rewrite_frontmatter(fm)
        self.assertFalse(changed)
        self.assertEqual(fm, result)

    def test_indentation_preserved(self):
        """Indentation should be preserved in rewritten content."""
        fm = '  title: Test\n  type: decision\n'
        result, changed = rewrite_frontmatter(fm)
        self.assertTrue(changed)
        self.assertIn('  template: decision', result)

    def test_empty_frontmatter(self):
        """Empty frontmatter should not change."""
        fm = ''
        result, changed = rewrite_frontmatter(fm)
        self.assertFalse(changed)


class TestRewriteFileContent(unittest.TestCase):
    """Test file content rewriting."""

    def test_file_with_type_note(self):
        """File with type: note should have it removed."""
        content = '---\ntitle: Test\ntype: note\n---\n\nBody content\n'
        result, changed = rewrite_file_content(content)
        self.assertTrue(changed)
        self.assertNotIn('type:', result)
        self.assertNotIn('template:', result)
        self.assertIn('Body content', result)

    def test_file_with_type_decision(self):
        """File with type: decision should become template: decision."""
        content = '---\ntitle: Test\ntype: decision\n---\n\nBody\n'
        result, changed = rewrite_file_content(content)
        self.assertTrue(changed)
        self.assertIn('template: decision', result)
        self.assertNotIn('type: decision', result)
        self.assertIn('Body', result)

    def test_no_frontmatter(self):
        """File without frontmatter should not change."""
        content = 'Just body content\n'
        result, changed = rewrite_file_content(content)
        self.assertFalse(changed)
        self.assertEqual(content, result)

    def test_empty_file(self):
        """Empty file should not change."""
        content = ''
        result, changed = rewrite_file_content(content)
        self.assertFalse(changed)

    def test_frontmatter_only_type_note_removed(self):
        """Frontmatter with only type: note should result in clean content."""
        content = '---\ntype: note\n---\n\nBody\n'
        result, changed = rewrite_file_content(content)
        self.assertTrue(changed)
        # Should not have empty frontmatter
        self.assertNotIn('---', result)  # Separators removed
        self.assertIn('Body', result)


class TestFindMarkdownFiles(unittest.TestCase):
    """Test markdown file discovery."""

    def test_find_markdown_files(self):
        """Should find all .md files including in hidden directories."""
        with tempfile.TemporaryDirectory() as tmpdir:
            # Create test files
            Path(tmpdir, 'test1.md').touch()
            Path(tmpdir, 'test2.txt').touch()
            subdir = Path(tmpdir, 'subdir')
            subdir.mkdir()
            Path(subdir, 'test3.md').touch()

            hidden = Path(tmpdir, '.hidden')
            hidden.mkdir()
            Path(hidden, 'test4.md').touch()

            files = find_markdown_files(tmpdir)
            file_names = {f.name for f in files}

            self.assertIn('test1.md', file_names)
            self.assertIn('test3.md', file_names)
            self.assertIn('test4.md', file_names)
            self.assertNotIn('test2.txt', file_names)


class TestIntegration(unittest.TestCase):
    """Integration tests with temporary files."""

    def test_round_trip_type_to_template(self):
        """Test complete file rewrite cycle."""
        original = '---\ntitle: Decision\ntype: decision\n---\n\nBody text.\n'
        result, changed = rewrite_file_content(original)

        self.assertTrue(changed)
        self.assertIn('template: decision', result)
        self.assertNotIn('type: decision', result)

        # Second pass should not change anything
        result2, changed2 = rewrite_file_content(result)
        self.assertFalse(changed2)
        self.assertEqual(result, result2)

    def test_preserve_other_fields(self):
        """Other fields should be preserved during rewrite."""
        content = '---\ntitle: Test\ntemplate: decision\nprivate: true\ntype: decision\ntags:\n  - important\n---\n\nBody\n'
        result, changed = rewrite_file_content(content)

        self.assertTrue(changed)
        self.assertIn('title: Test', result)
        self.assertIn('private: true', result)
        self.assertIn('- important', result)
        self.assertIn('template: decision', result)
        self.assertNotIn('type:', result)


if __name__ == '__main__':
    unittest.main()
