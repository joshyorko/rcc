#!/usr/bin/env python3
"""Bounded read-layout microbenchmark for the frozen RCC object subset."""
import hashlib
import json
import os
import resource
import shutil
import signal
import stat
import time
from pathlib import Path

HERE = Path(__file__).resolve().parent
SOURCE = Path('/workspace/work/rcc-swarm/evidence/performance-steer-20261011/runtime-prerequisites/attempt-10/attempt-10/provider-content')
INDEX_PATH = SOURCE / 'objects/sha256/e5/87/e587078b0b8d8bce38d4e665999b9a3a0d4d9fb1de1b87a8c3a4b223e71e50f6'
PLAN = HERE / 'frozen-plan.json'
OFFSET_INDEX = HERE / 'offset-index.json'
PACK = HERE / 'small-objects.concat'
NEGATIVE = HERE / 'negative-copy.concat'
RESULTS = HERE / 'results.json'
LIMIT_BYTES = 10 * 1024 * 1024
FREE_FLOOR = 2684354560
RSS_LIMIT = 536870912
BUF = 65536

class PhaseTimeout(Exception):
    pass

def timeout_handler(_signum, _frame):
    raise PhaseTimeout('phase exceeded 30 second watchdog')

signal.signal(signal.SIGALRM, timeout_handler)
signal.alarm(120)
start_total = time.monotonic()

def sha_file(path):
    h = hashlib.sha256()
    size = 0
    with open(path, 'rb', buffering=0) as f:
        while True:
            b = f.read(BUF)
            if not b:
                break
            size += len(b)
            h.update(b)
    return h.hexdigest(), size

def object_path(digest):
    raw = digest.removeprefix('sha256:')
    return SOURCE / 'objects' / 'sha256' / raw[:2] / raw[2:4] / raw

def read_all_hash(fd, expected_size, expected_hex, require_eof=True):
    h = hashlib.sha256()
    size = 0
    calls = 0
    while size < expected_size:
        b = os.read(fd, min(BUF, expected_size - size))
        calls += 1
        if not b:
            raise AssertionError(f'short read at {size}/{expected_size}')
        size += len(b)
        h.update(b)
    if require_eof:
        if os.read(fd, 1):
            calls += 1
            raise AssertionError('unexpected trailing bytes')
        calls += 1
    if size != expected_size or h.hexdigest() != expected_hex:
        raise AssertionError(f'content identity mismatch: {size}, {h.hexdigest()}')
    return calls

def proc_io():
    out = {}
    for line in Path('/proc/self/io').read_text().splitlines():
        k, v = line.split(':', 1)
        out[k] = int(v.strip())
    return out

def rss_hwm():
    for line in Path('/proc/self/status').read_text().splitlines():
        if line.startswith('VmHWM:'):
            return int(line.split()[1]) * 1024
    return int(resource.getrusage(resource.RUSAGE_SELF).ru_maxrss) * 1024

def free_bytes():
    return os.statvfs(HERE).f_bavail * os.statvfs(HERE).f_frsize

def allocated_owned():
    total = 0
    for p in HERE.iterdir():
        if p.is_file():
            total += p.stat().st_blocks * 512
    return total

def guard():
    if allocated_owned() > LIMIT_BYTES:
        raise AssertionError(f'owned allocated bytes exceed cap: {allocated_owned()}')
    if free_bytes() < FREE_FLOOR:
        raise AssertionError(f'free-space floor breached: {free_bytes()}')
    if rss_hwm() >= RSS_LIMIT:
        raise AssertionError(f'RSS high-water reached limit: {rss_hwm()}')
    if time.monotonic() - start_total > 120:
        raise PhaseTimeout('total watchdog exceeded 120 seconds')

def load_and_verify_plan():
    plan = json.loads(PLAN.read_text())
    index = json.loads(OFFSET_INDEX.read_text())
    assert len(index['entries']) == plan['selection_count'] == 4649
    assert index['pack_bytes'] == plan['pack_bytes'] == 4484936
    assert index['source_artifact'] == plan['source_artifact']
    assert index['source_index_digest'] == 'sha256:' + plan['source_index_sha256']
    assert index['source_index_bytes'] == plan['source_index_bytes']
    assert sha_file(OFFSET_INDEX) == (plan['offset_index_sha256'], plan['offset_index_bytes'])
    assert [x['digest'] for x in index['entries']] == sorted(x['digest'] for x in index['entries'])
    at = 0
    for item in index['entries']:
        assert item['offset'] == at and item['size'] < 4096
        at += item['size']
    assert at == plan['pack_bytes']
    return plan, index

