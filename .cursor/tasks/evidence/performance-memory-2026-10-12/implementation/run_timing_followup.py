"""Balanced ABBA timing follow-up for disputed unchanged construction/error paths."""
import fcntl
from pathlib import Path
import shutil
import subprocess
from run_checks import EVIDENCE, ENV, ROOT

BASE = Path("/tmp/guardy-performance-baseline-20261012-final")
CAND = Path("/tmp/guardy-performance-pipeline-candidate-20261012")
shutil.copytree(BASE, CAND, dirs_exist_ok=True)
for name in ["pipeline.go", "pipeline_performance_test.go"]:
    shutil.copy2(ROOT / name, CAND / name)

with open("/tmp/library-performance-audit-20261012.lock", "w") as lock:
    fcntl.flock(lock, fcntl.LOCK_EX)
    binaries = {}
    with (EVIDENCE / "timing-followup-build.txt").open("w") as out:
        for label, checkout in [("baseline", BASE), ("candidate", CAND)]:
            binary = Path("/tmp/guardy-performance-pipeline-" + label + ".test")
            subprocess.run(["go", "test", "-c", "-o", str(binary)], cwd=checkout, env=ENV, stdout=out, stderr=subprocess.STDOUT, check=True)
            binaries[label] = binary
    files = {label: (EVIDENCE / (label + "-pipeline-followup.txt")).open("w") for label in binaries}
    try:
        for round_no in range(10):
            for label in ["baseline", "candidate", "candidate", "baseline"]:
                output = files[label]
                command = [str(binaries[label]), "-test.run=^$", "-test.bench=^BenchmarkPipelineConstruction$/^(0|1)$|^BenchmarkPipelineRunAdjacent$/^error$", "-test.benchmem", "-test.benchtime=500ms", "-test.count=1"]
                output.write("# ABBA round " + str(round_no + 1) + " " + label + "\n$ " + " ".join(command) + "\n")
                output.flush()
                subprocess.run(command, env=ENV, stdout=output, stderr=subprocess.STDOUT, check=True)
            print("ABBA round", round_no + 1, "PASS", flush=True)
    finally:
        for file in files.values():
            file.close()
with (EVIDENCE / "benchstat-pipeline-followup.txt").open("w") as out:
    subprocess.run(["/tmp/guardy-perf-tools/benchstat", str(EVIDENCE / "baseline-pipeline-followup.txt"), str(EVIDENCE / "candidate-pipeline-followup.txt")], stdout=out, stderr=subprocess.STDOUT, check=True)
