from pathlib import Path
import json, hashlib, psutil, time, subprocess, statistics
p = Path(__file__).resolve().parent
c = json.loads((p/'consumer-result.json').read_text())
s = json.loads((p/'supervisor-result.json').read_text())
assert c['status'] == 'passed_actions_and_warm_reuse'
assert s['worker_exit_code'] == s['status'] == 0
checkout = Path('/workspace/work/rcc-swarm/actions-performance-20261011')
assert subprocess.check_output(['git','rev-parse','HEAD'],cwd=checkout,text=True).strip() == c['source']['actions_commit'] == 'a70993fafc99a9f94041485542d5a720797ca394'
assert not subprocess.check_output(['git','diff','HEAD','--'],cwd=checkout)
assert c['source']['rcc_sha256'] == hashlib.sha256(Path(c['source']['rcc_path']).read_bytes()).hexdigest()
assert c['pool']['settings'] == {'max_processes':1,'min_processes':1,'reuse_processes':True}
assert len(c['calls']) == 6
first = c['calls'][0]
identity = first['handle_identity']
lease = first['lease_inside']['matching_owner_entries'][0]
actualresults = []
for call in c['calls']:
    assert call['return_code'] == 0
    assert call['handle_identity'] == call['handle_identity_after'] == identity
    assert call['pool_counts_inside'] == {'idle':0,'running':1}
    assert call['pool_counts_after'] == {'idle':1,'running':0}
    for field in ['lease_inside','lease_after_idle']:
        assert call[field]['matching_owner_entries'] == [lease]
    raw = Path(call['result_file']).read_bytes()
    assert hashlib.sha256(raw).hexdigest() == call['result_file_sha256']
    assert json.loads(raw) == call['result'] == {'message':'','result':'rcc-v18.19.3','status':'PASS'}
    actualresults.append({'call':call['call'],'bytes':len(raw),'sha256':call['result_file_sha256']})
assert lease['artifactDigest'] == c['source']['artifact_digest']
assert lease['ownerPid'] == identity['wrapper_pid']
assert lease['materializationId'] == 'c2fd1aa_5a1fac3_02c4107a'
owner_start_from_ct = round((identity['wrapper_create_time'] - psutil.boot_time())*100)
assert str(owner_start_from_ct) == lease['ownerStart']
assert len(identity['worker_processes']) == 1
worker = identity['worker_processes'][0]
assert Path(worker['cmdline'][0]).resolve().is_relative_to(Path('/workspace/work/rcc-swarm/evidence/performance-steer-20261011/runtime-prerequisites/warm-reuse-attempt-12/consumer-home').resolve())
cleanup = c['cleanup']
assert cleanup['acceptance'] == 'verified'
assert cleanup['lease_exists_after_dispose'] is False
assert cleanup['remaining_owned_identities'] == []
assert not Path(cleanup['lease_path']).exists()
assert len(cleanup['receipts']) == 1
record = cleanup['receipts'][0]
raw_receipt = Path(record['path']).read_bytes()
assert hashlib.sha256(raw_receipt).hexdigest() == record['sha256']
receipt = json.loads(raw_receipt)
assert receipt['leaseId'] == lease['id']
assert receipt['artifactDigest'] == lease['artifactDigest']
assert receipt['verification']['valid']
assert receipt['status'] == 'failed' and receipt['exitCode'] == -1
seen = {}
for sample in s['process_samples']:
    for item in sample['processes']:
        seen[(item['pid'],item['create_time'])] = item
assert (identity['wrapper_pid'],identity['wrapper_create_time']) in seen
assert (worker['pid'],worker['create_time']) in seen
remaining = []
for (pid,ct),item in seen.items():
    try:
        q = psutil.Process(pid)
        if abs(q.create_time()-ct) < .01:
            remaining.append({'pid':pid,'status':q.status()})
    except psutil.NoSuchProcess:
        pass
