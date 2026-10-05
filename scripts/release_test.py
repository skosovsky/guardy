"""AAA conformance for isolated release preparation and exact local publication."""
from pathlib import Path
import tempfile
import unittest
from unittest import mock

import release


class ReleaseConformance(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="guardy-release-fixture-")
        self.addCleanup(self.temporary.cleanup)
        self.base = Path(self.temporary.name)
        self.source = self.base / "source checkout"
        self.source.mkdir()
        self.external = self.base / "external"
        self.external.mkdir()
        self.prefix = "example.com/release-fixture"
        self.external_path = self.prefix + "-addon"
        ext_source = self.base / "external-source"
        ext_source.mkdir()
        (ext_source / "go.mod").write_text("module " + self.external_path + "\n\ngo 1.27.1\n")
        (ext_source / "addon.go").write_text("package addon\nconst Value = 1\n")
        item = {"path": self.external_path, "dir": "."}
        release.stage_artifact(ext_source, self.external, item, [item], "v1.2.3")
        self.external_url = self.external.as_uri()
        self.write(".gitignore", "ignored-secret\n")
        self.write("README.md", "fixture\n")
        self.write("go.work", "go 1.27.1\nuse (\n .\n ./alpha\n ./beta\n)\n")
        self.write("go.mod", "module " + self.prefix + "\n\ngo 1.27.1\n\nrequire (\n " +
                   self.prefix + "/alpha v0.2.0 // indirect\n " + self.prefix +
                   "/beta v0.3.0\n " + self.external_path + " v1.2.3\n)\n\nreplace (\n " +
                   self.prefix + "/alpha => ./alpha\n " + self.prefix + "/beta => ./beta\n)\n")
        self.write("fixture.go", 'package fixture\nimport "' + self.prefix + '/beta"\nvar Value = beta.Value\n')
        self.write("fixture_test.go", 'package fixture\nimport "testing"\nfunc TestValue(t *testing.T) {\n // Arrange.\n want := 7\n // Act.\n got := Value\n // Assert.\n if got != want {t.Fatal(got)}\n}\n')
        self.write("alpha/go.mod", "module " + self.prefix + "/alpha\n\ngo 1.27.1\n")
        self.write("alpha/alpha.go", "package alpha\nconst Value = 7\n")
        self.write("beta/go.mod", "module " + self.prefix + "/beta\n\ngo 1.27.1\nrequire " +
                   self.prefix + "/alpha v0.1.0\nreplace " + self.prefix + "/alpha => ../alpha\n")
        self.write("beta/beta.go", 'package beta\nimport "' + self.prefix + '/alpha"\nvar Value = alpha.Value\n')
        release.run(["git", "init", "--quiet"], self.source)
        release.run(["git", "add", "--all"], self.source)
        release.run(["git", "-c", "commit.gpgsign=false", "-c", "user.name=Fixture",
                     "-c", "user.email=fixture@example.invalid", "commit", "--quiet", "-m", "fixture"], self.source)
        release.run(["git", "-c", "tag.gpgsign=false", "tag", "unrelated-local-tag"], self.source)
        # Source dirty/index state is intentional, and must survive every release phase.
        self.write("README.md", "staged caller change\n")
        release.run(["git", "add", "README.md"], self.source)
        self.write("untracked.md", "caller work\n")
        self.write("ignored-secret", "not a release input\n")
        self.original = self.source_state()

    def write(self, name, content):
        target = self.source / name
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(content)

    def source_state(self):
        return (release.run(["git", "rev-parse", "HEAD"], self.source),
                release.run(["git", "symbolic-ref", "HEAD"], self.source),
                release.run(["git", "show-ref"], self.source),
                release.run(["git", "diff", "--cached", "--binary"], self.source),
                release.run(["git", "status", "--porcelain", "--untracked-files=all"], self.source),
                release.tree_digest(self.source),
                release.digest((self.source / ".git/index").read_bytes()))

    def prepare(self, name="candidate"):
        output = self.base / name
        release.prepare(self.source, output, "v0.12.0", external=self.external_url)
        return output

    def assert_source_unchanged(self):
        self.assertEqual(self.source_state(), self.original)

    def test_prepare_is_deterministic_and_rewrites_exact_paths(self):
        # Arrange.
        first, second = self.base / "first", self.base / "second"
        # Act.
        one = release.prepare(self.source, first, "v0.12.0", self.external_url)
        two = release.prepare(self.source, second, "v0.12.0", self.external_url)
        # Assert: independent Git clones produce identical manifests and archives.
        self.assertEqual(one, two)
        self.assertEqual(release.artifact_digest(first / "proxy"), release.artifact_digest(second / "proxy"))
        root = release.mod_json(first / "repo")
        requirements = {item["Path"]: item for item in root["Require"]}
        self.assertEqual(requirements[self.prefix + "/alpha"]["Version"], "v0.12.0")
        self.assertTrue(requirements[self.prefix + "/alpha"]["Indirect"])
        self.assertEqual(requirements[self.external_path]["Version"], "v1.2.3")
        self.assertFalse((first / "repo/go.work").exists())
        self.assertFalse((first / "repo/ignored-secret").exists())
        self.assertTrue((first / "repo/untracked.md").exists())
        self.assert_source_unchanged()

    def test_verify_executes_artifacts_and_freezes_manifests(self):
        # Arrange.
        candidate = self.prepare()
        source_hash = release.tree_digest(candidate / "repo")
        # Act: real Go graph/race/consumer execution; fixture linter is a harmless success binary.
        release.verify(candidate, self.external_url, linter="true")
        # Assert.
        self.assertTrue((candidate / "verified.json").exists())
        self.assertEqual(release.tree_digest(candidate / "repo"), source_hash)
        self.assert_source_unchanged()

    def test_invalid_candidate_inputs_fail_before_publication(self):
        for defect in ("stale", "missing", "replace", "path", "artifact-bytes"):
            with self.subTest(defect=defect):
                # Arrange.
                candidate = self.prepare(defect)
                root = candidate / "repo"
                # Act.
                if defect == "stale":
                    release.run(["go", "mod", "edit", "-require=" + self.prefix + "/alpha@v0.1.0"], root, release.go_environment())
                elif defect == "missing":
                    (candidate / "proxy" / self.prefix / "@v/v0.12.0.zip").unlink()
                elif defect == "replace":
                    release.run(["go", "mod", "edit", "-replace=" + self.prefix + "/alpha=./alpha"], root, release.go_environment())
                elif defect == "path":
                    release.run(["go", "mod", "edit", "-module=" + self.prefix + "/different"], root / "alpha", release.go_environment())
                else:
                    (candidate / "proxy" / self.prefix / "@v/v0.12.0.zip").write_bytes(b"changed")
                # Assert.
                with self.assertRaises(release.ReleaseError):
                    release.validate_candidate(candidate)
                self.assertFalse((candidate / "verified.json").exists())
                self.assert_source_unchanged()

    def test_missing_internal_artifact_never_falls_back_to_external(self):
        # Arrange: a public-like external proxy holds the same module at the same version.
        candidate = self.prepare()
        manifest = release.validate_candidate(candidate)
        route = self.prefix + "/alpha/@v/v0.12.0.zip"
        duplicate = self.external / route
        duplicate.parent.mkdir(parents=True, exist_ok=True)
        duplicate.write_bytes((candidate / "proxy" / route).read_bytes())
        (candidate / "proxy" / route).unlink()
        # Act / Assert: request gets local 404 despite the external duplicate.
        with release.module_proxy(candidate / "proxy", manifest["modules"], self.external_url) as endpoint:
            with self.assertRaises(release.urllib.error.HTTPError) as error:
                release.urllib.request.urlopen(endpoint + "/" + route)
            self.assertEqual(error.exception.code, 404)
            error.exception.close()
            latest = self.external / self.prefix / "alpha/@latest"
            latest.write_text('{"Version":"v0.12.0"}\n')
            with self.assertRaises(release.urllib.error.HTTPError) as latest_error:
                release.urllib.request.urlopen(endpoint + "/" + self.prefix + "/alpha/@latest")
            self.assertEqual(latest_error.exception.code, 404)
            latest_error.exception.close()
        self.assert_source_unchanged()

    def test_prepare_failure_and_cancel_leave_no_partial_output(self):
        for interrupted in (False, True):
            with self.subTest(interrupted=interrupted):
                # Arrange.
                output = self.base / ("cancel" if interrupted else "failure")
                original_run = release.run

                def fail_download(args, cwd, env=None):
                    if args[:3] == ["go", "mod", "download"]:
                        if interrupted:
                            raise KeyboardInterrupt()
                        raise release.ReleaseError("injected download failure")
                    return original_run(args, cwd, env)

                # Act / Assert.
                with mock.patch.object(release, "run", side_effect=fail_download):
                    with self.assertRaises((KeyboardInterrupt, release.ReleaseError)):
                        release.prepare(self.source, output, "v0.12.0", self.external_url)
                self.assertFalse(output.exists())
                self.assertFalse(list(self.base.glob(".guardy-prepare-*")))
                self.assert_source_unchanged()

    def test_unsupported_major_and_module_path_rejected_without_candidate(self):
        # Arrange.
        output = self.base / "unsupported"
        # Act / Assert.
        with self.assertRaises(release.ReleaseError):
            release.prepare(self.source, output, "v2.0.0", self.external_url)
        self.assertFalse(output.exists())
        self.assert_source_unchanged()
        self.write("alpha/go.mod", "module " + self.prefix + "/alpha/v2\n\ngo 1.27.1\n")
        changed = self.source_state()
        with self.assertRaises(release.ReleaseError):
            release.prepare(self.source, output, "v1.0.0", self.external_url)
        self.assertFalse(output.exists())
        self.assertEqual(self.source_state(), changed)

    def test_failed_verification_invalidates_prior_stamp(self):
        # Arrange.
        candidate = self.prepare()
        release.verify(candidate, self.external_url, linter="true")
        # Act / Assert.
        with self.assertRaises(release.subprocess.CalledProcessError):
            release.verify(candidate, self.external_url, linter="false")
        self.assertFalse((candidate / "verified.json").exists())
        self.assert_source_unchanged()

    def test_root_two_digit_major_path_rejected(self):
        # Arrange: otherwise valid standalone root with a mismatched Go major suffix.
        source = self.base / "major-source"
        source.mkdir()
        (source / "go.mod").write_text("module example.com/fixture/v10\n\ngo 1.27.1\n")
        (source / "fixture.go").write_text("package fixture\n")
        release.run(["git", "init", "--quiet"], source)
        release.run(["git", "add", "--all"], source)
        release.run(["git", "-c", "commit.gpgsign=false", "-c", "user.name=Fixture",
                     "-c", "user.email=fixture@example.invalid", "commit", "--quiet", "-m", "fixture"], source)
        output = self.base / "major-candidate"
        # Act / Assert.
        with self.assertRaises(release.ReleaseError):
            release.prepare(source, output, "v0.12.0", self.external_url)
        self.assertFalse(output.exists())
        self.assertFalse(list(self.base.glob(".guardy-prepare-*")))
        self.assert_source_unchanged()

    def verified_candidate_and_remote(self):
        candidate = self.prepare()
        release.verify(candidate, self.external_url, linter="true")
        remote = self.base / "local-remote.git"
        release.run(["git", "init", "--quiet", "--bare", str(remote)], self.base)
        return candidate, remote

    def test_verify_cancel_cleans_cache_and_invalidates_stamp(self):
        # Arrange: interrupt after real graph/test execution, during lint.
        candidate = self.prepare()
        release.verify(candidate, self.external_url, linter="true")
        original_run = release.run
        original_temporary = release.tempfile.TemporaryDirectory
        caches = []

        def record_temporary(*args, **kwargs):
            directory = original_temporary(*args, **kwargs)
            caches.append(Path(directory.name))
            return directory

        def interrupt_lint(args, cwd, env=None):
            if args[0] == "true":
                raise KeyboardInterrupt()
            return original_run(args, cwd, env)

        # Act / Assert.
        with mock.patch.object(release.tempfile, "TemporaryDirectory", side_effect=record_temporary):
            with mock.patch.object(release, "run", side_effect=interrupt_lint):
                with self.assertRaises(KeyboardInterrupt):
                    release.verify(candidate, self.external_url, linter="true")
        self.assertFalse((candidate / "verified.json").exists())
        self.assertTrue(caches)
        self.assertTrue(all(not path.exists() for path in caches))
        self.assert_source_unchanged()

    def test_publish_pushes_only_planned_tags_and_cleans_refs(self):
        # Arrange.
        candidate, remote = self.verified_candidate_and_remote()
        before = release.run(["git", "show-ref"], candidate / "repo")
        manifest = release.validate_candidate(candidate)
        # Act: disposable local bare remote only.
        release.publish(candidate, str(remote))
        # Assert.
        refs = release.run(["git", "show-ref"], remote)
        expected = {"refs/tags/" + item["tag"] for item in manifest["modules"]}
        self.assertEqual({line.split()[1] for line in refs.splitlines()}, expected)
        self.assertNotIn("unrelated-local-tag", refs)
        self.assertEqual(release.run(["git", "show-ref"], candidate / "repo"), before)
        self.assert_source_unchanged()

    def test_publish_rejection_and_cancel_clean_exact_candidate_refs(self):
        for interrupted in (False, True):
            with self.subTest(interrupted=interrupted):
                # Arrange.
                candidate = self.prepare("publish-cancel" if interrupted else "publish-failure")
                release.verify(candidate, self.external_url, linter="true")
                remote = self.base / ("cancel.git" if interrupted else "reject.git")
                release.run(["git", "init", "--quiet", "--bare", str(remote)], self.base)
                (remote / "hooks/pre-receive").write_text("#!/bin/sh\nexit 1\n")
                (remote / "hooks/pre-receive").chmod(0o755)
                before = release.run(["git", "show-ref"], candidate / "repo")
                original_run = release.run

                def fail_push(args, cwd, env=None):
                    if interrupted and args[:2] == ["git", "push"]:
                        raise KeyboardInterrupt()
                    return original_run(args, cwd, env)

                # Act / Assert.
                with mock.patch.object(release, "run", side_effect=fail_push):
                    with self.assertRaises((KeyboardInterrupt, release.subprocess.CalledProcessError)):
                        release.publish(candidate, str(remote))
                self.assertEqual(release.run(["git", "show-ref"], candidate / "repo"), before)
                self.assertEqual(release.run(["git", "for-each-ref", "--format=%(refname)"], remote), "")
                self.assert_source_unchanged()

    def test_publish_cancel_after_tag_creation_cleans_ref(self):
        # Arrange: Git completes the mutation immediately before interruption.
        candidate, remote = self.verified_candidate_and_remote()
        before = release.run(["git", "show-ref"], candidate / "repo")
        original_run = release.run

        def interrupt_after_tag(args, cwd, env=None):
            value = original_run(args, cwd, env)
            if args[:4] == ["git", "-c", "tag.gpgsign=false", "tag"]:
                raise KeyboardInterrupt()
            return value

        # Act / Assert: no transient candidate ref survives; nothing was pushed.
        with mock.patch.object(release, "run", side_effect=interrupt_after_tag):
            with self.assertRaises(KeyboardInterrupt):
                release.publish(candidate, str(remote))
        self.assertEqual(release.run(["git", "show-ref"], candidate / "repo"), before)
        self.assertEqual(release.run(["git", "for-each-ref", "--format=%(refname)"], remote), "")
        self.assert_source_unchanged()


if __name__ == "__main__":
    unittest.main()
