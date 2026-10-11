#!/usr/bin/env python3
"""Independent stdlib read-only RCC Manifest-v1 .rcca closure verifier.

No extraction, installation, interpreter execution, network, or private-home reads.
The historical097 smoke is PRODUCED ONLY; this verifier is not consumption/N-1 proof.
Manifest/index JSON order is reconstructed from Go structs, with encoding/json's
HTML/U+2028/U+2029 escaping, rather than trusting input field order. Unknown schema
fields fail. This is a bounded v1 checker, not a replacement for RCC trust/ABI gates.
"""
import argparse
import datetime
import gzip
import hashlib
import io
import json
import os
from pathlib import Path
import posixpath
import re
import stat
import struct
import subprocess
import sys
import zipfile

SOURCE_COMMIT = "324524fffa4bd0f76c618f9fbb6ede30e733b2ff"
SOURCE_TREE = "cfae5c5ee9df0e5031e8e9ae37040b5c98ac6152"
ROOT = "rcc-environment/"
MAX_ARCHIVE = 256 << 20
MAX_MEMBERS = 32768
HEX = re.compile(r"^[0-9a-f]{64}$")
DIGEST = re.compile(r"^sha256:[0-9a-f]{64}$")
DESC = [("mediaType", str), ("digest", str), ("size", int)]
PLATFORM = [("os", str), ("arch", str), ("rccPlatform", str)]
BUILDER = [("kind", str), ("rccVersion", str), ("compatibilityKey", str)]
PYTHON = [("implementation", str), ("version", str), ("abi", str)]
OSREQ = [("family", str), ("minimumVersion", str), ("kernelMinimum", str),
         ("libc", str), ("libcMinimum", str), ("nativeArchitecture", str),
         ("translationPolicy", str), ("runtime", str), ("requiredLibraries", [str])]
CPUREQ = [("architecture", str), ("requiredFeatures", [str])]
FSREQ = [("caseSensitive", bool), ("symlinks", bool), ("junctions", bool),
         ("longPaths", bool), ("minimumMaxPath", int)]
COMPAT = [("schemaVersion", int), ("relocationVersion", str), ("python", PYTHON),
          ("os", OSREQ), ("cpu", CPUREQ), ("filesystem", FSREQ),
          ("systemRequirementsOverridden", bool)]
REQ = [("catalogReader", str), ("encoding", str), ("legacyLogicalDigestAlgorithm", str),
       ("requiredFeatures", [str]), ("compatibility", COMPAT)]
MANIFEST = [("mediaType", str), ("schemaVersion", int), ("artifactDigest", str),
            ("specification", DESC + [("sourceKind", str), ("platform", PLATFORM), ("builder", BUILDER)]),
            ("legacyBlueprint", DESC + [("legacyBlueprintKey", str)]),
            ("platform", PLATFORM), ("builder", BUILDER),
            ("catalogs", [DESC + [("legacyName", str)]]), ("objectIndex", DESC), ("requirements", REQ)]
ENTRY = [("legacyObjectId", str), ("storedDigest", str), ("storedSize", int),
         ("logicalSize", int), ("encoding", str), ("legacyLogicalDigestAlgorithm", str)]
INDEX = [("mediaType", str), ("schemaVersion", int), ("count", int),
         ("totalStoredBytes", int), ("totalLogicalBytes", int), ("encoding", str),
         ("legacyLogicalDigestAlgorithm", str), ("entries", [ENTRY])]
SOURCE_FILES = {
    "environmentartifact/manifest.go": [49, 197],
    "environmentartifact/compatibility.go": [11, 50],
    "environmentartifact/digest.go": [20, 90],
    "environmentartifact/canonical.go": [10, 47],
    "environmentartifact/index.go": [11, 95],
    "environmentartifact/archive.go": [15, 375],
    "environmentartifact/catalog.go": [22, 168],
    "environmentartifact/inventory.go": [107, 140],
    "environmentlifecycle/acquire.go": [290, 307],
    "htfs/directory.go": [47, 83],
}


def require(ok, message):
    if not ok:
        raise ValueError(message)


