"""Read-only independent receipt verification; never starts Go or the harness."""
import argparse
import datetime
import hashlib
import json
import os
import pathlib
import re
import stat
import statistics
import subprocess

BASE = pathlib.Path('/workspace/work/rcc-swarm/evidence/performance-admission/go-object-pack-native-preexecution-1600')
PREP = BASE / 'embed-7945-preparation'
CAS = pathlib.Path('/workspace/work/rcc-swarm/evidence/performance-steer-20261011/runtime-prerequisites/attempt-10/attempt-10/provider-content')
LAYOUT = pathlib.Path('/workspace/work/rcc-swarm/evidence/continuation-20261011/pack-layout-micro')
EXPECTED = {
    'index': ('e587078b0b8d8bce38d4e665999b9a3a0d4d9fb1de1b87a8c3a4b223e71e50f6', 1491800),
    'offset': ('e8dca8fa7d069883441bd8616e49234d338ab5707b3cc05d6b3a5693b2ba99e2', 525841),
    'pack': ('44d2341ac7cecf8a407e881bd4514e1f4fb6dd59530a4d172049d5c7e40f39a9', 4484936),
}
SCHEDULE = ['filesystem', 'pack', 'pack', 'filesystem', 'filesystem', 'pack', 'pack', 'filesystem', 'filesystem', 'pack']
SOURCE = '7945f956b428359928908a9f42e36417b113dafd'
HARNESS = '79cab09b35028beac804dde357bd789a0654d9e487b6960b63e0230cc8eb5f37'
FLOOR = 2688548864
ROOT = pathlib.Path('/workspace/work/rcc-swarm/evidence/performance-admission/go-object-pack-native-root-preexecution-1637')
RAM = pathlib.Path('/dev/shm/rcc-125-exact7945-embedprep-workers226')


def require(value, message):
    if not value:
        raise ValueError(message)


def file_hash(path):
    digest = hashlib.sha256()
    size = 0
    with path.open('rb') as stream:
        for block in iter(lambda: stream.read(65536), b''):
            digest.update(block)
            size += len(block)
    return digest.hexdigest(), size


def read_json(path, cap=512 * 1024):
    require(path.stat().st_size <= cap, f'JSON cap: {path}')
    return json.loads(path.read_bytes())


def inventory(path):
    digest, size = file_hash(path)
    return {'path': str(path), 'sha256': digest, 'bytes': size}


def original_paths():
    digest = EXPECTED['index'][0]
    return {
        'index': CAS / 'objects/sha256' / digest[:2] / digest[2:4] / digest,
        'offset': LAYOUT / 'offset-index.json',
        'pack': LAYOUT / 'small-objects.concat',
    }


