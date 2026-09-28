#!/usr/bin/env python3
"""Say where this repository's specs live, and which exist. One answer, no judgment.

Prints, for example:
    store: trellis
    specs: specs/login-rate-limit (2 segments)
or
    store: files (/repo/specs)
    specs: none

Exits 1 when the store is Trellis but cannot be read: never guess another store.
"""

import pathlib
import subprocess
import sys

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))

from spec_check import story_of  # noqa: E402
from specs import StoreError, all_segments, trellis_store  # noqa: E402


def main():
    cwd = pathlib.Path.cwd()
    top = subprocess.run(["git", "rev-parse", "--show-toplevel"], cwd=cwd,
                         capture_output=True, text=True, check=False)
    root = pathlib.Path(top.stdout.strip()) if top.returncode == 0 else cwd
    trellis = trellis_store(cwd)
    try:
        segments = all_segments(cwd if trellis else root)
    except StoreError as error:
        print(f"error: the spec store is Trellis but could not be read: {error}", file=sys.stderr)
        return 1
    print("store: trellis" if trellis else f"store: files ({root / 'specs'})")
    stories = {}
    for name, _ in segments:
        story = story_of(name)
        story = story[story.find("specs/"):] if "specs/" in story else story
        stories[story] = stories.get(story, 0) + 1
    if not stories:
        print("specs: none")
    for story, count in sorted(stories.items()):
        print(f"specs: {story} ({count} segment{'s' if count != 1 else ''})")
    return 0


if __name__ == "__main__":
    sys.exit(main())