def sha(data):
    return hashlib.sha256(data).hexdigest()


def hash_file(path):
    h = hashlib.sha256()
    with path.open("rb") as f:
        for block in iter(lambda: f.read(1 << 20), b""):
            h.update(block)
    return h.hexdigest()


def no_duplicate_pairs(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, "duplicate JSON key: " + key)
        result[key] = value
    return result


def decode(data, label):
    try:
        return json.loads(data.decode("utf-8"), object_pairs_hook=no_duplicate_pairs,
                          parse_constant=lambda value: (_ for _ in ()).throw(ValueError("non-JSON constant " + value)))
    except (ValueError, UnicodeError) as e:
        raise ValueError(label + ": " + str(e)) from e


def shaped(value, schema, label):
    if isinstance(schema, type):
        require(type(value) is schema, label + ": wrong primitive type")
        if schema is str:
            require(not any(0xD800 <= ord(c) <= 0xDFFF for c in value), label + ": invalid surrogate")
        if schema is int:
            require(-(1 << 63) <= value < (1 << 63), label + ": integer exceeds Go int64")
        return value
    if len(schema) == 1 and not isinstance(schema[0], tuple):
        require(type(value) is list, label + ": expected array")
        return [shaped(v, schema[0], label + "[]") for v in value]
    require(type(value) is dict, label + ": expected object")
    keys = [key for key, _ in schema]
    require(set(value) == set(keys), label + ": missing/unknown fields")
    return {key: shaped(value[key], field, label + "." + key) for key, field in schema}


def go_json(value):
    # These schemas contain only strings, bounded integers, booleans and arrays.
    # Go json.Marshal preserves struct declaration order and escapes HTML plus
    # U+2028/U+2029; no float-format compatibility claim is needed or made.
    encoded = json.dumps(value, ensure_ascii=False, separators=(",", ":"), allow_nan=False)
    for old, new in (("<", "\\u003c"), (">", "\\u003e"), ("&", "\\u0026"),
                     ("\u2028", "\\u2028"), ("\u2029", "\\u2029")):
        encoded = encoded.replace(old, new)
    return encoded.encode("utf-8")


def canonical(data, schema, label):
    value = shaped(decode(data, label), schema, label)
    require(go_json(value) == data, label + ": not Go-struct canonical JSON")
    return value


def sorted_strings(values, label):
    require(all(v and type(v) is str for v in values), label + ": empty/non-string entry")
    require(values == sorted(set(values)), label + ": not sorted unique")


def digest_hex(value):
    require(type(value) is str and DIGEST.fullmatch(value) is not None, "invalid SHA256 descriptor")
    return value[7:]


def gunzip(stored, limit, label, keep=False):
    total = 0
    h = hashlib.sha256()
    pieces = []
    with gzip.GzipFile(fileobj=io.BytesIO(stored), mode="rb") as stream:
        while True:
            block = stream.read(min(1 << 16, limit - total + 1))
            if not block:
                break
            total += len(block)
            require(total <= limit, label + ": gzip logical size/budget exceeded")
            h.update(block)
            if keep:
                pieces.append(block)
    return total, h.hexdigest(), b"".join(pieces) if keep else None


def check_zip_info(info):
    n = info.filename
    require(info.orig_filename == n and "\0" not in info.orig_filename, "ZIP raw name was truncated/noncanonical")
    require(n and n.startswith(ROOT) and not n.endswith("/") and "\\" not in n
            and "\0" not in n and posixpath.normpath(n) == n
            and not n.startswith("/") and all(c not in ("", ".", "..") for c in n.split("/")),
            "unsafe/noncanonical ZIP member: " + repr(n))
    mode = info.external_attr >> 16
    require(not info.is_dir() and (stat.S_IFMT(mode) in (0, stat.S_IFREG)), "nonregular ZIP member: " + n)
    require(not (info.flag_bits & 1), "encrypted ZIP member: " + n)
    require(info.file_size <= MAX_ARCHIVE, "oversized ZIP member: " + n)
    require(not info.file_size or info.compress_size > 0, "impossible compressed member size: " + n)
    require(not info.compress_size or info.file_size // info.compress_size <= 1000,
            "unsafe ZIP compression ratio: " + n)


