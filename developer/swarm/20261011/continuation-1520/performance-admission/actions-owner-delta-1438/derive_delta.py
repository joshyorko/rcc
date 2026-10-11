#!/usr/bin/env python3
"""Derive this read-only owner delta from retained public responses; no networking."""
import datetime
import hashlib
import json
import pathlib
import shutil

ROOT = pathlib.Path(__file__).resolve().parent
PRIOR = ROOT.parent / "actions-owner-refresh-1350"


def read(name):
    return json.loads((ROOT / name).read_bytes())


def digest(path):
    data = path.read_bytes()
    return {"sha256": hashlib.sha256(data).hexdigest(), "bytes": len(data)}


def write(name, value):
    (ROOT / name).write_text(json.dumps(value, indent=2, sort_keys=True) + "\n")


def body_record(filename, obj):
    data = obj["body"].encode("utf-8")
    (ROOT / filename).write_bytes(data)
    return {
        "file": filename, "id": obj["id"], "url": obj["html_url"],
        "created_at": obj["created_at"], "updated_at": obj["updated_at"],
        "author": obj["user"]["login"], **digest(ROOT / filename),
    }


provenance = read("fetch-provenance.json")
for record in provenance:
    actual = digest(ROOT / record["file"])
    assert actual["sha256"] == record["sha256"], record["file"]
    assert actual["bytes"] == record.get("bytes", record.get("size")), record["file"]

issues = {number: read(f"issue-{number}.json") for number in (101, 133, 134, 139, 143, 221)}
delta = read("initial-delta.json")
for item in delta["threads"]:
    current = issues[item["number"]]
    assert current["comments"] == item["new_comments"]
    assert hashlib.sha256(current["body"].encode()).hexdigest() == item["body_sha256"]

owner125 = next(x for x in read("rcc-new-comments-125-page-1.json") if x["id"] == 6109416621)
owner118 = next(x for x in read("rcc-new-comments-118-page-1.json") if x["id"] == 6109416839)
assert owner125["body"] == owner118["body"]
owner_bodies = [body_record("owner-rcc125-6109416621.md", owner125),
                body_record("owner-rcc118-6109416839.md", owner118)]
issue_bodies = [body_record(f"issue-{number}-body.md", issues[number]) for number in (101, 221)]
coordination = read("new-comments-133-page-1.json")
assert len(coordination) == 1 and coordination[0]["id"] == 6109883396
root_coord = body_record("coordination-actions133-6109883396.md", coordination[0])

since_counts = {}
for number in (101, 133, 134, 139, 143, 221):
    entries = read(f"new-comments-{number}-page-1.json")
    assert len(entries) < 100, "would require pagination"
    since_counts[str(number)] = len(entries)
pulls = read("open-pulls-page-1.json")
assert len(pulls) == 8 and len(pulls) < 100
pr221 = read("pull-221.json")
integration = read("integration-e3c-git.json")
head = "e3c29cde8878307912e55ed09b6436c0f91d06d5"
base = "a0d258a16095b95b1bf9000cb1201cddfaff4779"
assert integration["sha"] == pr221["head"]["sha"] == head
assert integration["tree"]["sha"] == "2238ed312babba9fd5ad89a3a1cc581ca6ad2dcf"
assert [x["sha"] for x in integration["parents"]] == [base, "74beef4d220c3a1e8f983cecd42b9bb84ac75e79"]
compare = read("compare-a0d-e3c.json")
assert compare["base_commit"]["sha"] == compare["merge_base_commit"]["sha"] == base
assert compare["status"] == "ahead" and compare["total_commits"] == 9
changed = [x["filename"] for x in compare["files"]]
assert len(changed) == 5
assert changed == ["action_server/src/actions/server/_server.py",
                   "action_server/src/actions/server/run_outputs/transport.py",
                   "action_server/tests/action_server_tests/run_outputs/test_transport.py",
                   "docs/skills/provenance-observability.md", "docs/skills/repository-operations.md"]
assert read("compatibility-source-b614-full.json")["message"].startswith("No commit found")
assert read("compatibility-source-b614-git-full.json")["message"] == "Not Found"

prior_hashes = {name: digest(PRIOR / name) for name in (
    "report.md", "source-bound.json", "immutable-source-proof.json", "owner-evidence-proof.json")}
