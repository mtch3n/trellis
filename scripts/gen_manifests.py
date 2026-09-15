#!/usr/bin/env python3
"""Generate every plugin manifest from plugin/plugin-build.json.

Name, version, description, author, license and keywords have to appear in each
harness's manifest. Kept by hand they drift, and a stale version in one of them
is invisible until someone installs it. One declaration, generated outputs, and
a test that fails when the two disagree.

    python3 scripts/gen_manifests.py            # write the manifests
    python3 scripts/gen_manifests.py --check    # exit 1 if any is out of date

Outputs are committed, so installing the plugin never needs a build step.
"""

import argparse
import json
import pathlib
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1]
BUILD = ROOT / "plugin/plugin-build.json"


def manifests(spec):
    """Every generated file, as (path, content). Key order is the file's."""
    shared = spec["shared"]
    out = []

    for harness in spec["harnesses"].values():
        out.append((ROOT / harness["path"], {**shared, **harness.get("extra", {})}))

    market = spec["marketplace"]
    out.append((ROOT / market["path"], {
        "name": shared["name"],
        "owner": market["owner"],
        "description": market["description"],
        "plugins": [{
            "name": shared["name"],
            "source": market["source"],
            "description": market["pluginDescription"],
        }],
    }))
    return out


def render(content):
    return json.dumps(content, indent=2) + "\n"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true",
                        help="report drift instead of writing")
    args = parser.parse_args()

    spec = json.loads(BUILD.read_text(encoding="utf-8"))
    stale = []
    for path, content in manifests(spec):
        want = render(content)
        have = path.read_text(encoding="utf-8") if path.exists() else None
        if have == want:
            continue
        rel = path.relative_to(ROOT)
        if args.check:
            stale.append(rel)
            continue
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(want, encoding="utf-8")
        print(f"wrote {rel}")

    if stale:
        print("stale manifests (run scripts/gen_manifests.py):", file=sys.stderr)
        for rel in stale:
            print(f"  {rel}", file=sys.stderr)
        return 1
    if args.check:
        print("manifests match plugin/plugin-build.json")
    return 0


if __name__ == "__main__":
    sys.exit(main())
