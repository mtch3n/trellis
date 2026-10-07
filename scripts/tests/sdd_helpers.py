"""Shared fixtures for the sdd plugin tests."""

import importlib.util
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[2]
PLUGIN = ROOT / "spec-driven-development"

# No test may start a real, billed shadow run.
os.environ["SDD_CLAUDE"] = shutil.which("true") or "true"
# No test may read or write the real Trellis store: sdd probes Trellis, and a
# test run once logged two thousand calls into the user's live database.
os.environ["TRELLIS_HOME"] = tempfile.mkdtemp(prefix="sdd-tests-trellis-")
os.environ.pop("TRELLIS_PROJECT", None)


def load(name, rel, real_shadow=False):
    """Load a plugin script as a module. A loaded verify starts no background
    shadow unless asked: a detached shadow outliving its test wrote into the
    test's directory while it was being removed, failing the cleanup."""
    spec = importlib.util.spec_from_file_location(name, ROOT / rel)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    if name == "verify" and not real_shadow:
        module.start_shadow = lambda root, top, tree: "started"
    return module


class Repo:
    def __init__(self, tmp):
        self.root = Path(tmp)
        self.git("init", "-q", "-b", "main")
        self.git("config", "user.email", "t@example.com")
        self.git("config", "user.name", "t")

    def git(self, *args):
        result = subprocess.run(["git", *args], cwd=self.root, check=True, capture_output=True, text=True)
        return result.stdout

    def write(self, rel, text="x\n"):
        path = self.root / rel
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text, encoding="utf-8")
        return path

    def commit(self, message="c"):
        self.git("add", "-A")
        self.git("commit", "-q", "--allow-empty", "-m", message)

    def edit_event(self, rel):
        return {"cwd": str(self.root), "tool_name": "Write", "tool_input": {"file_path": str(self.root / rel)}}