assert prior_hashes["report.md"]["sha256"] == "b9bf49c496ff9665654e9ee6f8313b2568e012dec96041134893f9453894ebc9"
assert prior_hashes["source-bound.json"]["sha256"] == "eca7b275691badd8371a71e7129b03a38fd02d05a88253c8e7d45d235b841ed8"

result = {
    "scope": "read-only delta since frozen1350 observation13:55:15Z; no Actions/runtime mutations",
    "snapshot_times": {"metadata": "2026-10-11T14:38:44Z", "comments_compare": "2026-10-11T14:40:01Z", "exact_source_availability": "2026-10-11T14:43:27Z"},
    "thread_delta": delta["threads"],
    "pagination": {"since": "2026-10-11T13:55:15Z", "semantics": "updated-since includes edits to existing comments", "actions_comment_counts_in_delta": since_counts, "all_delta_comment_pages_terminal_short": True, "open_pr_pages": 1, "open_pr_terminal_count": len(pulls), "old174comments_refetched": False},
    "current_public_integration": {"commit": head, "tree": integration["tree"]["sha"], "parents": [x["sha"] for x in integration["parents"]], "draft": pr221["draft"], "merged": pr221["merged"], "base": pr221["base"]["sha"], "compare_total_commits": 9, "compare_changed_files": changed, "adapter_and_acceptance_files_changed": False, "proof_files": ["integration-e3c-git.json", "compare-a0d-e3c.json"]},
    "open_prs": [{"number": x["number"], "head": x["head"]["sha"], "branch": x["head"]["ref"], "base": x["base"]["sha"], "draft": x["draft"], "updated_at": x["updated_at"]} for x in pulls],
    "owner_contract_bodies": owner_bodies,
    "campaign_bodies": issue_bodies,
    "root_coordination": root_coord,
    "claim_citations": [
        {"file": "owner-rcc125-6109416621.md", "line": line, "body_sha256": owner_bodies[0]["sha256"], "scope": "independently retained public owner assertion; underlying source/receipts not independently obtained"}
        for line in (1, 3, 5, 7, 13, 15, 17, 21, 22, 24, 26)
    ] + [{"file": "issue-101-body.md", "line": 7, "body_sha256": issue_bodies[0]["sha256"], "scope": "fresh coordinator lead reports completed full source tests"}],
    "newer_test_support": {
        "status": "OWNER_REPORTED_IMPLEMENTED_SOURCE_AND_SOURCE_TESTS; RAW_SOURCE_NOT_PUBLICLY_OBTAINED",
        "commit": "b614864ba9da55c13662894b6ef3c8515d9fdb25", "tree": "6fa4f5c1e9837a0d893868255575b355b3977fae",
        "reported_focused": {"passed": 180, "skipped": 1, "deselected": 3, "lint_types": "pass"},
        "reported_current_full": {"passed": 1363, "skipped": 13, "source": "current101/221lead; older preserved comment says inprogress"},
        "public_full_commit_http": 422, "public_git_commit_http": 404,
        "source_locally_or_remotely_reviewed_in_this_delta": False,
        "selector_name_cli_env_and_descriptor_schema_received": False,
        "contract": ["explicit isolated test selector", "operator-approved immutable provenance tuple", "native SHA256 before probe/cache/acquire/execute", "observed version/platform and required features", "unknown selector/missing binary/wrong bytes or platform/stale or co-mutated descriptor fail closed before fallback or artifact access"],
        "production_download_build_default": "v18.19.3 unchanged perowner and inherited public integration source",
    },
    "conditional_binaries_owner_reported": [
        {"version": "v18.19.5", "bytes": 22360226, "sha256": "1a617ad7c736fa67c605e20e5ebe3c7d54b02cd548f733e05c809cf49a48db1e", "asset": 549249380, "embedded_source": "d1aec7d0bb897a81274423c7a6bb747233f9c263", "go": "1.26.5", "vcs_modified": True, "clean_source_equivalence": "UNKNOWN; production promotion blocker", "functional_run": "NOT_RUN"},
        {"version": "v18.19.6", "bytes": 22384802, "sha256": "be55dadc391ae4ac7dd38356755a1698c595fdf41bd4a0141f8df7cc9d29f00e", "source": "727c1ff8b679cdbbe12836e53c87734a1edb0071", "tree": "cfae5c5ee9df0e5031e8e9ae37040b5c98ac6152", "run": 38105340592, "native_artifact": 11689755940, "receipt_artifact": 11689851536, "go": "1.26.9", "vcs_modified": False, "functional_run": "NOT_RUN", "newer_7945_source_qualification_inferred": False},
    ],
    "original_current_control_owner_reported": {
        "source": "b614864ba9da55c13662894b6ef3c8515d9fdb25", "tree": "6fa4f5c1e9837a0d893868255575b355b3977fae",
        "RCC3_binary_sha256": "7e588c01751ca2ae15ba13ef67f2f4b7567697a5a8389737059a73936f509428",
        "frozen_two_case_config_sha256": "8c5da5be25ebfcf86f286ee9f93ddad328c68b3243b0935ec5937c93376bb948",
        "publication_details": "PASS once; raw receipt not independently obtained here",
        "publication_receipt_sha256": "22121727f88ac95fbbef644231efb93884cff54f8ca7281e79b089c67eafa6bd",
        "staged_action": "STARTED_WITHOUT_TERMINAL_RESULT",
        "capacity_threshold_gib": 2.35, "sampled_namespace_bytes": 2529735510,
        "stop_target": "recorded pytest PID/create-time only", "cleanup": "ProcessTreeCleanupError; final descendants/readers absent",
        "failure_review_sha256": "d14c888b448f5ad59dcfc5ae26b75bda80901b297f8735902146fc6536227fc3", "failure_review_decision": "STOP",
        "raw_failure_review_and_config_obtained_here": False,
        "fix_state": "prospective once-owned package-cache sharing plus failure-metadata retention; preserve frozen helper and original failure",
        "version_incompatibility_inferred": False,
    },
    "admission": {
        "newer6_functionally_qualified": False, "real_six_workload_admission": False, "fresh_runtime_in_this_delta": 0,
        "next_actions_execution": "BLOCKED by owner original STOP, missing independently reviewable source/controller/fixture/failure receipts and guarded capacity",
        "independent_warm_RCC_component": "existing accepted provider/codec/pack observations remain; no new warm native/Actions run admitted by this delta",
        "without_new_consumer_home": "read-only metadata/version proof cannot establish warm materialization/Action; frozen historical homes cannot be mutated/reused without root and owner admission",
        "production_publication": "HOLD perroot; publication is not a prerequisite for isolated owner-qualified testing",
        "owners": ["Actions133", "Actions134", "Actions139", "Actions143", "PR221", "PR308"],
        "next_required": ["owner normally publishes immutable b614/source successor plus actual selectors/provenance predicate and negative source controls", "owner shares original config8c5/publication2212/failure-reviewd14 and failed supervisor/cleanup evidence with raw hashes", "owner independently reviews successor fixture/controller with bounded package-cache sharing and strict cleanup metadata while preserving original failed state", "owner states conditional once-each3→5→7276 admission and complete terminal/cleanup/capacity acceptance", "root confirms guarded capacity and quiet lease; no fresh320MB acquisition while only~23MiB abovefloor", "separate measured six-workload owner/controller fixtures; two-case wave cannot prove reuse/reload/drain/concurrency/recovery"],
    },
    "prior_frozen_reference": {"namespace": str(PRIOR), "hashes": prior_hashes, "old_source_blobs_refetched": False, "original_threecase_receipt_refetched": False},
    "resource_at_derivation": {"global_free_bytes": shutil.disk_usage(ROOT).free, "minimum_global_free_bytes": 2684354560, "new_runtime_processes": 0, "dependency_installs": 0},
}
write("source-bound.json", result)
write("derivation-receipt.json", {"numeric_derivation_exit_expected": 0, "timestamp": datetime.datetime.now(datetime.timezone.utc).isoformat(), "retained_fetch_records_verified": len(provenance), "owner_body_equal": True, "compare_five_files_and_ordered_parents_verified": True, "prior_four_receipt_hashes_checked": prior_hashes, "source_bound": digest(ROOT / "source-bound.json"), "runtime_experiments": 0})
print(json.dumps({"verified_fetch_records": len(provenance), "source_bound": digest(ROOT / "source-bound.json"), "runtime_experiments": 0}, sort_keys=True))