assert remaining == []
assert s['final_full_owned_bytes']['combined'] < s['limits']['stop_total_bytes'] < s['limits']['hard_total_bytes']
assert s['final_free_bytes'] > s['limits']['minimum_free_bytes']
assert not any(x.get('stop_trigger') for x in s['disk_scans'])
code = (p/'consumer_worker.py').read_text()
assert 'action_started = time.perf_counter_ns()\n                code = handle.run_action(' in code
assert 'run_action_wall_ns = time.perf_counter_ns() - action_started' in code
hashes = {n:hashlib.sha256((p/n).read_bytes()).hexdigest() for n in ['consumer_worker.py','run_consumer.py','consumer-result.json','supervisor-result.json','consumer-events.jsonl','harness-manifest.json','preflight.json']}
api = [v['run_action_wall_ns_excluding_obtain_return'] for v in c['calls']]
context = [v['wall_ns_including_obtain_and_return'] for v in c['calls']]
r = {
    'checkedAt':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),
    'result':'verified_stable3_warm_reuse_single_control',
    'source':c['source'], 'rawEvidenceHashes':hashes, 'actualResults':actualresults,
    'numericWorkerAndSupervisorExit':0,
    'poolConstructorPrewarmWallNs':c['pool']['constructor_prewarm_wall_ns'],
    'runActionApiWallsNs':api,'fiveWarmApiMedianNs':statistics.median(api[1:]),
    'instrumentedContextWallsNs':context,
    'contextIncludes':'obtain, diagnostic identity/lease checks, run_action and return; not pure API latency',
    'sameWorkerAcrossSixCalls':{'wrapperPid':identity['wrapper_pid'],'wrapperCreateTime':identity['wrapper_create_time'],'pythonPid':worker['pid'],'pythonCreateTime':worker['create_time'],'generation':identity['generation'],'handleObjectId':identity['handle_object_id'],'leaseId':lease['id'],'leaseObservedRawHash':lease['sha256'],'leaseOwnerStartTicksReconstructedFromSampledCreateTime':owner_start_from_ct},
    'leaseEvidenceLimit':'live lease metadata/hash was captured by source-checked observer; raw live JSON was not separately preserved and cannot be independently rehashed by root. Actual disposal receipt and result files were independently read.',
    'actualReceiptSha256':record['sha256'],'receiptStatus':'failed','receiptExitCode':-1,
    'teardownScope':'intentional dispose after six successful calls; wrapper return 0, lease gone, every sampled identity gone',
    'remainingRecordedProcesses':remaining,
    'sampledTreeRssSumBytes':s['max_sampled_process_tree_rss_sum_bytes'],
    'sampledIndividualRssBytes':s['max_sampled_individual_process_rss_bytes'],
    'rssLimit':'sampled, not peak; tree sum may double-count shared pages',
    'cpuLimit':'stage deltas cover surviving/new processes and observer; exited child CPU omitted',
    'outerWallNsIncludingSetupAndCleanup':s['wall_ns_including_setup_and_cleanup'],
    'ownedFootprintBytes':s['final_full_owned_bytes'],'finalFreeBytes':s['final_free_bytes'],
    'limits':s['limits'],'sourceMutationRootGitDiff':False,
    'qualification':'warm-only supported external community control, not current221 controller or candidate6 acceptance; no controlled optimization comparison',
    'rootVerificationSetupCorrection':'Initial root literal source-pattern assertion used run_action_start_ns; actual source uses action_started. Source was inspected and the correct immediate timer boundary verified; runtime files were not changed.'
}
(p/'root-independent-warm-reuse-verification.json').write_text(json.dumps(r,indent=2)+'\n')
print(json.dumps({k:r[k] for k in ['result','runActionApiWallsNs','fiveWarmApiMedianNs','poolConstructorPrewarmWallNs','sameWorkerAcrossSixCalls','remainingRecordedProcesses','ownedFootprintBytes','finalFreeBytes']},indent=2))
