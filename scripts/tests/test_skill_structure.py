"""Test for SKILL.md structural validation."""

import os
from pathlib import Path
import re
import unittest

ROOT = Path(__file__).resolve().parents[2]


class SkillStructureTest(unittest.TestCase):
    def test_skill_metadata_validity(self):
        """Validate SKILL.md frontmatter: name matches directory, description is meaningful."""
        skills_dir = ROOT / "plugin" / "skills"
        self.assertTrue(skills_dir.exists(), f"Skills directory not found at {skills_dir}")

        errors = []
        for skill_dir in sorted(skills_dir.iterdir()):
            if not skill_dir.is_dir():
                continue

            skill_md = skill_dir / "SKILL.md"
            if not skill_md.exists():
                errors.append(f"{skill_dir.name}: missing SKILL.md")
                continue

            content = skill_md.read_text(encoding="utf-8")

            # Extract frontmatter
            fm_match = re.match(r'^---\n(.*?)\n---\n', content, re.DOTALL)
            if not fm_match:
                errors.append(f"{skill_dir.name}: frontmatter does not parse")
                continue

            fm_text = fm_match.group(1)

            # Parse minimal YAML (name: and description:)
            name_match = re.search(r'^\s*name:\s*(.+)$', fm_text, re.MULTILINE)
            desc_match = re.search(r'^\s*description:\s*(.+)$', fm_text, re.MULTILINE)

            if not name_match:
                errors.append(f"{skill_dir.name}: name field missing")
                continue

            if not desc_match:
                errors.append(f"{skill_dir.name}: description field missing")
                continue

            name = name_match.group(1).strip()
            description = desc_match.group(1).strip()

            # Validate name matches directory name
            if name != skill_dir.name:
                errors.append(f"{skill_dir.name}: name '{name}' does not match directory name")

            # Validate description is non-trivial (> 20 chars)
            if len(description) <= 20:
                errors.append(f"{skill_dir.name}: description too short ('{description}')")

        if errors:
            self.fail("\n".join(errors))


if __name__ == "__main__":
    unittest.main()
