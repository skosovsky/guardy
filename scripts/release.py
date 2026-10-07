#!/usr/bin/env python3
"""Isolated prepare -> artifact verify -> explicit atomic tag publication.

Requires Python 3.9+, Git, Go 1.27.1+ and golangci-lint for verification.
No command mutates the source checkout. Internal dependencies never fall through
our local proxy to an older public release. Candidate manifests are immutable
through verification; caches and consumer directories are disposable.
"""
from __future__ import annotations

import argparse
import contextlib
import hashlib
import http.server
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import threading
import urllib.error
import urllib.request
import zipfile


class ReleaseError(Exception):
    """Invalid candidate or failed release operation."""


def run(args, cwd, env=None):
    result = subprocess.run(args, cwd=cwd, env=env, check=True,
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    return result.stdout.decode()


def go_environment(**overrides):
    env = dict(os.environ)
    env.update(GOWORK="off", GOENV="off", GOTOOLCHAIN="local", GOFLAGS="",
               GOPRIVATE="", GONOPROXY="none", GONOSUMDB="", GOSUMDB="off")
    env.update(overrides)
    return env


def mod_json(directory):
    return json.loads(run(["go", "mod", "edit", "-json"], directory, go_environment()))


def write_json(path, value):
    path.write_text(json.dumps(value, sort_keys=True, indent=2) + "\n")


def digest(data):
    return hashlib.sha256(data).hexdigest()


def inventory(root):
    modules = []
    for manifest in sorted(root.rglob("go.mod")):
        relative = manifest.relative_to(root)
        if any(part.startswith(".") or part == "vendor" for part in relative.parts):
            continue
        data = mod_json(manifest.parent)
        modules.append({"dir": str(relative.parent), "path": data["Module"]["Path"]})
    modules.sort(key=lambda item: (item["dir"] != ".", item["dir"]))
    if not modules or modules[0]["dir"] != ".":
        raise ReleaseError("source requires a root go.mod")
    paths = {item["path"] for item in modules}
    if len(paths) != len(modules):
        raise ReleaseError("duplicate module paths")
    root_path = modules[0]["path"]
    for item in modules:
        expected = root_path if item["dir"] == "." else root_path + "/" + item["dir"]
        if item["path"] != expected or re.search(r"/v(?:[2-9]|[1-9][0-9]+)(?:/|$)", item["path"]):
            raise ReleaseError("incompatible repository module path: " + item["dir"])
    return modules


def validate_version(version):
    match = re.fullmatch(r"v(0|1)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)", version)
    if not match:
        raise ReleaseError("expected explicit v0/v1 stable version; v2+ paths are unsupported")


def source_files(source):
    names = run(["git", "ls-files", "-z", "--cached", "--others", "--exclude-standard"], source)
    return sorted(set(name for name in names.split("\0") if name))


def tree_digest(root):
    files = []
    for path in sorted(root.rglob("*")):
        relative = path.relative_to(root)
        if ".git" in relative.parts or path.is_dir():
            continue
        if path.is_symlink():
            raise ReleaseError("candidate contains unsupported symlink")
        files.append([str(relative), digest(path.read_bytes()), path.stat().st_mode & 0o777])
    return digest(json.dumps(files, separators=(",", ":")).encode())


def rewrite_manifests(root, modules, version):
    internal = {item["path"] for item in modules}
    for item in modules:
        directory = root / item["dir"]
        original = mod_json(directory)
        args = ["go", "mod", "edit"]
        for require in original.get("Require") or []:
            if require["Path"] in internal:
                args.append("-require=" + require["Path"] + "@" + version)
        for replace in original.get("Replace") or []:
            old, new = replace["Old"], replace["New"]
            if not new.get("Version") or old["Path"] in internal:
                key = old["Path"] + ("@" + old["Version"] if old.get("Version") else "")
                args.append("-dropreplace=" + key)
        args.append("-fmt")
        run(args, directory, go_environment())
        current = mod_json(directory)
        before = {r["Path"]: (r["Version"], r.get("Indirect", False))
                  for r in original.get("Require") or [] if r["Path"] not in internal}
        after = {r["Path"]: (r["Version"], r.get("Indirect", False))
                 for r in current.get("Require") or [] if r["Path"] not in internal}
        if before != after:
            raise ReleaseError("external dependencies changed")


def dependency_order(root, modules):
    by_path = {item["path"]: item for item in modules}
    visiting, visited, ordered = set(), set(), []

    def visit(path):
        if path in visiting:
            raise ReleaseError("cyclic internal release requirements cannot be staged")
        if path in visited:
            return
        visiting.add(path)
        for require in mod_json(root / by_path[path]["dir"]).get("Require") or []:
            if require["Path"] in by_path:
                visit(require["Path"])
        visiting.remove(path)
        visited.add(path)
        ordered.append(by_path[path])

    for item in modules:
        visit(item["path"])
    return ordered


def escaped(path):
    return "".join("!" + ch.lower() if ch.isupper() else ch for ch in path)


def artifact_files(root, module, modules):
    directory = root / module["dir"]
    nested = [root / item["dir"] for item in modules
              if item != module and (root / item["dir"]).is_relative_to(directory)]
    files = []
    for path in sorted(directory.rglob("*")):
        relative = path.relative_to(directory)
        if path.is_dir() or any(part in (".git", "vendor") for part in relative.parts):
            continue
        if any(path.is_relative_to(child) for child in nested):
            continue
        if path.is_symlink():
            raise ReleaseError("module zip cannot contain symlinks")
        files.append((str(relative), path))
    if directory != root and not (directory / "LICENSE").exists() and (root / "LICENSE").exists():
        files.append(("LICENSE", root / "LICENSE"))
    return sorted(files)


def stage_artifact(root, proxy, module, modules, version):
    target = proxy / escaped(module["path"]) / "@v"
    target.mkdir(parents=True, exist_ok=True)
    (target / "list").write_text(version + "\n")
    write_json(target / (version + ".info"), {"Version": version, "Time": "2000-01-01T00:00:00Z"})
    (target / (version + ".mod")).write_bytes((root / module["dir"] / "go.mod").read_bytes())
    with zipfile.ZipFile(target / (version + ".zip"), "w", compression=zipfile.ZIP_DEFLATED) as archive:
        for relative, path in artifact_files(root, module, modules):
            entry = zipfile.ZipInfo(module["path"] + "@" + version + "/" + relative,
                                    date_time=(1980, 1, 1, 0, 0, 0))
            entry.compress_type = zipfile.ZIP_DEFLATED
            entry.external_attr = (0o100644 << 16)
            archive.writestr(entry, path.read_bytes())


@contextlib.contextmanager
def module_proxy(proxy, modules, external="https://proxy.golang.org"):
    internal = {escaped(item["path"]) for item in modules}

    class Handler(http.server.BaseHTTPRequestHandler):
        def do_GET(self):
            route = self.path.split("?", 1)[0].lstrip("/")
            if ".." in route.split("/"):
                self.send_error(400)
                return
            module = route.split("/@", 1)[0]
            try:
                if module in internal:
                    # Exact internal paths have no public fallback, even for a missing archive.
                    value = (proxy / route).read_bytes()
                else:
                    with urllib.request.urlopen(external.rstrip("/") + "/" + route, timeout=30) as response:
                        value = response.read()
                self.send_response(200)
                self.end_headers()
                self.wfile.write(value)
            except FileNotFoundError:
                self.send_error(404)
            except (urllib.error.URLError, OSError):
                self.send_error(502)

        def log_message(self, *_args):
            pass

    class ProxyServer(http.server.ThreadingHTTPServer):
        # Go downloads many dependencies concurrently; the default backlog of five
        # can reset connections before a worker accepts them.
        request_queue_size = 128

    server = ProxyServer(("127.0.0.1", 0), Handler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        yield "http://127.0.0.1:" + str(server.server_port)
    finally:
        server.shutdown()
        server.server_close()
        thread.join()


def artifact_digest(proxy):
    return tree_digest(proxy)


def prepare(source, output, version, external="https://proxy.golang.org"):
    source, output = Path(source).resolve(), Path(output).resolve()
    validate_version(version)
    if output.exists() or output.is_relative_to(source):
        raise ReleaseError("candidate output must be new and outside source checkout")
    source_revision = run(["git", "rev-parse", "HEAD"], source).strip()
    files = source_files(source)
    # Atomic installation; exceptions/interrupts never leave a half-prepared candidate.
    output.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix=".guardy-prepare-", dir=output.parent) as temporary:
        candidate = Path(temporary) / "candidate"
        candidate.mkdir()
        root = candidate / "repo"
        run(["git", "clone", "--quiet", "--no-hardlinks", "--no-checkout", str(source), str(root)], source)
        for name in files:
            origin = source / name
            if not origin.exists():
                continue  # A caller's deleted tracked file is absent from the snapshot.
            if origin.is_symlink():
                raise ReleaseError("source snapshot contains unsupported symlink")
            target = root / name
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(origin, target)
        source_digest = tree_digest(root)
        for path in root.rglob("go.work*"):
            if path.name in ("go.work", "go.work.sum"):
                path.unlink()
        modules = inventory(root)
        rewrite_manifests(root, modules, version)
        order = dependency_order(root, modules)
        proxy = candidate / "proxy"
        proxy.mkdir()
        with tempfile.TemporaryDirectory(prefix="guardy-release-download-") as cache:
            with module_proxy(proxy, modules, external) as endpoint:
                environment = go_environment(GOPROXY=endpoint, GOMODCACHE=cache)
                for item in order:
                    directory = root / item["dir"]
                    before = (directory / "go.mod").read_bytes()
                    run(["go", "mod", "download", "all"], directory, environment)
                    if (directory / "go.mod").read_bytes() != before:
                        raise ReleaseError("download changed module manifest: " + item["dir"])
                    stage_artifact(root, proxy, item, modules, version)
        for item in modules:
            item["version"] = version
            item["tag"] = version if item["dir"] == "." else item["dir"] + "/" + version
        commit_environment = dict(os.environ, GIT_AUTHOR_DATE="2000-01-01T00:00:00Z",
                                  GIT_COMMITTER_DATE="2000-01-01T00:00:00Z")
        # The snapshot already contains only source_files. A --no-checkout clone
        # has no index, so tracked-but-ignored source files require force to
        # keep the published commit identical to the staged module archives.
        run(["git", "add", "--force", "--all"], root)
        run(["git", "-c", "commit.gpgsign=false", "-c", "user.name=Guardy release",
             "-c", "user.email=release@guardy.invalid", "commit", "--quiet", "--allow-empty",
             "-m", "chore: release " + version], root, commit_environment)
        manifest = {"format": 1, "version": version, "sourceRevision": source_revision,
                    "sourceDigest": source_digest, "modules": modules,
                    "candidateRevision": run(["git", "rev-parse", "HEAD"], root).strip(),
                    "treeDigest": tree_digest(root), "artifactDigest": artifact_digest(proxy)}
        write_json(candidate / "candidate.json", manifest)
        validate_candidate(candidate)
        candidate.rename(output)
    return manifest


def validate_candidate(candidate):
    candidate = Path(candidate).resolve()
    manifest = json.loads((candidate / "candidate.json").read_text())
    if manifest.get("format") != 1:
        raise ReleaseError("unsupported candidate manifest format")
    validate_version(manifest["version"])
    root, proxy = candidate / "repo", candidate / "proxy"
    modules = inventory(root)
    expected = [{"dir": item["dir"], "path": item["path"]} for item in manifest["modules"]]
    if modules != expected:
        raise ReleaseError("candidate module inventory changed")
    internal = {item["path"] for item in modules}
    for item in manifest["modules"]:
        data = mod_json(root / item["dir"])
        if item["version"] != manifest["version"]:
            raise ReleaseError("inconsistent module release version")
        planned = manifest["version"] if item["dir"] == "." else item["dir"] + "/" + manifest["version"]
        if item["tag"] != planned:
            raise ReleaseError("invalid planned tag")
        for require in data.get("Require") or []:
            if require["Path"] in internal and require["Version"] != manifest["version"]:
                raise ReleaseError("stale internal requirement")
        for replace in data.get("Replace") or []:
            if not replace["New"].get("Version") or replace["Old"]["Path"] in internal:
                raise ReleaseError("candidate replacement masks release graph")
        for suffix in (".info", ".mod", ".zip"):
            artifact = proxy / escaped(item["path"]) / "@v" / (manifest["version"] + suffix)
            if not artifact.is_file():
                raise ReleaseError("missing candidate artifact")
    if any(path.name in ("go.work", "go.work.sum") for path in root.rglob("go.work*")):
        raise ReleaseError("candidate contains workspace overrides")
    if tree_digest(root) != manifest["treeDigest"] or artifact_digest(proxy) != manifest["artifactDigest"]:
        raise ReleaseError("candidate bytes changed")
    if run(["git", "rev-parse", "HEAD"], root).strip() != manifest["candidateRevision"]:
        raise ReleaseError("candidate commit changed")
    if run(["git", "status", "--porcelain"], root).strip():
        raise ReleaseError("candidate checkout is dirty")
    return manifest


def verification_digest(manifest):
    return digest(json.dumps(manifest, sort_keys=True, separators=(",", ":")).encode())


def verify_graph(directory, modules, environment):
    expected = {item["path"]: item["version"] for item in modules}
    text = run(["go", "list", "-m", "-json", "all"], directory, environment)
    decoder = json.JSONDecoder()
    while text.strip():
        item, offset = decoder.raw_decode(text.lstrip())
        text = text.lstrip()[offset:]
        if item["Path"] in expected and not item.get("Main"):
            if item.get("Version") != expected[item["Path"]] or item.get("Replace"):
                raise ReleaseError("selected internal module graph is not the candidate")


def verify(candidate, external="https://proxy.golang.org", linter="golangci-lint"):
    candidate = Path(candidate).resolve()
    stamp = candidate / "verified.json"
    stamp.unlink(missing_ok=True)
    manifest = validate_candidate(candidate)
    modules = manifest["modules"]
    root = candidate / "repo"
    with tempfile.TemporaryDirectory(prefix="guardy-release-verify-") as temporary:
        with module_proxy(candidate / "proxy", modules, external) as endpoint:
            environment = go_environment(GOPROXY=endpoint, GOMODCACHE=str(Path(temporary) / "cache"),
                                         GOFLAGS="-mod=readonly")
            for item in modules:
                directory = root / item["dir"]
                print("verify - " + item["dir"], flush=True)
                # Enumerate the graph independently before executing test-bearing examples too.
                verify_graph(directory, modules, environment)
                run(["go", "test", "-mod=readonly", "-race", "./..."], directory, environment)
                run([linter, "run", "--allow-serial-runners", "./..."], directory, environment)
            smoke = Path(temporary) / "consumer"
            smoke.mkdir()
            requirements = "\n".join(" " + item["path"] + " " + item["version"] for item in modules)
            (smoke / "go.mod").write_text("module guardy.release/consumer\n\ngo 1.27.1\n\nrequire (\n" + requirements + "\n)\n")
            root_path = modules[0]["path"]
            smoke_paths = [root_path]
            for suffix in ("build", "ext/jsonschema", "ext/jsonredact", "ext/guardyotel"):
                if root_path + "/" + suffix in {item["path"] for item in modules}:
                    smoke_paths.append(root_path + "/" + suffix)
            (smoke / "consumer_test.go").write_text("package consumer\nimport (\n" +
                "\n".join(' _ "' + path + '"' for path in smoke_paths) + "\n)\n")
            # This disposable consumer has no source replacements. Resolve once, then freeze.
            mutable = dict(environment, GOFLAGS="")
            run(["go", "mod", "tidy"], smoke, mutable)
            verify_graph(smoke, modules, environment)
            before = {name: (smoke / name).read_bytes() for name in ("go.mod", "go.sum") if (smoke / name).exists()}
            run(["go", "test", "-mod=readonly", "-race", "./..."], smoke, environment)
            if before != {name: (smoke / name).read_bytes() for name in before}:
                raise ReleaseError("readonly consumer manifests changed")
    validate_candidate(candidate)
    write_json(stamp, {"manifestDigest": verification_digest(manifest), "checks": ["graph", "race", "lint", "consumer"]})
    return manifest


def publish(candidate, remote):
    candidate = Path(candidate).resolve()
    manifest = validate_candidate(candidate)
    verified = json.loads((candidate / "verified.json").read_text())
    if verified.get("manifestDigest") != verification_digest(manifest):
        raise ReleaseError("publication requires verification of identical candidate")
    if not remote or remote.startswith("-") or remote == "origin":
        raise ReleaseError("supply an explicit remote URL/path, never an implicit origin")
    root = candidate / "repo"
    tags = [item["tag"] for item in manifest["modules"]]
    existing = set(run(["git", "tag", "--list"], root).splitlines())
    if existing.intersection(tags):
        raise ReleaseError("planned release tag already exists")
    created = []
    try:
        for tag in tags:
            created.append(tag)
            run(["git", "-c", "tag.gpgsign=false", "tag", tag, manifest["candidateRevision"]], root)
        run(["git", "push", "--atomic", remote] + ["refs/tags/" + tag + ":refs/tags/" + tag for tag in tags], root)
    finally:
        remaining = set(run(["git", "tag", "--list"], root).splitlines())
        for tag in created:
            if tag in remaining:
                run(["git", "tag", "--delete", tag], root)


def next_version(tags, kind):
    if kind not in ("patch", "break"):
        raise ReleaseError("expected patch or break release")
    versions = [tuple(map(int, match.groups())) for tag in tags
                if (match := re.fullmatch(r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)", tag))]
    major, minor, patch = max(versions, default=(0, 0, 0))
    if kind == "patch":
        patch += 1
    elif major == 0:
        minor, patch = minor + 1, 0
    else:
        major, minor, patch = major + 1, 0, 0
    version = "v%d.%d.%d" % (major, minor, patch)
    validate_version(version)
    return version


def release_train(source, kind, module_dirs, external="https://proxy.golang.org",
                  linter="golangci-lint", confirm=input):
    source = Path(source).resolve()
    if run(["git", "status", "--porcelain"], source).strip():
        raise ReleaseError("commit source changes before release")
    modules = inventory(source)
    supplied = {str(Path(name)) for name in module_dirs.split()}
    if supplied != {item["dir"] for item in modules}:
        raise ReleaseError("Makefile module inventory differs from source")
    remote = run(["git", "remote", "get-url", "--push", "origin"], source).strip()
    refs = run(["git", "ls-remote", "--tags", "--refs", remote], source)
    tags = [line.split()[1].removeprefix("refs/tags/") for line in refs.splitlines()]
    version = next_version(tags, kind)
    print("Release " + version + " (" + kind + "), " + str(len(modules)) + " modules", flush=True)
    if confirm("Proceed with release " + version + "? [y/N] ").strip().lower() != "y":
        raise ReleaseError("release cancelled")
    with tempfile.TemporaryDirectory(prefix="guardy-release-train-") as temporary:
        candidate = Path(temporary) / "candidate"
        manifest = prepare(source, candidate, version, external)
        verify(candidate, external, linter)
        publish(candidate, remote)
    print("Released " + version + ": " + manifest["candidateRevision"], flush=True)
    return manifest


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    phases = parser.add_subparsers(dest="phase", required=True)
    prepare_args = phases.add_parser("prepare")
    prepare_args.add_argument("--source", default=".")
    prepare_args.add_argument("--output", required=True)
    prepare_args.add_argument("--version", required=True)
    verify_args = phases.add_parser("verify")
    verify_args.add_argument("candidate")
    verify_args.add_argument("--linter", default=os.environ.get("GOLANGCI_LINT", "golangci-lint"))
    publish_args = phases.add_parser("publish")
    publish_args.add_argument("candidate")
    publish_args.add_argument("--remote", required=True)
    release_args = phases.add_parser("release")
    release_args.add_argument("kind", choices=("patch", "break"))
    release_args.add_argument("--modules", required=True)
    args = parser.parse_args()
    try:
        if args.phase == "prepare":
            print(json.dumps(prepare(args.source, args.output, args.version), indent=2))
        elif args.phase == "verify":
            verify(args.candidate, linter=args.linter)
            print("candidate verified; nothing published")
        elif args.phase == "publish":
            publish(args.candidate, args.remote)
        else:
            release_train(".", args.kind, args.modules,
                          linter=os.environ.get("GOLANGCI_LINT", "golangci-lint"))
    except (ReleaseError, subprocess.CalledProcessError, OSError, ValueError) as error:
        if isinstance(error, subprocess.CalledProcessError):
            print(error.stdout.decode(), file=os.sys.stderr)
            print(error.stderr.decode(), file=os.sys.stderr)
        parser.exit(1, str(error) + "\n")


if __name__ == "__main__":
    main()
