#!/usr/bin/env bash
# Run after helm dependency build; CHART_DIR may select a prepared chart copy.
set -euo pipefail
export CHART_DIR="${CHART_DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
python3 - <<'PYTEST'
import os
import subprocess

base = [os.environ.get("HELM", "helm"), "template", "gc-check", os.environ["CHART_DIR"],
        "--set", "secrets.existingSecret=gc-check",
        "--set", "postgresql.enabled=false,kafka.enabled=false,redis.enabled=false,temporal.enabled=false"]

def render(*settings):
    args = base.copy()
    for setting in settings:
        args.extend(["--set", setting])
    return subprocess.run(args, capture_output=True, text=True)

result = render()
assert result.returncode == 0, result.stderr
assert "name: GOMEMLIMIT" not in result.stdout
for quantity, expected in [("1Gi", "858993459"), ("2Gi", "1717986918"),
                           ("1G", "800000000"), ("1.5Gi", "1288490188")]:
    result = render("worker.gc.enabled=true", "worker.resources.limits.memory=" + quantity)
    assert result.returncode == 0, result.stderr
    documents = result.stdout.split("---")
    settings = [doc for doc in documents if "name: GOMEMLIMIT" in doc]
    assert len(settings) == 1, "GC settings must appear only on the worker"
    assert "deployment-worker.yaml" in settings[0]
    assert 'value: "' + expected + '"' in settings[0]
    assert result.stdout.count("name: GOGC") == 1
    print("PASS", quantity)

for setting in ["worker.resources.limits.memory=null", "worker.gc.memoryLimitRatio=0",
                "worker.gc.memoryLimitRatio=1", "worker.resources.limits.memory=1Zi",
                "worker.gc.gogc=oops", "env.GOMEMLIMIT=500MiB",
                "extraEnv[0].name=GOGC,extraEnv[0].value=50"]:
    result = render("worker.gc.enabled=true", setting)
    assert result.returncode != 0 and "worker.gc" in result.stderr, result.stderr
    print("PASS rejected", setting)
print("Worker GC rendering checks passed")
PYTEST
