#!/usr/bin/env python3
"""Run actual downstream boundaries in isolated source and published graphs."""
import argparse
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile


def run(command, directory, environment, log=None):
    result = subprocess.run(command, cwd=directory, env=environment,
                            stdout=subprocess.PIPE, stderr=subprocess.STDOUT, check=False)
    if log:
        log.write_bytes(result.stdout)
    if result.returncode:
        raise RuntimeError(result.stdout.decode(errors="replace"))
    return result.stdout.decode()


def copy_source(source, destination):
    shutil.copytree(source, destination, ignore=shutil.ignore_patterns(
        ".git", ".cursor", ".codex", ".agents", "__pycache__", "vendor", "*.out"))


def published_selection(target, manifest, environment):
    # Internal module versions are canonical in go.mod, including release rewrite.
    # Only external runtime selections are duplicated for intentional compatibility.
    data = json.loads(run(["go", "mod", "edit", "-json"], target, environment))
    if data.get("Replace"):
        raise RuntimeError("published consumer must not have replacements")
    selected = {item["Path"]: item["Version"] for item in data["Require"]}
    if any(selected.get(path) != version for path, version in manifest["published"].items()):
        raise RuntimeError("published selection differs from conformance manifest")
    return selected


def conformance(mode, output, source_paths):
    root = Path(__file__).resolve().parent.parent
    consumer = root / "integration/downstream"
    manifest = json.loads((consumer / "conformance.json").read_text())
    output.mkdir(parents=True, exist_ok=True)
    environment = dict(os.environ, GOENV="off", GOTOOLCHAIN="local", GOWORK="off",
                       GOFLAGS="", GOPRIVATE="", GONOPROXY="none", GONOSUMDB="",
                       GOPROXY=os.environ.get("GOPROXY", "https://proxy.golang.org"))
    with tempfile.TemporaryDirectory(prefix="guardy-downstream-") as temporary:
        workspace = Path(temporary).resolve()
        revisions = {}
        selected = {}
        if mode == "sources":
            checkout = workspace / "guardy"
            copy_source(root, checkout)
            revisions["guardy"] = {"revision": run(["git", "rev-parse", "HEAD"], root, environment).strip(), "status": run(["git", "status", "--porcelain"], root, environment)}
            directories = [checkout, checkout / "ext/jsonschema", checkout / "integration/downstream"]
            for name, selection in manifest["sources"].items():
                destination = workspace / name
                if source_paths.get(name):
                    source = source_paths[name].resolve()
                    revisions[name] = {
                        "revision": run(["git", "rev-parse", "HEAD"], source, environment).strip(),
                        "status": run(["git", "status", "--porcelain"], source, environment),
                    }
                    copy_source(source, destination)
                else:
                    run(["git", "clone", "--no-checkout", selection["repository"], str(destination)], workspace, environment)
                    run(["git", "checkout", "--detach", selection["revision"]], destination, environment)
                    revisions[name] = {"revision": run(["git", "rev-parse", "HEAD"], destination, environment).strip(), "status": ""}
                directories.append(destination)
            # The file is isolated from the caller's workspace and sibling checkouts.
            work = workspace / "go.work"
            work.write_text("go 1.27.1\n\nuse (\n" + "\n".join("\t" + json.dumps(str(path)) for path in directories) + "\n)\n")
            environment["GOWORK"] = str(work)
            target = checkout / "integration/downstream"
        else:
            target = workspace / "consumer"
            copy_source(consumer, target)
            selected = published_selection(target, manifest, environment)
        run(["go", "mod", "download"], target, environment, output / "download.log")
        graph = run(["go", "list", "-m", "-json", "all"], target, environment, output / "modules.json")
        if mode == "published" and '"Replace"' in graph:
            raise RuntimeError("published graph unexpectedly has replacements")
        (output / "selection.json").write_text(json.dumps({"mode": mode, "sources": revisions, "published": selected or manifest["published"]}, indent=2) + "\n")
        run(["go", "test", "-mod=readonly", "-race", "-count=1", "-v", "./..."], target, environment, output / "test.log")
        print(mode + ": downstream race conformance PASS (" + str(output) + ")", flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=["sources", "published"])
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--toolsy", type=Path)
    parser.add_argument("--prompty", type=Path)
    args = parser.parse_args()
    conformance(args.mode, args.output.resolve(), {"toolsy": args.toolsy, "prompty": args.prompty})


if __name__ == "__main__":
    main()
