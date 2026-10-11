#!/usr/bin/env python3
"""Bounded GET-only component benchmark for the admitted RCC filesystem provider."""
import argparse
import concurrent.futures
import hashlib
import http.client
import json
import os
import resource
import select
import signal
import socket
import stat
import subprocess
import sys
import threading
import time
import traceback
import urllib.parse
from pathlib import Path

PARAMS = Path('/workspace/work/rcc-swarm/evidence/continuation-20261011/performance-admission/execution-parameters.json')
FIXTURE = Path('/workspace/work/rcc-swarm/evidence/continuation-20261011/performance-admission/streaming-source-fixture.json')
RUN_ROOT = Path('/workspace/work/rcc-swarm/evidence/continuation-20261011/provider-baseline-luna')
BINARY = Path('/workspace/work/rcc-swarm/evidence/continuation-20261011/strict-consumer/rcc-main727')
PROVIDER = Path('/workspace/work/rcc-swarm/evidence/performance-steer-20261011/runtime-prerequisites/attempt-10/attempt-10/provider-content')
MAX_NEW_BYTES = 10_485_760
MIN_FREE = 2_684_354_560
MAX_CLIENT_RSS = 536_870_912
TOTAL_TIMEOUT = 300.0
CONTROL_TIMEOUT = 60.0
REQUEST_TIMEOUT = 30.0
SCHEDULE = [1, 4, 4, 1, 1, 4]
CHUNK = 65_536


def sha256_file(path):
    h = hashlib.sha256()
    with open(path, 'rb', buffering=0) as f:
        while True:
            b = f.read(CHUNK)
            if not b:
                break
            h.update(b)
    return h.hexdigest()


def digest_path(root, digest):
    algo, hx = digest.split(':', 1)
    if algo != 'sha256' or len(hx) != 64 or any(c not in '0123456789abcdef' for c in hx):
        raise ValueError(f'invalid sha256 digest: {digest!r}')
    return root / 'objects' / 'sha256' / hx[:2] / hx[2:4] / hx


def load_descriptors(params):
    fixture = params['fixture']
    root = Path(fixture['provider_root'])
    manifest_path = root / 'manifests/sha256/ec/37/ec3795182e7261439a8b9a9a9b7bd0834b7a7d65202067b17e5589e06f41b83e'
    manifest_raw = manifest_path.read_bytes()
    manifest = json.loads(manifest_raw)
    descriptors = []
    for key in ('specification', 'legacyBlueprint', 'objectIndex'):
        descriptors.append({'mediaType': manifest[key]['mediaType'], 'digest': manifest[key]['digest'], 'size': manifest[key]['size']})
    for entry in manifest['catalogs']:
        descriptors.append({'mediaType': entry['mediaType'], 'digest': entry['digest'], 'size': entry['size']})
    index_desc = manifest['objectIndex']
    index_path = digest_path(root, index_desc['digest'])
    index_raw = index_path.read_bytes()
    index = json.loads(index_raw)
    for entry in index['entries']:
        descriptors.append({'mediaType': 'application/vnd.rcc.hololib.object.v12+gzip', 'digest': entry['storedDigest'], 'size': entry['storedSize']})
    return root, manifest_path, manifest_raw, manifest, index_path, index_raw, index, descriptors


