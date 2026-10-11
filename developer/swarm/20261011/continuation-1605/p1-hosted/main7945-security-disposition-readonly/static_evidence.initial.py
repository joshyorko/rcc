#!/usr/bin/env python3
"""Read existing evidence, Git objects and executable metadata; execute no target/scanner."""
import collections
import hashlib
import json
import pathlib
import shutil
import struct
import subprocess

OWN = pathlib.Path(__file__).resolve().parent
RAW = OWN.parent / "main7945-shipping-binary-security"
OLD = OWN.parent.parent / "security-consumer"
REV = "7945f956b428359928908a9f42e36417b113dafd"
BASE = "727c1ff8b679cdbbe12836e53c87734a1edb0071"
REPO = "/workspace/rcc"


def digest(p):
    h = hashlib.sha256()
    with p.open("rb") as f:
        for b in iter(lambda: f.read(65536), b""):
            h.update(b)
    return {"path": str(p), "bytes": p.stat().st_size, "sha256": h.hexdigest()}


def write(name, data):
    (OWN / name).write_text(json.dumps(data, separators=(",", ":"), ensure_ascii=False) + "\n")


def stream(p):
    s = p.read_text()
    decoder = json.JSONDecoder()
    pos = 0
    objects = []
    while pos < len(s):
        while pos < len(s) and s[pos].isspace():
            pos += 1
        if pos == len(s):
            break
        start = pos
        obj, pos = decoder.raw_decode(s, pos)
        objects.append((obj, s.count("\n", 0, start) + 1))
    return objects


def symbol_metadata(p):
    """Inspect ELF sections, PE COFF symbol count, or Mach-O nlist names only."""
    with p.open("rb") as f:
        first = f.read(64)
        read_bytes = len(first)
        if first[:6] == b"\x7fELF\x02\x01":
            offset = struct.unpack_from("<Q", first, 40)[0]
            size, count = struct.unpack_from("<HH", first, 58)
            f.seek(offset)
            sections = f.read(size * count)
            read_bytes += len(sections)
            assert len(sections) == size * count
            types = [struct.unpack_from("<I", sections, i * size + 4)[0] for i in range(count)]
            return {"format": "ELF64-little", "section_count": count,
                    "SHT_SYMTAB_present": 2 in types, "scanner_ErrNoSymbols_condition": 2 not in types,
                    "metadata_bytes_read": read_bytes}
        if first[:2] == b"MZ":
            offset = struct.unpack_from("<I", first, 60)[0]
            f.seek(offset)
            coff = f.read(24)
            read_bytes += len(coff)
            assert coff[:4] == b"PE\0\0"
            pointer, count = struct.unpack_from("<II", coff, 12)
            return {"format": "PE", "symbol_table_pointer": pointer, "COFF_symbol_count": count,
                    "scanner_ErrNoSymbols_condition": count == 0, "metadata_bytes_read": read_bytes}
        assert first[:4] == b"\xcf\xfa\xed\xfe", first[:4]
        count, size = struct.unpack_from("<II", first, 16)
        f.seek(32)
        commands = f.read(size)
        read_bytes += len(commands)
        pos = 0
        tables = []
        for _ in range(count):
            cmd, cmdsize = struct.unpack_from("<II", commands, pos)
            assert cmdsize >= 8 and pos + cmdsize <= len(commands)
            if cmd == 2:
                tables.append(struct.unpack_from("<IIII", commands, pos + 8))
            pos += cmdsize
        assert len(tables) == 1
        symoff, nsyms, stroff, strsize = tables[0]
        f.seek(symoff)
        symbols = f.read(nsyms * 16)
        f.seek(stroff)
        strings = f.read(strsize)
        read_bytes += len(symbols) + len(strings)
        assert len(symbols) == nsyms * 16 and len(strings) == strsize
        names = []
        for i in range(nsyms):
            start = struct.unpack_from("<I", symbols, i * 16)[0]
            assert start < len(strings)
            end = strings.index(b"\0", start)
            names.append(strings[start:end])
        return {"format": "MachO64-little", "nlist_symbol_count": nsyms,
                "runtime_main_present": b"runtime.main" in names,
                "scanner_ErrNoSymbols_condition": b"runtime.main" not in names,
                "metadata_bytes_read": read_bytes}


