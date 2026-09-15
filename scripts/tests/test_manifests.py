"""Run with python3 -B -m unittest discover -s scripts/tests -v."""

import importlib.util
import json
from pathlib import Path
import unittest


ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location("gen_manifests", ROOT / "scripts/gen_manifests.py")
GEN = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(GEN)


class ManifestTests(unittest.TestCase):
    def setUp(self):
        self.spec = json.loads((ROOT / "plugin/plugin-build.json").read_text(encoding="utf-8"))

    def test_committed_manifests_match_the_declaration(self):
        # The point of the generator: a hand edit to any manifest fails here
        # rather than shipping a stale version to whoever installs it.
        for path, content in GEN.manifests(self.spec):
            rel = path.relative_to(ROOT)
            self.assertTrue(path.exists(), f"{rel} is missing")
            self.assertEqual(
                path.read_text(encoding="utf-8"), GEN.render(content),
                f"{rel} is out of date; run python3 scripts/gen_manifests.py")

    def test_every_harness_carries_the_same_shared_metadata(self):
        shared = self.spec["shared"]
        for name, harness in self.spec["harnesses"].items():
            manifest = json.loads((ROOT / harness["path"]).read_text(encoding="utf-8"))
            for key, value in shared.items():
                self.assertEqual(manifest.get(key), value,
                                 f"{name} disagrees about {key}")

    def test_a_harness_may_not_override_shared_metadata(self):
        # extra exists to add what a harness needs, not to fork the shared
        # fields — that is the drift this file is here to prevent.
        for name, harness in self.spec["harnesses"].items():
            overlap = set(harness.get("extra", {})) & set(self.spec["shared"])
            self.assertEqual(overlap, set(), f"{name} overrides shared keys {overlap}")

    def test_the_marketplace_points_at_the_plugin_directory(self):
        market = json.loads((ROOT / self.spec["marketplace"]["path"]).read_text(encoding="utf-8"))
        source = market["plugins"][0]["source"]
        self.assertTrue((ROOT / source).is_dir(), f"marketplace source {source} is not a directory")
        # Only what lives under this directory reaches an installed user.
        for harness in self.spec["harnesses"].values():
            self.assertTrue(harness["path"].startswith(source.lstrip("./") + "/"),
                            f"{harness['path']} sits outside the published tree")

    def test_there_is_exactly_one_marketplace_manifest(self):
        found = sorted(
            str(p.relative_to(ROOT)) for p in ROOT.rglob("marketplace.json")
            if "node_modules" not in p.parts and ".git" not in p.parts
        )
        self.assertEqual(found, [self.spec["marketplace"]["path"]],
                         "a second marketplace manifest is a second source of truth")


if __name__ == "__main__":
    unittest.main()