def source_receipt(entries):
    receipt = []
    for item in entries:
        path = object_path(item['digest'])
        st = os.lstat(path)
        if not stat.S_ISREG(st.st_mode) or stat.S_ISLNK(st.st_mode) or st.st_nlink != 1:
            raise AssertionError(f'unsafe source object: {path}')
        digest, size = sha_file(path)
        expected = item['digest'].split(':', 1)[1]
        if digest != expected or size != item['size']:
            raise AssertionError(f'source mismatch {item["digest"]}')
        receipt.append({'digest': item['digest'], 'size': size, 'mode': stat.S_IMODE(st.st_mode), 'mtime_ns': st.st_mtime_ns, 'inode': st.st_ino})
    return receipt

def create_pack(entries):
    h = hashlib.sha256()
    size = 0
    flags = os.O_WRONLY | os.O_CREAT | os.O_EXCL
    fd = os.open(PACK, flags, 0o600)
    try:
        for item in entries:
            source = object_path(item['digest'])
            srcfd = os.open(source, os.O_RDONLY | getattr(os, 'O_NOFOLLOW', 0))
            obj_h = hashlib.sha256()
            obj_size = 0
            try:
                st = os.fstat(srcfd)
                if not stat.S_ISREG(st.st_mode) or st.st_size != item['size']:
                    raise AssertionError(f'source changed during pack creation: {source}')
                while True:
                    block = os.read(srcfd, BUF)
                    if not block:
                        break
                    obj_h.update(block)
                    h.update(block)
                    obj_size += len(block)
                    size += len(block)
                    view = memoryview(block)
                    while view:
                        written = os.write(fd, view)
                        view = view[written:]
                if obj_size != item['size'] or obj_h.hexdigest() != item['digest'].split(':', 1)[1]:
                    raise AssertionError(f'source changed during copy: {source}')
            finally:
                os.close(srcfd)
    finally:
        os.fsync(fd)
        os.close(fd)
    os.chmod(PACK, 0o600)
    if size != 4484936:
        raise AssertionError(f'pack size mismatch {size}')
    return h.hexdigest(), size

def verify_slices(entries, pack_path):
    fd = os.open(pack_path, os.O_RDONLY | getattr(os, 'O_NOFOLLOW', 0))
    try:
        st = os.fstat(fd)
        expected_total = sum(x['size'] for x in entries)
        if not stat.S_ISREG(st.st_mode) or st.st_size != expected_total:
            raise AssertionError('pack size/type mismatch')
        for item in entries:
            os.lseek(fd, item['offset'], os.SEEK_SET)
            read_all_hash(fd, item['size'], item['digest'].split(':', 1)[1], require_eof=False)
    finally:
        os.close(fd)

def timed_pass(kind, entries):
    guard()
    io_before = proc_io()
    wall0 = time.perf_counter_ns()
    cpu0 = time.process_time_ns()
    opens = reads = seeks = 0
    signal.alarm(30)
    try:
        if kind == 'individual':
            for item in entries:
                fd = os.open(object_path(item['digest']), os.O_RDONLY | getattr(os, 'O_NOFOLLOW', 0))
                opens += 1
                try:
                    st = os.fstat(fd)
                    if not stat.S_ISREG(st.st_mode) or st.st_size != item['size']:
                        raise AssertionError('individual source metadata mismatch')
                    reads += read_all_hash(fd, item['size'], item['digest'].split(':', 1)[1])
                finally:
                    os.close(fd)
        else:
            fd = os.open(PACK, os.O_RDONLY | getattr(os, 'O_NOFOLLOW', 0))
            opens += 1
            try:
                st = os.fstat(fd)
                if not stat.S_ISREG(st.st_mode) or st.st_size != 4484936:
                    raise AssertionError('pack metadata mismatch')
                for item in entries:
                    os.lseek(fd, item['offset'], os.SEEK_SET)
                    seeks += 1
                    reads += read_all_hash(fd, item['size'], item['digest'].split(':', 1)[1], require_eof=False)
            finally:
                os.close(fd)
    finally:
        signal.alarm(0)
    cpu_ns = time.process_time_ns() - cpu0
    wall_ns = time.perf_counter_ns() - wall0
    io_after = proc_io()
    guard()
    return {'method': kind, 'wall_ns': wall_ns, 'process_cpu_ns': cpu_ns, 'application_opens': opens, 'application_read_calls': reads, 'application_seek_calls': seeks, 'proc_io_delta': {k: io_after[k] - io_before[k] for k in io_before}, 'rss_hwm_bytes': rss_hwm()}

def expect_rejection(label, action):
    try:
        action()
    except (AssertionError, OSError):
        return {'case': label, 'rejected': True}
    raise AssertionError(f'negative case unexpectedly passed: {label}')

