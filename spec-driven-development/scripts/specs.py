"""Where a repository's specs live, and their text.

Trellis is the store when the CLI is on PATH and the directory resolves to a
Trellis project; otherwise the repository's specs/ directory is.
"""

import json
import pathlib
import shutil
import subprocess


def trellis_entries(directory, cwd):
    """Vault entries under directory, or None when Trellis is not the store."""
    if not shutil.which("trellis"):
        return None
    try:
        result = subprocess.run(["trellis", "vault", "ls", directory], cwd=cwd,
                                capture_output=True, text=True, timeout=20, check=False)
    except (OSError, subprocess.TimeoutExpired):
        return None
    if result.returncode:
        return None
    return json.loads(result.stdout).get("entries") or []


def trellis_segments(directory, cwd):
    entries = trellis_entries(directory, cwd)
    if entries is None:
        return None
    # Entry files are the source of truth, so read them rather than a rendering.
    return [(e["ref"], pathlib.Path(e["path"]).read_text(encoding="utf-8")) for e in entries]


def file_segments(root, paths=("specs",)):
    out = []
    for raw in paths:
        path = pathlib.Path(root) / raw
        files = sorted(path.rglob("*.md")) if path.is_dir() else [path] if path.is_file() else []
        for f in files:
            out.append((str(f.relative_to(root)) if f.is_relative_to(root) else str(f),
                        f.read_text(encoding="utf-8")))
    return out


def all_segments(root):
    """Every spec segment in the store this repository uses."""
    found = trellis_segments("specs", root)
    return found if found is not None else file_segments(root)
