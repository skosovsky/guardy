"""Release rewriting must preserve the consumer's published selection contract."""
import json
import os
from pathlib import Path
import tempfile
import unittest

import downstream_conformance as conformance
import release


class DownstreamSelectionTests(unittest.TestCase):
    def test_release_rewrites_internal_require_without_stale_selection(self):
        # Arrange: real consumer manifests, without a dependency download.
        repository = Path(__file__).resolve().parent.parent
        manifest = json.loads((repository / "integration/downstream/conformance.json").read_text())
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary).resolve()
            consumer = root / "integration/downstream"
            consumer.mkdir(parents=True)
            (root / "go.mod").write_bytes((repository / "go.mod").read_bytes())
            (consumer / "go.mod").write_bytes((repository / "integration/downstream/go.mod").read_bytes())
            modules = [{"dir": ".", "path": "github.com/skosovsky/guardy"},
                       {"dir": "ext/jsonschema", "path": "github.com/skosovsky/guardy/ext/jsonschema"},
                       {"dir": "integration/downstream", "path": "github.com/skosovsky/guardy/integration/downstream"}]
            schema = root / "ext/jsonschema"
            schema.mkdir(parents=True)
            (schema / "go.mod").write_bytes((repository / "ext/jsonschema/go.mod").read_bytes())
            # Act.
            release.rewrite_manifests(root, modules, "v0.99.0")
            selected = conformance.published_selection(consumer, manifest, dict(os.environ, GOWORK="off"))
            # Assert.
            self.assertEqual(selected["github.com/skosovsky/guardy"], "v0.99.0")
            self.assertEqual(selected["github.com/skosovsky/guardy/ext/jsonschema"], "v0.99.0")
            for path, version in manifest["published"].items():
                self.assertEqual(selected[path], version)

    def test_replacements_and_external_drift_fail_closed(self):
        # Arrange.
        manifest = {"published": {"example.org/runtime": "v0.1.0"}}
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary).resolve()
            for dependency in ["require example.org/runtime v0.2.0\n",
                               "require example.org/runtime v0.1.0\nreplace example.org/runtime => ../runtime\n"]:
                (root / "go.mod").write_text("module example.org/consumer\ngo 1.27.1\n" + dependency)
                # Act / Assert.
                with self.assertRaises(RuntimeError):
                    conformance.published_selection(root, manifest, dict(os.environ, GOWORK="off"))


if __name__ == "__main__":
    unittest.main()
