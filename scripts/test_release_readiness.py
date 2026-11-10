import copy
import json
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).parent))
import release_publication as publication
import release_readiness as readiness
import test_release_publication as fixtures

SOURCE, VERSION = fixtures.SOURCE, fixtures.VERSION


class ReadinessTests(unittest.TestCase):
    def setUp(self):
        self.scratch = tempfile.TemporaryDirectory()
        self.addCleanup(self.scratch.cleanup)
        self.root = Path(self.scratch.name)
        bundle = self.root / 'prepared'
        bundle.mkdir()
        self.manifest = fixtures.ReleasePublicationTests().review_fixture(bundle)
        self.artifact = {'id': 456, 'name': 'rcc-release-preparation', 'expired': False,
                         'digest': 'sha256:' + 'b' * 64}
        self.review = self.root / 'review'
        with patch.object(publication, 'validate_actual_binary_metadata'):
            publication.stage_review_evidence(bundle, self.review, SOURCE, VERSION, 123,
                self.manifest, self.artifact, 'c' * 40, 789)
        self.jobs = [{'name': name, 'id': i + 1, 'conclusion': 'success'}
                     for i, name in enumerate(publication.REQUIRED_JOBS)]
        self.run = {'id': 123, 'run_attempt': 1, 'workflow_id': 42, 'event': 'workflow_dispatch',
                    'head_branch': 'main', 'head_sha': SOURCE, 'conclusion': 'success'}
        self.previous = {'id': 10, 'tag_name': 'v18.19.5', 'draft': False, 'prerelease': False}
        self.main = SOURCE
        self.calls = []
        self.output = self.root / 'readiness.json'

    def api(self, url, token):
        self.calls.append(url)
        if url.endswith('git/ref/heads/main'):
            return {'object': {'sha': self.main}}
        if url.endswith('releases/latest'):
            return copy.deepcopy(self.previous)
        if '/git/ref/tags/' in url:
            return {'object': {'type': 'commit', 'sha': 'd' * 40}}
        if '/compare/' in url:
            return {'status': 'ahead', 'merge_base_commit': {'sha': 'd' * 40}}
        if url.endswith('/actions/runs/123'):
            return copy.deepcopy(self.run)
        if url.endswith('/actions/workflows/42'):
            return {'path': '.github/workflows/rcc.yaml'}
        if '/jobs?' in url:
            return {'jobs': copy.deepcopy(self.jobs), 'total_count': len(self.jobs)}
        if '/artifacts?' in url:
            return {'artifacts': [copy.deepcopy(self.artifact)]}
        raise AssertionError('Unexpected API request: ' + url)

    def invoke(self, api=None):
        with patch.object(publication, '_api_get', side_effect=api or self.api), \
             patch.object(publication, 'verify_release_absent', return_value=True):
            return readiness.create_readiness(self.review, SOURCE, VERSION, 123, self.manifest,
                                             'c' * 40, 789, self.output, 'test-token')

    def test_complete_audited_evidence_is_ready_for_review_never_publication(self):
        result = self.invoke()
        self.assertEqual(result['status'], 'READY_FOR_OWNER_REVIEW')
        self.assertEqual(result['sourceSha'], SOURCE)
        self.assertEqual(result['full11']['robotStatistics']['skip'], 1)
        self.assertEqual(len(result['nativePlatforms']), 4)
        self.assertEqual(len(result['assets']), 9)
        self.assertFalse(result['publication']['authorized'])
        self.assertEqual(result['publication']['approvalState'], 'OWNER_APPROVAL_REQUIRED')
        self.assertEqual(result['publication']['inputs']['asset_manifest_sha256'], self.manifest)
        self.assertEqual(json.loads(self.output.read_text()), result)
        self.assertNotIn('/tmp/home', self.output.read_text())
        with self.assertRaisesRegex(publication.ReleaseGateError, 'already exists'):
            self.invoke()

    def test_missing_failed_skipped_wrong_sha_and_nonpreparation_runs_deny(self):
        for mutation in ('missing', 'failed', 'skipped', 'sha', 'event', 'attempt'):
            with self.subTest(mutation=mutation):
                jobs, run = copy.deepcopy(self.jobs), dict(self.run)
                if mutation == 'missing': self.jobs.pop()
                elif mutation in ('failed', 'skipped'): self.jobs[0]['conclusion'] = mutation
                elif mutation == 'sha': self.run['head_sha'] = 'e' * 40
                elif mutation == 'event': self.run['event'] = 'push'
                else: self.run['run_attempt'] = None
                with self.assertRaises(publication.ReleaseGateError): self.invoke()
                self.assertFalse(self.output.exists())
                self.jobs, self.run = jobs, run

    def test_stale_main_before_or_during_audit_denies_output(self):
        self.main = 'f' * 40
        with self.assertRaisesRegex(publication.ReleaseGateError, 'stale'): self.invoke()
        self.main = SOURCE
        def mutate(url, token):
            data = self.api(url, token)
            if '/jobs?' in url: self.main = 'f' * 40
            return data
        with self.assertRaisesRegex(publication.ReleaseGateError, 'Main changed'): self.invoke(mutate)
        self.assertFalse(self.output.exists())

    def test_changed_evidence_digest_manifest_and_duplicate_artifact_deny(self):
        def duplicate(url, token):
            result = self.api(url, token)
            if '/artifacts?' in url: result['artifacts'] *= 2
            return result
        with self.assertRaises(publication.ReleaseGateError): self.invoke(duplicate)
        original = dict(self.artifact)
        self.artifact['digest'] = 'sha256:' + 'f' * 64
        with self.assertRaises(publication.ReleaseGateError): self.invoke()
        self.artifact = original
        (self.review / 'evidence/native/windows_amd64/output.xml').write_text('modified')
        with self.assertRaises(publication.ReleaseGateError): self.invoke()
        self.assertFalse(self.output.exists())

    def test_tag_collision_missing_token_and_unstable_previous_release_deny(self):
        with patch.object(publication, '_api_get', side_effect=self.api), \
             patch.object(publication, 'verify_release_absent', side_effect=publication.ReleaseGateError('tag already exists')):
            with self.assertRaisesRegex(publication.ReleaseGateError, 'tag already exists'):
                readiness.create_readiness(self.review, SOURCE, VERSION, 123, self.manifest, 'c' * 40,
                                          789, self.output, 'test-token')
        self.previous['prerelease'] = True
        with self.assertRaises(publication.ReleaseGateError): self.invoke()
        with self.assertRaisesRegex(publication.ReleaseGateError, 'token is required'):
            readiness.create_readiness(self.review, SOURCE, VERSION, 123, self.manifest, 'c' * 40,
                                      789, self.output, '')
        self.assertFalse(self.output.exists())

    def test_published_baseline_or_rerun_change_denies_output(self):
        for mutate in ('publication', 'rerun'):
            with self.subTest(mutate=mutate):
                self.previous['id'], self.run['run_attempt'] = 10, 1
                count = 0
                def api(url, token):
                    nonlocal count
                    result = self.api(url, token)
                    if '/jobs?' in url:
                        count += 1
                        if count == 2:
                            if mutate == 'publication': self.previous['id'] = 11
                            else: self.run['run_attempt'] = 2
                    return result
                with self.assertRaises(publication.ReleaseGateError): self.invoke(api)
                self.assertFalse(self.output.exists())


if __name__ == '__main__':
    unittest.main()
