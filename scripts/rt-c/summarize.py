#!/usr/bin/env python3
from __future__ import annotations

import csv
import glob
import json
import math
import pathlib
import sys


def latencies(pattern: str) -> list[float]:
    values = []
    for path in glob.glob(pattern):
        with open(path, encoding="utf-8") as source:
            for line in source:
                fields = line.split()
                if len(fields) >= 3 and fields[2].isdigit():
                    values.append(int(fields[2]) / 1000)
    return sorted(values)


def percentile(values: list[float], fraction: float) -> float:
    if not values:
        raise ValueError("no latency samples")
    return values[max(0, math.ceil(len(values) * fraction) - 1)]


def summary(values: list[float]) -> dict[str, float | int]:
    return {
        "samples": len(values),
        "p50_ms": percentile(values, 0.50),
        "p95_ms": percentile(values, 0.95),
        "p99_ms": percentile(values, 0.99),
        "max_ms": values[-1],
    }


results = pathlib.Path(sys.argv[1])
delivery_disk_bytes = int(sys.argv[2])
with open(results / "manifest.json", encoding="utf-8") as source:
    manifest = json.load(source)
server_version_num = int(manifest["postgresql_server_version_num"])
if not 170000 <= server_version_num < 180000:
    raise ValueError(f"PostgreSQL 17 required, found {server_version_num}")
with open(results / "missing-partition-sqlstate.txt", encoding="utf-8") as source:
    missing_partition_sqlstate = source.read().strip()
if missing_partition_sqlstate != "23514":
    raise ValueError(
        f"missing partition SQLSTATE is {missing_partition_sqlstate!r}, want '23514'"
    )
query = summary(latencies(str(results / "query-.*")))
baseline = summary(latencies(str(results / "control-baseline-.*")))
pressure = summary(latencies(str(results / "control-pressure-.*")))

with open(results / "capacity.csv", encoding="utf-8") as source:
    capacity_rows = list(csv.DictReader(source))
expected_partitions = int(manifest["days"])
if len(capacity_rows) != expected_partitions:
    raise ValueError(
        f"capacity evidence has {len(capacity_rows)} partitions, want {expected_partitions}"
    )
total_bytes = sum(int(row["total_bytes"]) for row in capacity_rows)
capacity_fraction = total_bytes / delivery_disk_bytes
control_ratio = pressure["p95_ms"] / baseline["p95_ms"]

report = {
    "candidate_sha": manifest["git_revision"],
    "query": {**query, "threshold_ms": 500, "status": "PASS" if query["p95_ms"] <= 500 else "BREACHED"},
    "capacity": {
        "partitions": len(capacity_rows),
        "total_bytes": total_bytes,
        "delivery_disk_bytes": delivery_disk_bytes,
        "fraction": capacity_fraction,
        "absolute_threshold_bytes": 100_000_000_000,
        "fraction_threshold": 0.30,
        "status": "PASS" if total_bytes <= 100_000_000_000 and capacity_fraction <= 0.30 else "BREACHED",
    },
    "control": {
        "baseline": baseline,
        "under_write_pressure": pressure,
        "p95_ratio": control_ratio,
        "threshold_ratio": 2.0,
        "status": "PASS" if control_ratio <= 2.0 else "BREACHED",
    },
    "missing_partition_sqlstate": {
        "value": missing_partition_sqlstate,
        "evidence": "missing-partition-sqlstate.txt",
        "status": "PASS",
    },
}
with open(results / "summary.json", "w", encoding="utf-8") as target:
    json.dump(report, target, ensure_ascii=False, indent=2)
    target.write("\n")
print(json.dumps(report, ensure_ascii=False, indent=2))
if any(report[key]["status"] == "BREACHED" for key in ("query", "capacity", "control")):
    sys.exit(1)
