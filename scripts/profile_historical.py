#!/usr/bin/env python3
"""Run historical RCC fixtures against a verified reduced local channel (Linux)."""
import argparse
import json
import os
from pathlib import Path
import re
import select
import signal
import socket
import statistics
import shutil
import subprocess
import time

from profile_package_providers import prepare_profile_channel, retain_provider_inputs, sha

ROOT = Path(__file__).resolve().parent.parent
IMPORTS = {'medium': 'import numpy,pandas,robot', 'large': 'import numpy,pandas,scipy,pyarrow,robot',
           'heavy': 'import numpy,pandas,scipy,pyarrow,robot,sklearn,xgboost,lightgbm,plotly'}


def check_space(path):
    if shutil.disk_usage(path).free < 20 * 1024**3:
        raise RuntimeError('less than 20 GiB free on benchmark filesystem')
    fs = subprocess.check_output(['findmnt', '-no', 'FSTYPE', '-T', str(path)], text=True).strip()
    if fs == 'btrfs':
        usage = subprocess.check_output(['btrfs', 'filesystem', 'usage', '-b', str(path)], text=True)
        unallocated = re.search(r'Device unallocated:\s*(\d+)', usage)
        metadata = re.findall(r'^Metadata,[^\n]*\(([\d.]+)%\)', usage, re.M)
        if not unallocated or not metadata or int(unallocated[1]) < 5*1024**3 or max(map(float,metadata)) > 90:
            raise RuntimeError('btrfs allocation or metadata threshold prevents this benchmark')


def execute(cmd, cwd, env, label):
    start = time.monotonic()
    with (cwd/(label+'.stdout')).open('wb') as out, (cwd/(label+'.stderr')).open('wb') as err:
        with subprocess.Popen(cmd, cwd=cwd, env=env, stdout=out, stderr=err, start_new_session=True) as proc:
            fd = os.pidfd_open(proc.pid)
            try:
                if not select.select([fd], [], [], 3600)[0]:
                    os.killpg(proc.pid, signal.SIGKILL)
                _, status, usage = os.wait4(proc.pid, 0)
                proc.returncode = os.waitstatus_to_exitcode(status)
            finally:
                os.close(fd)
    return {'command': cmd, 'exit': proc.returncode, 'seconds': time.monotonic()-start,
            'user_seconds': usage.ru_utime, 'system_seconds': usage.ru_stime,
            'wait4_maxrss_kib': usage.ru_maxrss}


def parse_fixture(path):
    # The pinned historical fixtures use simple scalar conda and pip lists.
    packages, pip = [], []
    inside, pip_section = False, False
    for line in path.read_text().splitlines():
        if line == 'dependencies:':
            inside = True
        elif inside and line.strip() == '- pip:':
            pip_section = True
        elif inside and line.startswith('  - ') and not pip_section:
            packages.append(line[4:].strip())
        elif inside and pip_section and line.startswith('    - '):
            pip.append(line[6:].strip())
    if not packages or not pip:
        raise ValueError('unsupported historical fixture structure')
    return packages, pip


def inspect_cpu_profile(binary, directory):
    profile = directory / 'cpu.pprof'
    if not profile.is_file() or profile.stat().st_size == 0:
        raise RuntimeError(f'RCC did not emit a CPU profile at {profile}')
    for label, flags in [('flat', ['-top']), ('cumulative', ['-top', '-cum']), ('raw', ['-raw'])]:
        result = execute(['go', 'tool', 'pprof', *flags, str(binary), str(profile)],
                         directory, os.environ, 'pprof-' + label)
        if result['exit']:
            raise RuntimeError(f'go tool pprof failed for {profile}: see pprof-{label}.stderr')
    raw = (directory / 'pprof-raw.stdout').read_text()
    if 'cpu/nanoseconds' not in raw or 'samples/count' not in raw:
        raise RuntimeError(f'unexpected profile sample types in {profile}')
    sample_rows = re.findall(r'^\s*(\d+)\s+\d+:', raw, re.M)
    sample_count = sum(map(int, sample_rows))
    if sample_count == 0:
        raise RuntimeError(f'RCC CPU profile contains no samples: {profile}')
    return {'sha256': sha(profile), 'size_bytes': profile.stat().st_size, 'cpu_samples': sample_count}