def validate_selection():
    paths = original_paths()
    for name, path in paths.items():
        require(file_hash(path) == EXPECTED[name], f'original {name} identity changed')
    index = read_json(paths['index'], 2 * 1024 * 1024)
    offsets = read_json(paths['offset'], 1024 * 1024)
    small = {entry['storedDigest'].split(':', 1)[1]: entry for entry in index['entries'] if entry['storedSize'] < 4096}
    selected = offsets['entries']
    require(len(small) == len(selected) == 4649, 'selection count')
    require(offsets['format'] == 'rcc-experiment-raw-concat-v1', 'offset format')
    require(offsets['pack_bytes'] == 4484936, 'offset total')
    end = 0
    previous = ''
    oracle = hashlib.sha256()
    source_closure = hashlib.sha256()
    actual_pack = RAM / 'run-native-02/pack.bin'
    require(file_hash(actual_pack) == EXPECTED['pack'], 'actual RAM pack identity')
    provider = RAM / 'run-native-02/provider'
    require(actual_pack.stat().st_dev == provider.stat().st_dev, 'arms use different devices')
    pack_stream = actual_pack.open('rb')
    for entry in selected:
        digest = entry['digest'].split(':', 1)[-1]
        require(re.fullmatch('[0-9a-f]{64}', digest) is not None, 'digest format')
        require(previous < digest, 'digest ordering/uniqueness')
        previous = digest
        require(digest in small, 'selection member')
        size = small[digest]['storedSize']
        require(type(size) is int and 0 < size < 4096, 'stored-size range')
        require(entry['size'] == size and entry['offset'] == end, 'contiguous offset/size')
        end += size
        record = f'{digest}:{size}\n'.encode()
        oracle.update(record)
        source_closure.update(record)
        path = CAS / 'objects/sha256' / digest[:2] / digest[2:4] / digest
        metadata = path.lstat()
        require(stat.S_ISREG(metadata.st_mode), 'source object leaf not regular')
        require(file_hash(path) == (digest, size), 'source object digest/size')
        copied = provider / 'objects/sha256' / digest[:2] / digest[2:4] / digest
        require(stat.S_ISREG(copied.lstat().st_mode), 'owned object not regular')
        require(file_hash(copied) == (digest, size), 'owned provider object digest/size')
        pack_stream.seek(entry['offset'])
        content = pack_stream.read(size)
        require(len(content) == size and hashlib.sha256(content).hexdigest() == digest, 'actual pack slice identity')
        source_closure.update(f'{digest}:{stat.S_IMODE(metadata.st_mode)}:{metadata.st_mtime_ns}:{path}\n'.encode())
    pack_stream.close()
    require(end == 4484936, 'selected byte total')
    return oracle.hexdigest(), source_closure.hexdigest(), [inventory(p) for p in paths.values()] + [inventory(actual_pack)]


