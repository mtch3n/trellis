"""Migration facts from git: which files are migrations, their versions, and collisions."""

import fnmatch
import pathlib
import re
import sys

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))

from state import git  # noqa: E402

# A migrations directory holds one file (0013_x.sql) or one directory
# (prisma: 20250101_x/migration.sql) per version. Only a leading number that
# stands alone is a version: Alembic's 27c6a30d7c24_x.py is a hash, not 27.
MIGRATION_DIRS = ("migrations", "migrate")
VERSIONED = re.compile(r"\d+(?:[_.-]|$)")


def migration_unit(rel, patterns):
    """(namespace, unit) for a repo-relative path, or None if not a migration.

    Configured globs share one namespace. Otherwise each migrations directory
    is its own namespace, and the unit is the entry directly inside it.
    """
    if patterns:
        if any(fnmatch.fnmatch(rel, p) for p in patterns):
            return "configured", pathlib.PurePosixPath(rel).name
        return None
    parts = pathlib.PurePosixPath(rel).parts
    for i in range(len(parts) - 2, -1, -1):
        if parts[i] in MIGRATION_DIRS and i + 1 < len(parts):
            unit = parts[i + 1]
            if VERSIONED.match(unit):
                return "/".join(parts[: i + 1]), unit
    return None


def version(unit):
    match = re.search(r"\d+", unit)
    return int(match.group()) if match else None


def units_on(ref, root, patterns):
    """{(namespace, version): {unit: path}} for every migration in ref."""
    listing = git(["ls-tree", "-r", "--name-only", ref], root) or ""
    found = {}
    for rel in listing.splitlines():
        hit = migration_unit(rel, patterns)
        if hit and version(hit[1]) is not None:
            found.setdefault((hit[0], version(hit[1])), {})[hit[1]] = rel
    return found


def default_branch(root):
    head = git(["symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"], root)
    if head:
        return head.strip().split("/", 1)[-1]
    for name in ("main", "master"):
        if git(["rev-parse", "--verify", "--quiet", f"refs/heads/{name}"], root):
            return name
    return None


def draft_of(rel, patterns):
    """(migrations directory, file name) when rel is a draft, else None.

    A draft sits in a draft/ directory directly inside a migrations directory:
    one named like MIGRATION_DIRS, or one a configured glob covers.
    """
    path = pathlib.PurePosixPath(rel)
    if len(path.parts) < 3 or path.parent.name != "draft":
        return None
    home = path.parent.parent
    if home.name in MIGRATION_DIRS or any(pathlib.PurePosixPath(p).parent == home for p in patterns):
        return home.as_posix(), path.name
    return None


def repo_files(root):
    listed = git(["ls-files", "--cached", "--others", "--exclude-standard"], root) or ""
    return [rel for rel in listed.splitlines() if (pathlib.Path(root) / rel).is_file()]


def drafts(root, patterns):
    """Every draft in the working tree, as repository-relative paths."""
    return sorted(rel for rel in repo_files(root) if draft_of(rel, patterns))


def check_draft(root, rel, patterns):
    """A draft of the same name, with other content, on another local branch."""
    if not draft_of(rel, patterns):
        return []
    mine = git(["hash-object", rel], root)
    current = (git(["rev-parse", "--abbrev-ref", "HEAD"], root) or "").strip()
    problems = []
    for branch in (git(["for-each-ref", "--format=%(refname:short)", "refs/heads"], root) or "").split():
        if branch == current:
            continue
        theirs = git(["rev-parse", "--verify", "--quiet", f"{branch}:{rel}"], root)
        if theirs and mine and theirs.strip() != mine.strip():
            problems.append(f"branch {branch} has a different draft at {rel}. Rename one of them, "
                            "or the second to merge will conflict.")
    return problems


def is_new(root, rel):
    """Neither in HEAD nor on the default branch."""
    if git(["cat-file", "-e", f"HEAD:{rel}"], root) is not None:
        return False
    base = default_branch(root)
    return not base or git(["cat-file", "-e", f"{base}:{rel}"], root) is None


def check_migration(root, rel, patterns, release=True):
    hit = migration_unit(rel, patterns)
    if not hit or version(hit[1]) is None:
        return []
    namespace, unit = hit
    key = (namespace, version(unit))
    problems = []

    if not release and is_new(root, rel):
        home = namespace if namespace != "configured" else pathlib.PurePosixPath(rel).parent.as_posix()
        return [f"{rel} is a new numbered migration, and numbers are assigned only at release. "
                f"Write it as a draft instead: {home}/draft/<slug>{pathlib.PurePosixPath(unit).suffix}. "
                "The user merges drafts with /sdd:releasing-migrations."]

    # Two units with one version in this tree.
    here = pathlib.Path(root)
    siblings = set()
    if namespace != "configured":
        for entry in (here / namespace).iterdir():
            if version(entry.name) == key[1] and entry.name != unit:
                siblings.add(entry.name)
    else:
        tracked = (git(["ls-files", "--cached", "--others", "--exclude-standard"], root) or "").splitlines()
        for other in tracked:
            got = migration_unit(other, patterns)
            if got and got[1] != unit and version(got[1]) == key[1]:
                siblings.add(got[1])
    for other in sorted(siblings):
        problems.append(f"{unit} and {other} share version {key[1]} in this working tree.")

    # The same version taken by a different migration on another branch.
    current = (git(["rev-parse", "--abbrev-ref", "HEAD"], root) or "").strip()
    branches = (git(["for-each-ref", "--format=%(refname:short)", "refs/heads"], root) or "").split()
    for branch in branches:
        if branch == current:
            continue
        for other, path in units_on(branch, root, patterns).get(key, {}).items():
            if other != unit and other not in siblings:
                problems.append(
                    f"{unit} takes version {key[1]}, which branch {branch} already uses for {path}. "
                    "Whichever merges second will not run on databases that have the first.")

    # A migration that already landed on the default branch, now edited.
    base = default_branch(root)
    if base:
        landed = git(["show", f"{base}:{rel}"], root)
        path = here / rel
        if landed is not None and path.is_file() and path.read_text(encoding="utf-8", errors="replace") != landed:
            problems.append(
                f"{rel} is already on {base} and now differs from it. A database that "
                "applied it will not apply the edit. If it has never been released, say so "
                "to the user; otherwise add a new migration.")
    return problems
