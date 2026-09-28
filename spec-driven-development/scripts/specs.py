"""Where a repository's specs live, and their text.

Trellis is the store when a `.trellis` marker is found from the working
directory up to the repository root and the CLI is on PATH; otherwise the
repository's specs/ directory is. Once Trellis is the store, failing to read it
is an error, never a reason to fall back to specs/.
"""

import json
import pathlib
import shutil
import subprocess

TIMEOUT = 20


class StoreError(Exception):
    """The spec store is Trellis, but it could not be read."""


def trellis_store(cwd):
    """True when this directory's specs live in Trellis."""
    if not shutil.which("trellis"):
        return False
    here = pathlib.Path(cwd).resolve()
    top = subprocess.run(["git", "rev-parse", "--show-toplevel"], cwd=here, capture_output=True,
                         text=True, check=False)
    stop = pathlib.Path(top.stdout.strip()).resolve() if top.returncode == 0 else here
    for directory in (here, *here.parents):
        if (directory / ".trellis").is_file():
            return True
        if directory == stop:
            return False
    return False


def trellis_entries(directory, cwd):
    """Vault entries under directory. Raises StoreError when they cannot be read."""
    try:
        result = subprocess.run(["trellis", "vault", "ls", directory], cwd=cwd,
                                capture_output=True, text=True, timeout=TIMEOUT, check=False)
    except (OSError, subprocess.TimeoutExpired) as error:
        raise StoreError(f"trellis vault ls {directory} failed: {error}") from error
    if result.returncode:
        raise StoreError(f"trellis vault ls {directory} exited {result.returncode}: {result.stderr.strip()}")
    try:
        return json.loads(result.stdout).get("entries") or []
    except ValueError as error:
        raise StoreError(f"trellis vault ls {directory} printed no JSON") from error


def trellis_segments(directory, cwd):
    # Entry files are the source of truth, so read them rather than a rendering.
    return [(e["ref"], pathlib.Path(e["path"]).read_text(encoding="utf-8")) for e in trellis_entries(directory, cwd)]


def file_segments(root, paths=("specs",)):
    """Segments under specs/, named by POSIX paths on every platform."""
    root = pathlib.Path(root)
    out = []
    for raw in paths:
        path = root / raw
        files = sorted(path.rglob("*.md")) if path.is_dir() else [path] if path.is_file() else []
        for f in files:
            name = f.relative_to(root).as_posix() if f.is_relative_to(root) else f.as_posix()
            out.append((name, f.read_text(encoding="utf-8")))
    return out


def all_segments(root):
    """Every spec segment in the store this repository uses. Raises StoreError."""
    return trellis_segments("specs", root) if trellis_store(root) else file_segments(root)
