import concurrent.futures
import datetime
import hashlib
import json
import stat
import urllib.request
import xml.etree.ElementTree as ET
import zipfile
from pathlib import Path

here = Path(__file__).resolve().parent
owner = here.parent / "actions-owner-refresh-1350" / "owner-evidence"
review = json.loads((owner / "pr308-current618-threecase-independent-OWNERFINAL-independent-current308-original-threecase-review.json").read_text())
archive = here / "actions308-owner-original-threecase.zip"
assert archive.stat().st_size == review["original_ZIP"]["bytes"]
assert hashlib.sha256(archive.read_bytes()).hexdigest() == review["original_ZIP"]["sha256"]
members = []
with zipfile.ZipFile(archive) as zipped:
    infos = zipped.infolist()
    assert len(infos) == 7 and len({i.filename for i in infos}) == 7
    assert sum(i.file_size for i in infos) == 20503
    for info in infos:
        path = Path(info.filename)
        assert not path.is_absolute() and ".." not in path.parts
        assert not stat.S_ISLNK(info.external_attr >> 16)
        payload = zipped.read(info)
        members.append({"path": info.filename, "bytes": len(payload), "sha256": hashlib.sha256(payload).hexdigest()})
    def get(name):
        return json.loads(zipped.read("rcc-provider-rollback-evidence/" + name))
    admission = get("acceptance-summary.json")
    lifecycle = get("lifecycle-receipt.json")
    publication = get("published-details-receipt.json")
    consumer = get("staged-consumer-receipt.json")
    junit = ET.fromstring(zipped.read("rcc-provider-rollback-junit.xml"))
    assert admission["source"]["candidate_sha"] == review["source_S"]["commit"]
    assert admission["source"]["workflow_control_sha"] == review["control_C"]["commit"]
    assert admission["test"]["exit_code"] == 0
    assert admission["admission"] == {"issues": [], "passed": True}
    assert admission["rcc"]["version"] == "v18.19.3"
    assert admission["rcc"]["sha256"] == "7e588c01751ca2ae15ba13ef67f2f4b7567697a5a8389737059a73936f509428"
    suite = junit.find("testsuite")
    assert {k: int(suite.attrib[k]) for k in ["tests", "failures", "errors", "skipped"]} == {"tests": 3, "failures": 0, "errors": 0, "skipped": 0}
    cases = suite.findall("testcase")
    assert len(cases) == 3 and not any(c.find("failure") is not None or c.find("error") is not None or c.find("skipped") is not None for c in cases)
    assert lifecycle["status"] == publication["status"] == consumer["status"] == "PASS"
    assert [lifecycle[k]["result"] for k in ["first_run", "second_run", "recovered_run"]] == ["last-good", "last-good", "recovered"]
    assert lifecycle["provider_ops_before_failure"] == lifecycle["provider_ops_after_failure"] == lifecycle["provider_ops_after_recovery"]
    assert len(lifecycle["provider_ops_before_failure"]) == 2
    cleanup = lifecycle["action_server_process_exit"]
    assert cleanup["shutdown_http_status"] == 200 and cleanup["returncode_before_forced_cleanup"] == 1 and cleanup["forced_stop_used"] is False
    assert cleanup["owned_descendants_before_stop"] == cleanup["same_owned_descendants_remaining_after_stop"] == []
    assert publication["publication"]["invocation_count"] == 1
    assert hashlib.sha256(publication["publication"]["raw_json"].encode()).hexdigest() == publication["publication"]["raw_json_sha256"]
    published = json.loads(publication["publication"]["raw_json"])
    assert published["artifactDigest"] == publication["publication"]["artifact_digest"]
    assert published["specificationDigest"] == publication["publication"]["specification_digest"]
    assert consumer["source_inventory"] == consumer["staged_inventory"]
    assert consumer["source_sha256"] == consumer["staged_sha256"]
    assert consumer["typed_action_result"]["core_version"] == "1.0.2"

def source_pin(pin):
    url = "https://raw.githubusercontent.com/joshyorko/actions/" + review["source_S"]["commit"] + "/" + pin["path"]
    with urllib.request.urlopen(urllib.request.Request(url, headers={"Accept-Encoding": "identity"}), timeout=45) as response:
        payload = response.read()
    assert len(payload) == pin["bytes"] and hashlib.sha256(payload).hexdigest() == pin["sha256"]
    git_blob = hashlib.sha1(b"blob " + str(len(payload)).encode() + b"\0" + payload).hexdigest()
    assert git_blob == pin["git_blob"]
    assert admission["source"]["files_sha256"][pin["name"]] == pin["sha256"]
    target = here / "source" / pin["path"]
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_bytes(payload)
    return {"path": pin["path"], "sha256": pin["sha256"], "git_blob": git_blob, "bytes": len(payload)}

with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
    pins = list(pool.map(source_pin, review["source_Git_pins_verified"]))
record = {
    "timestamp": datetime.datetime.now(datetime.timezone.utc).isoformat(),
    "verdict": "ROOT_ACTUAL_ORIGINAL_ZIP_AND_NINE_SOURCE_PINS_VERIFIED; EXACT_PUBLIC_THREE_CASE_RCC3_SCOPE_ONLY",
    "source": review["source_S"], "control": review["control_C"], "target": review["target"],
    "artifact_id": 11699213923, "actual_zip_sha256": review["original_ZIP"]["sha256"],
    "members": members, "source_pins": pins, "junit": {"tests": 3, "failures": 0, "errors": 0, "skipped": 0},
    "fixture_case_elapsed_seconds": {c.attrib["name"]: float(c.attrib["time"]) for c in cases},
    "elapsed_scope": "JUnit fixture durations include setup and cleanup; not cold-first Action/API/acquisition timers, CPU/RSS/I/O, comparative optimization evidence or a newly executed workload",
    "observations": "RCC3 exact binary, Core1.0.2 typed managed worker, last-good recovery with unchanged two provider operations, one actual publication, scoped natural return before cleanup",
    "no_new_runtime_or_actions_mutation": True,
    "remaining": "No candidate6 compatibility, current221 target/fullCI/controller/native/private134/positive offline-warm qualification or universal descendant-reaping proof. No performance gain inferred."
}
(here / "root-verification.json").write_text(json.dumps(record, indent=2) + "\n")
print(json.dumps({"verdict": record["verdict"], "members": len(members), "source_pins": len(pins), "junit": record["junit"], "fixture_durations": record["fixture_case_elapsed_seconds"]}))
