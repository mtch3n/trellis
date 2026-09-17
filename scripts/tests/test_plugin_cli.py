"""Optional real-CLI check: set TRELLIS_TEST_BINARY to a built Trellis binary."""

import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]


@unittest.skipUnless(os.environ.get("TRELLIS_TEST_BINARY"), "set TRELLIS_TEST_BINARY for integration")
class PluginCLITest(unittest.TestCase):
    def test_claim_resume_compact_and_handoff(self):
        binary = Path(os.environ["TRELLIS_TEST_BINARY"]).resolve()
        with tempfile.TemporaryDirectory(prefix="trellis-hook-integration-") as directory:
            cwd = Path(directory) / "project"
            cwd.mkdir()
            # The hook runs whatever `trellis` PATH names first. Only the binary
            # under test may answer, never one installed elsewhere, so it is
            # copied under that name into a directory of its own.
            bin_dir = Path(directory) / "bin"
            bin_dir.mkdir()
            trellis = bin_dir / ("trellis.exe" if os.name == "nt" else "trellis")
            shutil.copy2(binary, trellis)
            # Only ever a scratch home: a branch binary once migrated a real one.
            env = {key: value for key, value in os.environ.items()
                   if not key.startswith(("TRELLIS_", "CLAUDE_"))}
            env.update(TRELLIS_HOME=str(Path(directory) / "data"),
                       PATH=str(bin_dir) + os.pathsep + env.get("PATH", ""))
            self.assertEqual(Path(shutil.which("trellis", path=env["PATH"])).resolve(), trellis.resolve())
            session = "plugin-integration-session"
            actor = "agent:" + hashlib.sha256(session.encode()).hexdigest()[:32]

            def cli(*args):
                result = subprocess.run([str(trellis), *args], cwd=cwd,
                                        env={**env, "TRELLIS_AGENT": actor},
                                        text=True, capture_output=True, timeout=10)
                self.assertEqual(result.returncode, 0, result.stderr)
                return json.loads(result.stdout)

            def hook(mode, **fields):
                result = subprocess.run(
                    [os.sys.executable, "-B", str(ROOT / "plugin/hooks/trellis_hook.py"), mode],
                    input=json.dumps(dict(session_id=session, cwd=str(cwd), **fields)),
                    cwd=directory, env=env, text=True, capture_output=True, timeout=10,
                )
                self.assertEqual(result.returncode, 0, result.stderr)
                return json.loads(result.stdout) if result.stdout else None

            cli("init", "--key", "HOOKTEST")
            start = hook("session-start", source="startup")
            self.assertIn(actor, start["hookSpecificOutput"]["additionalContext"])
            card = cli("card", "new", "--title", "Verify hook handoff")
            cli("card", "claim", card["ref"])
            cli("card", "move", card["ref"], "in-progress")
            for source in ("resume", "compact"):
                context = hook("session-start", source=source)["hookSpecificOutput"]["additionalContext"]
                self.assertIn(actor, context)
                self.assertIn(card["ref"], context)
                self.assertEqual(cli("card", "show", card["ref"])["claimed_by"], actor)
            self.assertIn("systemMessage", hook("stop"))
            cli("card", "comment", card["ref"], "--body", "Integration handoff verified")
            self.assertIsNone(hook("stop"))

            unpinned = Path(directory) / "unpinned"
            unpinned.mkdir()
            silent = subprocess.run(
                [os.sys.executable, "-B", str(ROOT / "plugin/hooks/trellis_hook.py"), "session-start"],
                input=json.dumps(dict(session_id=session, cwd=str(unpinned), source="startup")),
                cwd=directory, env=env, text=True, capture_output=True, timeout=10,
            )
            self.assertEqual(silent.returncode, 0, silent.stderr)
            self.assertEqual(silent.stdout, "")

            ghost = Path(directory) / "ghost"
            ghost.mkdir()
            (ghost / ".trellis").write_text("/GHOST\n")
            missing = subprocess.run(
                [os.sys.executable, "-B", str(ROOT / "plugin/hooks/trellis_hook.py"), "session-start"],
                input=json.dumps(dict(session_id=session, cwd=str(ghost), source="startup")),
                cwd=directory, env=env, text=True, capture_output=True, timeout=10,
            )
            self.assertIn("unavailable", json.loads(missing.stdout)["hookSpecificOutput"]["additionalContext"])
