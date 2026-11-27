#!/usr/bin/env python3
"""Exercise a signed native provider artifact across isolated Linux homes."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'robot_tests'))
from environment_signed_archive import library as signed
from qualify_rattler_local import prepare_channel


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--rcc', type=Path, required=True)
    parser.add_argument('--environment', type=Path, required=True)
    parser.add_argument('--cache-home', type=Path, help='Prepared UV cache for a UV-owned runtime')
    parser.add_argument('--conda-cache', type=Path, help='Verified local Conda package archives')
    parser.add_argument('--scratch-parent', type=Path, default=Path('/var/tmp'))
    args = parser.parse_args()
    binary = args.rcc.resolve()
    with tempfile.TemporaryDirectory(prefix='codex-provider-artifact-', dir=args.scratch_parent) as tmp:
        root = Path(tmp)
        publisher, consumer = root / 'publisher', root / 'consumer'
        publisher.mkdir()
        consumer.mkdir()
        consumer_bin=root/'consumer-bin'/binary.name
        consumer_bin.parent.mkdir()
        shutil.copy2(binary, consumer_bin)
        if args.conda_cache:
            prepare_channel(publisher, args.conda_cache)
        for name in ('uv', 'uvpython') if args.cache_home else ():
            shutil.copytree(args.cache_home / name, publisher / name, symlinks=True)
        pubenv = signed.environment_artifact_process_environment(publisher)
        conenv = signed.environment_artifact_process_environment(consumer, offline=True)
        manifest = root / 'package.yaml'
        shutil.copyfile(args.environment, manifest)
        carrier = root / 'trust'
        with (root / 'server.out').open('w+') as out, (root / 'server.err').open('w+') as err:
            server = subprocess.Popen([str(binary), 'cache', 'serve', '--json', '--root', str(root / 'provider')], env=pubenv, stdout=out, stderr=err)
            try:
                url = signed.wait_for_json_file(root / 'server.out')['url']
                signed._run(binary, ['provider', 'add', 'office', '--type', 'http', '--url', url, '--json'], pubenv)
                key, metadata_key = root / 'artifact.key', root / 'metadata.key'
                roots, metadata_roots = root / 'roots.json', root / 'metadata-roots.json'
                signed._write_private_key(key, signed.ARTIFACT_SEED)
                signed._write_private_key(metadata_key, signed.METADATA_SEED)
                signed._trust_roots(roots, signed.ARTIFACT_KEY_ID, signed.ARTIFACT_PUBLIC)
                signed._metadata_roots(metadata_roots, signed.METADATA_PUBLIC)
                result = signed._run(binary, ['env', 'publish', '--environment', manifest, '--provider', 'office', '--trust-carrier', carrier, '--trust-carrier-type', 'filesystem', '--signing-key', key, '--signing-key-id', signed.ARTIFACT_KEY_ID, '--json'], pubenv)
                digest = json.loads(result.stdout)['artifactDigest']
                signed._install_metadata(binary, pubenv, digest, carrier, 1, metadata_key, [])
                archive = root / 'signed.rcca'
                signed._export(binary, pubenv, digest, 'office', carrier, archive)
                archive_hash = hashlib.file_digest(archive.open('rb'), 'sha256').hexdigest()
            finally:
                server.terminate()
                server.wait(timeout=10)
        state = root / 'metadata-state'
        acquire_args = signed._acquire_args(archive, digest, roots, metadata_roots, state)
        receipt = json.loads(signed._run(consumer_bin, acquire_args, conenv).stdout)
        assert receipt['artifactDigest'] == digest and receipt['trustAttachmentsConsumed'] is True
        trust = receipt['trustCarrierPath']
        marker = root / 'executed'
        execution = signed._run(consumer_bin, ['env', 'exec', '--artifact', digest, '--trust-carrier', trust, '--trust-carrier-type', 'filesystem', '--trust-roots', roots, '--metadata-trust-roots', metadata_roots, '--metadata-state', state, '--strict-remote', '--json', '--', 'python', '-c', f"from pathlib import Path; Path({str(marker)!r}).write_text('executed')"], conenv)
        assert marker.read_text() == 'executed' and json.loads(execution.stdout)['exitCode'] == 0
        warm = json.loads(signed._run(consumer_bin, acquire_args, conenv).stdout)
        assert warm['artifactDigest'] == digest
        print(json.dumps({'artifactDigest': digest, 'archiveSha256': archive_hash, 'rccSha256': hashlib.file_digest(binary.open('rb'), 'sha256').hexdigest(), 'helperSha256': hashlib.file_digest(binary.with_name('rcc-rattler-helper').open('rb'), 'sha256').hexdigest(), 'acquire': receipt, 'execExitCode': 0, 'warmAcquire': warm}, indent=2))


if __name__ == '__main__':
    main()
