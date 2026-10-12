"""Final F2 matrix plus increased balanced samples for both initial regressions."""
import fcntl
from pathlib import Path
import shutil
import subprocess
from run_checks import EVIDENCE, ENV, ROOT, run

BASE = Path("/tmp/guardy-performance-baseline-20261012-final")
shutil.copy2(ROOT / "internal/jsondoc/bind_performance_test.go", BASE / "internal/jsondoc/bind_performance_test.go")
for label, checkout in [("baseline", BASE), ("candidate", ROOT)]:
    run(["go", "test", "-run", "^$", "-bench", "^BenchmarkBind", "-benchmem", "-benchtime=300ms", "-count=10", "./internal/jsondoc"], checkout, label + "-bind.txt")
with (EVIDENCE / "benchstat-bind.txt").open("w") as out:
    subprocess.run(["/tmp/guardy-perf-tools/benchstat", str(EVIDENCE / "baseline-bind.txt"), str(EVIDENCE / "candidate-bind.txt")], stdout=out, stderr=subprocess.STDOUT, check=True)

with open("/tmp/library-performance-audit-20261012.lock", "w") as lock:
    fcntl.flock(lock, fcntl.LOCK_EX)
    binaries = {}
    with (EVIDENCE / "bind-followup-build.txt").open("w") as out:
        for label, checkout in [("baseline", BASE), ("candidate", ROOT)]:
            binary = Path("/tmp/guardy-performance-bind-" + label + ".test")
            subprocess.run(["go", "test", "-c", "-o", str(binary), "./internal/jsondoc"], cwd=checkout, env=ENV, stdout=out, stderr=subprocess.STDOUT, check=True)
            binaries[label] = binary
    files = {label: (EVIDENCE / (label + "-bind-followup.txt")).open("w") for label in binaries}
    try:
        for round_no in range(10):
            for label in ["baseline", "candidate", "candidate", "baseline"]:
                output = files[label]
                command = [str(binaries[label]), "-test.run=^$", "-test.bench=^BenchmarkBindAdmission$/^(malformed|trailing)$", "-test.benchmem", "-test.benchtime=300ms", "-test.count=1"]
                output.write("# ABBA round " + str(round_no + 1) + " " + label + "\n$ " + " ".join(command) + "\n")
                output.flush()
                subprocess.run(command, env=ENV, stdout=output, stderr=subprocess.STDOUT, check=True)
            print("Bind ABBA round", round_no + 1, "PASS", flush=True)
    finally:
        for file in files.values():
            file.close()
with (EVIDENCE / "benchstat-bind-followup.txt").open("w") as out:
    subprocess.run(["/tmp/guardy-perf-tools/benchstat", str(EVIDENCE / "baseline-bind-followup.txt"), str(EVIDENCE / "candidate-bind-followup.txt")], stdout=out, stderr=subprocess.STDOUT, check=True)