def validate_build_inputs():
    manifest_path = BASE / 'source-manifest.json'
    require(file_hash(manifest_path)[0] == 'c1bc6f5c00a96c9e1e3dc4bcf4bb39b832ef525bc2795f570af61765f17246e6', 'source manifest hash')
    manifest = read_json(manifest_path)
    require(manifest['source_commit'] == SOURCE, 'manifest source')
    tree = subprocess.check_output(['git', '-C', '/workspace/rcc', 'rev-parse', SOURCE + '^{tree}']).decode().strip()
    require(tree == manifest['source_tree'] == '49baf943b9d64ef79af56ec8f34eebe0385477b1', 'actual immutable Git tree')
    actual_git = {}
    for entry in subprocess.check_output(['git', '-C', '/workspace/rcc', 'ls-tree', '-r', '-z', SOURCE]).split(b'\0'):
        if entry:
            descriptor, relative = entry.split(b'\t', 1)
            mode, kind, blob = descriptor.split()
            actual_git[relative.decode()] = (mode.decode(), blob.decode())
    source = RAM / 'source-run-native-02'
    tracked_hash = hashlib.sha256()
    for entry in sorted(manifest['snapshot_files'], key=lambda x: x['path']):
        path = source / entry['path']
        require(stat.S_ISREG(path.lstat().st_mode), 'tracked source not regular')
        data = path.read_bytes()
        sha = hashlib.sha256(data).hexdigest()
        blob = hashlib.sha1(b'blob ' + str(len(data)).encode() + b'\0' + data).hexdigest()
        require((len(data), sha) == (entry['bytes'], entry['sha256']), 'tracked source byte mismatch: ' + entry['path'])
        require(actual_git[entry['path']] == (entry['git_mode'], blob) and blob == entry['git_blob'], 'tracked Git binding: ' + entry['path'])
        tracked_hash.update(entry['path'].encode() + b'\0' + sha.encode() + b'\0' + str(len(data)).encode() + b'\n')
    require(len(manifest['snapshot_files']) == 227 and sum(x['bytes'] for x in manifest['snapshot_files']) == 1541402, 'tracked source counts')
    gen = read_json(PREP / 'generated-inputs-receipt.json')
    gen_hash = hashlib.sha256()
    gen_total = 0
    for relative, item in sorted(gen['generated_embed_inputs'].items()):
        path = source / relative
        require(stat.S_ISREG(path.lstat().st_mode), 'generated input not regular')
        sha, size = file_hash(path)
        require((sha, size) == (item['sha256'], item['size']), 'generated input identity: ' + relative)
        gen_total += size
        gen_hash.update(relative.encode() + b'\0' + sha.encode() + b'\0' + str(size).encode() + b'\n')
    require(len(gen['generated_embed_inputs']) == 39 and gen_total == 7474027, 'generated counts')
    require(gen_hash.hexdigest() == gen['generated_embed_tree_sha256'] == '07e3c5978f32cbf9889bb24461e8cb2beee0b3f5358c4a6674b2337bc235e9e8', 'generated closure')
    require(file_hash(source / 'prep-probe/main.go')[0] == HARNESS, 'actual compiled harness source')
    cache_manifest = read_json(PREP / 'offline-gomodcache-manifest.json', 1024 * 1024)
    caches = []
    for directory, readonly in [(RAM / 'gomodcache', False), (RAM / 'gomodcache-run-native-02', True)]:
        expected = {x['path'] for x in cache_manifest['files']}
        actual = set()
        for path in directory.rglob('*'):
            mode = path.lstat().st_mode
            require(not stat.S_ISLNK(mode), 'module cache symlink')
            if stat.S_ISREG(mode):
                actual.add(path.relative_to(directory).as_posix())
            else:
                require(stat.S_ISDIR(mode), 'module cache special entry')
        require(actual == expected, 'module cache file census')
        closure = hashlib.sha256()
        observed = hashlib.sha256()
        for entry in cache_manifest['files']:
            path = directory / entry['path']
            sha, size = file_hash(path)
            actual_mode = stat.S_IMODE(path.lstat().st_mode)
            require((sha, size, actual_mode) == (entry['sha256'], entry['size'], entry['mode'] & ~0o222 if readonly else entry['mode']), 'cache bytes/mode mismatch')
            for digest, mode in [(closure, entry['mode']), (observed, actual_mode)]:
                digest.update(entry['path'].encode() + b'\0' + str(size).encode() + b'\0' + str(mode).encode() + b'\0' + sha.encode() + b'\n')
        require(closure.hexdigest() == cache_manifest['tree_sha256'] == '57a5f811603b86df1c2cc2cdf64bdca47eaca0ced8cca6bb8382caa776729e43', 'cache closure')
        caches.append({'path': str(directory), 'files': len(actual), 'bytes': sum(x['size'] for x in cache_manifest['files']), 'canonical_sha256': closure.hexdigest(), 'actual_modes_sha256': observed.hexdigest(), 'readonly_projection': readonly})
    return {'tracked_count': 227, 'tracked_bytes': 1541402, 'tracked_tree_sha256': tracked_hash.hexdigest(), 'generated_count': 39, 'generated_bytes': gen_total, 'generated_tree_sha256': gen_hash.hexdigest(), 'actual_git_tree': tree, 'current_module_caches': caches, 'temporal_scope': 'current bytes equal frozen pre-run manifests; supervisory post-run verification was skipped at home guard'}


def home_state():
    paths = [pathlib.Path('/dev'), pathlib.Path('/dev/shm'), RAM, RAM / 'run-native-02', RAM / 'run-native-02/rcc-home']
    rows = []
    for path in paths:
        st = path.lstat()
        require(stat.S_ISDIR(st.st_mode), 'home ancestor not a real directory')
        rows.append({'path': str(path), 'mode': oct(stat.S_IMODE(st.st_mode)), 'uid': st.st_uid, 'gid': st.st_gid, 'device': st.st_dev})
    require(rows[-3]['mode'] == rows[-2]['mode'] == '0o700', 'enclosing private mode')
    require(rows[-1]['mode'] == '0o750' and all(x['uid'] == os.getuid() for x in rows[-3:]), 'actual home guard failure')
    children = []
    for path in sorted(paths[-1].iterdir()):
        st = path.lstat()
        require(stat.S_ISDIR(st.st_mode), 'private home entry not real directory')
        children.append({'name': path.name, 'mode': oct(stat.S_IMODE(st.st_mode)), 'uid': st.st_uid, 'gid': st.st_gid})
    return {'ancestors': rows, 'immediate_children': children, 'postcondition': 'FAILED: RCC home0750 instead of required0700; enclosing owned directories0700'}