def inventory(params):
    root = Path(params['fixture']['provider_root'])
    if root.resolve(strict=True) != root:
        raise RuntimeError('provider root or an ancestor resolves through a symlink')
    if (root / '.restore-state').exists() or (root / '.restore-state').is_symlink():
        raise RuntimeError('provider has .restore-state; refusing to launch')
    for rel in ('objects/sha256', 'manifests/sha256', 'tmp'):
        p = root / rel
        if not p.is_dir() or p.is_symlink():
            raise RuntimeError(f'required provider path is not a real directory: {rel}')
    root, manifest_path, manifest_raw, manifest, index_path, index_raw, index, descriptors = load_descriptors(params)
    fx = params['fixture']
    if hashlib.sha256(manifest_raw).hexdigest() != fx['manifest_raw_sha256']:
        raise RuntimeError('manifest raw SHA256 mismatch')
    if hashlib.sha256(index_raw).hexdigest() != fx['index']['digest'].split(':', 1)[1] or len(index_raw) != fx['index']['size']:
        raise RuntimeError('object-index raw hash/size mismatch')
    if index.get('count') != fx['content_objects'] or len(index.get('entries', [])) != fx['content_objects']:
        raise RuntimeError('object-index count mismatch')
    if index.get('totalStoredBytes') != fx['stored_content_bytes']:
        raise RuntimeError('object-index stored-byte total mismatch')
    if len({d['digest'] for d in descriptors}) != fx['full_CAS_objects']:
        raise RuntimeError('selected descriptor set is not unique or count differs')
    if sum(int(d['size']) for d in descriptors) != fx['full_CAS_stored_bytes']:
        raise RuntimeError('selected full-CAS stored-byte total differs')

    expected = {}
    for d in descriptors:
        p = digest_path(root, d['digest'])
        rel = p.relative_to(root).as_posix()
        if rel in expected:
            raise RuntimeError(f'duplicate object path {rel}')
        expected[rel] = {'sha256': d['digest'].split(':', 1)[1], 'size': int(d['size'])}
    mrel = manifest_path.relative_to(root).as_posix()
    expected[mrel] = {'sha256': fx['manifest_raw_sha256'], 'size': len(manifest_raw)}
    audit = root / '.audit'
    if not audit.is_file() or audit.is_symlink():
        raise RuntimeError('expected provider .audit is absent or not a regular file')

    actual_files = {}
    actual_dirs = set()
    for base, dirs, files in os.walk(root, topdown=True, followlinks=False):
        bp = Path(base)
        relbase = bp.relative_to(root).as_posix()
        if relbase != '.':
            st = bp.lstat()
            if not stat.S_ISDIR(st.st_mode):
                raise RuntimeError(f'non-directory path in provider closure: {relbase}')
            actual_dirs.add(relbase)
        for name in list(dirs):
            p = bp / name
            st = p.lstat()
            if not stat.S_ISDIR(st.st_mode):
                raise RuntimeError(f'symlink or non-directory in provider closure: {p.relative_to(root)}')
        for name in files:
            p = bp / name
            rel = p.relative_to(root).as_posix()
            st = p.lstat()
            if not stat.S_ISREG(st.st_mode) or st.st_nlink != 1:
                raise RuntimeError(f'non-regular or hardlinked provider file: {rel}')
            actual_files[rel] = p
    expected_files = set(expected) | {'.audit'}
    if set(actual_files) != expected_files:
        missing = sorted(expected_files - set(actual_files))[:5]
        extra = sorted(set(actual_files) - expected_files)[:5]
        raise RuntimeError(f'provider closure file set mismatch; missing={missing}, extra={extra}')
    expected_dirs = {'objects', 'objects/sha256', 'manifests', 'manifests/sha256', 'tmp'}
    for rel in expected:
        pp = Path(rel).parent
        while str(pp) != '.':
            expected_dirs.add(pp.as_posix())
            pp = pp.parent
    if actual_dirs != expected_dirs:
        raise RuntimeError(f'provider closure directory set mismatch; missing={sorted(expected_dirs-actual_dirs)[:5]}, extra={sorted(actual_dirs-expected_dirs)[:5]}')
    if any((root / 'tmp').iterdir()):
        raise RuntimeError('provider tmp directory is not empty')

    files = {}
    for rel, p in actual_files.items():
        st = p.lstat()
        digest = sha256_file(p)
        exp = expected.get(rel)
        if exp and (st.st_size != exp['size'] or digest != exp['sha256']):
            raise RuntimeError(f'provider content digest/size mismatch: {rel}')
        files[rel] = {'size': st.st_size, 'sha256': digest, 'mode': stat.S_IMODE(st.st_mode), 'mtime_ns': st.st_mtime_ns}
    dirs = {'.': {'mode': stat.S_IMODE(root.lstat().st_mode), 'mtime_ns': root.lstat().st_mtime_ns}}
    for rel in actual_dirs:
        st = (root / rel).lstat()
        dirs[rel] = {'mode': stat.S_IMODE(st.st_mode), 'mtime_ns': st.st_mtime_ns}
    return {
        'root': str(root), 'manifest_sha256': hashlib.sha256(manifest_raw).hexdigest(),
        'index_sha256': hashlib.sha256(index_raw).hexdigest(), 'indexed_objects': len(index['entries']),
        'content_bytes': sum(int(d['size']) for d in descriptors[4:]), 'full_cas_objects': len(descriptors),
        'full_cas_bytes': sum(int(d['size']) for d in descriptors), 'files': files, 'directories': dirs,
        'audit_sha256': files['.audit']['sha256'], 'tmp_empty': True, 'restore_state_absent': True,
    }