def tree(rev):
    text = subprocess.check_output(["git", "ls-tree", "-r", "-l", rev], cwd=REPO, text=True)
    entries = {}
    for line in text.splitlines():
        meta, path = line.split("\t", 1)
        mode, typ, oid, *size = meta.split()
        entries[path] = {"mode": mode, "type": typ, "oid": oid, "bytes": int(size[0]) if size and size[0].isdigit() else None}
    return entries


base, current = tree(BASE), tree(REV)
peer_path = OWN.parent / "main7945-native-independent-review/verification.json"
peer = json.loads(peer_path.read_text())["shippingInputComparison727vs7945"]
projection = {}
predicates = {"goSources": lambda p: p.endswith(".go"),
              "goModuleManifests": lambda p: p.rsplit("/", 1)[-1] in ("go.mod", "go.sum"),
              "embeddedBlobInputs": lambda p: p.startswith(("blobs/assets/", "blobs/docs/"))}
for name, predicate in predicates.items():
    a = {p: x for p, x in base.items() if predicate(p)}
    b = {p: x for p, x in current.items() if predicate(p)}
    changed = sorted(p for p in set(a) | set(b) if a.get(p) != b.get(p))
    # Match peer's explicitly literal backslash-zero/backslash-n encoding.
    enc = b"".join(p.encode() + b"\\0" + b[p]["oid"].encode() + b"\\n" for p in sorted(b))
    h = hashlib.sha256(enc).hexdigest()
    assert not changed and h == peer[name]["headManifestSha256"]
    projection[name] = {"base_count": len(a), "current_count": len(b), "changed_paths": changed,
                        "manifest_sha256": h, "encoding": "sorted path + literal\\0 + blobSHA1 + literal\\n; peer compatibility",
                        "mode_type_blob_size_equal": a == b}
assert [projection[n]["current_count"] for n in predicates] == [429, 4, 3]
policies = [p for p in current if p.rsplit("/", 1)[-1] == "SECURITY.md"]
assert not policies

summary = json.loads((RAW / "final-scan-summary.json").read_text())
print("summary_run_schema", list(summary["runs"][0]))
streams = ["01-rcc-linux64.stdout.jsonl", "02-rcc-macos64.stdout.jsonl", "03-rcc-macosarm64.stdout.jsonl", "04-rcc-windows64.exe.stdout.jsonl",
           "05-rccremote-linux64-successor.stdout.jsonl", "06-rccremote-macos64-successor.stdout.jsonl", "07-rccremote-macosarm64-successor.stdout.jsonl", "08-rccremote-windows64.exe-successor.stdout.jsonl"]