def catalog_check(root, manifest, entries):
    require(type(root) is dict and type(root.get("tree")) is dict, "incomplete catalog")
    platform = manifest["platform"]["rccPlatform"]
    require(root.get("platform") == platform and root.get("blueprint") == manifest["legacyBlueprint"]["legacyBlueprintKey"],
            "catalog platform/blueprint binding mismatch")
    identity = root.get("identity")
    require(type(identity) is str and identity and posixpath.basename(root.get("path", "").replace("\\", "/")) == identity,
            "catalog producer path/identity mismatch")
    windows = platform.startswith("windows_")
    seen = set()
    counts = {"regularFiles": 0, "symlinks": 0, "directories": 0, "rewriteSpans": 0}

    def name_ok(name):
        require(type(name) is str and name not in ("", ".", "..") and "/" not in name and "\0" not in name,
                "unsafe catalog component")
        if windows:
            require("\\" not in name and re.match(r"^[A-Za-z]:", name) is None, "unsafe Windows catalog component")

    def link_ok(parts, target):
        require(type(target) is str and target and not target.startswith("/"), "unsafe catalog link")
        if windows:
            require("\\" not in target and re.match(r"^[A-Za-z]:", target) is None, "unsafe Windows catalog link")
        depth = len(parts) - 1
        for component in target.split("/"):
            if component in ("", "."):
                continue
            if component == "..":
                depth -= 1
                require(depth >= 0, "catalog link escapes root")
            else:
                name_ok(component)
                depth += 1
        counts["symlinks"] += 1

    def walk(directory, parts):
        require(type(directory) is dict, "invalid catalog directory")
        require(len(parts) < 512, "catalog exceeds verifier depth bound")
        if directory.get("symlink"):
            require(parts, "catalog root cannot be link")
            link_ok(parts, directory["symlink"])
            return
        mode = directory.get("mode")
        require(type(mode) is int and mode & ~((1 << 31) | 0o777) == 0, "unsupported catalog directory mode")
        dirs, files = directory.get("subdirs"), directory.get("files")
        require(type(dirs) is dict and type(files) is dict and not (set(dirs) & set(files)), "catalog file/directory collision")
        counts["directories"] += 1
        for key, child in dirs.items():
            name_ok(key)
            require(type(child) is dict and child.get("name") == key, "catalog directory key/name mismatch")
            walk(child, parts + [key])
        for key, f in files.items():
            name_ok(key)
            require(type(f) is dict and f.get("name") == key, "catalog file key/name mismatch")
            if f.get("symlink"):
                link_ok(parts + [key], f["symlink"])
                continue
            require(type(f.get("mode")) is int and f["mode"] & ~0o777 == 0, "unsupported catalog file mode")
            logical_id = f.get("digest")
            require(logical_id in entries and type(f.get("size")) is int and f["size"] == entries[logical_id]["logicalSize"],
                    "catalog/index object or size mismatch")
            for offset in f.get("rewrite", []):
                require(type(offset) is int and 0 <= offset <= f["size"] - len(identity.encode("utf-8")), "unsafe catalog rewrite offset")
                counts["rewriteSpans"] += 1
            seen.add(logical_id)
            counts["regularFiles"] += 1
    walk(root["tree"], [])
    require(seen == set(entries), "index contains unreferenced catalog objects")
    counts["uniqueReferencedObjects"] = len(seen)
    return counts


def qualify_source(source_root):
    root = Path(source_root)
    tree = subprocess.check_output(["git", "rev-parse", "HEAD^{tree}"], cwd=root, text=True).strip()
    require(tree == SOURCE_TREE, "source checkout tree differs from pinned schema tree")
    records = []
    for path, lines in SOURCE_FILES.items():
        b = subprocess.check_output(["git", "show", SOURCE_COMMIT + ":" + path], cwd=root)
        require((root / path).read_bytes() == b, "source checkout bytes differ: " + path)
        records.append({"path": path, "sha256": sha(b), "lines": lines,
                        "url": "https://github.com/joshyorko/rcc/blob/" + SOURCE_COMMIT + "/" + path + "#L" + str(lines[0]) + "-L" + str(lines[1])})
    return {"commit": SOURCE_COMMIT, "tree": tree, "files": records}


