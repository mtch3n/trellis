"""Run with python3 -B -m unittest discover -s scripts/tests -v."""

import json
from pathlib import Path
import shutil
import subprocess
import unittest


ROOT = Path(__file__).resolve().parents[2]
PLUGIN = ROOT / "plugin"


@unittest.skipUnless(shutil.which("claude"), "claude CLI not installed")
class BandPackagingTests(unittest.TestCase):
    def test_GATE_C19_validate_passes_with_command_hooks_and_the_band_module(self):
        hooks = json.loads((PLUGIN / "hooks/hooks.json").read_text(encoding="utf-8"))
        commands = [hook["type"] for groups in hooks["hooks"].values()
                    for group in groups for hook in group["hooks"]]
        self.assertTrue(commands)
        self.assertEqual(set(commands), {"command"})
        self.assertEqual(hooks["modules"], ["./band.tsx"])

        result = subprocess.run(["claude", "plugin", "validate", str(PLUGIN)],
                                capture_output=True, text=True, timeout=120, check=False)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)


if __name__ == "__main__":
    unittest.main()