assets = json.loads((OWN.parent / "main7945-assets-independent-review/verification.json").read_text())["assets"]
rows, raw_findings, input_receipts = [], [], []
osvs = {}
for i, filename in enumerate(streams, 1):
    name = filename.split("-", 1)[1].replace("-successor.stdout.jsonl", "").replace(".stdout.jsonl", "")
    p = RAW / filename
    messages = stream(p)
    config = next(x["config"] for x, _ in messages if "config" in x)
    assert config["scanner_version"] == "v1.1.4" and config["scan_mode"] == "binary" and config["scan_level"] == "symbol"
    assert config["db_last_modified"] == "2026-10-08T22:31:09Z"
    for obj, _ in messages:
        if obj.get("osv", {}).get("id") in ("GO-2026-5970", "GO-2026-6629"):
            osvs[obj["osv"]["id"]] = obj["osv"]
    findings = [(obj["finding"], line) for obj, line in messages if "finding" in obj]
    levels = collections.Counter("symbol" if x["trace"][0].get("function") else "package" if x["trace"][0].get("package") else "module" for x, _ in findings)
    assert len(findings) == 39 and levels == {"symbol": 35, "package": 2, "module": 2}
    assert all(len(x["trace"]) == 1 and not x["trace"][0].get("position") for x, _ in findings)
    binary = pathlib.Path("/dev/shm/rcc-main7945-release-review-root/assets-review") / name / "assets" / name
    bh = digest(binary)
    assert bh["sha256"] == assets[name]["assetSha256"] and bh["bytes"] == assets[name]["assetBytes"]
    metadata = symbol_metadata(binary)
    assert metadata["scanner_ErrNoSymbols_condition"]
    for ordinal, (finding, line) in enumerate(findings, 1):
        raw_findings.append({"target": name, "raw_file": str(p), "line": line, "ordinal": ordinal, "finding": finding})
    counts = collections.Counter(x["osv"] for x, _ in findings)
    rows.append({"target": name, "raw": digest(p), "config": config, "levels": dict(levels), "counts": dict(counts),
                 "all_trace_lengths": [1], "root_or_caller_frames": 0, "binary": bh, "static_symbol_metadata": metadata})
    input_receipts.append(digest(p))
assert len(raw_findings) == 312

for osv_id, osv in osvs.items():
    affected = {s for a in osv["affected"] for p in a["ecosystem_specific"]["imports"] for s in p["symbols"]}
    wanted = {s for s in affected if s.rsplit(".", 1)[-1][0].isupper()}
    for row in rows:
        got = {".".join(filter(None, (f["finding"]["trace"][0].get("receiver"), f["finding"]["trace"][0].get("function")))) for f in raw_findings if f["target"] == row["target"] and f["finding"]["osv"] == osv_id and f["finding"]["trace"][0].get("function")}
        assert got == wanted, (osv_id, row["target"], got ^ wanted)
write("raw-findings-preserved.json", raw_findings)
write("supplied-advisories.json", osvs)

old_receipts = []
old_targets = json.loads((OLD / "govulncheck-targets.json").read_text())
for old in old_targets["scans"]:
    h = digest(OLD / old["log"])
    assert h["sha256"] == old["stdout_sha256"] and h["bytes"] == old["stdout_bytes"]
    old_receipts.append(h)

proof = {"source_commit": REV, "tree": subprocess.check_output(["git", "rev-parse", REV + "^{tree}"], cwd=REPO, text=True).strip(),
         "source_projection_comparison": projection, "peer_receipt": digest(peer_path),
         "SECURITY_md_files": policies, "policy_gap": "no root or scoped SECURITY.md in exact7945tree; product CLI docs used, no attacker-role waiver inferred",
         "scanner_tag_commit": "d1f380186385b4f64e00313f31743df8e4b89a77", "binary_streams": rows,
         "total_finding_messages": len(raw_findings), "unique_advisory_ids": list(osvs),
         "fallback_explanation": "All8 actual symbol-table preconditions trigger ErrNoSymbols; immutable v1.1.4 returns nilPkgSymbols, substitutes advisory symbols, suppresses unexported nextComposed and emits synthetic one-frame traces. No RCC entrypoint call graph is computed.",
         "all8_exact_exported_advisory_symbol_sets": True, "prior_source_logs": old_receipts,
         "prior_source_targets": old_targets, "prior_module_status": json.loads((OLD / "govulncheck-module-status.json").read_text()),
         "scanner_binary_execution": False, "target_execution": False, "source_builds": 0,
         "limits": {"global_free_bytes": shutil.disk_usage(OWN).free, "floor_bytes": 2684354560},
         "skill_reference_gaps": ["security-guidance.md and static-finding-assessment.md unavailable via supplied cloud skills.read; root/scoped SECURITY.md absent"],
         "separate_unresolved": ["15dashboardalerts mappingHTTP403; no per-alert disposition", "seven reported inherited vet diagnostics separate; no freshvet performed"]}