def inventory(home, cwd, env, fixture):
    pythons = list(home.glob('holotree/*/bin/python'))
    if len(pythons) != 1:
        raise RuntimeError(f'expected one holotree Python, found {len(pythons)}')
    python = pythons[0]
    prefix = python.parent.parent
    for name in ('rcc_plan.log', 'golden-ee.yaml', 'identity.yaml', '.messages.txt'):
        if (prefix / name).is_file():
            shutil.copyfile(prefix / name, cwd / name)
    records = []
    for path in sorted((prefix/'conda-meta').glob('*.json')):
        data = json.loads(path.read_text())
        records.append({k: data.get(k) for k in ('name', 'version', 'build', 'build_number', 'sha256', 'url', 'fn')})
    files = [p for p in prefix.rglob('*') if p.is_file() and not p.is_symlink()]
    scripts = [{'path': str(p.relative_to(prefix)), 'size': p.stat().st_size, 'sha256': sha(p)}
               for p in files if p.name.endswith(('-pre-link.sh', '-post-link.sh', '-pre-unlink.sh'))]
    (cwd/'installed.json').write_text(json.dumps({'conda': records, 'resolved_conda_count':len(records), 'file_count': len(files),
        'file_bytes': sum(p.stat().st_size for p in files), 'symlink_count':sum(p.is_symlink() for p in prefix.rglob('*')),
        'link_scripts': scripts}, indent=2)+'\n')
    checks = [execute([str(python), '-m', 'pip', 'list', '--format=json'], cwd, env, 'pip-list'),
              execute([str(python), '-I', '-c', IMPORTS[fixture] + '; print("imports ok")'], cwd, env, 'imports')]
    if any(x['exit'] for x in checks):
        raise RuntimeError('installed inventory or import validation failed')
    return checks


