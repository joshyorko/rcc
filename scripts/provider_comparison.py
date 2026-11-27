#!/usr/bin/env python3
"""Compare legacy conda and local rattler package creation on Linux."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import time
import statistics
import sys


def digest(path):
    h = hashlib.sha256()
    with path.open('rb') as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b''):
            h.update(chunk)
    return h.hexdigest()


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--baseline', type=Path, required=True)
    p.add_argument('--candidate', type=Path, required=True)
    p.add_argument('--cache', type=Path, default=Path.home() / '.robocorp/pkgs')
    p.add_argument('--runs', type=int, default=5)
    p.add_argument('--receipt', type=Path, required=True)
    args = p.parse_args()
    if args.runs < 2:
        p.error('--runs must be at least 2 (one cold, one warm)')
    with tempfile.TemporaryDirectory(prefix='provider-comparison-') as tmp:
        root = Path(tmp)
        channel = root / 'channel'
        records = {s: {'info': {'subdir': s}, 'packages': {}, 'packages.conda': {}, 'repodata_version': 1}
                   for s in ('linux-64', 'noarch')}
        copied = 0
        for info in args.cache.glob('*/info/repodata_record.json'):
            record = json.loads(info.read_text())
            if record.get('subdir') not in records:
                continue
            archive = args.cache / record['fn']
            if not archive.is_file():
                continue
            if digest(archive) != record['sha256'] or archive.stat().st_size != record['size']:
                raise SystemExit(f'cached archive verification failed: {archive}')
            folder = channel / record['subdir']
            folder.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(archive, folder / archive.name)
            kind = 'packages.conda' if archive.suffix == '.conda' else 'packages'
            records[record['subdir']][kind][archive.name] = record
            copied += 1
        if not copied:
            raise SystemExit('no verified cached openssl archive found')
        for subdir, record in records.items():
            folder = channel / subdir
            folder.mkdir(parents=True, exist_ok=True)
            (folder / 'repodata.json').write_text(json.dumps(record))
        (channel / 'channeldata.json').write_text('{}')
        print('workload: openssl; verified archives:', copied)
        receipt = {'channel_records': copied, 'cases': {}}
        for name, binary, provider in (
            ('baseline legacy conda', args.baseline, 'conda'),
            ('candidate legacy micromamba', args.candidate, 'micromamba'),
            ('candidate rattler local', args.candidate, 'rattler'),
        ):
            print(f'\n{name}: sha256={digest(binary)}')
            samples = {phase: [] for phase in ("startup","cold","warm")}
            for n in range(args.runs):
                home = root / f'{provider}-{n}-home'
                home.mkdir()
                (home / 'channel').symlink_to(channel, target_is_directory=True)
                manifest = root / f'{provider}-{n}.yaml'
                if provider == 'rattler':
                    text = ('dependencies: rcc-provider-plan-v1\nrcc:\n  schema-version: 1\n'
                            '  layers:\n    - provider: rattler\n      channels: [local]\n      packages: [openssl]\n')
                else:
                    text = f'channels:\n  - {channel.resolve()}\ndependencies:\n  - openssl\n'
                manifest.write_text(text)
                env = dict(os.environ, ROBOCORP_HOME=str(home), HOME=str(root), TMPDIR=str(root), RCC_TELEMETRY='off')
                build = ['holotree', 'variables', str(manifest), '--json', '--no-retry-build']
                for phase in samples:
                    arguments = ['version'] if phase == 'startup' else build
                    rss = root / 'rss'
                    start = time.perf_counter()
                    wrapper = "import resource,subprocess,sys; r=subprocess.run(sys.argv[2:]); open(sys.argv[1],'w').write(str(resource.getrusage(resource.RUSAGE_CHILDREN).ru_maxrss)); sys.exit(r.returncode)"
                    result = subprocess.run([sys.executable,'-c',wrapper,str(rss),str(binary.resolve()), *arguments], env=env, text=True, capture_output=True, timeout=300)
                    elapsed = time.perf_counter() - start
                    if result.returncode:
                        raise SystemExit(f'{name} {phase}: {result.stderr}')
                    samples[phase].append({'seconds':elapsed, 'maxRSSKiB':int(rss.read_text())})
                exe = next(home.glob('holotree/*/bin/openssl'))
                version = subprocess.check_output([str(exe),'version'],text=True).strip()
            receipt['cases'][name] = {'binarySHA256': digest(binary), 'openssl':version, 'samples':samples}
            print(name, {k:round(statistics.median(x['seconds'] for x in v),3) for k,v in samples.items()},flush=True)
        args.receipt.write_text(json.dumps(receipt, indent=2)+'\n')
        print('\nAll runs completed; homes/manifests removed with temporary directory.')


if __name__ == '__main__':
    main()
