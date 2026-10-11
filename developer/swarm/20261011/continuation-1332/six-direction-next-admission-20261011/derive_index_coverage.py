#!/usr/bin/env python3
"""Evidence-only reduction of an immutable existing index; no object reads."""
import hashlib
import json
import os
from pathlib import Path
import resource
import time

START = time.monotonic_ns()
BASE = Path(__file__).resolve().parent
INPUT_PLAN = BASE.parent / "encoding-micro/plan.json"
PLAN_SHA256 = "0b335cdc43d1bfa3eff201e0f0690176f5d3f38d04319d190f33166d8b2a5e0d"
FLOOR = 2684354560
CAP = 1048576


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def free():
    s = os.statvfs(BASE)
    return s.f_bavail * s.f_frsize


def allocated():
    return BASE.stat().st_blocks * 512 + sum(p.lstat().st_blocks * 512 for p in BASE.rglob("*"))


def save(name, value):
    raw = (json.dumps(value, indent=2, sort_keys=True) + "\n").encode()
    assert allocated() + ((len(raw) + 4095) // 4096) * 4096 <= CAP
    assert free() >= FLOOR
    with (BASE / name).open("xb") as out:
        out.write(raw)


assert free() >= FLOOR
raw_plan = INPUT_PLAN.read_bytes()
assert sha(raw_plan) == PLAN_SHA256
plan = json.loads(raw_plan)
index_file = Path(plan["fixture"]["index_file"])
raw = index_file.read_bytes()
descriptor = plan["fixture"]["index_descriptor"]
assert len(raw) == descriptor["size"]
assert sha(raw) == descriptor["digest"].split(":")[1]
index = json.loads(raw)
entries = index["entries"]
assert all(e["storedSize"] >= 0 and e["logicalSize"] >= 0 for e in entries)
totals = {"objects": len(entries), "stored_bytes": sum(e["storedSize"] for e in entries),
          "logical_bytes": sum(e["logicalSize"] for e in entries)}
assert totals["objects"] == index["count"] == 5537
assert totals["stored_bytes"] == index["totalStoredBytes"] == 69466848
assert totals["logical_bytes"] == index["totalLogicalBytes"] == 170590205
assert len({e["storedDigest"] for e in entries}) == totals["objects"]


def summarize(rows):
    n = len(rows)
    s = sum(e["storedSize"] for e in rows)
    l = sum(e["logicalSize"] for e in rows)
    return {"objects": n, "stored_bytes": s, "logical_bytes": l,
            "request_fraction": n / totals["objects"],
            "stored_body_byte_fraction": s / totals["stored_bytes"],
            "logical_byte_fraction": l / totals["logical_bytes"]}


ordered = sorted(entries, key=lambda e: (e["storedSize"], e["storedDigest"]))


def weighted_quantile(q, weight):
    # Exact integer comparison; first size whose cumulative mass reaches q.
    numerator, denominator = q
    total = sum(1 if weight == "objects" else e[weight] for e in ordered)
    running = 0
    for e in ordered:
        running += 1 if weight == "objects" else e[weight]
        if running * denominator >= total * numerator:
            return e["storedSize"]
    raise AssertionError("missing quantile")


selected = plan["fixture"]["selected"]
by_digest = {e["storedDigest"]: e for e in entries}
for e in selected:
    actual = by_digest[e["storedDigest"]]
    assert all(actual[key] == e[key] for key in ("storedSize", "logicalSize", "legacyObjectId", "encoding"))

result = {
    "schema": "rcc-existing-index-coverage-v1",
    "scope": "Static index-derived coverage only. No runtime/provider/CAS object read, codec execution, working-set trace, syscall or latency attribution.",
    "source": {"input_plan": str(INPUT_PLAN), "input_plan_sha256": PLAN_SHA256,
               "artifact_digest": plan["fixture"]["artifact_digest"],
               "index_file": str(index_file), "index_descriptor": descriptor,
               "index_raw_sha256": sha(raw), "index_schema": index["schemaVersion"],
               "producer": "historical unmodified Actions a709 stable3 attempt10; main727 is later provider component source, not original producer"},
    "totals": totals,
    "weighting": "One request per unique indexed stored object, matching accepted provider full-index GET controls. Stored bytes are HTTP body bytes; logical bytes are index-declared decompressed content bytes. Four CAS metadata objects, manifest, audit, framing, headers, native kind and actual action working set are excluded.",
    "bands": {
        "stored_below_4096": summarize([e for e in entries if e["storedSize"] < 4096]),
        "stored_4096_to_below_65536": summarize([e for e in entries if 4096 <= e["storedSize"] < 65536]),
        "stored_at_least_65536": summarize([e for e in entries if e["storedSize"] >= 65536]),
    },
    "cumulative_stored_size_less_than": [
        {"threshold_bytes": t, **summarize([e for e in entries if e["storedSize"] < t])}
        for t in (256, 1024, 4096, 16384, 65536, 262144, 1048576)
    ],
    "stored_size_weighted_quantiles": [
        {"quantile_numerator": n, "quantile_denominator": d,
         "request_weighted_stored_size": weighted_quantile((n, d), "objects"),
         "stored_byte_weighted_stored_size": weighted_quantile((n, d), "storedSize"),
         "logical_byte_weighted_stored_size": weighted_quantile((n, d), "logicalSize")}
        for n, d in ((1, 2), (9, 10), (95, 100), (99, 100))
    ],
    "largest_stored_objects_coverage": [
        {"top_n": n, **summarize(sorted(entries, key=lambda e: (-e["storedSize"], e["storedDigest"]))[:n])}
        for n in (1, 10, 20, 50, 100)
    ],
    "codec_existing_sample": summarize(selected),
    "codec_excluded_logical_above_1048576": summarize([e for e in entries if e["logicalSize"] > 1048576]),
    "codec_excluded_empty_logical": summarize([e for e in entries if e["logicalSize"] == 0]),
    "no_extrapolation": "Request or byte concentration does not identify end-to-end bottlenecks, codec effect on excluded objects, native/relocation safety, pack RPC savings, lazy touched-set or clone/image benefit.",
}
assert sum(row["objects"] for row in result["bands"].values()) == totals["objects"]
assert sum(row["stored_bytes"] for row in result["bands"].values()) == totals["stored_bytes"]
save("coverage.json", result)
usage = resource.getrusage(resource.RUSAGE_SELF)
save("computation-receipt.json", {"scope": "one evidence-only immutable index reduction, not a benchmark",
     "numeric_exit_on_success": 0, "script_sha256": sha(Path(__file__).read_bytes()),
     "coverage_sha256": sha((BASE / "coverage.json").read_bytes()),
     "wall_ns_including_index_read_validation_and_output": time.monotonic_ns() - START,
     "self_maxrss_kib": usage.ru_maxrss, "free_bytes": free(),
     "namespace_allocated_bytes_before_receipt": allocated(), "new_evidence_cap_bytes": CAP,
     "mutations": "new owned evidence files only; no fixture/provider/CAS/home/source writes"})
print(json.dumps({"exit": 0, "objects": totals["objects"], "coverage_sha256": sha((BASE / "coverage.json").read_bytes())}))
