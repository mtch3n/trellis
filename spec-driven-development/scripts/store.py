#!/usr/bin/env python3
"""Say where this repository's specs live, and which exist. One answer, no judgment.

Prints, for example:
    store: trellis
    specs: specs/login-rate-limit (2 segments)
or
    store: files (/repo/specs)
    specs: none

Trellis is the store when the CLI is on PATH and this directory resolves to a
Trellis project; otherwise the repository's specs/ directory is.
"""

import json
import pathlib
import shutil
import subprocess
import sys


def trellis_specs(cwd):
    if not shutil.which("trellis"):
        return None
    result = subprocess.run(["trellis", "vault", "ls", "specs"], cwd=cwd,
                            capture_output=True, text=True, timeout=20, check=False)
    if result.returncode:
        return None
    stories = {}
    for entry in json.loads(result.stdout).get("entries") or []:
        parts = entry["slug"].split("/")
        story = "/".join(parts[:2]) if len(parts) > 2 else entry["slug"]
        stories[story] = stories.get(story, 0) + 1
    return stories


def file_specs(root):
    base = root / "specs"
    stories = {}
    if base.is_dir():
        for path in sorted(base.iterdir()):
            if path.is_dir():
                stories[f"specs/{path.name}"] = len(list(path.rglob("*.md")))
            elif path.suffix == ".md":
                stories[f"specs/{path.stem}"] = 1
    return stories


def main():
    cwd = pathlib.Path.cwd()
    top = subprocess.run(["git", "rev-parse", "--show-toplevel"], cwd=cwd,
                         capture_output=True, text=True, check=False)
    root = pathlib.Path(top.stdout.strip()) if top.returncode == 0 else cwd
    stories = trellis_specs(cwd)
    if stories is not None:
        print("store: trellis")
    else:
        stories = file_specs(root)
        print(f"store: files ({root / 'specs'})")
    if not stories:
        print("specs: none")
    for story, count in sorted(stories.items()):
        print(f"specs: {story} ({count} segment{'s' if count != 1 else ''})")
    return 0


if __name__ == "__main__":
    sys.exit(main())
