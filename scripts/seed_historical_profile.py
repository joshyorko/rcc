#!/usr/bin/env python3
"""Seed a reproducible local channel from a pinned historical RCC fixture."""
import argparse
import hashlib
import json
import mmap
import os
import re
import resource
from pathlib import Path
import shutil
import subprocess
import sys
import urllib.request

from profile_historical import check_space, parse_fixture
from profile_package_providers import sha


def frozen_records(path, wanted):
    """Select immutable records without loading the entire conda-forge index."""
    selected = {}
    pattern = rb'"([^"\\]+\.(?:conda|tar\.bz2))"\s*:\s*(?=\{)'
    with path.open('rb') as stream, mmap.mmap(stream.fileno(), 0, access=mmap.ACCESS_READ) as data:
        for match in re.finditer(pattern, data):
            filename = match.group(1).decode('utf-8')
            if filename in wanted:
                for length in (8192, 65536, 1048576):
                    try:
                        record, _ = json.JSONDecoder().raw_decode(data[match.end():match.end()+length].decode('utf-8',errors='replace'))
                        break
                    except json.JSONDecodeError:
                        continue
                else:
                    raise ValueError(f'cannot decode frozen record {filename}')
                selected[filename] = record
                if len(selected) == len(wanted):
                    break
    if selected.keys() != wanted:
        raise ValueError(f'live resolution differs from snapshot: {sorted(wanted-selected.keys())}')
    return selected


