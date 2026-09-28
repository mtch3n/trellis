#!/usr/bin/env python3
"""Exit 1 while any draft migration remains. Run it before tagging, and in CI."""

import pathlib
import sys

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))

import state  # noqa: E402
from migrations import drafts  # noqa: E402


def main():
    top = pathlib.Path((state.git(["rev-parse", "--show-toplevel"], pathlib.Path.cwd()) or ".").strip())
    left = drafts(top, state.load_config(top).get("migrations") or [])
    for rel in left:
        print(f"draft migration not merged: {rel}")
    if left:
        print("merge them with /sdd:releasing-migrations before tagging")
    return 1 if left else 0


if __name__ == "__main__":
    sys.exit(main())