def negative_tests(entries, pack_hash):
    shutil.copyfile(PACK, NEGATIVE)
    os.chmod(NEGATIVE, 0o600)
    guard()
    with open(NEGATIVE, 'r+b', buffering=0) as f:
        f.truncate(4484935)
    trunc = expect_rejection('truncated owned copy', lambda: verify_slices(entries, NEGATIVE))
    shutil.copyfile(PACK, NEGATIVE)
    with open(NEGATIVE, 'r+b', buffering=0) as f:
        f.seek(entries[0]['offset'])
        b = f.read(1)
        if not b:
            raise AssertionError('cannot mutate first object in negative copy')
        f.seek(entries[0]['offset'])
        f.write(bytes([b[0] ^ 1]))
        f.flush()
        os.fsync(f.fileno())
    corrupt = expect_rejection('corrupt owned copy', lambda: verify_slices(entries, NEGATIVE))
    shutil.copyfile(PACK, NEGATIVE)
    if sha_file(NEGATIVE) != (pack_hash, 4484936):
        raise AssertionError('negative copy restoration mismatch')
    return [trunc, corrupt]

def main():
    guard()
    plan, offset = load_and_verify_plan()
    index_doc = json.loads(INDEX_PATH.read_text())
    source_index_hash, source_index_size = sha_file(INDEX_PATH)
    if source_index_hash != plan['source_index_sha256'] or source_index_size != plan['source_index_bytes']:
        raise AssertionError('source index no longer matches frozen identity')
    entries = offset['entries']
    source_before = source_receipt(entries)
    assert len(source_before) == 4649
    if PACK.exists():
        # A prior harness stop can leave the owned concat file after its
        # source-by-source verified copy. Reuse only after complete slice and
        # whole-file verification against the frozen index.
        pack_hash, pack_size = sha_file(PACK)
        if pack_size != plan['pack_bytes']:
            raise AssertionError(f'existing owned pack has unexpected size: {pack_size}')
    else:
        pack_hash, pack_size = create_pack(entries)
    verify_slices(entries, PACK)
    if sha_file(PACK) != (pack_hash, pack_size):
        raise AssertionError('whole pack hash mismatch')
    # Rehash all selected originals after pack creation and before any timed pass.
    source_pre_timing = source_receipt(entries)
    if source_pre_timing != source_before:
        raise AssertionError('source metadata or bytes changed before timing')
    schedule = plan['control_schedule']
    controls = []
    for n, kind in enumerate(schedule, 1):
        row = timed_pass(kind, entries)
        row['replicate'] = n
        controls.append(row)
    # Rehash all originals and pack before negative tests; no source writes are allowed.
    source_after = source_receipt(entries)
    if source_after != source_before:
        raise AssertionError('original object bytes or metadata changed during controls')
    if sha_file(PACK) != (pack_hash, pack_size):
        raise AssertionError('primary pack changed during controls')
    negatives = negative_tests(entries, pack_hash)
    if sha_file(PACK) != (pack_hash, pack_size):
        raise AssertionError('primary pack changed during negative tests')
    if source_receipt(entries) != source_before:
        raise AssertionError('original source changed during negative tests')
    guard()
    result = {
        'status': 'PASS', 'scope': 'local small-object layout component only; not production format or remote engine',
        'source_root': str(SOURCE), 'source_index_sha256': source_index_hash, 'source_index_bytes': source_index_size,
        'selection_count': len(entries), 'selected_stored_bytes': pack_size, 'selected_sha256_set_identity': json.loads((HERE/'preflight.json').read_text())['selected_sha256_set_identity'],
        'pack_sha256': pack_hash, 'pack_bytes': pack_size, 'offset_index_sha256': plan['offset_index_sha256'],
        'controls': controls, 'negative_cases': negatives,
        'source_before': source_before, 'source_after': source_after,
        'environment': {'cache_policy': 'OS page cache warm from preflight, pack construction and untimed full verification; no cache eviction', 'measurement': 'single-process sequential schedule; no concurrent benchmark', 'buffer_bytes_max': BUF, 'timed_sha256_and_size_validation': 'enabled in both methods', 'timed_get_os_syscalls': 'not measured; counters are application-level os.open/os.read/os.lseek calls', 'proc_io_scope': '/proc/self/io cumulative process-counter deltas', 'rss_high_water_bytes': rss_hwm(), 'owned_allocated_bytes_at_end': allocated_owned(), 'free_bytes_at_end': free_bytes(), 'elapsed_seconds': time.monotonic()-start_total},
        'negative_copy': str(NEGATIVE), 'primary_pack': str(PACK),
    }
    RESULTS.write_text(json.dumps(result, sort_keys=True, indent=2) + '\n')
    os.chmod(RESULTS, 0o600)
    guard()
    print(json.dumps({k: result[k] for k in ('status','selection_count','selected_stored_bytes','pack_sha256','controls','negative_cases','environment')}, sort_keys=True))
    signal.alarm(0)

if __name__ == '__main__':
    main()