def copy_verified(source, target, expected_sha, expected_size=None):
    if target.exists():
        if sha(target) != expected_sha or (expected_size is not None and target.stat().st_size != expected_size):
            raise ValueError(f"cached input verification failed: {target}")
        return
    target.parent.mkdir(parents=True, exist_ok=True)
    temporary = target.with_name(target.name + '.part')
    digest, size = hashlib.sha256(), 0
    try:
        with urllib.request.urlopen(source, timeout=90) as response, temporary.open('wb') as output:
            while block := response.read(1024 * 1024):
                size += len(block)
                if expected_size is not None and size > expected_size:
                    raise ValueError(f'archive exceeds repodata size: {target.name}')
                output.write(block)
                digest.update(block)
        if digest.hexdigest() != expected_sha or (expected_size is not None and size != expected_size):
            raise ValueError(f"download verification failed: {source}")
        temporary.replace(target)
    finally:
        temporary.unlink(missing_ok=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--fixture', required=True, type=Path, help='historical profile YAML')
    parser.add_argument('--repodata', required=True, type=Path, help='directory containing subdir/repodata.json.zst and SHA256SUMS')
    parser.add_argument('--micromamba', required=True, type=Path, help='verified micromamba 2.10.0 executable')
    parser.add_argument('--cache', required=True, type=Path, help='reusable archive cache')
    parser.add_argument('--output', required=True, type=Path, help='new output directory')
    parser.add_argument('--zstd', default='zstd', help='native zstd executable')
    parser.add_argument('--pip-version', help='common exact post-processing tool for all arms (historical fixtures leave pip unpinned)')
    parser.add_argument('--public-sharded-seed', action='store_true', help='solve against public sharded conda-forge then validate all selected records against the frozen snapshot')
    parser.add_argument('--saved-solution', type=Path, help='reuse a previous successful sharded dry-run, verifying frozen records and re-solving locally')
    args = parser.parse_args()
    fixture, repodata, binary, cache, output = (p.resolve() for p in
        (args.fixture, args.repodata, args.micromamba, args.cache, args.output))
    if output.exists():
        parser.error(f'output already exists: {output}')
    output.mkdir(parents=True)
    channel = output / 'channel'
    receipt = {'fixture': str(fixture), 'fixture_sha256': sha(fixture),
               'repodata_source': str(repodata), 'micromamba': str(binary),
               'micromamba_sha256': sha(binary), 'packages': [], 'pip': {}}
    try:
        check_space(output)
        if receipt['micromamba_sha256'] != '701f39c06f8e6a670a0e186c8479b9e5c0502a2f0cda6cef65d83bdd2875e8bd':
            raise ValueError('expected pinned Linux amd64 micromamba 2.10.0')
        sums = {}
        for line in (repodata / 'SHA256SUMS').read_text().splitlines():
            digest, name = line.split(None, 1)
            sums[str(Path(name.strip().lstrip('*')))] = digest
        for subdir in ('linux-64', 'noarch'):
            compressed = repodata / subdir / 'repodata.json.zst'
            expected = sums.get(str(compressed)) or sums[f'{subdir}/{compressed.name}']
            if sha(compressed) != expected:
                raise ValueError(f'repodata checksum mismatch: {compressed}')
            destination = channel / subdir / 'repodata.json'
            destination.parent.mkdir(parents=True, exist_ok=True)
            with destination.open('wb') as stream:
                subprocess.run([args.zstd, '-d', '-c', str(compressed)], stdout=stream, check=True)
            receipt.setdefault('repodata', {})[subdir] = {
                'compressed_sha256': expected, 'compressed_size': compressed.stat().st_size,
                'headers': (repodata / subdir / 'headers').read_text(),
                'json_sha256': sha(destination), 'json_size': destination.stat().st_size}
        packages, pip_requirements = parse_fixture(fixture)
        if not any(spec.split('=', 1)[0] == 'pip' for spec in packages):
            packages.append('pip='+args.pip_version if args.pip_version else 'pip')
        receipt['common_installer_pin'] = args.pip_version
        route = ['-c','https://conda.anaconda.org/conda-forge'] if args.public_sharded_seed else ['--offline','-c',str(channel)]
        receipt['seed_source'] = 'public sharded solve checked against frozen records' if args.public_sharded_seed else 'frozen flat repodata solve'
        cmd = [str(binary), '--root-prefix', str(output/'solver-home'), '--no-env', '--no-rc', 'create', '--dry-run', '--json',
               '--strict-channel-priority', *route, '-p', str(output / 'solve-prefix'), *packages]
        receipt['solve_command'] = cmd
        # Limit the seed solver to half the available host RAM. Preserve a
        # failed solve rather than overcommitting a shared Linux workstation.
        available = int(re.search(r'^MemAvailable:\s*(\d+)', Path('/proc/meminfo').read_text(), re.M)[1])*1024
        limit = min(8*1024**3, available//2)
        receipt['seed_address_space_limit_bytes'] = limit
        def solve(command):
            return subprocess.run(command, capture_output=True, timeout=900,
                                  preexec_fn=lambda: resource.setrlimit(resource.RLIMIT_AS,(limit,limit)))
        if args.saved_solution:
            if not args.public_sharded_seed:
                raise ValueError('a saved sharded solution needs --public-sharded-seed')
            receipt['saved_solution_sha256'] = sha(args.saved_solution)
            result = subprocess.CompletedProcess(cmd,0,args.saved_solution.read_bytes(),b'')
        else:
            result = solve(cmd)
        (output / 'solve.stdout').write_bytes(result.stdout)
        (output / 'solve.stderr').write_bytes(result.stderr)
        if result.returncode:
            (output / 'solve.json').write_bytes(result.stdout)
            raise RuntimeError(f'micromamba solve failed ({result.returncode}); diagnostics retained in {output}')
        solution = json.loads(result.stdout)
        (output / 'solve.json').write_text(json.dumps(solution, indent=2, sort_keys=True) + '\n')
        actions = solution.get('actions', {})
        records = actions.get('LINK', [])
        if not records:
            raise ValueError('dry-run solve returned no LINK records')
        if args.public_sharded_seed:
            for subdir in ('linux-64','noarch'):
                chosen = [r for r in records if r['subdir'] == subdir]
                frozen = frozen_records(channel/subdir/'repodata.json',{r['fn'] for r in chosen})
                for r in chosen:
                    for key in ('name','version','build','size','sha256'):
                        if r.get(key,[]) != frozen[r['fn']].get(key,[]):
                            raise ValueError(f'changed frozen {key} for {r["fn"]}')
                    r.update(frozen[r['fn']])
                # Solver JSON may normalize MatchSpecs. Prove the full selected
                # set solves using the original dependency and constraint strings.
                reduced = output/'verified-channel'/subdir
                reduced.mkdir(parents=True)
                index = {'info':{'subdir':subdir},'packages':{},'packages.conda':{},'repodata_version':1}
                for filename, data in frozen.items():
                    index['packages.conda' if filename.endswith('.conda') else 'packages'][filename] = data
                (reduced/'repodata.json').write_text(json.dumps(index))
            verify_cmd = cmd.copy()
            verify_cmd[verify_cmd.index('https://conda.anaconda.org/conda-forge')] = str(output/'verified-channel')
            verify_cmd.insert(verify_cmd.index('--dry-run'),'--offline')
            verified = solve(verify_cmd)
            (output/'verified-solve.stdout').write_bytes(verified.stdout)
            (output/'verified-solve.stderr').write_bytes(verified.stderr)
            if verified.returncode or {x['sha256'] for x in json.loads(verified.stdout)['actions']['LINK']} != {x['sha256'] for x in records}:
                raise ValueError('frozen dependency graph did not reproduce every selected archive')
        for record in records:
            fn = record.get('fn') or record.get('filename') or Path(record.get('url', '')).name
            subdir = record.get('subdir', 'noarch' if 'noarch' in record.get('url', '') else 'linux-64')
            digest, size = record.get('sha256'), record.get('size')
            if not fn or Path(fn).name != fn or not fn.endswith(('.conda','.tar.bz2')) or not re.fullmatch('[a-f0-9]{64}',digest or '') or not isinstance(size,int) or size <= 0 or subdir not in ('linux-64', 'noarch'):
                raise ValueError(f'incomplete solved archive record: {record}')
            archive = cache / subdir / fn
            url = f'https://conda.anaconda.org/conda-forge/{subdir}/{fn}'
            copy_verified(url, archive, digest, size)
            dest_dir = output/'records'/subdir
            dest_dir.mkdir(parents=True,exist_ok=True)
            target = dest_dir/fn
            try:
                os.link(archive,target)
            except OSError:
                shutil.copyfile(archive,target)
            info = dest_dir / (fn+'.metadata') / 'info' / 'repodata_record.json'
            info.parent.mkdir(parents=True, exist_ok=True)
            info.write_text(json.dumps({**record, 'fn': fn, 'subdir': subdir, 'sha256': digest, 'size': size}, sort_keys=True) + '\n')
            receipt['packages'].append({'subdir': subdir, 'filename': fn, 'sha256': digest, 'size': size})
        wheels = output / 'wheels'
        wheels.mkdir()
        for wheel in pip_requirements:
            if wheel.lower().startswith('robotframework==6.1.1'):
                source = repodata.parent / 'wheels' / 'robotframework-6.1.1-py3-none-any.whl'
                target = wheels / source.name
                shutil.copyfile(source, target)
                receipt['pip'] = {'requirement': wheel, 'filename': target.name,
                                  'sha256': sha(target), 'size': target.stat().st_size}
        receipt['package_count'] = len(receipt['packages'])
        receipt['input_refs'] = {'fixture_sha256': receipt['fixture_sha256'],
                                 'micromamba_sha256': receipt['micromamba_sha256']}
        receipt['output_sizes'] = {str(p.relative_to(output)): p.stat().st_size
                                   for p in sorted(output.rglob('*')) if p.is_file()}
        (output / 'receipt.json').write_text(json.dumps(receipt, indent=2, sort_keys=True) + '\n')
    except Exception:
        (output / 'receipt.json').write_text(json.dumps(receipt, indent=2, sort_keys=True) + '\n')
        raise


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        print(f'error: {error}', file=sys.stderr)
        sys.exit(1)
