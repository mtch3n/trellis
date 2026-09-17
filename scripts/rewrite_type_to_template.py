#!/usr/bin/env python3
"""One-off: move entries from `type:` to `template:`.

Trellis no longer reads an entry's `type:`; `template:` is the only
classification, and an entry without one has no shape to keep. For every *.md
file under a vault directory's "knowledge" subtree(s), revision directories
included, this rewrites the top-level `type:` key of the leading frontmatter.
A vault directory (~/.trellis/projects or ~/.trellis/global) also holds
artifacts/ -- user-uploaded files Trellis never wrote frontmatter into -- and
this script never walks into it:

This script runs at switch-over, BEFORE the migration renames anything, so it
reads the tree as it is then: entries still carry `type:` and still live under
`knowledge/`, not `vault/`. Both spellings are deliberate. Renaming either one
here would make the script find nothing to convert, which is its whole job.

  type: note  -> removed (note was the "no template" default)
  type: <x>   -> template: <x>
  type: <x> beside an existing template: -> removed

Nothing else in a file changes. Dry run by default; --apply writes, each file
atomically. Take a backup first.
"""

import argparse
import os
import re
import sys
import tempfile
from pathlib import Path

TYPE_RE = re.compile(r"^type:[ \t]*(.*?)[ \t]*$")
TEMPLATE_RE = re.compile(r"^template:")


def _frontmatter_range(lines):
    """Return (start, end) indexes of the frontmatter's own lines, or None."""
    if not lines or lines[0].rstrip("\r\n") != "---":
        return None
    for i in range(1, len(lines)):
        if lines[i].rstrip("\r\n") == "---":
            return 1, i
    return None


def _unquote(value):
    if len(value) >= 2 and value[0] == value[-1] and value[0] in "'\"":
        return value[1:-1]
    return value


def rewrite_content(content):
    """Return (new_content, old_value, new_value); old_value is None when the
    file needs no change. new_value is None when type: is only dropped."""
    lines = content.splitlines(keepends=True)
    span = _frontmatter_range(lines)
    if span is None:
        return content, None, None
    start, end = span
    header = lines[start:end]
    has_template = any(TEMPLATE_RE.match(line) for line in header)
    for offset, line in enumerate(header):
        match = TYPE_RE.match(line.rstrip("\r\n"))
        if not match:
            continue
        raw = match.group(1)
        value = _unquote(raw)
        ending = line[len(line.rstrip("\r\n")):]
        i = start + offset
        if has_template or value in ("", "note"):
            del lines[i]
            return "".join(lines), value, None
        lines[i] = "template: " + raw + ending
        return "".join(lines), value, value
    return content, None, None


def markdown_files(vault_dir):
    """Every *.md file under a "knowledge" subtree of vault_dir, revision
    directories included, sorted. vault_dir (~/.trellis/projects or
    ~/.trellis/global) can also hold projects/<KEY>/artifacts/ --
    user-uploaded files, `text/*` included, that Trellis never wrote and
    this function never walks into."""
    vault_dir = Path(vault_dir)
    found = []
    for root, dirs, files in os.walk(vault_dir):
        root_path = Path(root)
        if "knowledge" not in root_path.relative_to(vault_dir).parts:
            # Not inside a knowledge subtree yet: keep looking for one below
            # (projects/<KEY>/knowledge), but never by way of artifacts/.
            dirs[:] = [d for d in dirs if d != "artifacts"]
            continue
        found.extend(root_path / name for name in files if name.endswith(".md"))
    return sorted(found)


def write_atomic(path, data):
    fd, tmp = tempfile.mkstemp(dir=path.parent, prefix="." + path.name + ".tmp-")
    try:
        with os.fdopen(fd, "wb") as f:
            f.write(data)
            f.flush()
            os.fsync(f.fileno())
        os.chmod(tmp, path.stat().st_mode & 0o7777)
        os.replace(tmp, path)
    except BaseException:
        if os.path.exists(tmp):
            os.unlink(tmp)
        raise


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("vault_dir", help="a vault directory, e.g. ~/.trellis/projects or ~/.trellis/global")
    parser.add_argument("--apply", action="store_true", help="write the changes (default: only print them)")
    args = parser.parse_args(argv)

    if not os.path.isdir(args.vault_dir):
        print(f"error: {args.vault_dir} is not a directory", file=sys.stderr)
        return 1
    verb = "rewrote" if args.apply else "would rewrite"
    changed = failed = 0
    for path in markdown_files(args.vault_dir):
        try:
            raw = path.read_bytes()
            text = raw.decode("utf-8")
        except (OSError, UnicodeDecodeError) as err:
            print(f"skipped {path}: {err}", file=sys.stderr)
            failed += 1
            continue
        new, old, value = rewrite_content(text)
        if old is None:
            continue
        after = f"template: {value}" if value is not None else "no template"
        print(f"{verb} {path}: type: {old} -> {after}")
        if args.apply:
            write_atomic(path, new.encode("utf-8"))
        changed += 1
    print(f"{verb} {changed} file(s)" + (f", {failed} skipped" if failed else ""))
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
