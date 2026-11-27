#!/usr/bin/env python3
"""Qualify a pinned RCC/helper pair with verified cached conda archives on Linux."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import zipfile


def prepare_channel(home, cache):
    channel = home / 'channel'
    records = {s: {'info': {'subdir': s}, 'packages': {}, 'packages.conda': {}, 'repodata_version': 1} for s in ('linux-64', 'noarch')}
    for record_path in cache.glob('*/info/repodata_record.json'):
        record = json.loads(record_path.read_text())
        subdir, filename = record.get('subdir'), record.get('fn')
        if subdir not in records or not filename:
            continue
        archive = cache / filename
        if not archive.is_file() or not record.get('sha256'):
            continue
        digest = hashlib.file_digest(archive.open('rb'), 'sha256').hexdigest()
        if digest != record['sha256'] or archive.stat().st_size != record['size']:
            raise ValueError(f'archive verification failed: {archive}')
        target = channel / subdir / filename
        target.parent.mkdir(parents=True, exist_ok=True)
        try:
            os.link(archive, target)
        except OSError:
            shutil.copyfile(archive, target)
        key = 'packages.conda' if filename.endswith('.conda') else 'packages'
        records[subdir][key][filename] = record
    for subdir, data in records.items():
        folder = channel / subdir
        folder.mkdir(parents=True, exist_ok=True)
        (folder / 'repodata.json').write_text(json.dumps(data))
    (channel / 'channeldata.json').write_text('{}')
    return channel, records


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--rcc', type=Path, required=True)
    parser.add_argument('--cache', type=Path, default=Path.home() / '.robocorp/pkgs')
    parser.add_argument('--native-only', action='store_true')
    parser.add_argument('--pypi', choices=('pip','uv'))
    parser.add_argument('--conda', choices=('rattler','micromamba'), default='rattler')
    args = parser.parse_args()
    with tempfile.TemporaryDirectory(prefix='codex-rcc-provider-') as tmp:
        root = Path(tmp)
        home = root / 'home'
        channel, records = prepare_channel(home, args.cache)
        manifest = root / 'package.yaml'
        packages = ['openssl'] if args.native_only else ['python=3.12.15', 'pip=25.0.1', 'zlib=1.3.2']
        if args.pypi=="uv":
            packages.append("uv=0.9.26")
        manifest.write_text('dependencies: rcc-provider-plan-v1\nrcc:\n  schema-version: 1\n  layers:\n    - provider: ' + args.conda + '\n      channels: [local]\n      packages: [' + ', '.join(packages) + ']\n')
        env = dict(os.environ, ROBOCORP_HOME=str(home))
        if args.pypi:
            wheels=home/"wheels"
            wheels.mkdir()
            dist="provider_receipt-1.0.dist-info"
            with zipfile.ZipFile(wheels/"provider_receipt-1.0-py3-none-any.whl","w") as wheel:
                files={"provider_receipt.py": "value=306\n", dist+"/METADATA":"Metadata-Version: 2.1\nName: provider-receipt\nVersion: 1.0\n", dist+"/WHEEL":"Wheel-Version: 1.0\nGenerator: rcc-test\nRoot-Is-Purelib: true\nTag: py3-none-any\n"}
                files[dist+"/RECORD"]="".join(name+",,\n" for name in [*files,dist+"/RECORD"])
                for name,body in files.items():
                    wheel.writestr(name,body)
            with manifest.open("a") as stream:
                stream.write("    - provider: "+args.pypi+"\n      packages: [provider-receipt==1.0]\n")
            (root/"index").mkdir()
            env["RCC_ENDPOINT_PYPI"]=(root/"index").as_uri()+"/"
        proc = subprocess.run([str(args.rcc.resolve()), 'holotree', 'variables', str(manifest), '--json', '--no-retry-build'], env=env, text=True, capture_output=True, timeout=300)
        print('archives:', sum(len(d['packages']) + len(d['packages.conda']) for d in records.values()))
        print('exit:', proc.returncode, '\nstdout:', proc.stdout, '\nstderr:', proc.stderr)
        if proc.returncode:
            helper = args.rcc.resolve().with_name('rcc-rattler-helper')
            request = {'protocol': 1, 'prefix': str(root / 'diagnostic-prefix'),
                       'cache': str(root / 'diagnostic-cache'), 'platform': 'linux-64',
                       'channels': [str(channel)], 'packages': packages}
            diagnostic = subprocess.run([str(helper)], input=json.dumps(request), text=True,
                                        capture_output=True, timeout=120,
                                        env={'HOME': str(root), 'TMPDIR': str(root)})
            print('helper exit:', diagnostic.returncode, '\nstdout:', diagnostic.stdout,
                  '\nstderr:', diagnostic.stderr)
            raise SystemExit(proc.returncode)
        if args.native_only:
            executable = next(home.glob('holotree/**/bin/openssl'))
            print(subprocess.run([str(executable), 'version'], capture_output=True, text=True, check=True).stdout)
            return
        python = list(home.glob('holotree/**/bin/python'))
        if not python:
            raise SystemExit('staged python not found')
        result = subprocess.run([str(python[0]), '-c', 'import sys,zlib; print(sys.version); print(zlib.ZLIB_VERSION)'], capture_output=True, text=True, check=True)
        if args.pypi:
            subprocess.run([str(python[0]),"-I","-c","import provider_receipt; assert provider_receipt.value==306"],check=True)
            print("Installed import passed:",args.conda,args.pypi)
        print('staged interpreter:', result.stdout)


if __name__ == '__main__':
    main()
