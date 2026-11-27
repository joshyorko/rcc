#!/usr/bin/env python3
"""Profile RCC process CPU for matched local package-provider workloads on Linux."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import shutil
import tempfile
import time
import zipfile
import statistics
import re



def sha(path):
    digest = hashlib.sha256()
    with path.open('rb') as f:
        for block in iter(lambda: f.read(1024 * 1024), b''):
            digest.update(block)
    return digest.hexdigest()


def prepare_profile_channel(output, cache, native=None):
    """Build a channel from micromamba package records in flat or nested caches."""
    import platform
    channel = output / 'channel'
    if native is None:
        native = {'x86_64': 'linux-64', 'aarch64': 'linux-aarch64'}[platform.machine()]
    records = {s: {'info': {'subdir': s}, 'packages': {}, 'packages.conda': {}, 'repodata_version': 1} for s in (native, 'noarch')}
    for record_path in sorted(cache.rglob('info/repodata_record.json')):
        record = json.loads(record_path.read_text())
        subdir, filename = record.get('subdir'), record.get('fn')
        if subdir not in records or not filename or not record.get('sha256'):
            continue
        if Path(filename).name != filename or filename in ('.', '..'):
            raise ValueError('invalid package archive filename')
        candidates = (record_path.parents[2] / filename, record_path.parent.parent / filename, cache / filename)
        archive = next((p for p in candidates if p.is_file()), None)
        if archive is None:
            continue
        if sha(archive) != record['sha256'] or archive.stat().st_size != record.get('size'):
            raise ValueError(f'archive verification failed: {archive}')
        target = channel / subdir / filename
        target.parent.mkdir(parents=True, exist_ok=True)
        if target.exists():
            if sha(target) != record['sha256']:
                raise ValueError(f'conflicting package archive: {filename}')
            continue
        try:
            os.link(archive, target)
        except OSError:
            shutil.copyfile(archive, target)
        records[subdir]['packages.conda' if filename.endswith('.conda') else 'packages'][filename] = record
    for subdir, data in records.items():
        folder = channel / subdir
        folder.mkdir(parents=True, exist_ok=True)
        (folder / 'repodata.json').write_text(json.dumps(data))
    (channel / 'channeldata.json').write_text('{}')
    return channel, records


def run(argv, cwd, env, label):
    start = time.perf_counter()
    result = subprocess.run(argv, cwd=cwd, env=env, capture_output=True, timeout=600)
    elapsed = time.perf_counter() - start
    (cwd / (label + '.stdout')).write_bytes(result.stdout)
    (cwd / (label + '.stderr')).write_bytes(result.stderr)
    if result.returncode:
        raise RuntimeError(f'{label} exited {result.returncode}; see {cwd}')
    return elapsed


def retain_provider_inputs(channel, records, output):
    inventory = {'channel': str(channel), 'subdirs': {}}
    for subdir, data in records.items():
        inventory['subdirs'][subdir] = {'packages': {}, 'packages.conda': {}}
        for kind in ('packages', 'packages.conda'):
            for filename, record in data[kind].items():
                inventory['subdirs'][subdir][kind][filename] = {
                    'sha256': record['sha256'], 'size': record['size'],
                    'name': record.get('name'), 'version': record.get('version'),
                    'build': record.get('build'),
                }
    (output / 'channel-inputs.json').write_text(json.dumps(inventory, indent=2) + '\n')


def inventory_tree(root, output):
    items = []
    for path in sorted(root.rglob('*')):
        rel = str(path.relative_to(root))
        if path.is_symlink():
            items.append({'path': rel, 'symlink': os.readlink(path)})
        elif path.is_file():
            items.append({'path': rel, 'size': path.stat().st_size, 'sha256': sha(path)})
    (output / 'inventory.json').write_text(json.dumps(items, indent=2) + '\n')


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
        channel, records = prepare_profile_channel(a.output, a.cache)
        retain_provider_inputs(channel, records, a.output)
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
        for key, binary in (('baseline', a.baseline), ('candidate', a.candidate)):
            with tempfile.TemporaryDirectory(dir=a.output) as probe:
                env = dict(os.environ, ROBOCORP_HOME=probe, HOME=probe)
                for flag in ('version', 'help'):
                    run([str(binary), '--' + flag], a.output, env, f'{key}-{flag}')
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
                        (cwd/'command.json').write_text(json.dumps(cmd, indent=2)+'\n')
                        elapsed = run(cmd, cwd, env, 'rcc')
                        if mode == 'profiled':
                            for label, flags in [('flat',['-top']), ('cumulative',['-top','-cum']), ('types',['-raw'])]:
                                run(['go','tool','pprof',*flags,str(binary),str(cwd/'cpu.pprof')],cwd,os.environ,'pprof-'+label)
                        receipt['samples'].append({'case':case, 'mode':mode, 'run':n, 'phase':phase, 'seconds':elapsed})
                    python = next(home.glob('holotree/*/bin/python'))
                    run([str(python), '-I', '-c', 'import provider_receipt,zlib,sys; assert provider_receipt.value == 306; print(sys.version, zlib.ZLIB_VERSION)'], base, env, 'verify')
                    space = python.parent.parent
                    for file in space.rglob('*'):
                        if file.is_file() and (file.name.endswith('.log') or file.name in ('golden-ee.yaml', 'identity.yaml')):
                            dest = base / 'provider-files' / file.relative_to(space)
                            dest.parent.mkdir(parents=True, exist_ok=True)
                            shutil.copyfile(file, dest)
                    inventory_tree(home, base)
                    shutil.rmtree(home)
        summarize(a.output, receipt, a.baseline, a.candidate)
        shutil.rmtree(channel)
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


def spread(values):
    return {'median': round(statistics.median(values), 4), 'range': [round(min(values), 4), round(max(values), 4)]}


def summarize(output, receipt, baseline, candidate):
    summary = {}
    for case, binary in (('baseline', baseline), ('micromamba', candidate), ('rattler', candidate)):
        summary[case] = {}
        for phase in ('cold', 'warm'):
            samples = [s['seconds'] for s in receipt['samples'] if s['case'] == case and s['mode'] == 'unprofiled' and s['phase'] == phase]
            stage = {}
            for log in (output / case / 'unprofiled').glob('*/' + phase + '/rcc.stderr'):
                prior = None
                for step, seconds in re.findall(r'Progress:\s*(\d+)/15\s+v\S+\s+([\d.]+)s', log.read_text()):
                    if prior is not None:
                        stage.setdefault(prior + '->' + step, []).append(float(seconds))
                    prior = step
            summary[case][phase] = {'unprofiled_seconds': spread(samples), 'stage_intervals_seconds': {k: spread(v) for k, v in stage.items()}}
            profs = sorted((output / case / 'profiled').glob('*/' + phase + '/cpu.pprof'))
            for kind, flags in (('flat', ['-top']), ('cumulative', ['-top', '-cum'])):
                cmd = ['go', 'tool', 'pprof', *flags, str(binary), *map(str, profs)]
                result = subprocess.run(cmd, capture_output=True)
                (output / f'{case}-{phase}-{kind}.txt').write_bytes(result.stdout + result.stderr)
                if result.returncode:
                    raise RuntimeError(f'pooled pprof {kind} failed; see {output}')
            raw = subprocess.run(['go', 'tool', 'pprof', '-raw', str(binary), *map(str, profs)], capture_output=True)
            raw_text = raw.stdout.decode(errors='replace')
            (output / f'{case}-{phase}-samples.txt').write_bytes(raw.stdout + raw.stderr)
            if raw.returncode:
                raise RuntimeError(f'pooled pprof -raw failed; see {output}')
            types = re.search(r'^Samples:\n([^\n]+)', raw_text, re.M)
            if not types:
                raise RuntimeError('pprof output has no sample type header')
            found = [token.split('/')[0] for token in types[1].split()]
            summary[case][phase]['sample_types'] = found
            summary[case][phase]['missing_sample_types'] = [t for t in ('cpu', 'inuse_space', 'alloc_space') if t not in found]
    (output / 'summary.json').write_text(json.dumps(summary, indent=2) + '\n')


if __name__ == '__main__':
    main()