def ns_allocated_bytes():
    total = 0
    for base, dirs, files in os.walk(RUN_ROOT, followlinks=False):
        for name in dirs + files:
            p = Path(base) / name
            st = p.lstat()
            if stat.S_ISLNK(st.st_mode):
                raise RuntimeError(f'symlink appeared in owned namespace: {p}')
            total += getattr(st, 'st_blocks', 0) * 512
    return total


def free_bytes():
    st = os.statvfs(RUN_ROOT)
    return st.f_bavail * st.f_frsize


def check_guards():
    available = free_bytes()
    owned = ns_allocated_bytes()
    if available < MIN_FREE:
        raise RuntimeError(f'free-space guard breached: {available} < {MIN_FREE}')
    if owned > MAX_NEW_BYTES:
        raise RuntimeError(f'owned-data guard breached: {owned} > {MAX_NEW_BYTES}')
    return {'free_bytes': available, 'owned_allocated_bytes': owned}


def proc_status(pid):
    d = {}
    try:
        for line in Path(f'/proc/{pid}/status').read_text().splitlines():
            if line.startswith('VmRSS:'):
                d['rss_bytes'] = int(line.split()[1]) * 1024
                break
        fields = Path(f'/proc/{pid}/stat').read_text()
        rest = fields[fields.rfind(')') + 2:].split()
        d['cpu_ticks'] = int(rest[11]) + int(rest[12])
        io = {}
        for line in Path(f'/proc/{pid}/io').read_text().splitlines():
            key, value = line.split(':', 1)
            io[key] = int(value.strip())
        d['io'] = io
        return d
    except FileNotFoundError:
        return None


def wait_status_exitcode(status):
    if os.WIFEXITED(status):
        return os.WEXITSTATUS(status)
    if os.WIFSIGNALED(status):
        return -os.WTERMSIG(status)
    return None