def check_resources(supervisor, admission):
    require(supervisor['elapsed_total_s'] < 180, 'total deadline')
    require(supervisor['limits'] == {'workspace_bytes': 33554432, 'ram_bytes': 1073741824, 'sampled_rss_bytes': 2147483648, 'floor_bytes': FLOOR, 'total_seconds': 180, 'build_seconds': 90, 'pass_seconds': 30}, 'changed limits')
    require(admission['limits'] == {'workspace': 33554432, 'ram': 1073741824, 'sampled_rss': 2147483648, 'floor': FLOOR, 'combined_seconds': 180, 'build_seconds': 90, 'pass_seconds': 30}, 'actual admission limits')
    aggregate = []
    for stage in supervisor['stages']:
        require(stage['numeric_exit'] == 0 and stage['stop_reason'] is None, 'failed stage')
        require(stage['reader_threads_joined'] is True, 'reader joins missing')
        require(stage['process_group_quiescent'] is True, 'process group not quiescent')
        for row in stage['samples']:
            require(row['workspace_free_bytes'] >= FLOOR, 'sample free-floor breach')
            require(row['owned_ram_allocated_bytes'] <= 1073741824, 'sample RAM breach')
            require(row['workspace_allocated_bytes'] <= 33554432, 'sample workspace breach')
            require(0 < row['rss_bytes_sampled'] <= 2147483648, 'invalid/excess sampled RSS')
            aggregate.append(row)
    require(aggregate, 'no resource observations')
    require(supervisor['owned_ram_allocated_bytes_at_receipt'] <= 1073741824, 'receipt RAM')
    require(supervisor['workspace_allocated_bytes_before_final_receipt'] <= 33554432, 'receipt workspace')
    return {
        'sample_count': len(aggregate),
        'max_sampled_group_plus_supervisor_RSS': max(row['rss_bytes_sampled'] for row in aggregate),
        'min_observed_free': min(row['workspace_free_bytes'] for row in aggregate),
        'max_observed_RAM': max(row['owned_ram_allocated_bytes'] for row in aggregate),
        'max_observed_workspace': max(row['workspace_allocated_bytes'] for row in aggregate),
        'census_scope': 'supervisor observed process-group members; not proof against arbitrary escaped descendants',
        'sampling_scope': 'finite sampled observations, not a continuous hard quota or peak-RSS proof',
        'missing_supervisor_final_guard': 'Home-mode guard interrupted remaining post-run checks; root subsequently verified current bytes/resources separately.',
    }