def main():
    p = argparse.ArgumentParser(description=__doc__)
    for arg in ('baseline', 'candidate', 'cache', 'output'):
        p.add_argument('--'+arg, required=True, type=Path)
    p.add_argument('--fixture', choices=tuple(IMPORTS), required=True)
    p.add_argument('--wheel-dir', required=True, type=Path)
    p.add_argument('--case', choices=('baseline','micromamba','rattler'), action='append', help='limit execution to the specified RCC/provider arm')
    p.add_argument('--skip-profiles', action='store_true', help='time additional unprofiled repetitions after a separately retained profile run')
    p.add_argument('--pin-records', action='store_true', help='constrain every resolved Conda name, version and build within the digest-verified local channel')
    p.add_argument('--runs', type=int, default=1)
    a = p.parse_args()
    if a.runs < 1:
        p.error('--runs must be positive')
    interfaces = sorted(name for _, name in socket.if_nameindex())
    if interfaces != ['lo']:
        p.error('run the historical comparison in a network namespace with loopback only')
    baseline, candidate, cache, output = (x.resolve() for x in (a.baseline, a.candidate, a.cache, a.output))
    fixture = ROOT/'robot_tests'/f'profile_conda_{a.fixture}.yaml'
    original = subprocess.run(['git', 'show', '2a3e1a38e4343f44a57f97a496fad0c6700d2a2d:robot_tests/'+fixture.name], cwd=ROOT, capture_output=True, check=True).stdout
    if fixture.read_bytes() != original:
        raise ValueError('fixture differs from historical Git object')
    packages, pip = parse_fixture(fixture)
    wheels = a.wheel_dir.resolve()
    wheel = wheels/'robotframework-6.1.1-py3-none-any.whl'
    if sha(wheel) != 'ee0d512d557e72ed760dd075525f6226baaab309010a48f9c9bf1f416ca434f7':
        raise ValueError('historical Robot Framework wheel digest mismatch')
    output.mkdir(parents=True, exist_ok=False)
    receipt = {'fixture': str(fixture), 'fixture_sha256': sha(fixture), 'input_commit': '2a3e1a38e4343f44a57f97a496fad0c6700d2a2d',
               'semantics': 'reduced-channel solve; not exact record installation', 'network_interfaces':interfaces,
               'binaries': {str(x): sha(x) for x in (baseline, candidate)}, 'samples': []}
    try:
        channel, records = prepare_profile_channel(output, cache)
        retain_provider_inputs(channel, records, output)
        if not any(v['packages'] or v['packages.conda'] for v in records.values()):
            raise RuntimeError('no verified cached archives')
        receipt['repodata_sha256'] = {str(x.relative_to(channel)): sha(x) for x in channel.rglob('repodata.json')}
        pip_versions = {r['version'] for sub in records.values() for section in ('packages', 'packages.conda')
                        for r in sub[section].values() if r['name'] == 'pip'}
        if len(pip_versions) != 1:
            raise ValueError('expected one resolved pip version in fixture channel')
        pip_version = pip_versions.pop()
        receipt['pip_version'] = pip_version
        # The historical pip section needs an interpreter-local installer. Pin the
        # version selected by the common seed solve in all three manifests.
        packages = [*packages, 'pip='+pip_version]
        if a.pin_records:
            packages = sorted(f"{r['name']}={r['version']}={r['build']}" for sub in records.values()
                              for section in ('packages','packages.conda') for r in sub[section].values())
            receipt['semantics'] = 'all record versions and builds pinned in verified channel; RCC still invokes its solver'
        receipt['wheel_sha256'] = sha(wheel)
        for case, binary, provider in [('baseline', baseline, None), ('micromamba', candidate, 'micromamba'), ('rattler', candidate, 'rattler')]:
            if a.case and case not in a.case:
                continue
            cache_name = 'rattler-cache' if provider == 'rattler' else 'pkgs'
            seed = output/case/'cache-seed'
            for mode in (('unprofiled',) if a.skip_profiles else ('unprofiled','profiled')):
                for index in range((a.runs if mode=='unprofiled' else 1)*2):
                    n, seeded = divmod(index, 2)
                    check_space(output)
                    base = output/case/mode/str(n)/('seeded' if seeded else 'cold')
                    home = base/'home'
                    home.mkdir(parents=True)
                    if seeded:
                        shutil.copytree(seed, home/cache_name, symlinks=True)
                    (home/'channel').symlink_to(channel, target_is_directory=True)
                    (home/'wheels').symlink_to(wheels, target_is_directory=True)
                    manifest = base/'conda.yaml'
                    if provider:
                        manifest.write_text('dependencies: rcc-provider-plan-v1\nrcc:\n  schema-version: 1\n  layers:\n    - provider: '+provider+'\n      channels: [local]\n      packages: '+json.dumps(packages)+'\n    - provider: pip\n      version: '+json.dumps(pip_version)+'\n      packages: '+json.dumps(pip)+'\n')
                    else:
                        manifest.write_text('channels:\n  - '+str(channel)+'\ndependencies:\n'+''.join('  - '+x+'\n' for x in packages)+'  - pip:\n'+''.join('    - '+x+'\n' for x in pip))
                    env = dict(os.environ, HOME=str(home), ROBOCORP_HOME=str(home), TMPDIR=str(base), RCC_TELEMETRY='off',
                               PIP_CONFIG_FILE=os.devnull, PIP_NO_INDEX='1', PIP_FIND_LINKS=str(wheels),
                               RCC_ENDPOINT_PYPI=wheels.as_uri()+'/', PIP_CACHE_DIR=str(home/'pip-cache'))
                    sample = {'case': case, 'mode': mode, 'run': n, 'seeded': bool(seeded), 'phases': []}
                    receipt['samples'].append(sample)
                    try:
                        for phase in ('seeded-package-cache' if seeded else 'cold-package-cache', 'warm-holotree-replay'):
                            cwd = base/phase
                            cwd.mkdir()
                            cmd = [str(binary)] + (['--pprof', str(cwd/'cpu.pprof')] if mode == 'profiled' else []) + ['holotree', 'variables', str(manifest), '--json', '--no-retry-build']
                            result = execute(cmd, cwd, env, 'rcc')
                            result['progress_seconds'] = dict(re.findall(r'Progress:\s*(\d+)/15\s+v\S+\s+([\d.]+)s',
                                                                         (cwd/'rcc.stderr').read_text(errors='replace')))
                            sample['phases'].append({'phase': phase, **result})
                            (output/'receipt.json').write_text(json.dumps(receipt,indent=2)+'\n')
                            if mode == 'profiled':
                                sample['phases'][-1]['cpu_profile'] = inspect_cpu_profile(binary, cwd)
                            if result['exit']:
                                raise RuntimeError(f'{case}/{mode}/{n}/{phase} failed: {result["exit"]}')
                            sample['phases'][-1]['inventory_checks'] = inventory(home, cwd, env, a.fixture)
                        if not seed.exists():
                            shutil.move(home/cache_name, seed)
                    finally:
                        shutil.rmtree(home)
            shutil.rmtree(seed)
    except Exception as exc:
        receipt['failure'] = str(exc)
        raise
    finally:
        groups = {}
        for sample in receipt['samples']:
            for phase in sample['phases']:
                key = '/'.join((sample['case'], sample['mode'], phase['phase']))
                if 'inventory_checks' in phase:
                    groups.setdefault(key, []).append(phase['seconds'])
        receipt['ranges'] = {k: {'n': len(v), 'min': min(v), 'median': statistics.median(v), 'max': max(v)} for k, v in groups.items()}
        (output/'receipt.json').write_text(json.dumps(receipt, indent=2)+'\n')
        if (output/'channel').exists():
            shutil.rmtree(output/'channel')
        for seed in output.glob('*/cache-seed'):
            shutil.rmtree(seed)


if __name__ == '__main__':
    main()
