from pathlib import Path
import importlib.util, sys, os, json, hashlib, subprocess
root = Path(__file__).resolve().parent
source = root / "actions-source/action_server/src/actions/server/_rcc_runtime_adapter.py"
spec = importlib.util.spec_from_file_location("readonly_current_actions_rcc_adapter", source)
module = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = module
spec.loader.exec_module(module)
binaries = [
 ("hosted097", "/workspace/work/rcc-swarm/evidence/hosted-native/pr233-current-09796/native-runtime-binaries-extracted/rcc-linux64", "b5aa13f0921abd7c4b459c12b3042e58fecb8b7220756ad76ed7d651d74dbfd1"),
 ("released-n1-18.19.5", "/workspace/work/rcc-swarm/tools/rcc", "1a617ad7c736fa67c605e20e5ebe3c7d54b02cd548f733e05c809cf49a48db1e"),
 ("supported18.19.3-control", "/workspace/work/rcc-swarm/ownedtools/rcc-v18.19.3", "7e588c01751ca2ae15ba13ef67f2f4b7567697a5a8389737059a73936f509428"),
]
results = []
for label, name, expected_hash in binaries:
 path = Path(name)
 home = root / "root-version-probe-homes" / label
 home.mkdir(parents=True, exist_ok=True)
 os.environ["ROBOCORP_HOME"] = str(home)
 digest = hashlib.sha256(path.read_bytes()).hexdigest()
 if digest != expected_hash:
  raise RuntimeError("Binary identity mismatch " + label)
 direct = subprocess.run([str(path), "--version"], capture_output=True, text=True, check=False)
 record = {"label": label, "path": str(path), "binary_sha256": digest,
 "direct_version": {"exit_code": direct.returncode, "stdout": direct.stdout, "stderr": direct.stderr}}
 for call_name, call in [
  ("verify_rcc_version", lambda: module.verify_rcc_version(path)),
  ("get_rcc_location_override", lambda: module.get_rcc_location()),
 ]:
  os.environ["ACTIONS_RUNTIME_RCC_BINARY"] = str(path)
  try:
   value = call()
   record[call_name] = {"accepted": True, "value": str(value)}
  except module.RccRuntimeError as exc:
   record[call_name] = {"accepted": False, "error_type": type(exc).__name__, "phase": exc.phase, "error": str(exc)}
 results.append(record)
report = {"actions_commit": "a70993fafc99a9f94041485542d5a720797ca394", "adapter_source_sha256": hashlib.sha256(source.read_bytes()).hexdigest(), "adapter_required_version": module.RCC_VERSION, "performed": "Unmodified stdlib-only adapter import, actual --version verifier and override probes. No environment build/acquire/exec or performance measurement.", "results": results}
(root / "root-version-probe-results.json").write_text(json.dumps(report, indent=2)+"\n")
print(json.dumps(report, indent=2))
