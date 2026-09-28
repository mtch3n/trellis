"""The .sdd/ folder: where it is, and how to write to it safely.

Every worktree of a repository shares the main worktree's .sdd/, so parallel
agents see one queue. Writers hold .sdd/lock, which works on every platform.
"""

from datetime import datetime, timezone
import json
import os
import pathlib
import shutil
import subprocess
import tempfile
import time

GITIGNORE = "*\n!.gitignore\n!config.json\n"
LOCK_WAIT = 10
LOCK_STALE = 30


def git(args, cwd, timeout=10):
    try:
        result = subprocess.run(["git", *args], cwd=cwd, capture_output=True,
                                text=True, timeout=timeout, check=False)
    except (OSError, subprocess.TimeoutExpired):
        return None
    return result.stdout if result.returncode == 0 else None


def repo_root(cwd):
    """The main worktree's root, or cwd outside git."""
    common = git(["rev-parse", "--path-format=absolute", "--git-common-dir"], cwd)
    if common:
        common = pathlib.Path(common.strip())
        if common.name == ".git":
            return common.parent
    top = git(["rev-parse", "--show-toplevel"], cwd)
    return pathlib.Path(top.strip()) if top else pathlib.Path(cwd)


def head(root):
    out = git(["rev-parse", "HEAD"], root)
    return out.strip() if out else ""


def now_iso(now=None):
    return (now or datetime.now(timezone.utc)).isoformat(timespec="seconds")


def folder(root):
    return pathlib.Path(root) / ".sdd"


def ensure(root):
    base = folder(root)
    base.mkdir(exist_ok=True)
    ignore = base / ".gitignore"
    if not ignore.exists():
        ignore.write_text(GITIGNORE, encoding="utf-8")
    return base


def load_config(root):
    path = folder(root) / "config.json"
    if not path.is_file():
        return {}
    data = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(data, dict):
        raise ValueError(".sdd/config.json must be an object")
    return data


class Lock:
    """An exclusive lock file under .sdd/; one older than `stale` seconds is taken over."""

    def __init__(self, root, name="lock", wait=LOCK_WAIT, stale=LOCK_STALE):
        self.path = ensure(root) / name
        self.path.parent.mkdir(parents=True, exist_ok=True)
        self.wait, self.stale = wait, stale

    def __enter__(self):
        deadline = time.monotonic() + self.wait
        while True:
            try:
                os.close(os.open(self.path, os.O_CREAT | os.O_EXCL | os.O_WRONLY))
                return self
            except FileExistsError:
                try:
                    if time.time() - self.path.stat().st_mtime > self.stale:
                        self.path.unlink()
                        continue
                except FileNotFoundError:
                    continue
                if time.monotonic() > deadline:
                    raise TimeoutError(f"{self.path} is held by another process")
                time.sleep(0.01)

    def __exit__(self, *exc):
        self.path.unlink(missing_ok=True)


def read_jsonl(path):
    """(rows, skipped): every parseable object line, and how many were not."""
    rows, skipped = [], 0
    if not path.is_file():
        return rows, skipped
    for line in path.read_text(encoding="utf-8", errors="replace").splitlines():
        if not line.strip():
            continue
        try:
            row = json.loads(line)
        except ValueError:
            skipped += 1
            continue
        if isinstance(row, dict):
            rows.append(row)
        else:
            skipped += 1
    return rows, skipped


def append_jsonl(path, rows):
    with open(path, "a", encoding="utf-8") as stream:
        stream.write("".join(json.dumps(row, ensure_ascii=False) + "\n" for row in rows))


def replace_jsonl(path, rows):
    tmp = path.with_suffix(path.suffix + ".tmp")
    tmp.write_text("".join(json.dumps(row, ensure_ascii=False) + "\n" for row in rows), encoding="utf-8")
    os.replace(tmp, path)


def tree_hash(top):
    """Hash of the working tree as git would commit it, untracked files included."""
    index = git(["rev-parse", "--path-format=absolute", "--git-path", "index"], top)
    with tempfile.TemporaryDirectory() as tmp:
        scratch = pathlib.Path(tmp) / "index"
        if index and pathlib.Path(index.strip()).is_file():
            shutil.copyfile(index.strip(), scratch)
        env = {**os.environ, "GIT_INDEX_FILE": str(scratch)}
        for args in (["add", "-A"], ["write-tree"]):
            result = subprocess.run(["git", *args], cwd=top, env=env, capture_output=True,
                                    text=True, timeout=120, check=False)
            if result.returncode:
                raise ValueError(f"git {' '.join(args)} failed: {result.stderr.strip()}")
    return result.stdout.strip()
