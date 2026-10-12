"""Same-toolchain comparison. Copy only baseline-compatible benchmark files."""
from pathlib import Path
import shutil
import subprocess
from run_checks import EVIDENCE, ROOT, run

BASE = Path("/tmp/guardy-performance-baseline-20261012-final")
for name in ["pipeline_performance_test.go", "internal/jsondoc/bind_performance_test.go"]:
    shutil.copy2(ROOT / name, BASE / name)

# Construction and adjacent valid/error paths are included in the same matrix.
for label, checkout in [("baseline", BASE), ("candidate", ROOT)]:
    run(["go", "test", "-run", "^$", "-bench", "^BenchmarkPipeline", "-benchmem", "-benchtime=300ms", "-count=10"], checkout, label + "-pipeline.txt")
    run(["go", "test", "-run", "^$", "-bench", "^BenchmarkBind", "-benchmem", "-benchtime=300ms", "-count=10", "./internal/jsondoc"], checkout, label + "-bind.txt")
    run(["go", "test", "-run", "^$", "-bench", "^BenchmarkStreamComponents$", "-benchmem", "-benchtime=300ms", "-count=10"], checkout, label + "-stream.txt")

for matrix in ["pipeline", "bind", "stream"]:
    command = ["/tmp/guardy-perf-tools/benchstat", str(EVIDENCE / ("baseline-" + matrix + ".txt")), str(EVIDENCE / ("candidate-" + matrix + ".txt"))]
    with (EVIDENCE / ("benchstat-" + matrix + ".txt")).open("w") as out:
        subprocess.run(command, stdout=out, stderr=subprocess.STDOUT, check=True)
print("benchstat comparisons saved", flush=True)