def verify(args, result):
    p = Path(args.archive)
    require(p.is_file() and not p.is_symlink(), "archive must be a regular nonsymlink file")
    before = p.stat()
    require(before.st_size <= MAX_ARCHIVE, "archive exceeds RCC 256MiB encoded bound")
    result["archive"] = {"path": str(p.resolve()), "bytes": before.st_size, "sha256": hash_file(p),
                         "producer_label": args.producer_label, "consumption_claim": False}
    result["source"] = qualify_source(args.source_root)
    with zipfile.ZipFile(p, "r") as z:
        infos = z.infolist()
        require(len(infos) <= MAX_MEMBERS, "too many ZIP members")
        names = [i.filename for i in infos]
        require(len(set(names)) == len(names), "duplicate ZIP members")
        require(names == sorted(names), "noncanonical ZIP member ordering")
        for info in infos:
            check_zip_info(info)
        require(sum(i.file_size for i in infos) <= MAX_ARCHIVE, "archive cumulative ZIP logical bytes exceed256MiB")
        info_map = {i.filename: i for i in infos}
        checked = {}

        def member(name):
            require(name in info_map, "missing ZIP member: " + name)
            data = z.read(info_map[name])  # Reads member only, validates CRC; never extracts.
            require(len(data) == info_map[name].file_size, "ZIP member size mismatch: " + name)
            checked[name] = sha(data)
            return data

        def descriptor(name, d):
            data = member(name)
            require(type(d["size"]) is int and d["size"] >= 0 and len(data) == d["size"], "descriptor size mismatch: " + name)
            require(sha(data) == digest_hex(d["digest"]), "descriptor SHA256 mismatch: " + name)
            return data

        manifest_data = member(ROOT + "manifest.json")
        manifest = canonical(manifest_data, MANIFEST, "Manifest")
        require(manifest["mediaType"] == "application/vnd.rcc.environment.manifest.v1+json" and manifest["schemaVersion"] == 1, "unsupported Manifest")
        identity = {k: v for k, v in manifest.items() if k != "artifactDigest"}
        actual_identity = "sha256:" + sha(go_json(identity))
        require(actual_identity == manifest["artifactDigest"], "Manifest Artifact identity mismatch")
        result["manifest_identity"] = {"verified": True, "artifactDigest": actual_identity,
                                      "method": "independent Go-v1 struct-order JSON reconstruction excluding artifactDigest"}
        if args.expected_artifact:
            require(actual_identity == args.expected_artifact, "Artifact differs from expected receipt")
        require(manifest["specification"]["platform"] == manifest["platform"] and manifest["specification"]["builder"] == manifest["builder"], "contradictory Manifest specification metadata")
        platform = manifest["platform"]
        require(platform["rccPlatform"] == platform["os"] + "_" + platform["arch"] and platform["rccPlatform"] in ("linux_amd64", "darwin_amd64", "darwin_arm64", "windows_amd64"), "unsupported/contradictory Manifest platform")
        req = manifest["requirements"]
        require((req["catalogReader"], req["encoding"], req["legacyLogicalDigestAlgorithm"], req["requiredFeatures"]) == ("v12", "gzip", "sha256", []), "unsupported Manifest reader/storage features")
        comp = req["compatibility"]
        require(comp["schemaVersion"] == 1 and comp["os"]["family"] == platform["os"] and comp["os"]["nativeArchitecture"] == platform["arch"] and comp["cpu"]["architecture"] == platform["arch"], "contradictory compatibility platform")
        for label, values in (("libraries", comp["os"]["requiredLibraries"]), ("CPU features", comp["cpu"]["requiredFeatures"])):
            sorted_strings(values, label)
        require(len(manifest["catalogs"]) == 1, "Manifest must contain one catalog")
        cat_d = manifest["catalogs"][0]
        legacy_key = manifest["legacyBlueprint"]["legacyBlueprintKey"]
        require(re.fullmatch(r"[0-9a-f]{16}", legacy_key) is not None and cat_d["legacyName"] == legacy_key + "v12." + platform["rccPlatform"], "catalog legacy identity mismatch")
        for d, media in ((manifest["specification"], "application/vnd.rcc.environment.specification.v1+json"),
                         (manifest["legacyBlueprint"], "application/vnd.rcc.environment.legacy-blueprint.v1+yaml"),
                         (cat_d, "application/vnd.rcc.holotree.catalog.v12+gzip"),
                         (manifest["objectIndex"], "application/vnd.rcc.environment.object-index.v1+json")):
            require(d["mediaType"] == media, "descriptor mediaType mismatch")
        spec = descriptor(ROOT + "specifications/" + digest_hex(manifest["specification"]["digest"]), manifest["specification"])
        require(type(decode(spec, "specification")) is dict, "specification must be a JSON object")
        blueprint = descriptor(ROOT + "legacy-blueprints/" + digest_hex(manifest["legacyBlueprint"]["digest"]), manifest["legacyBlueprint"])
        cat = descriptor(ROOT + "catalogs/" + digest_hex(cat_d["digest"]), cat_d)
        idx = canonical(descriptor(ROOT + "object-index.json", manifest["objectIndex"]), INDEX, "ObjectIndex")
        require(idx["mediaType"] == manifest["objectIndex"]["mediaType"] and idx["schemaVersion"] == 1 and idx["encoding"] == "gzip" and idx["legacyLogicalDigestAlgorithm"] == "sha256", "unsupported index")
        entries = idx["entries"]
        require(idx["count"] == len(entries) and idx["count"] >= 0, "object count mismatch")
        ids = [e["legacyObjectId"] for e in entries]
        require(all(HEX.fullmatch(i) is not None for i in ids) and ids == sorted(set(ids)), "legacy IDs not sorted canonical unique")
        require(all(e["storedSize"] >= 0 and e["logicalSize"] >= 0 and e["encoding"] == "gzip" and e["legacyLogicalDigestAlgorithm"] == "sha256" for e in entries), "invalid object entry")
        stored_total, logical_total = sum(e["storedSize"] for e in entries), sum(e["logicalSize"] for e in entries)
        require((stored_total, logical_total) == (idx["totalStoredBytes"], idx["totalLogicalBytes"]), "index byte totals mismatch")
        require(logical_total <= args.max_logical_bytes, "indexed logical closure exceeds verifier budget")
        by_id = {e["legacyObjectId"]: e for e in entries}
        verified_stored = {}
        for e in entries:
            h = digest_hex(e["storedDigest"])
            name = ROOT + "objects/" + h
            stored = descriptor(name, {"digest": e["storedDigest"], "size": e["storedSize"]})
            n, logical_hash, _ = gunzip(stored, e["logicalSize"], name)
            require(n == e["logicalSize"] and logical_hash == e["legacyObjectId"], "gzip logical SHA256/size mismatch: " + name)
            require(h not in verified_stored or verified_stored[h] == (e["storedSize"], e["logicalSize"], e["legacyObjectId"]), "conflicting duplicate stored object")
            verified_stored[h] = (e["storedSize"], e["logicalSize"], e["legacyObjectId"])
        _, catalog_logical_hash, catalog_json = gunzip(cat, MAX_ARCHIVE, "catalog", keep=True)
        counts = catalog_check(decode(catalog_json, "v12 catalog"), manifest, by_id)
        for name in names:
            if name not in checked:
                member(name)  # Includes optional attachments/index: ZIP CRC+SHA only.
        object_names = {ROOT + "objects/" + h for h in verified_stored}
        actual_objects = {n for n in names if n.startswith(ROOT + "objects/")}
        require(actual_objects == object_names, "unindexed object ZIP members")
        canonical_metadata = all(i.compress_type == zipfile.ZIP_STORED and (i.external_attr >> 16) == (stat.S_IFREG | 0o644) and i.extra == bytes.fromhex("555405000100000000") for i in infos)
        result["closure"] = {"verified": True, "indexedObjects": len(entries), "uniqueStoredObjects": len(verified_stored),
                             "indexedStoredBytes": stored_total, "indexedLogicalBytes": logical_total,
                             "archiveMembers": len(infos), "checkedMembers": len(checked),
                             "archiveMemberBytes": sum(i.file_size for i in infos),
                             "catalogLogicalSha256": catalog_logical_hash, "catalog": counts,
                             "canonicalNamesAndGoManifestIndexJSON": True,
                             "writerStoredModeAndEpochExtraMetadata": canonical_metadata}
        result["descriptor_checks"] = {"manifestFileSha256": sha(manifest_data), "objectIndex": manifest["objectIndex"],
                                       "catalog": cat_d, "specification": manifest["specification"],
                                       "legacyBlueprint": manifest["legacyBlueprint"]}
        result["extra_members_crc_sha256_only"] = {n: checked[n] for n in names if n.startswith(ROOT + "attestations/") or n == ROOT + "platform-index.json"}
    after = p.stat()
    require((before.st_size, before.st_mtime_ns, before.st_ino) == (after.st_size, after.st_mtime_ns, after.st_ino)
            and result["archive"]["sha256"] == hash_file(p), "archive changed during verification")
    result["archive"]["unchanged_after_read"] = True
    result["status"] = "PASS"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("archive")
    parser.add_argument("--source-root", default="/workspace/work/rcc-swarm/hosted-rc-gate")
    parser.add_argument("--producer-label", required=True, help="Exact producing source/receipt attribution; no consumption claim")
    parser.add_argument("--expected-artifact")
    parser.add_argument("--max-logical-bytes", type=int, default=2 << 30, help="Conservative verifier budget, not a new RCC format limit")
    parser.add_argument("--output", help="Optional JSON evidence file; refuses overwrite")
    args = parser.parse_args()
    result = {"schemaVersion": 1, "verifier": str(Path(__file__).resolve()), "verifierSha256": hash_file(Path(__file__)),
              "command": [sys.executable] + sys.argv, "startedAt": datetime.datetime.now(datetime.timezone.utc).isoformat(),
              "readOnlyArchive": True, "extracted": False, "executedArtifact": False,
              "limits": {"encodedArchiveBytes": MAX_ARCHIVE, "archiveMemberBytes": MAX_ARCHIVE,
                         "archiveMembers": MAX_MEMBERS, "indexedLogicalBytes": args.max_logical_bytes},
              "limitations": ["No artifact import/acquire/execute, rollback, runtime ABI/CPU/filesystem or N-1 consumption proof",
                              "No signature/provenance/SBOM/revocation authenticity or policy/freshness verification",
                              "Optional platform-index and attestation semantics are not independently validated; their ZIP CRC/SHA are recorded",
                              "Legacy SipHash blueprint key is bound to catalog metadata but not independently recomputed",
                              "Semantic specification descriptor and duplicate-free JSON object verified; full Go RawMessage canonicalization not claimed",
                              "Does not replace Go ZIP parser/reader behavior or claim full RCC schema/security test coverage",
                              "Manifest/index canonical serialization is limited to known integer/string/bool v1 schema; future/unknown fields fail"]}
    try:
        verify(args, result)
        code = 0
    except Exception as e:
        result.update(status="FAIL", errorType=type(e).__name__, error=str(e))
        code = 1
    result["numericExit"] = code
    result["completedAt"] = datetime.datetime.now(datetime.timezone.utc).isoformat()
    encoded = json.dumps(result, indent=2, ensure_ascii=False) + "\n"
    if args.output:
        try:
            with open(args.output, "x", encoding="utf-8") as f:
                f.write(encoded)
        except OSError as e:
            print("Could not create evidence output: " + str(e), file=sys.stderr)
            return 2
    print(encoded, end="")
    return code


if __name__ == "__main__":
    sys.exit(main())
