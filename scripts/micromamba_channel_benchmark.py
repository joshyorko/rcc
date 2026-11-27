#!/usr/bin/env python3
"""Measure micromamba repodata traffic against a deterministic loopback channel."""

import argparse
import hashlib
import http.server
import json
import os
import statistics
import subprocess
import tempfile
import threading
import time
from pathlib import Path


class Channel(http.server.BaseHTTPRequestHandler):
    requests = []
    mode = "flat"

    def do_GET(self):
        type(self).requests.append({"path": self.path, "time": time.time()})
        # Empty, valid repodata provides a stable no-package solve. Shard
        # endpoints deliberately return 404 so the client exercises fallback.
        if self.path.endswith("repodata.json"):
            body = json.dumps({"info": {"subdir": self.path.split("/")[-2]}, "packages": {}, "packages.conda": {}, "repodata_version": 1}).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
        else:
            self.send_error(404)

    def log_message(self, *_args):
        pass


def digest(path):
    h = hashlib.sha256()
    with open(path, "rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            h.update(block)
    return h.hexdigest()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--runs", type=int, default=5)
    parser.add_argument("--binary", action="append", required=True, help="name=/absolute/path/to/micromamba")
    parser.add_argument("--receipt", type=Path, required=True)
    args = parser.parse_args()
    if args.runs < 1:
        parser.error("--runs must be positive")
    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Channel)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    results = []
    try:
        base = f"http://127.0.0.1:{server.server_port}"
        for item in args.binary:
            name, raw = item.split("=", 1)
            binary = Path(raw).resolve(strict=True)
            for run in range(args.runs):
                Channel.requests = []
                with tempfile.TemporaryDirectory(prefix="rcc-mm-channel-") as home:
                    env = {**os.environ, "MAMBA_ROOT_PREFIX": str(Path(home) / "root"), "MAMBA_NO_BANNER": "1"}
                    started = time.perf_counter()
                    proc = subprocess.run([str(binary), "search", "rcc-benchmark-no-such-package", "-c", base, "--override-channels", "--json"], env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=90)
                    elapsed = time.perf_counter() - started
                    results.append({"binary": name, "binary_sha256": digest(binary), "run": run + 1,
                                    "exit_code": proc.returncode, "elapsed_seconds": elapsed,
                                    "requests": list(Channel.requests), "stdout": proc.stdout.decode(errors="replace")[-2000:],
                                    "stderr": proc.stderr.decode(errors="replace")[-2000:]})
                    if proc.returncode != 0:
                        raise RuntimeError(f"{name} run {run + 1} failed ({proc.returncode}); see receipt")
    finally:
        server.shutdown()
        thread.join()
        server.server_close()
    report = {"fixture": "empty-flat-repodata-loopback", "runs_per_binary": args.runs,
              "platform": os.uname().machine, "results": results,
              "summary": {}}
    for name in sorted({r["binary"] for r in results}):
        durations = [r["elapsed_seconds"] for r in results if r["binary"] == name]
        report["summary"][name] = {"median_seconds": statistics.median(durations), "min_seconds": min(durations),
                                   "max_seconds": max(durations)}
    args.receipt.parent.mkdir(parents=True, exist_ok=True)
    args.receipt.write_text(json.dumps(report, indent=2) + "\n")


if __name__ == "__main__":
    main()