write("source-bound-proof.json", proof)

ranks = {}
rank = 0
for osv_id in ("GO-2026-5970", "GO-2026-6629"):
    for idx, item in enumerate(raw_findings):
        if item["finding"]["osv"] == osv_id:
            rank += 1
            ranks[idx] = rank
triaged = []
for idx, item in enumerate(raw_findings):
    f = item["finding"]
    osv = osvs[f["osv"]]
    norm = f["osv"] == "GO-2026-5970"
    triaged.append({"triage_item_id": f"triage-{idx+1:03d}", "input_id": f"{f['osv']}/{item['target']}/finding-{item['ordinal']}", "source_type": "scanner_ticket", "title": osv["summary"],
        "normalized_input": {"vulnerable_component": "golang.org/x/text", "claimed_source": "invalid UTF-8 input" if norm else "crafted Nickname-profile input with short destination buffer", "claimed_sink": "norm.Iter infinite loop" if norm else "precis Nickname transformation out-of-bounds panic", "claimed_control": "unknown", "affected_version_or_path": "v0.28.0", "preconditions": ["reachable affected operation", "input and runtime conditions in supplied advisory", "supported product trust boundary"], "impact": "denial of service if advisory conditions are reachable", "references": [osv["database_specific"]["url"]]},
        "verdict": "needs_review", "confidence": "medium", "affected_locations": [{"label": "affected dependency", "path": "go.mod", "lines": "45", "detail": "unremediated v0.28.0; binary stripped fallback provides no entrypoint trace"}],
        "reachable_path": ["affected dependency declaration and embedded module version observed", "stripped symbol tables cause synthetic advisory-symbol finding", "product entrypoint and advisory trigger not established"],
        "boundary_assessment": {"product_surface": "CLI/remote-cache server", "source_trust": "unknown", "boundary_crossed": None, "policy_basis": "noSECURITY.md; AGENTS.md describes CLI, no attacker-role exclusion established"},
        "exploitability_stack_rank": {"rank_queue": "needs_review", "rank": ranks[idx], "rationale": "norm imported in prior source scan but not called; prioritize over module-only precis. Equal-scope duplicates retain input order." if norm else "precis was module-only in prior source scan; no supported Nickname caller established. Equal-scope duplicates retain input order.", "drivers": ["source reachability unresolved", "required malformed-input/profile preconditions", "binary fallback overapproximation"]},
        "evidence": [f"{item['raw_file']}:{item['line']}", "source-bound-proof.json binds actual8bytes/headers and tool algorithm"], "counterevidence": ["727 source4target call-traced0 with identical429Go/4module/3embedded inputs", "single synthetic frame lacks RCC caller or position"], "proof_gaps": ["supported reachable malformed-input/profile boundary", "no fresh7945source scan; prior sourceDBtimestamp unrecorded and LinuxCGO1 differs from shippedCGO0", "dashboard alert mapping unavailable"], "recommended_next_step": "root review exact source/input-equivalence and fallback proof; conditional source-mode shipping-target validation only if separately admitted", "fix_finding_handoff": None})
write("triage-finding-v0.json", {"schema_version": "triage-finding/v0", "repository": {"path": REPO, "revision": REV}, "findings": triaged})
write("derivation-receipt.json", {"scope": "static evidence derivation only", "expected_numeric_exit": 0, "targets_executed": 0, "scanners_executed": 0, "all8_current_binary_hashes_match": True, "all8_stripped_fallback_preconditions_verified": True, "all312_inputs_preserved": len(triaged), "source_projection_unchanged": True, "proof": digest(OWN / "source-bound-proof.json")})
print(json.dumps({"static_rows": len(rows), "raw_findings": len(raw_findings), "symbol_shapes": 280, "real_entrypoint_traces": 0, "fallback_preconditions_verified": True, "proof": digest(OWN / "source-bound-proof.json"), "free_bytes": shutil.disk_usage(OWN).free}, separators=(",", ":")))
