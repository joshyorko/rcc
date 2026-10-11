"""Stub-only checks for supervisor helpers; never invokes worker or codec calls."""
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import unittest
sys.dont_write_bytecode = True
from unittest.mock import patch

RUNNER = Path('/workspace/work/rcc-swarm/evidence/continuation-20261011/encoding-large-micro/encoding_large.py')
SPEC = importlib.util.spec_from_file_location('encoding_large_static', RUNNER)
MOD = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MOD)
PLAN = json.loads(MOD.PLAN_PATH.read_text())


class Child:
    pid = 202
    def __init__(self, states): self.states = iter(states)
    def poll(self): return next(self.states)


class SupervisorStaticChecks(unittest.TestCase):
    def test_event_lines_and_eof_tail_are_preserved(self):
        first = b'{"event":"phase_start","phase":"codec"}\n{"event":"phase_done"}'
        pending, events = MOD.parse_event_bytes(b'', first)
        self.assertEqual(pending, b'{"event":"phase_done"}')
        pending, tail = MOD.parse_event_bytes(pending, b'', final=True)
        self.assertEqual(pending, b'')
        self.assertEqual(events + tail, [{"event":"phase_start", "phase":"codec"}, {"event":"phase_done"}])

    def test_eof_records_normal_numeric_zero_exit_and_final_event(self):
        child = Child([0])
        code, pending, events = MOD.finish_worker_output(child, b'{"event":"complete","status":"PASS","sample_count":500}')
        self.assertEqual(code, 0)
        self.assertEqual(pending, b'')
        self.assertEqual(events, [{"event":"complete", "status":"PASS", "sample_count":500}])

    def test_eof_while_worker_live_is_rejected(self):
        child = Child([None])
        with self.assertRaisesRegex(RuntimeError, 'worker is still live'):
            MOD.finish_worker_output(child, b'')

    def test_ps_tree_requires_live_worker_and_counts_it(self):
        fake = subprocess.CompletedProcess([], 0, stdout='101 1 100\n202 101 300\n', stderr='')
        with patch.object(MOD.subprocess, 'run', return_value=fake):
            rss, pids, seen = MOD.process_tree_rss(101, 202)
        self.assertEqual((rss, pids, seen), (409600, [101, 202], True))

    def test_ps_tree_fails_closed_when_live_worker_missing(self):
        fake = subprocess.CompletedProcess([], 0, stdout='101 1 100\n', stderr='')
        with patch.object(MOD.subprocess, 'run', return_value=fake):
            with self.assertRaisesRegex(RuntimeError, 'known live worker PID missing'):
                MOD.process_tree_rss(101, 202)

    def test_poll_to_ps_exit_race_is_recorded_as_exited(self):
        child = Child([None, 0])
        with patch.object(MOD, 'process_tree_rss', side_effect=[RuntimeError('worker disappeared'), (102400, [101], False)]):
            rss, pids, seen, alive = MOD.sample_process_tree(101, child)
        self.assertEqual((rss, pids, seen, alive), (102400, [101], False, False))

    def test_live_worker_missing_in_race_fails_closed(self):
        child = Child([None, None])
        with patch.object(MOD, 'process_tree_rss', side_effect=RuntimeError('worker missing')):
            with self.assertRaisesRegex(RuntimeError, 'worker missing'):
                MOD.sample_process_tree(101, child)

    def valid_worker_result(self):
        calls = {phase: PLAN['schedule']['calls_per_phase'] for phase in MOD.PHASES}
        objects = PLAN['source']['objects']
        receipt = {entry['storedDigest']: {'sha256': '0' * 64, 'size': entry['storedSize']} for entry in objects}
        logical_total = PLAN['source']['selected_logical_bytes'] * PLAN['schedule']['repetitions']
        phase_summary = {phase: {'object_calls': 100, 'logical_bytes_sum': logical_total} for phase in MOD.PHASES}
        loaded_modules = {name: {'path': str(Path(info['path']).resolve()), 'sha256': info['sha256']}
                          for name, info in PLAN['toolchain']['module_hashes'].items()}
        toolchain_runtime = {'python_executable': PLAN['toolchain']['python_executable'],
                             'python_sha256': PLAN['toolchain']['python_sha256'],
                             'zlib_version': PLAN['toolchain']['zlib_version'],
                             'python_zstandard_version': PLAN['toolchain']['python_zstandard_version'],
                             'libzstd_version': PLAN['toolchain']['libzstd_version'],
                             'loaded_modules': loaded_modules}
        samples = [{'phase': phase, 'repetition': rep, 'storedDigest': entry['storedDigest'],
                    'logical_output_sha256': entry['legacyObjectId'], 'verified': True}
                   for rep in range(PLAN['schedule']['repetitions'])
                   for phase in MOD.PHASES for entry in objects]
        return {
            'status': 'PASS', 'plan_sha256': MOD.digest(MOD.PLAN_PATH.read_bytes()),
            'runner_sha256': MOD.digest(RUNNER.read_bytes()), 'calls_by_phase': calls,
            'sample_count': sum(calls.values()),
            'untimed_codec_calls': dict(PLAN['measurement']['preparation_call_counts_expected']),
            'canonical_frame_receipts': {'gzip': receipt, 'zstd': receipt},
            'phase_summary': phase_summary, 'toolchain_runtime': toolchain_runtime,
            'source_before': {'same': True}, 'source_after': {'same': True},
            'samples': samples
        }

    def test_result_guard_accepts_complete_frozen_counts(self):
        MOD.validate_worker_result(self.valid_worker_result(), PLAN, MOD.digest(RUNNER.read_bytes()))

    def test_result_guard_rejects_incomplete_timed_counts(self):
        result = self.valid_worker_result()
        result['calls_by_phase']['zstd-level3-decode'] -= 1
        with self.assertRaisesRegex(RuntimeError, 'timed-call counts'):
            MOD.validate_worker_result(result, PLAN, MOD.digest(RUNNER.read_bytes()))

    def test_result_guard_rejects_wrong_untimed_counts(self):
        result = self.valid_worker_result()
        result['untimed_codec_calls']['original_gzip_decode_for_input_validation'] -= 1
        with self.assertRaisesRegex(RuntimeError, 'untimed preparation counts'):
            MOD.validate_worker_result(result, PLAN, MOD.digest(RUNNER.read_bytes()))

    def test_result_guard_rejects_incomplete_canonical_frames(self):
        result = self.valid_worker_result()
        result['canonical_frame_receipts']['gzip'].pop(next(iter(result['canonical_frame_receipts']['gzip'])))
        with self.assertRaisesRegex(RuntimeError, 'canonical frame receipt'):
            MOD.validate_worker_result(result, PLAN, MOD.digest(RUNNER.read_bytes()))

    def test_result_guard_rejects_source_mutation(self):
        result = self.valid_worker_result()
        result['source_after'] = {'same': False}
        with self.assertRaisesRegex(RuntimeError, 'source changes'):
            MOD.validate_worker_result(result, PLAN, MOD.digest(RUNNER.read_bytes()))

    def test_result_guard_rejects_loaded_module_path_mismatch(self):
        result = self.valid_worker_result()
        result['toolchain_runtime']['loaded_modules']['zstandard_backend']['path'] = '/unexpected/backend.so'
        with self.assertRaisesRegex(RuntimeError, 'loaded module receipt'):
            MOD.validate_worker_result(result, PLAN, MOD.digest(RUNNER.read_bytes()))


if __name__ == '__main__':
    unittest.main(verbosity=2)