def verify(run):
    supervisor_path = run / 'supervisor-result.json'
    supervisor = read_json(supervisor_path)
    require(supervisor['status'] == 'FAIL' and supervisor['error'] == 'GuardFailure: private RCC runtime home permissions changed', 'unexpected supervisor outcome')
    require(supervisor['numeric_outer_exit'] == 1 and supervisor['numeric_build_exit'] == supervisor['numeric_harness_exit'] == 0, 'actual numeric exits')
    admission = read_json(ROOT / 'root-admission-native-v8.json')
    require(file_hash(PREP / 'supervise_native_v8.py')[0] == admission['supervisor_sha256'] == 'b39fe8994f2e1df5ed0f948aa03e15eef97b01691a3eddc2e8fbadb5f27453c4', 'actual supervisor binding')
    outer = read_json(ROOT / 'actual-v8-outer-result.json')
    require(outer['numeric_outer_exit'] == 1 and outer['reader_threads_joined'] is True and outer['errors'] == [], 'actual outer failure/joins')
    require(outer['admission_sha256'] == file_hash(ROOT / 'root-admission-native-v8.json')[0], 'outer admission binding')
    require([stage['phase'] for stage in supervisor['stages']] == ['build', 'harness'], 'stage census')
    require(supervisor['source_commit'] == SOURCE, 'source attribution')
    require(file_hash(PREP / 'harness-successor-v2.go')[0] == HARNESS, 'reviewed harness changed')
    inherited = supervisor['inherited_rlimit_fsize']
    require(inherited['checked_without_changing_parent_limits'] is True, 'missing inherited-limit preflight')
    for key in ['soft_limit_bytes', 'hard_limit_bytes']:
        require(inherited[key] is None or inherited[key] >= inherited['required_bytes'], 'inherited file cap insufficient')
    receipt_path = run / 'results.jsonl'
    require(receipt_path.stat().st_size <= 512 * 1024, 'result size cap')
    events = [json.loads(line) for line in receipt_path.read_text().splitlines()]
    require(len(events) == 12, 'event census')
    require([event['event'] for event in events] == ['run_start'] + ['pass_complete'] * 10 + ['run_complete'], 'event sequence')
    oracle, closure, original_inventory = validate_selection()
    final = events[-1]['result']
    require(final['status'] == 'PASS' and final['source_commit'] == SOURCE, 'final status/source')
    require(final['schedule'] == SCHEDULE, 'schedule')
    require(final['selected_count'] == final['untimed_pack_slice_count'] == 4649, 'selected/untimed count')
    require(final['selected_bytes'] == final['untimed_pack_slice_bytes'] == 4484936, 'selected/untimed bytes')
    require(final['untimed_pack_ordered_sha256'] == oracle, 'untimed oracle')
    require(final['source_closure_before_sha256'] == final['source_closure_after_sha256'] == closure, 'actual source closure')
    for name, prefix in [('index', 'index'), ('offset', 'offset_index'), ('pack', 'pack')]:
        require(final[prefix + '_sha256'] == final[prefix + '_after_sha256'] == EXPECTED[name][0], name + ' before/after identity')
    require(len(final['untimed_controls_passed']) == 6, 'negative-control count')
    for text in ['corrupted provider', 'missing provider', 'wrong declared object size', 'out-of-range and overflowing', 'wrong stored digest', 'independent']:
        require(any(text in control for control in final['untimed_controls_passed']), 'negative missing: ' + text)
    passes = final['passes']
    require(len(passes) == 10, 'raw pass count')
    reps = {'filesystem': 0, 'pack': 0}
    for position, value in enumerate(passes, 1):
        arm = SCHEDULE[position - 1]
        reps[arm] += 1
        require(events[position]['position'] == position and events[position]['pass'] == value, 'partial/final raw mismatch')
        require(value['arm'] == arm and value['replicate'] == reps[arm], 'arm/order/replicate')
        require(value['objects'] == 4649 and value['bytes'] == 4484936 and value['ordered_result_sha256'] == oracle, 'per-pass identity')
        require(value['provider_getobject_calls'] == (4649 if arm == 'filesystem' else 0), 'GetObject count')
        require(value['read_at_calls'] == (4649 if arm == 'pack' else 0), 'ReadAt count')
        require(value['pack_file_opens_during_pass'] == 0, 'pack reopened during timing')
        for field in ['wall_ns', 'cpu_user_ns', 'cpu_system_ns', 'proc_io_rchar_delta', 'proc_io_read_bytes_delta', 'proc_io_syscr_delta', 'rss_bytes_sampled_after_pass']:
            require(type(value[field]) is int and value[field] >= 0, 'counter unknown/negative: ' + field)
        require(0 < value['wall_ns'] < 30 * 10**9, 'per-pass time bound')
        require(0 < value['rss_bytes_sampled_after_pass'] <= 2147483648, 'RSS sample bound')
    markers = supervisor['stages'][1]['progress_events']
    require(len(markers) == 20, 'marker census')
    for i in range(10):
        start, done = markers[2*i:2*i+2]
        require(start['event'] == 'PASS_START' and done['event'] == 'PASS_DONE', 'marker order')
        require(start['position'] == done['position'] == i+1, 'marker position')
        require(start['arm'] == done['arm'] == passes[i]['arm'], 'marker arm')
        require(start['replicate'] == done['replicate'] == passes[i]['replicate'], 'marker replicate')
        require(done['wall_ns'] == passes[i]['wall_ns'], 'marker timer')
        require(0 <= done['monotonic_s'] - start['monotonic_s'] <= 30, 'marker watchdog interval')
    input_binding = validate_build_inputs()
    require(file_hash(run / 'rcc-go-packprobe') == ('38c84cf54b637f170591e96fbd70f7acac955f57f37b5384459f40e5e82d952c', 10277531), 'actual binary bytes')
    metadata = (ROOT / 'actual-v8-binary-metadata.txt').read_text()
    for text in ['go1.26.9', 'github.com/joshyorko/rcc/prep-probe', 'CGO_ENABLED=0', 'GOARCH=amd64', 'GOOS=linux', '-trimpath=true']:
        require(text in metadata, 'root observed actual build metadata: ' + text)
    require('vcs.revision=' not in metadata, 'unexpected embedded source revision')
    pairs = []
    for i in range(0, 10, 2):
        pair = {value['arm']: value for value in passes[i:i+2]}
        a, b = pair['filesystem']['wall_ns'], pair['pack']['wall_ns']
        pairs.append({'pair': i//2+1, 'order': SCHEDULE[i:i+2], 'filesystem_ns': a, 'pack_ns': b, 'difference_ns': a-b, 'ratio': a/b})
    stats = {arm: {'wall_ns_median': statistics.median(value['wall_ns'] for value in passes if value['arm'] == arm), 'wall_ns_range': [min(value['wall_ns'] for value in passes if value['arm'] == arm), max(value['wall_ns'] for value in passes if value['arm'] == arm)], 'cpu_ns_median': statistics.median(value['cpu_user_ns'] + value['cpu_system_ns'] for value in passes if value['arm'] == arm)} for arm in reps}
    return {'status': 'INDEPENDENT_RAW_COMPONENT_VERIFIED_WITH_SUPERVISOR_GUARD_FAIL_PRESERVED', 'utc': datetime.datetime.now(datetime.timezone.utc).isoformat(), 'actual_numeric': {'direct_outer': 1, 'supervisor': 1, 'build': 0, 'harness': 0}, 'actual_supervisor_error': supervisor['error'], 'source': SOURCE, 'original_inputs': original_inventory, 'ordered_oracle': oracle, 'actual_selected_source_closure': closure, 'build_input_binding': input_binding, 'home_state': home_state(), 'resources': check_resources(supervisor, admission), 'raw_passes': passes, 'pairs': pairs, 'statistics': stats, 'receipt_hashes': [inventory(supervisor_path), inventory(receipt_path), inventory(run / 'rcc-go-packprobe'), inventory(ROOT / 'actual-v8-outer-result.json'), inventory(ROOT / 'actual-v8-binary-metadata.txt'), inventory(ROOT / 'root-admission-native-v8.json'), inventory(PREP / 'supervise_native_v8.py'), inventory(PREP / 'harness-successor-v2.go')], 'limitations': ['Whole invocation FAILED; independent raw-data validation does not waive or repair the home0700 postcondition.', 'Component comparison only; no Actions/native materialization/end-to-end or default-format winner.', 'API calls are not measured filesystem-open syscall counts.', 'Five pairs are threeAB/twoBA with shared warm OS and Go allocator/GC state.', 'Wall excludes setup and receipt flushing; CPU includes observer overhead and concurrent Go threads/GC; post-passRSS is not peak.', 'Negative-control receipts bind to reviewed source but no independent corruption rerun is performed.', 'Root actual build-metadata observation is cited; this verifier hashes the binary but does not independently parse Go buildinfo.']}


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--run', type=pathlib.Path, required=True)
    parser.add_argument('--output', type=pathlib.Path, required=True)
    args = parser.parse_args()
    own = pathlib.Path(__file__).resolve().parent
    require(args.output.resolve().parent == own and not args.output.exists(), 'output must be new in reviewer namespace')
    try:
        result = verify(args.run)
        result['numeric_exit'] = 0
        numeric = 0
    except Exception as error:
        result = {'status': 'INDEPENDENT_REVIEW_FAILED', 'error': f'{type(error).__name__}: {error}', 'numeric_exit': 1}
        numeric = 1
    args.output.write_text(json.dumps(result, indent=2) + '\n')
    print(json.dumps({'status': result['status'], 'numeric_exit': numeric, 'output': str(args.output)}))
    raise SystemExit(numeric)
