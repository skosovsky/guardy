"""Extra balanced samples for the noisy positive mixed-document median."""
import fcntl
import subprocess
from run_checks import EVIDENCE, ENV

with open("/tmp/library-performance-audit-20261012.lock", "w") as lock:
    fcntl.flock(lock, fcntl.LOCK_EX)
    files = {label: (EVIDENCE / (label + "-mixed-followup.txt")).open("w") for label in ["baseline", "candidate"]}
    try:
        for round_no in range(10):
            for label in ["baseline", "candidate", "candidate", "baseline"]:
                command = ["/tmp/guardy-performance-bind-" + label + ".test", "-test.run=^$", "-test.bench=^BenchmarkBindDocuments$/^mixed$", "-test.benchmem", "-test.benchtime=500ms", "-test.count=1"]
                output = files[label]
                output.write("# ABBA round " + str(round_no + 1) + " " + label + "\n$ " + " ".join(command) + "\n")
                output.flush()
                subprocess.run(command, env=ENV, stdout=output, stderr=subprocess.STDOUT, check=True)
            print("Mixed ABBA round", round_no + 1, "PASS", flush=True)
    finally:
        for output in files.values():
            output.close()
with (EVIDENCE / "benchstat-mixed-followup.txt").open("w") as out:
    subprocess.run(["/tmp/guardy-perf-tools/benchstat", str(EVIDENCE / "baseline-mixed-followup.txt"), str(EVIDENCE / "candidate-mixed-followup.txt")], stdout=out, stderr=subprocess.STDOUT, check=True)
