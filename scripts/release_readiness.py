"""Export read-only readiness after canonical preparation and evidence validation.

Consumes the exporter's already-audited, bounded evidence. Never dispatches,
merges, tags, publishes or substitutes for the publisher's artifact validation.
"""
import argparse
import json
import os
import re
import tempfile
from pathlib import Path
from xml.etree import ElementTree

import release_publication as publication


def _robot_stats(path):
    total = ElementTree.parse(path).getroot().find('./statistics/total/stat')
    publication._require(total is not None, 'Robot totals are missing')
    return {key: int(total.get(key, '0')) for key in ('pass', 'fail', 'skip')}


def create_readiness(review_root, source_sha, version, run_id, manifest_sha,
                     exporter_sha, exporter_run_id, output, token,
                     api_url='https://api.github.com', repository='joshyorko/rcc'):
    require = publication._require
    require(repository == 'joshyorko/rcc', 'Only the RCC repository is supported')
    publication.validate_inputs(source_sha, version)
    require(bool(token), 'Read-only GitHub token is required')
    output = Path(output)
    require(not output.exists(), 'Readiness output already exists; select a new output path')
    repo_api = f"{api_url.rstrip('/')}/repos/{repository}"
    get = lambda path: publication._api_get(f'{repo_api}/{path}', token)
    require(get('git/ref/heads/main').get('object', {}).get('sha') == source_sha,
            'Preparation is stale: source must equal current main')
    run = get(f'actions/runs/{run_id}')
    require(type(run.get('run_attempt')) is int and run['run_attempt'] > 0, 'Run attempt is missing')
    artifact = publication.verify_preparation_api(api_url, token, repository, run_id,
                                                  source_sha, '.github/workflows/rcc.yaml')
    root = Path(review_root)
    publication.validate_review_evidence(root, source_sha, version, run_id,
        artifact['id'], exporter_run_id, manifest_sha, artifact['digest'], exporter_sha)
    # The review sidecar is emitted only after canonical ZIP and actual-binary
    # verification. Rechecking its canonical API identity denies substituted runs.
    publication.verify_release_absent(api_url, token, repository, version)
    previous = get('releases/latest')
    tag = previous.get('tag_name')
    require(previous.get('draft') is False and previous.get('prerelease') is False
            and isinstance(tag, str) and re.fullmatch(r'v\d+\.\d+\.\d+', tag),
            'Previous release is not a stable published version')
    prior_version = tuple(map(int, tag[1:].split('.')))
    require(re.fullmatch(r'v\d+\.\d+\.\d+', version), 'Candidate version must be stable')
    current_version = tuple(map(int, version[1:].split('.')))
    require(current_version == (*prior_version[:2], prior_version[2] + 1),
            'Candidate must be the next patch after the published release')
    obj = get(f'git/ref/tags/{tag}').get('object', {})
    published_ref_object = dict(obj)
    for _ in range(5):
        if obj.get('type') != 'tag':
            break
        obj = get(f"git/tags/{obj.get('sha')}").get('object', {})
    require(obj.get('type') == 'commit' and publication.SHA_RE.fullmatch(obj.get('sha', '')),
            'Previous release tag has no valid commit')
    previous_sha = obj['sha']
    require(type(previous.get('id')) is int and previous['id'] > 0, 'Previous release identity is missing')
    ancestry = get(f'compare/{previous_sha}...{source_sha}')
    require(ancestry.get('status') == 'ahead'
            and ancestry.get('merge_base_commit', {}).get('sha') == previous_sha,
            'Previous published source is not an ancestor of this preparation')
    jobs_data = get(f'actions/runs/{run_id}/jobs?per_page=100')
    jobs = jobs_data.get('jobs')
    require(isinstance(jobs, list) and jobs_data.get('total_count') == len(jobs),
            'Preparation job results are incomplete')
    require(len({job.get('name') for job in jobs}) == len(jobs), 'Preparation jobs are ambiguous')
    by_name = {job['name']: job for job in jobs}
    require(all(by_name.get(name, {}).get('conclusion') == 'success' for name in publication.REQUIRED_JOBS),
            'Required preparation jobs are missing or unsuccessful')
    identity = publication._read_json(root / 'review-identity.json')
    manifest = publication._read_json(root / 'evidence/asset-manifest.json')
    native = {}
    for platform in publication.NATIVE_RECEIPTS:
        rel = f'evidence/native/{platform}'
        native[platform] = {
            'receiptSha256': publication._sha256(root / rel / 'native-runtime-receipt.json'),
            'robotStatistics': _robot_stats(root / rel / 'output.xml'),
        }
    release = root / 'evidence/release-candidate'
    rollback = publication._read_json(release / 'n1/n1-rollback-state.json')
    gate_exit = publication._read_json(release / 'release-candidate-exit.json')
    receipt = {
        'schemaVersion': 1, 'status': 'READY_FOR_OWNER_REVIEW',
        'sourceSha': source_sha, 'version': version, 'observedCurrentMainSha': source_sha, 'releaseMetadataPr': None,
        'previousRelease': {'tag': tag, 'sourceSha': previous_sha},
        'preparation': {'workflow': '.github/workflows/rcc.yaml', 'runId': run_id,
            'runUrl': f'https://github.com/{repository}/actions/runs/{run_id}', 'runAttempt': run['run_attempt'],
            'artifactId': artifact['id'], 'artifactZipSha256': artifact['digest'],
            'assetManifestSha256': manifest_sha,
            'jobs': {name: {'conclusion': by_name[name]['conclusion'], 'id': by_name[name].get('id')}
                     for name in publication.REQUIRED_JOBS}},
        'assets': manifest['assets'],
        'full11': {'exit': {key: gate_exit[key] for key in ('sourceSha', 'gateExitCode', 'teeExitCode')},
                   'gates': publication._read_json(release / 'release-candidate-v1.json')['gates'],
                   'robotStatistics': _robot_stats(release / 'robot/output.xml')},
        'nativePlatforms': native,
        'consumerEvidenceSha256': publication._sha256(root / 'evidence/native/linux_amd64/jat-class-consumer-receipt.json'),
        'selfHostEvidenceSha256': publication._sha256(release / 'self-host-v1.json'),
        'n1Rollback': {'archiveRollback': rollback['archiveRollback'], 'evidenceSha256': publication._sha256(release / 'n1/n1-rollback-state.json')},
        'evidenceInventory': identity['files'],
        'exporter': {'sourceSha': exporter_sha, 'runId': exporter_run_id},
        'tagCollision': False,
        'publication': {'approvalState': 'OWNER_APPROVAL_REQUIRED', 'authorized': False,
            'workflow': '.github/workflows/publish-release.yml', 'environment': 'rcc-production',
            'inputs': {'preparation_run_id': str(run_id), 'source_sha': source_sha,
                       'expected_version': version, 'asset_manifest_sha256': manifest_sha}},
        'limitations': ['Readiness is a point-in-time observation, not publication authorization.',
            'Evidence must come from the preceding canonical exporter in the same trusted job; arbitrary local sidecars do not prove canonical provenance.',
            'Metadata-PR association and automatic preparation dispatch are not qualified by this read-only exporter.',
            'Protected publication repeats policy, artifact and collision checks; Homebrew dispatch access is not proved here.',
            'Platform signing limits remain those in docs/release-process.md; Robot skips are reported, not promoted to passes.'],
    }
    # API-backed identities can change while local receipt validation runs.
    require(get('git/ref/heads/main').get('object', {}).get('sha') == source_sha,
            'Main changed during readiness audit')
    fresh = get('releases/latest')
    require(fresh.get('id') == previous.get('id') and fresh.get('tag_name') == tag,
            'Published baseline changed during readiness audit')
    require(get(f'git/ref/tags/{tag}').get('object') == published_ref_object, 'Published tag moved during readiness audit')
    require(get(f'actions/runs/{run_id}').get('run_attempt') == run['run_attempt'], 'Preparation rerun changed during readiness audit')
    require(publication.verify_preparation_api(api_url, token, repository, run_id, source_sha, '.github/workflows/rcc.yaml') == artifact, 'Canonical artifact changed during readiness audit')
    publication.verify_release_absent(api_url, token, repository, version)
    output.parent.mkdir(parents=True, exist_ok=True)
    temporary = None
    try:
        with tempfile.NamedTemporaryFile(mode='w', dir=output.parent, prefix='.readiness-', delete=False) as stream:
            temporary = Path(stream.name)
            stream.write(json.dumps(receipt, indent=2, sort_keys=True) + '\n')
        os.replace(temporary, output)
    finally:
        if temporary is not None and temporary.exists():
            temporary.unlink()
    return receipt


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('review-root', 'source-sha', 'version', 'manifest-sha', 'exporter-sha', 'output'):
        parser.add_argument('--' + name, required=True)
    for name in ('run-id', 'exporter-run-id'):
        parser.add_argument('--' + name, type=int, required=True)
    args = parser.parse_args()
    receipt = create_readiness(Path(args.review_root), args.source_sha, args.version, args.run_id,
        args.manifest_sha, args.exporter_sha, args.exporter_run_id, Path(args.output), os.environ.get('GH_TOKEN'))
    print(f"{receipt['status']}: {args.version} at {args.source_sha}; protected publication still requires owner approval")


if __name__ == '__main__':
    main()