def spawn_server(profile=False):
    env = {
        'PATH': os.environ.get('PATH', '/usr/bin:/bin'),
        'HOME': str(RUN_ROOT),
        'TMPDIR': str(RUN_ROOT / 'tmp'),
        'LANG': 'C.UTF-8', 'LC_ALL': 'C.UTF-8',
        'ROBOCORP_HOME': str(RUN_ROOT / ('rcc-home-profile' if profile else 'rcc-home')),
        'RCC_HOLOTREE_MODE': 'private',
        'RCC_TELEMETRY_ENABLED': 'false',
    }
    rfd, wfd = os.pipe()
    err_path = RUN_ROOT / 'logs' / ('profile.stderr.log' if profile else 'server.stderr.log')
    pid = os.fork()
    if pid == 0:
        try:
            os.close(rfd)
            os.dup2(wfd, 1)
            os.close(wfd)
            errfd = os.open(err_path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
            os.dup2(errfd, 2)
            if errfd not in (1, 2):
                os.close(errfd)
            base = [str(BINARY), 'cache', 'serve', '--root', str(PROVIDER), '--listen', '127.0.0.1:0', '--backend', 'filesystem', '--json']
            if profile:
                base = [str(BINARY), '--pprof', str(RUN_ROOT / 'provider-cpu.pprof')] + base[1:]
            os.execve(str(BINARY), base, env)
        except BaseException as e:
            try:
                os.write(2, f'server exec failed: {e}\n'.encode())
            finally:
                os._exit(127)
    os.close(wfd)
    os.set_blocking(rfd, False)
    started = time.monotonic()
    buf = bytearray()
    line = None
    while time.monotonic() - started < 15:
        check_guards()
        readable, _, _ = select.select([rfd], [], [], 0.1)
        if readable:
            part = os.read(rfd, 4096)
            if not part:
                break
            buf.extend(part)
            if b'\n' in buf:
                line = bytes(buf).split(b'\n', 1)[0]
                break
    if line is None:
        try:
            os.kill(pid, signal.SIGTERM)
        except ProcessLookupError:
            pass
        ended, status, usage = os.wait4(pid, 0)
        os.close(rfd)
        raise RuntimeError(f'RCC server failed to emit bounded startup JSON; wait_exit={wait_status_exitcode(status)}, stdout={buf!r}, stderr={err_path.read_text(errors="replace")[:2000]}')
    startup = json.loads(line)
    if startup.get('root') != str(PROVIDER.resolve(strict=True)):
        os.kill(pid, signal.SIGTERM)
        os.wait4(pid, 0)
        os.close(rfd)
        raise RuntimeError(f'RCC server returned unexpected root: {startup!r}')
    parsed = urllib.parse.urlsplit(startup.get('url', ''))
    if parsed.scheme != 'http' or parsed.hostname != '127.0.0.1' or not parsed.port:
        os.kill(pid, signal.SIGTERM)
        os.wait4(pid, 0)
        os.close(rfd)
        raise RuntimeError(f'RCC server returned non-loopback URL: {startup!r}')
    return {'pid': pid, 'rfd': rfd, 'url': startup['url'], 'port': parsed.port, 'profile': profile, 'stderr': str(err_path), 'started_at': time.monotonic(), 'reaped': False}


def stop_server(server):
    if server is None or server.get('reaped'):
        return None
    pid = server['pid']
    try:
        os.kill(pid, signal.SIGTERM)
    except ProcessLookupError:
        pass
    deadline = time.monotonic() + 10.0
    got = None
    while time.monotonic() < deadline:
        try:
            got = os.wait4(pid, os.WNOHANG)
        except ChildProcessError:
            break
        if got and got[0] == pid:
            break
        time.sleep(0.05)
    forced = False
    if not got or got[0] != pid:
        forced = True
        try:
            os.kill(pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        got = os.wait4(pid, 0)
    server['reaped'] = True
    if server.get('rfd') is not None:
        try:
            os.close(server['rfd'])
        except OSError:
            pass
    _, status, usage = got
    code = wait_status_exitcode(status)
    return {'exit_code': code, 'forced': forced, 'ru_utime_s': usage.ru_utime, 'ru_stime_s': usage.ru_stime, 'ru_maxrss_kib': usage.ru_maxrss, 'wall_from_spawn_s': time.monotonic() - server['started_at']}


def port_closed(port):
    try:
        with socket.create_connection(('127.0.0.1', port), timeout=0.25):
            return False
    except OSError:
        return True


def read_descriptors_for_client():
    params = json.loads(PARAMS.read_text())
    _, _, _, _, _, _, _, descriptors = load_descriptors(params)
    return descriptors[4:]


def client_main(url, workers, label):
    parsed = urllib.parse.urlsplit(url)
    host = parsed.hostname
    port = parsed.port
    descriptors = read_descriptors_for_client()
    if workers not in (1, 4):
        raise ValueError('client worker count must be 1 or 4')
    parts = [descriptors[i::workers] for i in range(workers)]
    barrier = threading.Barrier(workers + 1)

    def do_worker(worker_index, items):
        conn = http.client.HTTPConnection(host, port, timeout=REQUEST_TIMEOUT)
        result = {'worker': worker_index, 'gets_started': 0, 'gets_completed': 0, 'body_bytes': 0, 'connections': 1}
        barrier.wait(timeout=10)
        try:
            for d in items:
                hx = d['digest'].split(':', 1)[1]
                path = '/v1/objects/sha256/' + hx
                result['gets_started'] += 1
                conn.request('GET', path, headers={'Accept-Encoding': 'identity'})
                response = conn.getresponse()
                if response.status != 200:
                    response.read(CHUNK)
                    raise RuntimeError(f'{label}: {path} returned HTTP {response.status}')
                if response.getheader('Content-Type') != 'application/octet-stream':
                    response.read(CHUNK)
                    raise RuntimeError(f'{label}: {path} content-type {response.getheader("Content-Type")!r}')
                try:
                    content_length = int(response.getheader('Content-Length', ''))
                except ValueError:
                    raise RuntimeError(f'{label}: {path} has invalid Content-Length')
                if content_length != int(d['size']):
                    raise RuntimeError(f'{label}: {path} Content-Length {content_length} != {d["size"]}')
                h = hashlib.sha256()
                n = 0
                while True:
                    b = response.read(CHUNK)
                    if not b:
                        break
                    n += len(b)
                    h.update(b)
                if n != int(d['size']) or h.hexdigest() != hx:
                    raise RuntimeError(f'{label}: {path} EOF digest/size mismatch (size={n}, sha256={h.hexdigest()})')
                result['gets_completed'] += 1
                result['body_bytes'] += n
        finally:
            conn.close()
        return result

    with concurrent.futures.ThreadPoolExecutor(max_workers=workers, thread_name_prefix='rcc-get') as pool:
        futures = [pool.submit(do_worker, i, part) for i, part in enumerate(parts)]
        start_cpu = time.process_time_ns()
        start_wall = time.perf_counter_ns()
        barrier.wait(timeout=10)
        worker_results = [f.result() for f in futures]
        end_wall = time.perf_counter_ns()
        end_cpu = time.process_time_ns()
    usage = resource.getrusage(resource.RUSAGE_SELF)
    result = {
        'label': label, 'workers': workers,
        'gets_started': sum(x['gets_started'] for x in worker_results),
        'gets_completed': sum(x['gets_completed'] for x in worker_results),
        'body_bytes': sum(x['body_bytes'] for x in worker_results),
        'connections': sum(x['connections'] for x in worker_results),
        'wall_ns': end_wall - start_wall, 'client_cpu_ns': end_cpu - start_cpu,
        'ru_maxrss_kib': usage.ru_maxrss, 'worker_results': worker_results,
    }
    if result['gets_completed'] != 5537 or result['body_bytes'] != 69466848 or result['connections'] != workers:
        raise RuntimeError(f'{label}: control totals differ from fixture: {result}')
    print(json.dumps(result, sort_keys=True), flush=True)


def proc_io_delta(before, after):
    if not before or not after:
        return None
    keys = sorted(set(before.get('io', {})) & set(after.get('io', {})))
    return {k: after['io'][k] - before['io'][k] for k in keys}


def launch_client(server, workers, label, deadline, max_sample):
    cmd = [sys.executable, str(Path(__file__).resolve()), '--client', server['url'], str(workers), label]
    env = {'PATH': os.environ.get('PATH', '/usr/bin:/bin'), 'PYTHONPATH': '', 'PYTHONUNBUFFERED': '1'}
    proc = subprocess.Popen(cmd, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, env=env, cwd=str(RUN_ROOT))
    started = time.monotonic()
    timeout_reason = None
    peak_client_rss = 0
    peak_server_rss = 0
    guard_samples = []
    server_before = proc_status(server['pid'])
    next_guard = started
    while proc.poll() is None:
        now = time.monotonic()
        if now - started > CONTROL_TIMEOUT:
            timeout_reason = 'per-control watchdog exceeded 60 seconds'
            break
        if now - deadline > 0:
            timeout_reason = 'total watchdog exceeded 300 seconds'
            break
        if now >= next_guard:
            try:
                g = check_guards()
                guard_samples.append(g)
            except Exception as e:
                timeout_reason = str(e)
                break
            cs = proc_status(proc.pid)
            ss = proc_status(server['pid'])
            if cs:
                peak_client_rss = max(peak_client_rss, cs.get('rss_bytes', 0))
            if ss:
                peak_server_rss = max(peak_server_rss, ss.get('rss_bytes', 0))
            if peak_client_rss >= MAX_CLIENT_RSS:
                timeout_reason = f'client RSS guard exceeded {MAX_CLIENT_RSS} bytes'
                break
            next_guard = now + 0.1
        time.sleep(0.02)
    if timeout_reason:
        proc.terminate()
        try:
            proc.wait(timeout=2)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait(timeout=2)
        stdout, stderr = proc.communicate()
        raise RuntimeError(f'{label}: {timeout_reason}; client exit={proc.returncode}; stdout={stdout[-1000:]}; stderr={stderr[-1000:]}')
    stdout, stderr = proc.communicate(timeout=1)
    server_after = proc_status(server['pid'])
    if proc.returncode != 0:
        raise RuntimeError(f'{label}: client exit={proc.returncode}; stdout={stdout[-1000:]}; stderr={stderr[-2000:]}')
    result = json.loads(stdout.strip())
    client_rss = int(result['ru_maxrss_kib']) * 1024
    peak_client_rss = max(peak_client_rss, client_rss)
    if peak_client_rss >= MAX_CLIENT_RSS:
        raise RuntimeError(f'{label}: client RSS high-water {peak_client_rss} >= {MAX_CLIENT_RSS}')
    result['sampled_client_peak_rss_bytes'] = peak_client_rss
    result['sampled_server_peak_rss_bytes'] = peak_server_rss
    result['wall_parent_s'] = time.monotonic() - started
    result['server_cpu_ticks_delta'] = (server_after['cpu_ticks'] - server_before['cpu_ticks']) if server_before and server_after else None
    result['server_io_delta'] = proc_io_delta(server_before, server_after)
    result['guard_sample_count'] = len(guard_samples)
    result['free_bytes_min_sampled'] = min((x['free_bytes'] for x in guard_samples), default=None)
    result['owned_allocated_bytes_peak_sampled'] = max((x['owned_allocated_bytes'] for x in guard_samples), default=None)
    return result


def write_json(path, value):
    tmp = path.with_suffix(path.suffix + '.tmp')
    tmp.write_text(json.dumps(value, indent=2, sort_keys=True) + '\n')
    os.replace(tmp, path)


def main_run():
    params = json.loads(PARAMS.read_text())
    if params['status'] != 'PROPOSED_NOT_EXECUTED_ROOT_ADMISSION_REQUIRED':
        raise RuntimeError(f'unexpected execution parameter status {params["status"]!r}')
    allowed_bootstrap = {'stream_baseline.py', '__pycache__', 'run.log', 'prelaunch-failure.log'}
    if RUN_ROOT.exists() and any(p.name not in allowed_bootstrap for p in RUN_ROOT.iterdir()):
        raise RuntimeError(f'owned namespace has unexpected pre-existing entries: {RUN_ROOT}')
    RUN_ROOT.mkdir(mode=0o700, parents=True, exist_ok=True)
    os.chmod(RUN_ROOT, 0o700)
    (RUN_ROOT / 'logs').mkdir(mode=0o700)
    (RUN_ROOT / 'rcc-home').mkdir(mode=0o700)
    (RUN_ROOT / 'rcc-home-profile').mkdir(mode=0o700)
    if sha256_file(BINARY) != params['source']['binary_sha256']:
        raise RuntimeError('RCC source binary hash mismatch')
    fixture = json.loads(FIXTURE.read_text())
    source_root = Path('/workspace/work/rcc-swarm/final-main-727')
    verified_sources = []
    for source in fixture['source_files']:
        source_path = source_root / source['path']
        actual = sha256_file(source_path)
        if actual != source['sha256']:
            raise RuntimeError(f'source file hash mismatch: {source_path}')
        verified_sources.append({'path': source['path'], 'sha256': actual})
    if BINARY.stat().st_size > 30_000_000:
        raise RuntimeError('RCC source binary size unexpected')
    before = inventory(params)
    if before['full_cas_objects'] != params['fixture']['full_CAS_objects'] or before['full_cas_bytes'] != params['fixture']['full_CAS_stored_bytes']:
        raise RuntimeError('full CAS inventory differs from approved fixture')
    preflight = {'source_commit': params['source']['rcc_commit'], 'source_tree': params['source']['rcc_tree'], 'binary': str(BINARY), 'binary_sha256': sha256_file(BINARY), 'verified_source_files': verified_sources, 'before': before, 'preflight_guards': check_guards(), 'started_utc': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime())}
    write_json(RUN_ROOT / 'preflight.json', preflight)

    started_all = time.monotonic()
    results = []
    server = None
    server_usage = None
    profile_results = None
    try:
        server = spawn_server(False)
        for ix, workers in enumerate(SCHEDULE, 1):
            if time.monotonic() - started_all > TOTAL_TIMEOUT:
                raise RuntimeError('total watchdog exceeded 300 seconds')
            label = f'control-{ix:02d}-workers-{workers}'
            result = launch_client(server, workers, label, started_all + TOTAL_TIMEOUT, None)
            results.append(result)
        server_usage = stop_server(server)
        if server_usage['exit_code'] != 0 or server_usage['forced']:
            raise RuntimeError(f'unprofiled RCC server shutdown failed: {server_usage}')
        if not port_closed(server['port']):
            raise RuntimeError('unprofiled loopback listener remains open after RCC exit')
        server = None

        profile_path = RUN_ROOT / 'provider-cpu.pprof'
        server = spawn_server(True)
        profile_result = launch_client(server, 1, 'profile-sequential-full-pass', started_all + TOTAL_TIMEOUT, None)
        profile_results = {'client': profile_result}
        server_usage = stop_server(server)
        if server_usage['exit_code'] != 0 or server_usage['forced']:
            raise RuntimeError(f'profiled RCC server shutdown failed: {server_usage}')
        if not port_closed(server['port']):
            raise RuntimeError('profiled loopback listener remains open after RCC exit')
        server = None
        if not profile_path.is_file() or profile_path.stat().st_size == 0:
            raise RuntimeError('RCC --pprof run did not produce a nonempty profile')
        profile_results['path'] = str(profile_path)
        profile_results['size'] = profile_path.stat().st_size
        profile_results['sha256'] = sha256_file(profile_path)

    finally:
        if server is not None and not server.get('reaped'):
            server_usage = stop_server(server)

    after = inventory(params)
    if before != after:
        # atime is intentionally excluded; inventory compares paths, hashes, sizes, modes and mtimes.
        raise RuntimeError('provider closure or directory census changed across run')
    guards_final = check_guards()
    if time.monotonic() - started_all > TOTAL_TIMEOUT:
        raise RuntimeError('total watchdog exceeded 300 seconds')
    payload = {
        'status': 'PASS', 'scope': params['scope'], 'source': params['source'], 'fixture': {
            'artifact_digest': params['fixture']['artifact_digest'], 'content_objects': params['fixture']['content_objects'],
            'content_bytes': params['fixture']['stored_content_bytes'], 'full_CAS_objects': after['full_cas_objects'],
            'full_CAS_bytes': after['full_cas_bytes'], 'index_sha256': after['index_sha256'], 'manifest_sha256': after['manifest_sha256'],
            'provider_closure_unchanged': True, 'atime_claim': 'not compared; reads may update atime',
        },
        'controls': results, 'profile_separate_run': profile_results, 'server_final_wait4': server_usage,
        'total_wall_s': time.monotonic() - started_all, 'final_guards': guards_final,
        'provider_before_after_equal': before == after,
        'caveats': [params['controls']['cache_policy'], 'The client is Python http.client; transport is loopback without TLS/auth.', 'GET-only loopback provider component; not full materialization, RCC consumer, Actions concurrency, or end-to-end runtime'],
    }
    write_json(RUN_ROOT / 'results.json', payload)
    return payload


def main():
    if len(sys.argv) >= 2 and sys.argv[1] == '--client':
        try:
            client_main(sys.argv[2], int(sys.argv[3]), sys.argv[4])
            return 0
        except Exception:
            traceback.print_exc(file=sys.stderr)
            return 1
    parser = argparse.ArgumentParser()
    parser.add_argument('--preflight-only', action='store_true')
    args = parser.parse_args()
    params = json.loads(PARAMS.read_text())
    if args.preflight_only:
        result = inventory(params)
        print(json.dumps({k: v for k, v in result.items() if k not in ('files', 'directories')}, sort_keys=True))
        return 0
    result = main_run()
    print(json.dumps({'status': result['status'], 'total_wall_s': result['total_wall_s'], 'controls': len(result['controls']), 'profile_sha256': result['profile_separate_run']['sha256']}))
    return 0

if __name__ == '__main__':
    raise SystemExit(main())
