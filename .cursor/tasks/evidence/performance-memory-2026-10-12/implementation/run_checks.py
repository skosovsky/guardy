"""Serialized, fail-fast library gates. Run from repository root."""
import fcntl
import os
from pathlib import Path
import subprocess
import sys
import time

ROOT = Path.cwd()
EVIDENCE = ROOT / ".cursor/tasks/evidence/performance-memory-2026-10-12/implementation"
ENV = dict(os.environ, GOWORK="off", GOCACHE="/tmp/library-performance-gocache-20261012", GOMAXPROCS="10")


def run(command, cwd, log):
    with open("/tmp/library-performance-audit-20261012.lock", "w") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        with (EVIDENCE / log).open("w") as output:
            output.write("cwd=" + str(cwd) + "\n$ " + " ".join(command) + "\n")
            output.write(time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()) + "\n")
            output.flush()
            result = subprocess.run(command, cwd=cwd, env=ENV, stdout=output, stderr=subprocess.STDOUT)
            output.write("\nexit=" + str(result.returncode) + "\n")
        print(log, "PASS" if result.returncode == 0 else "FAIL", flush=True)
        if result.returncode:
            sys.exit(result.returncode)


if __name__ == "__main__":
    for module in [".", "ext/jsonschema", "ext/jsonredact", "build", "ext/guardyotel", "integration", "integration/downstream"]:
        run(["go", "test", "-race", "-count=1", "./..."], ROOT / module, "race-" + module.replace("/", "-").replace(".", "root") + ".txt")
