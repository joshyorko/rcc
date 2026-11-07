#!/usr/bin/env python3
"""Temporarily remove each security guard, require its regression to fail, restore.

Run only in an exclusively owned checkout, with no concurrent Go builds/tests.
The original bytes are restored even when a test fails or times out.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--evidence", type=Path, required=True)
    args = parser.parse_args()
    args.evidence.mkdir(parents=True, exist_ok=False)
    repo = Path(__file__).resolve().parents[3]
    cases = (
        ("authentication", "artifacttrust/metadata.go",
         "!ed25519.Verify(ed25519.PublicKey(public), append([]byte(metadataSignatureDomain), data...), signature)",
         "(len(signature) < 0)",
         "TestMetadataEnvelopeAuthenticatesAllPolicyInputs", "unsigned mutation accepted"),
        ("rollback", "artifacttrust/metadata_state.go",
         "if payload.Sequence < last.Sequence || payload.Sequence == last.Sequence && digest != last.Digest {",
         "if false && (payload.Sequence < last.Sequence || payload.Sequence == last.Sequence && digest != last.Digest) {",
         "TestStrictPolicyCommitsAuthenticatedRevocationBeforeDenying", "revocation rollback:"),
    )
    env = dict(os.environ, GOMAXPROCS="2", GOTOOLCHAIN="go1.26.9")
    receipts = []
    for label, name, old, new, test, expected in cases:
        path = repo / name
        original = path.read_bytes()
        (args.evidence / (path.name + ".original")).write_bytes(original)
        text = original.decode()
        if text.count(old) != 1:
            raise SystemExit(f"source guard drifted: {name}")
        mutated = text.replace(old, new).encode()
        try:
            path.write_bytes(mutated)
            command = ["go", "test", "-p", "1", "./artifacttrust", "-run", "^" + test + "$", "-count=1", "-v"]
            result = subprocess.run(command, cwd=repo, env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=60)
            (args.evidence / (label + ".log")).write_bytes(result.stdout)
            receipts.append({"guard": label, "source": name, "originalSHA256": hashlib.sha256(original).hexdigest(), "mutationSHA256": hashlib.sha256(mutated).hexdigest(), "command": command, "exit": result.returncode})
            if result.returncode == 0 or expected.encode() not in result.stdout:
                raise SystemExit(f"regression did not detect {label} removal")
        finally:
            if path.read_bytes() != mutated:
                raise SystemExit(f"concurrent edit detected; original retained in {args.evidence}")
            path.write_bytes(original)
    (args.evidence / "mutations.json").write_text(json.dumps(receipts, indent=2) + "\n")
    command = ["go", "test", "-p", "1", "./artifacttrust", "-run", "^(TestMetadataEnvelopeAuthenticatesAllPolicyInputs|TestStrictPolicyCommitsAuthenticatedRevocationBeforeDenying)$", "-count=1", "-v"]
    result = subprocess.run(command, cwd=repo, env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=60)
    (args.evidence / "restored.log").write_bytes(result.stdout)
    if result.returncode:
        raise SystemExit("restored source did not pass")
    print("Both removed guards detected; original source restored and regressions pass.")


if __name__ == "__main__":
    main()
