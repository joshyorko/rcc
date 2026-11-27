#!/usr/bin/env python3
"""Exercise a signed native provider artifact across isolated Linux homes."""
import argparse
import json
import platform
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'robot_tests'))
from environment_signed_archive import library as signed
from qualify_rattler_local import prepare_channel
from profile_package_providers import sha


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--rcc', type=Path, required=True)
    parser.add_argument('--environment', type=Path, required=True)
    parser.add_argument('--cache-home', type=Path, help='Prepared UV cache for a UV-owned runtime')
    parser.add_argument('--conda-cache', type=Path, help='Verified local Conda package archives')
    parser.add_argument('--publisher-home', type=Path)
    parser.add_argument('--wheel-dir', type=Path)
    parser.add_argument('--target')
    parser.add_argument('--scratch-parent', type=Path)
    args = parser.parse_args()
    binary = args.rcc.resolve()
    with tempfile.TemporaryDirectory(prefix='codex-provider-artifact-', dir=args.scratch_parent) as tmp:
        root = Path(tmp)
        publisher, consumer = args.publisher_home or root / 'publisher', root / 'consumer'
        publisher.mkdir(exist_ok=True)
        consumer.mkdir()
        consumer_bin=root/'consumer-bin'/binary.name
        consumer_bin.parent.mkdir()
        shutil.copy2(binary, consumer_bin)
        if args.conda_cache:
            prepare_channel(publisher, args.conda_cache)
        for name in ('uv', 'uvpython') if args.cache_home else ():
            shutil.copytree(args.cache_home / name, publisher / name, symlinks=True)
        pubenv = signed.environment_artifact_process_environment(publisher)
        if args.wheel_dir:
            pubenv.update(PIP_NO_INDEX='1', UV_NO_INDEX='1', PIP_FIND_LINKS=str(args.wheel_dir),
                          UV_FIND_LINKS=str(args.wheel_dir), RCC_ENDPOINT_PYPI=args.wheel_dir.resolve().as_uri() + '/')
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
                archive_hash = sha(archive)
            finally:
                server.terminate()
                server.wait(timeout=10)
        state = root / 'metadata-state'
        acquire_args = signed._acquire_args(archive, digest, roots, metadata_roots, state)
        wrong_authority = root / 'wrong-metadata-roots.json'
        signed._metadata_roots(wrong_authority, signed.ARTIFACT_PUBLIC)
        rejected = signed._acquire_args(archive, digest, roots, wrong_authority, root / 'rejected-state')
        denial = signed._run(consumer_bin, rejected, conenv, expected=None)
        if 'metadata-invalid' not in denial.stderr:
            raise RuntimeError(f'expected a metadata authority rejection: {denial.stderr}')
        receipt = json.loads(signed._run(consumer_bin, acquire_args, conenv).stdout)
        assert receipt['artifactDigest'] == digest and receipt['trustAttachmentsConsumed'] is True
        trust = receipt['trustCarrierPath']
        marker = root / 'executed'
        if args.wheel_dir:
            probe = ("import sys,zlib,provider_receipt; from pathlib import Path; "
                     "assert provider_receipt.value == 306; assert zlib.ZLIB_RUNTIME_VERSION == '1.3.2'; "
                     f"p=Path(sys.executable).resolve(strict=True); assert p.is_relative_to(Path({receipt['path']!r}).resolve(strict=True)); "
                     f"Path({str(marker)!r}).write_text(str(p))")
        else:
            probe = f"from pathlib import Path; Path({str(marker)!r}).write_text('executed')"
        execution = signed._run(consumer_bin, ['env', 'exec', '--artifact', digest, '--trust-carrier', trust, '--trust-carrier-type', 'filesystem', '--trust-roots', roots, '--metadata-trust-roots', metadata_roots, '--metadata-state', state, '--strict-remote', '--json', '--', 'python', '-c', probe], conenv)
        assert marker.is_file() and json.loads(execution.stdout)['exitCode'] == 0
        warm = json.loads(signed._run(consumer_bin, acquire_args, conenv).stdout)
        assert warm['artifactDigest'] == digest and warm['trustAttachmentsConsumed']
        assert warm['cacheHit'] == 'local-materialization' and warm['materializationId'] == receipt['materializationId']
        helper = binary.with_name('rcc-rattler-helper' + binary.suffix)
        print(json.dumps({'artifactDigest': digest, 'archiveSha256': archive_hash, 'rccSha256': sha(binary),
                          'helperSha256': sha(helper) if helper.is_file() else None,
                          'target': args.target or platform.platform(), 'inputSha256': sha(manifest),
                          'wrongAuthorityExit': denial.returncode, 'acquire': receipt,
                          'execExitCode': 0, 'warmAcquire': warm}))


if __name__ == '__main__':
    main()
