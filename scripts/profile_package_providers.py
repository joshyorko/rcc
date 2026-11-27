#!/usr/bin/env python3
"""Profile RCC process CPU for matched local package-provider workloads on Linux."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import time
import zipfile

from qualify_rattler_local import prepare_channel


def sha(path):
    with path.open('rb') as f:
        return hashlib.file_digest(f, 'sha256').hexdigest()


def run(argv, cwd, env, label):
    start = time.perf_counter()
    result = subprocess.run(argv, cwd=cwd, env=env, capture_output=True, timeout=600)
    elapsed = time.perf_counter() - start
    (cwd / (label + '.stdout')).write_bytes(result.stdout)
    (cwd / (label + '.stderr')).write_bytes(result.stderr)
    if result.returncode:
        raise RuntimeError(f'{label} exited {result.returncode}; see {cwd}')
    return elapsed


def main():
    p = argparse.ArgumentParser(description=__doc__)
    for flag in ('baseline', 'candidate', 'cache', 'output'):
        p.add_argument('--' + flag, type=Path, required=True)
    p.add_argument('--runs', type=int, default=1)
    a = p.parse_args()
    if a.runs < 1:
        p.error('--runs must be positive')
    a.baseline, a.candidate, a.cache, a.output = (x.resolve() for x in (a.baseline, a.candidate, a.cache, a.output))
    a.output.mkdir(parents=True, exist_ok=False)
    try:
        channel, records = prepare_channel(a.output, a.cache)
        if not sum(len(v['packages']) + len(v['packages.conda']) for v in records.values()):
            raise RuntimeError('no verified cached archives')
        wheels = a.output / 'wheels'
        wheels.mkdir()
        dist = 'provider_receipt-1.0.dist-info'
        files = {'provider_receipt.py': 'value=306\n', dist+'/METADATA': 'Metadata-Version: 2.1\nName: provider-receipt\nVersion: 1.0\n', dist+'/WHEEL': 'Wheel-Version: 1.0\nGenerator: rcc-profile\nRoot-Is-Purelib: true\nTag: py3-none-any\n'}
        files[dist+'/RECORD'] = ''.join(n+',,\n' for n in [*files, dist+'/RECORD'])
        with zipfile.ZipFile(wheels/'provider_receipt-1.0-py3-none-any.whl', 'w') as z:
            for name, body in files.items():
                z.writestr(name, body)
        receipt = {'binaries': {str(x): sha(x) for x in (a.baseline, a.candidate)}, 'samples': []}
        for case, binary, provider in [('baseline', a.baseline, None), ('micromamba', a.candidate, 'micromamba'), ('rattler', a.candidate, 'rattler')]:
            for mode in ('profiled', 'unprofiled'):
                for n in range(a.runs):
                    base = a.output / case / mode / str(n)
                    home = base / 'home'
                    home.mkdir(parents=True)
                    (home/'channel').symlink_to(channel, target_is_directory=True)
                    (home/'wheels').symlink_to(wheels, target_is_directory=True)
                    manifest = base/'conda.yaml'
                    packages = ['python=3.12.15', 'pip=25.0.1', 'zlib=1.3.2']
                    if provider:
                        manifest.write_text('dependencies: rcc-provider-plan-v1\nrcc:\n  schema-version: 1\n  layers:\n    - provider: '+provider+'\n      channels: [local]\n      packages: ['+', '.join(packages)+']\n    - provider: pip\n      packages: [provider-receipt==1.0]\n')
                    else:
                        manifest.write_text('channels:\n  - '+str(channel)+'\ndependencies:\n'+''.join('  - '+x+'\n' for x in packages)+'  - pip:\n    - provider-receipt==1.0\n')
                    env = dict(os.environ, ROBOCORP_HOME=str(home), HOME=str(home), TMPDIR=str(base), PIP_NO_INDEX='1', PIP_FIND_LINKS=str(wheels), RCC_ENDPOINT_PYPI=wheels.as_uri()+'/', RCC_TELEMETRY='off')
                    for phase in ('cold', 'warm'):
                        cwd = base/phase
                        cwd.mkdir()
                        cmd = [str(binary)]
                        if mode == 'profiled':
                            cmd += ['--pprof', str(cwd/'cpu.pprof')]
                        cmd += ['holotree', 'variables', str(manifest), '--json', '--no-retry-build']
                        elapsed = run(cmd, cwd, env, 'rcc')
                        (cwd/'command.json').write_text(json.dumps(cmd, indent=2)+'\n')
                        if mode == 'profiled':
                            for label, flags in [('flat',['-top']), ('cumulative',['-top','-cum']), ('types',['-raw'])]:
                                run(['go','tool','pprof',*flags,str(binary),str(cwd/'cpu.pprof')],cwd,os.environ,'pprof-'+label)
                        receipt['samples'].append({'case':case, 'mode':mode, 'run':n, 'phase':phase, 'seconds':elapsed})
                    python = next(home.glob('holotree/*/bin/python'))
                    run([str(python), '-I', '-c', 'import provider_receipt,zlib,sys; assert provider_receipt.value == 306; print(sys.version, zlib.ZLIB_VERSION)'], base, env, 'verify')
                    import shutil
                    shutil.rmtree(home)
        (a.output/'receipt.json').write_text(json.dumps(receipt, indent=2)+'\n')
    finally:
        # Inventory even failed runs; symlinks are recorded separately, never followed.
        lines = []
        for path in sorted(a.output.rglob('*')):
            if 'channel' in path.relative_to(a.output).parts or 'home' in path.relative_to(a.output).parts: continue
            if path.is_symlink():
                lines.append(f'SYMLINK {path.relative_to(a.output)} -> {os.readlink(path)}')
            elif path.is_file() and path.name != 'SHA256SUMS':
                lines.append(f'{sha(path)}  {path.relative_to(a.output)}')
        (a.output/'SHA256SUMS').write_text('\n'.join(lines)+'\n')


if __name__ == '__main__':
    main()
