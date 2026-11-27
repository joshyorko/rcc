#!/usr/bin/env python3
"""Measure local executable extraction and durable write costs."""
import argparse
import gzip
import hashlib
import json
import os
import platform
import shutil
import subprocess
import tempfile
import time
from pathlib import Path


def digest_file(path):
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1 << 20), b""):
            digest.update(chunk)
    return digest.hexdigest()


def decode_and_durable_write(archive, destination, decoder, expected_sha256):
    """Decode/copy, hash inline, and fsync the output in one measured section."""
    digest = hashlib.sha256()
    started = time.perf_counter()
    with destination.open("wb") as output:
        if decoder:
            process = subprocess.Popen(decoder, stdin=archive.open("rb"), stdout=subprocess.PIPE)
            try:
                while chunk := process.stdout.read(1 << 20):
                    output.write(chunk)
                    digest.update(chunk)
                if process.wait() != 0:
                    raise subprocess.CalledProcessError(process.returncode, decoder)
            finally:
                process.stdout.close()
        else:
            with archive.open("rb") as source:
                for chunk in iter(lambda: source.read(1 << 20), b""):
                    output.write(chunk)
                    digest.update(chunk)
        output.flush()
        os.fsync(output.fileno())
    elapsed = time.perf_counter() - started
    actual = digest.hexdigest()
    if actual != expected_sha256:
        raise ValueError(f"SHA-256 mismatch: expected {expected_sha256}, got {actual}")
    return elapsed, destination.stat().st_size, actual


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--embedded-gzip", type=Path, required=True)
    parser.add_argument("--expected-sha256", required=True)
    parser.add_argument("--runs", type=int, default=5)
    parser.add_argument("--receipt", type=Path, required=True)
    args = parser.parse_args()
    if args.runs != 5:
        parser.error("this receipt protocol requires exactly five runs")

    raw, embedded = args.binary.resolve(strict=True), args.embedded_gzip.resolve(strict=True)
    expected = args.expected_sha256.lower()
    if len(expected) != 64 or any(c not in "0123456789abcdef" for c in expected):
        parser.error("--expected-sha256 must be 64 lowercase hexadecimal characters")
    actual = digest_file(raw)
    if actual != expected:
        raise SystemExit(f"binary SHA-256 mismatch: expected {expected}, got {actual}")

    with tempfile.TemporaryDirectory(prefix="rcc-bootstrap-bench-") as temp_dir:
        root = Path(temp_dir)
        expanded = root / "embedded.raw"
        with gzip.open(embedded, "rb") as compressed, expanded.open("wb") as output:
            shutil.copyfileobj(compressed, output, 1 << 20)
        variants = [("raw", raw, None), ("embedded_gzip", embedded, ["gzip", "-dc"])]
        for name, command in (("gzip_opt9", ["gzip", "-9", "-n", "-c", str(raw)]),
                              ("zstd_19", ["zstd", "-19", "-q", "-c", str(raw)]),
                              ("lz4_hc", ["lz4", "-12", "-q", "-c", str(raw)])):
            if shutil.which(command[0]):
                archive = root / name
                with archive.open("wb") as output:
                    subprocess.run(command, stdout=output, check=True)
                decoder = {"gzip_opt9": ["gzip", "-dc"], "zstd_19": ["zstd", "-q", "-dc"],
                           "lz4_hc": ["lz4", "-q", "-dc"]}[name]
                variants.append((name, archive, decoder))

        results = []
        for name, archive, decoder in variants:
            measurements = []
            for run in range(1, 6):
                output = root / f"{name}-{run}.out"
                seconds, size, verified = decode_and_durable_write(archive, output, decoder, expected)
                measurements.append({"run": run, "archive_bytes": archive.stat().st_size,
                                     "decode_copy_hash_fsync_seconds": seconds,
                                     "output_bytes": size, "sha256": verified})
            results.append({"variant": name, "measurements": measurements})

        report = {"scope": "local-filesystem executable decode/copy, inline SHA-256, and output fsync microbenchmark; no network or host-cache claims",
                  "platform": platform.platform(), "machine": platform.machine(),
                  "filesystem": subprocess.check_output(["stat", "-f", "-c", "%T", str(raw.parent)], text=True).strip(),
                  "runs": 5, "expected_sha256": expected, "binary_sha256": actual,
                  "embedded_gzip_sha256": digest_file(embedded), "raw_bytes": raw.stat().st_size,
                  "expanded_bytes": expanded.stat().st_size,
                  "peak_child_rss": "omitted: GNU /usr/bin/time cannot reliably wrap this in-process pipeline without changing the timed workload",
                  "variants": results}
        args.receipt.parent.mkdir(parents=True, exist_ok=True)
        args.receipt.write_text(json.dumps(report, indent=2) + "\n")


if __name__ == "__main__":
    main()
