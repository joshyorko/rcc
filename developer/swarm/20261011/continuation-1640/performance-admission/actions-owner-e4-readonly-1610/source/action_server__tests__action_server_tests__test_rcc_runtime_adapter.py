import json
import subprocess
import sys
import threading
import time
from pathlib import Path

import pytest


def test_artifact_digest_parser_accepts_only_exact_identity():
    from actions.server._rcc_runtime_adapter import (
        RccRuntimeError,
        parse_artifact_digest,
    )

    assert (
        parse_artifact_digest({"artifact": "sha256:" + "a" * 64})
        == "sha256:" + "a" * 64
    )
    assert (
        parse_artifact_digest({"artifact": {"digest": "sha256:" + "b" * 64}})
        == "sha256:" + "b" * 64
    )
    assert (
        parse_artifact_digest({"artifactDigest": "sha256:" + "c" * 64})
        == "sha256:" + "c" * 64
    )
    for payload in ({}, {"artifact": "not-a-digest"}, {"digest": "sha256:" + "a" * 63}):
        with pytest.raises(RccRuntimeError, match="artifact"):
            parse_artifact_digest(payload)


def test_publish_artifact_details_preserves_one_pinned_rcc_payload_and_provider():
    from actions.server._rcc_runtime_adapter import (
        RccPublishedArtifactDetails,
        publish_artifact_details,
    )

    specification_digest = "sha256:" + "a" * 64
    artifact_digest = "sha256:" + "b" * 64
    provider = "https://cache.example:8134/root"
    payload = {
        "artifactDigest": artifact_digest,
        "specificationDigest": specification_digest,
        "legacyBlueprintKey": "fixture-blueprint",
        "objectCount": 1,
        "uploadedBytes": 12,
        "reusedBytes": 34,
    }
    calls = []

    def runner(*args):
        calls.append(args)
        return 0, json.dumps(payload), ""

    details = publish_artifact_details(
        Path("/package/package.yaml"),
        Path("/opt/rcc"),
        provider=provider,
        runner=runner,
    )

    assert details == RccPublishedArtifactDetails(
        specification_digest=specification_digest,
        artifact_digest=artifact_digest,
    )
    assert calls == [
        (
            str(Path("/opt/rcc")),
            "env",
            "publish",
            "--environment",
            str(Path("/package/package.yaml")),
            "--json",
            "--provider",
            provider,
        )
    ]


@pytest.mark.parametrize(
    "updates",
    [
        {"specificationDigest": None},
        {"specificationDigest": "sha256:" + "a" * 63},
        {"artifactDigest": None},
        {"artifactDigest": "sha256:" + "b" * 63},
        {"artifact_digest": "not-a-digest"},
        {"digest": "sha256:" + "c" * 64},
        {"artifact": {"digest": "sha256:" + "c" * 64}},
        {"specification_digest": "sha256:" + "c" * 64},
    ],
)
def test_publish_artifact_details_rejects_missing_malformed_or_conflicting_identity(
    updates,
):
    from actions.server._rcc_runtime_adapter import (
        RccRuntimeError,
        publish_artifact_details,
    )

    payload = {
        "artifactDigest": "sha256:" + "a" * 64,
        "specificationDigest": "sha256:" + "b" * 64,
        **updates,
    }

    with pytest.raises(RccRuntimeError, match="publish"):
        publish_artifact_details(
            Path("/package/package.yaml"),
            Path("/opt/rcc"),
            runner=lambda *args: (0, json.dumps(payload), ""),
        )


@pytest.mark.parametrize(
    "payload",
    [
        {"artifact": "sha256:" + "a" * 64},
        {"artifact_digest": "sha256:" + "a" * 64},
        {"artifactDigest": "sha256:" + "a" * 64},
        {"digest": "sha256:" + "a" * 64},
        {"artifact": {"digest": "sha256:" + "a" * 64}},
        {"environment_artifact": {"digest": "sha256:" + "a" * 64}},
    ],
)
def test_legacy_publish_artifact_keeps_artifact_only_payload_compatibility(payload):
    from actions.server._rcc_runtime_adapter import publish_artifact

    calls = []

    def runner(*args):
        calls.append(args)
        return 0, json.dumps(payload), ""

    assert (
        publish_artifact(Path("/package/package.yaml"), Path("/opt/rcc"), runner=runner)
        == "sha256:" + "a" * 64
    )
    assert len(calls) == 1
    assert calls[0][1:3] == ("env", "publish")


def test_runtime_descriptor_has_no_activation_path_authority():
    from actions.server._rcc_runtime_adapter import RccRuntimeDescriptor

    descriptor = RccRuntimeDescriptor(
        artifact_digest="sha256:" + "a" * 64,
        source_generation="gen-1",
        source_hash="b" * 64,
    )
    serialized = json.loads(descriptor.to_json())
    assert serialized["runtime"]["kind"] == "rcc"
    assert serialized["runtime"]["artifact_digest"] == "sha256:" + "a" * 64
    for forbidden in (
        "PYTHON_EXE",
        "CONDA_PREFIX",
        "ROBOCORP_HOME",
        "holotree",
        "materialization",
    ):
        assert forbidden not in descriptor.to_json()


def test_import_command_uses_artifact_exec_not_python_exe(tmp_path):
    from actions.server._rcc_runtime_adapter import (
        RccRuntimeDescriptor,
        build_exec_command,
    )

    descriptor = RccRuntimeDescriptor(artifact_digest="sha256:" + "c" * 64)
    rcc_location = tmp_path / "rcc"
    command = build_exec_command(
        rcc_location,
        descriptor,
        ["python", "-c", "import actions"],
        receipt_file=None,
    )
    assert command[:5] == [
        str(rcc_location),
        "env",
        "exec",
        "--artifact",
        "sha256:" + "c" * 64,
    ]
    assert "PYTHON_EXE" not in command
    assert command[-4:] == ["--", "python", "-c", "import actions"]


def test_worker_command_has_inherit_streams_and_receipt(tmp_path):
    from actions.server._rcc_runtime_adapter import (
        RccRuntimeDescriptor,
        build_exec_command,
    )

    receipt = tmp_path / "receipt.json"
    descriptor = RccRuntimeDescriptor(artifact_digest="sha256:" + "d" * 64)
    command = build_exec_command(
        Path("/opt/rcc"),
        descriptor,
        ["python", "-m", "preload_actions_server_main"],
        receipt_file=receipt,
    )
    assert "--inherit-streams" in command
    assert "--receipt-file" in command
    assert str(receipt) in command
    assert command[command.index("--") + 1 :] == [
        "python",
        "-m",
        "preload_actions_server_main",
    ]


def test_provider_trust_reference_is_bound_to_exec_and_never_redacted(tmp_path):
    from actions.server._rcc_runtime_adapter import (
        RccRuntimeDescriptor,
        build_exec_command,
    )

    provider = "http://127.0.0.1:8134"
    descriptor = RccRuntimeDescriptor(
        artifact_digest="sha256:" + "d" * 64,
        provider_reference=provider,
    )
    serialized = descriptor.to_json()
    assert provider in serialized
    command = build_exec_command(
        Path("/opt/rcc"),
        descriptor,
        ["python", "-m", "preload_actions_server_main"],
        receipt_file=None,
    )
    assert command[command.index("--provider") + 1] == provider


@pytest.mark.parametrize("provider", ["local", "dakota-cache", "http://cache:8134"])
def test_supported_provider_reference_forms_are_preserved(tmp_path, provider):
    from actions.server._rcc_runtime_adapter import (
        RccRuntimeDescriptor,
        build_exec_command,
    )

    descriptor = RccRuntimeDescriptor(
        artifact_digest="sha256:" + "d" * 64,
        provider_reference=provider,
    )
    assert descriptor.provider_reference == provider
    command = build_exec_command(
        Path("/opt/rcc"), descriptor, ["python"], receipt_file=None
    )
    assert command[command.index("--provider") + 1] == provider


def test_legacy_descriptor_without_trust_binding_cannot_exec():
    from actions.server._rcc_runtime_adapter import (
        RccRuntimeDescriptor,
        RccRuntimeError,
        build_exec_command,
    )

    legacy = {
        "runtime": {
            "artifact_digest": "sha256:" + "d" * 64,
            "kind": "rcc",
            "contract_version": "rcc-runtime/v1",
        }
    }
    descriptor = RccRuntimeDescriptor.from_dict(legacy)
    with pytest.raises(RccRuntimeError, match="trust context"):
        build_exec_command(Path("/opt/rcc"), descriptor, ["python"], receipt_file=None)


@pytest.mark.parametrize(
    "provider",
    [
        "http://user:password@127.0.0.1:8134",
        "http://127.0.0.1:8134?token=secret",
        "http://127.0.0.1:8134#secret",
        "http://127.0.0.1:8134\n--other-arg",
        "//user:secret@provider",
        "provider?token=secret",
        "http:provider?token=secret",
        "invalid profile name",
    ],
)
def test_provider_reference_with_secret_or_control_data_is_rejected_before_rcc(
    tmp_path, provider
):
    from actions.server._rcc_runtime_adapter import RccRuntimeError, prepare_runtime

    package_yaml = tmp_path / "package.yaml"
    package_yaml.write_text("spec-version: v2\ndependencies: {python: '3.11'}\n")
    calls = []
    with pytest.raises(RccRuntimeError, match="provider") as error:
        prepare_runtime(
            package_yaml,
            Path("/opt/rcc"),
            provider=provider,
            runner=lambda *args: calls.append(args) or (1, "", "should not run"),
        )
    assert "secret" not in str(error.value)
    assert calls == []


def test_failed_publish_is_phase_error_without_fallback(tmp_path):
    from actions.server._rcc_runtime_adapter import RccRuntimeError, publish_artifact

    with pytest.raises(RccRuntimeError, match="publish"):
        publish_artifact(
            Path("/does/not/exist"),
            Path("/tmp/rcc"),
            runner=lambda *args: (1, "", "no"),
        )


def test_rcc_failure_keeps_meaningful_error_instead_of_generic_exit_line():
    from actions.server._rcc_runtime_adapter import RccRuntimeError, publish_artifact

    with pytest.raises(RccRuntimeError, match="provider refused trust metadata"):
        publish_artifact(
            Path("/package.yaml"),
            Path("/opt/rcc"),
            runner=lambda *args: (
                1,
                "",
                "Error: provider refused trust metadata\n"
                "[rcc] exit status will be: 1!\n",
            ),
        )


def test_acquire_rejects_missing_or_conflicting_identity():
    from actions.server._rcc_runtime_adapter import RccRuntimeError, acquire_artifact

    digest = "sha256:" + "e" * 64
    for result in ({}, {"artifactDigest": "sha256:" + "f" * 64}):
        with pytest.raises(RccRuntimeError, match="artifact"):
            acquire_artifact(
                digest,
                Path("/opt/rcc"),
                runner=lambda *args, result=result: (0, json.dumps(result), ""),
            )


def test_rcc_version_mismatch_fails_closed():
    from actions.server._rcc_runtime_adapter import RccRuntimeError, verify_rcc_version

    with pytest.raises(RccRuntimeError, match="unsupported RCC version"):
        verify_rcc_version(Path("/opt/rcc"), runner=lambda *args: (0, "v18.19.1\n", ""))


def test_source_only_reload_reuses_verified_artifact_without_republishing(tmp_path):
    from actions.server._rcc_runtime_adapter import prepare_runtime

    package_yaml = tmp_path / "package.yaml"
    package_yaml.write_text(
        "spec-version: v2\n" "dependencies:\n" "  conda-forge:\n" "    - python=3.11\n"
    )
    digest = "sha256:" + "a" * 64
    calls = []

    def runner(*args):
        calls.append(args)
        if args[2] == "publish":
            return 0, json.dumps({"artifact": digest}), ""
        return (
            0,
            json.dumps({"artifactDigest": digest, "verification": {"valid": True}}),
            "",
        )

    first = prepare_runtime(
        package_yaml,
        Path("/opt/rcc"),
        source_generation="source-1",
        provider="http://127.0.0.1:8134",
        runner=runner,
    )
    second = prepare_runtime(
        package_yaml,
        Path("/opt/rcc"),
        source_generation="source-2",
        provider="http://127.0.0.1:8134",
        previous_descriptor=first,
        runner=runner,
    )

    assert [call[2] for call in calls] == ["publish", "acquire"]
    assert first.artifact_digest == second.artifact_digest == digest
    assert second.source_generation == "source-2"
    assert second.provider_reference == "http://127.0.0.1:8134"


def test_snapshot_environment_input_preserves_original_cache_identity(tmp_path):
    from actions.server._rcc_runtime_adapter import prepare_runtime

    original = tmp_path / "package" / "package.yaml"
    original.parent.mkdir()
    original.write_text("spec-version: v2\ndependencies: {python: '3.12'}\n")
    snapshot_one = tmp_path / "data" / "snapshot-one" / "package.yaml"
    snapshot_two = tmp_path / "data" / "snapshot-two" / "package.yaml"
    snapshot_one.parent.mkdir(parents=True)
    snapshot_two.parent.mkdir(parents=True)
    snapshot_one.write_text(
        "spec-version: v2\ndependencies: {python: '3.12'}\nname: first\n"
    )
    snapshot_two.write_text(
        "spec-version: v2\ndependencies: {python: '3.12'}\nname: second\n"
    )
    digest = "sha256:" + "a" * 64
    calls = []

    def runner(*args):
        calls.append(args)
        if args[2] == "publish":
            return 0, json.dumps({"artifact": digest}), ""
        return (
            0,
            json.dumps({"artifactDigest": digest, "verification": {"valid": True}}),
            "",
        )

    first = prepare_runtime(
        snapshot_one,
        Path("/opt/rcc"),
        environment_identity=original,
        source_generation="source-one",
        provider="http://127.0.0.1:8134",
        runner=runner,
    )
    second = prepare_runtime(
        snapshot_two,
        Path("/opt/rcc"),
        environment_identity=original,
        source_generation="source-two",
        provider="http://127.0.0.1:8134",
        previous_descriptor=first,
        runner=runner,
    )

    assert [call[2] for call in calls] == ["publish", "acquire"]
    assert calls[0][calls[0].index("--environment") + 1] == str(snapshot_one)
    assert first.artifact_digest == second.artifact_digest == digest
    assert second.source_generation == "source-two"
    assert second.preparation_class == "source-reuse"


def test_cached_artifact_is_revalidated_and_rebuilt_when_materialization_disappears(
    tmp_path,
):
    from actions.server._rcc_runtime_adapter import prepare_runtime

    package_yaml = tmp_path / "package.yaml"
    package_yaml.write_text("spec-version: v2\ndependencies: {python: '3.11'}\n")
    first_digest = "sha256:" + "a" * 64
    replacement_digest = "sha256:" + "b" * 64
    calls = []
    missing = False

    def runner(*args):
        nonlocal missing
        calls.append(args)
        if args[2] == "publish":
            return (
                0,
                json.dumps(
                    {"artifact": replacement_digest if missing else first_digest}
                ),
                "",
            )
        if missing and args[4] == first_digest:
            return 1, "", "artifact is not materialized"
        digest = replacement_digest if missing else first_digest
        return (
            0,
            json.dumps({"artifactDigest": digest, "verification": {"valid": True}}),
            "",
        )

    prepare_runtime(package_yaml, Path("/opt/rcc"), runner=runner)
    missing = True
    descriptor = prepare_runtime(package_yaml, Path("/opt/rcc"), runner=runner)

    assert descriptor.artifact_digest == replacement_digest
    assert [call[2] for call in calls] == [
        "publish",
        "acquire",
        "acquire",
        "publish",
        "acquire",
    ]


def test_non_materialization_acquire_failure_does_not_replace_pinned_identity(
    tmp_path,
):
    from actions.server._rcc_runtime_adapter import (
        RccRuntimeDescriptor,
        RccRuntimeError,
        environment_spec_fingerprint,
        prepare_runtime,
    )

    package_yaml = tmp_path / "package.yaml"
    package_yaml.write_text("spec-version: v2\ndependencies: {python: '3.11'}\n")
    digest = "sha256:" + "c" * 64
    calls = []
    previous = RccRuntimeDescriptor(
        artifact_digest=digest,
        environment_fingerprint=environment_spec_fingerprint(package_yaml),
    )

    def runner(*args):
        calls.append(args)
        return 1, "", "permission denied by provider"

    with pytest.raises(RccRuntimeError, match="acquire"):
        prepare_runtime(
            package_yaml,
            Path("/opt/rcc"),
            previous_descriptor=previous,
            runner=runner,
        )

    assert [call[2] for call in calls] == ["acquire"]


def test_environment_change_does_not_reuse_cached_artifact(tmp_path):
    from actions.server._rcc_runtime_adapter import prepare_runtime

    package_yaml = tmp_path / "package.yaml"
    package_yaml.write_text("spec-version: v2\ndependencies: {python: '3.11'}\n")
    calls = []
    digests = iter(("sha256:" + "a" * 64, "sha256:" + "b" * 64))

    def runner(*args):
        calls.append(args)
        if args[2] == "publish":
            return 0, json.dumps({"artifact": next(digests)}), ""
        digest = args[4]
        return (
            0,
            json.dumps({"artifactDigest": digest, "verification": {"valid": True}}),
            "",
        )

    first = prepare_runtime(package_yaml, Path("/opt/rcc"), runner=runner)
    package_yaml.write_text("spec-version: v2\ndependencies: {python: '3.12'}\n")
    second = prepare_runtime(package_yaml, Path("/opt/rcc"), runner=runner)

    assert len(calls) == 4
    assert first.artifact_digest != second.artifact_digest


def test_environment_fingerprint_classifies_pythonpath_as_source_change(tmp_path):
    from actions.server._rcc_runtime_adapter import classify_environment_change

    before = tmp_path / "before.yaml"
    after = tmp_path / "after.yaml"
    before.write_text(
        "spec-version: v2\ndependencies: {python: '3.11'}\npythonpath: [src]\n"
    )
    after.write_text(
        "spec-version: v2\ndependencies: {python: '3.11'}\npythonpath: [src, tests]\n"
    )

    assert classify_environment_change(before, after) == "source"


def test_restart_reacquires_existing_artifact_without_republishing(tmp_path):
    from actions.server._rcc_runtime_adapter import (
        RccRuntimeDescriptor,
        environment_spec_fingerprint,
        prepare_runtime,
    )

    package_yaml = tmp_path / "package.yaml"
    package_yaml.write_text("spec-version: v2\ndependencies: {python: '3.11'}\n")
    digest = "sha256:" + "c" * 64
    calls = []

    def runner(*args):
        calls.append(args)
        return (
            0,
            json.dumps({"artifactDigest": digest, "verification": {"valid": True}}),
            "",
        )

    previous = RccRuntimeDescriptor(
        artifact_digest=digest,
        environment_fingerprint=environment_spec_fingerprint(package_yaml),
    )
    prepare_runtime(
        package_yaml,
        Path("/opt/rcc"),
        previous_descriptor=previous,
        runner=runner,
    )

    assert [call[2] for call in calls] == ["acquire"]


def test_legacy_descriptor_is_reprepared_with_current_provider(tmp_path):
    from actions.server._rcc_runtime_adapter import (
        RccRuntimeDescriptor,
        environment_spec_fingerprint,
        prepare_runtime,
    )

    package_yaml = tmp_path / "package.yaml"
    package_yaml.write_text("spec-version: v2\ndependencies: {python: '3.11'}\n")
    digest = "sha256:" + "c" * 64
    calls = []

    def runner(*args):
        calls.append(args)
        return (
            0,
            json.dumps({"artifactDigest": digest, "verification": {"valid": True}}),
            "",
        )

    legacy = RccRuntimeDescriptor.from_dict(
        {
            "runtime": {
                "artifact_digest": digest,
                "environment_fingerprint": environment_spec_fingerprint(package_yaml),
                "kind": "rcc",
                "contract_version": "rcc-runtime/v1",
            }
        }
    )
    assert legacy.provider_context_bound is False
    prepared = prepare_runtime(
        package_yaml,
        Path("/opt/rcc"),
        provider="http://127.0.0.1:8134",
        previous_descriptor=legacy,
        runner=runner,
    )

    assert [call[2] for call in calls] == ["acquire"]
    assert "--provider" in calls[0]
    assert calls[0][calls[0].index("--provider") + 1] == "http://127.0.0.1:8134"
    assert prepared.provider_reference == "http://127.0.0.1:8134"
    assert prepared.provider_context_bound is True


def test_missing_persisted_artifact_republishes_and_acquires_new_identity(tmp_path):
    from actions.server._rcc_runtime_adapter import (
        RccRuntimeDescriptor,
        environment_spec_fingerprint,
        prepare_runtime,
    )

    package_yaml = tmp_path / "package.yaml"
    package_yaml.write_text("spec-version: v2\ndependencies: {python: '3.11'}\n")
    old_digest = "sha256:" + "a" * 64
    new_digest = "sha256:" + "b" * 64
    calls = []

    def runner(*args):
        calls.append(args)
        if args[2] == "acquire" and args[4] == old_digest:
            return 1, "", "artifact is not materialized"
        if args[2] == "publish":
            return 0, json.dumps({"artifact": new_digest}), ""
        return (
            0,
            json.dumps({"artifactDigest": new_digest, "verification": {"valid": True}}),
            "",
        )

    previous = RccRuntimeDescriptor(
        artifact_digest=old_digest,
        environment_fingerprint=environment_spec_fingerprint(package_yaml),
    )
    descriptor = prepare_runtime(
        package_yaml,
        Path("/opt/rcc"),
        previous_descriptor=previous,
        runner=runner,
    )

    assert descriptor.artifact_digest == new_digest
    assert [call[2] for call in calls] == ["acquire", "publish", "acquire"]


def test_acquire_rejects_invalid_artifact_verification():
    from actions.server._rcc_runtime_adapter import RccRuntimeError, acquire_artifact

    digest = "sha256:" + "d" * 64
    with pytest.raises(RccRuntimeError, match="verification"):
        acquire_artifact(
            digest,
            Path("/opt/rcc"),
            runner=lambda *args: (
                0,
                json.dumps(
                    {
                        "artifactDigest": digest,
                        "verification": {"valid": False},
                    }
                ),
                "",
            ),
        )


def test_acquire_rejects_missing_artifact_verification():
    from actions.server._rcc_runtime_adapter import RccRuntimeError, acquire_artifact

    digest = "sha256:" + "e" * 64
    with pytest.raises(RccRuntimeError, match="verification"):
        acquire_artifact(
            digest,
            Path("/opt/rcc"),
            runner=lambda *args: (
                0,
                json.dumps({"artifactDigest": digest}),
                "",
            ),
        )


def test_reload_marks_running_generation_non_reusable(monkeypatch):
    import sys
    from types import SimpleNamespace

    monkeypatch.setitem(
        sys.modules, "termcolor", SimpleNamespace(colored=lambda value, **kwargs: value)
    )
    from actions.server import _actions_process_pool as process_pool

    class OldProcess:
        can_reuse = True

    old_process = OldProcess()
    pool = process_pool.ActionsProcessPool.__new__(process_pool.ActionsProcessPool)
    pool._lock = process_pool.threading.Lock()
    pool._idle_processes = {}
    pool._running_processes = {"old": {old_process}}
    pool._warmup_processes_unlocked = lambda **kwargs: None
    pool.action_package_id_to_action_package = {"old": "old-package"}
    pool.actions = ["old-action"]
    pool._cycle_actions_iterator = iter(pool.actions)
    new_action = type("Action", (), {"enabled": True, "name": "new-action"})()
    pool.on_reload({"new": "new-package"}, [new_action])

    assert old_process.can_reuse is False
    assert pool.action_package_id_to_action_package == {"new": "new-package"}
    assert pool.actions == [new_action]


def test_reload_rolls_back_routing_when_new_generation_warmup_fails(monkeypatch):
    import sys
    from types import SimpleNamespace

    monkeypatch.setitem(
        sys.modules, "termcolor", SimpleNamespace(colored=lambda value, **kwargs: value)
    )
    from actions.server import _actions_process_pool as process_pool

    class OldProcess:
        can_reuse = True

    old_process = OldProcess()
    old_map = {"old": "old-package"}
    old_action = type("Action", (), {"enabled": True, "name": "old-action"})()
    pool = process_pool.ActionsProcessPool.__new__(process_pool.ActionsProcessPool)
    pool._lock = process_pool.threading.Lock()
    pool._idle_processes = {}
    pool._running_processes = {"old": {old_process}}
    pool.action_package_id_to_action_package = old_map
    pool.actions = [old_action]
    pool._cycle_actions_iterator = iter([old_action])

    def fail_warmup(**kwargs):
        raise RuntimeError("new generation failed")

    pool._warmup_processes_unlocked = fail_warmup
    new_action = type("Action", (), {"enabled": True, "name": "new-action"})()

    with pytest.raises(RuntimeError, match="new generation failed"):
        pool.on_reload({"new": "new-package"}, [new_action])

    assert pool.action_package_id_to_action_package is old_map
    assert pool.actions == [old_action]
    assert old_process.can_reuse is True


def test_route_and_pool_reload_commits_after_inflight_old_call(monkeypatch):
    from threading import Event, Thread
    from types import SimpleNamespace

    from actions.server import _server

    class FakeApp:
        def __init__(self):
            self.router = SimpleNamespace(routes=["old-route"])

    app = FakeApp()
    monkeypatch.setattr("actions.server._app.get_app", lambda: app)
    events = []
    old_call_started = Event()
    release_old_call = Event()

    class FakeRoutes:
        def __init__(self):
            self.action_package_id_to_action_package = {"old": "old-package"}
            self.actions = ["old-action"]
            self.registered_route_names = {"old-route"}
            self.mcp_server_setup_helper = SimpleNamespace(_catalog=["old"])

        def prepare_actions(self):
            events.append("routes-prepare")
            return ["new-route"]

        def publish_prepared_actions(self, prepared):
            events.append("routes-register")

    class FakePool:
        def on_reload(self, packages, actions):
            events.append("pool-prepare")

    def old_call():
        old_call_started.set()
        release_old_call.wait(timeout=2)
        events.append("old-call-complete")

    old_call_thread = Thread(target=old_call)
    old_call_thread.start()
    assert old_call_started.wait(timeout=2)

    routes = FakeRoutes()
    _server._reload_action_generation(
        routes, FakePool(), ["new-action"], {"new": "new"}
    )
    release_old_call.set()
    old_call_thread.join(timeout=2)

    assert events[:3] == ["routes-prepare", "pool-prepare", "routes-register"]
    assert events[-1] == "old-call-complete"
    assert app.router.routes == ["old-route"]


def test_admitted_route_pins_process_lookup_to_old_generation(monkeypatch):
    import asyncio

    asyncio.run(_test_admitted_route_pins_process_lookup_to_old_generation(monkeypatch))


def test_scheduler_admission_pins_pool_and_package_across_reload(monkeypatch, tmp_path):
    """A scheduled run keeps its admitted pool and package during reload."""
    import asyncio
    import threading
    from contextlib import nullcontext
    from types import SimpleNamespace

    from actions.server import (
        _actions_process_pool,
        _actions_run,
        _artifact_storage,
        _models,
        _runs_state_cache,
        _settings,
    )
    from actions.server._robo_utils import run_in_thread

    class RuntimeInfo:
        class OnCancel:
            def register(self, _callback):
                return nullcontext()

        on_cancel = OnCancel()

        def is_canceled(self):
            return False

    class Handle:
        pid = 123

        def __enter__(self):
            return self

        def __exit__(self, *_args):
            return False

        def run_action(
            self,
            _run,
            _package,
            _action,
            _input,
            _artifacts,
            _output,
            result_json,
            *_args,
        ):
            result_json.write_text('{"result": "old-generation"}')
            return 0

    class Pool:
        def __init__(self, generation):
            self.generation = generation
            self.calls = []

        def obtain_process_for_action(
            self, action, runtime_info, *, generation, action_package
        ):
            self.calls.append((action, runtime_info, generation, action_package))
            return Handle()

    class DB:
        def connect(self):
            return nullcontext()

        def first(self, *_args):
            return run

    class ArtifactStorage:
        def run_artifacts_dir(self, _relative):
            return tmp_path

    class RunsState:
        def create_run_runtime_info(self, _run_id):
            return RuntimeInfo()

    old_pool = Pool(4)
    new_pool = Pool(5)
    current_pool = old_pool
    worker_started = threading.Event()
    allow_worker = threading.Event()

    def dispatch(func):
        result = {}

        def worker():
            worker_started.set()
            allow_worker.wait(timeout=5)
            try:
                result["value"] = func()
            except BaseException as exc:  # pragma: no cover - surfaced by result()
                result["error"] = exc

        thread = threading.Thread(target=worker)
        thread.start()

        class Future:
            def result(self):
                thread.join(timeout=5)
                if "error" in result:
                    raise result["error"]
                return result["value"]

        return Future()

    def get_pool():
        return current_pool

    run = SimpleNamespace()
    action = SimpleNamespace(name="scheduled", id="action-id")
    old_package = SimpleNamespace(id="old-package")
    result = {}

    monkeypatch.setattr(
        _settings,
        "get_settings",
        lambda: SimpleNamespace(datadir=tmp_path, reuse_processes=False),
    )
    monkeypatch.setattr(_runs_state_cache, "get_global_runs_state", lambda: RunsState())
    monkeypatch.setattr(
        _artifact_storage, "get_artifact_storage", lambda: ArtifactStorage()
    )
    monkeypatch.setattr(_actions_process_pool, "get_actions_process_pool", get_pool)
    monkeypatch.setattr(_models, "get_db", lambda: DB())
    monkeypatch.setattr(_actions_run, "_set_run_as_running", lambda *_args: None)
    monkeypatch.setattr(_actions_run, "_set_run_as_finished_ok", lambda *_args: None)
    monkeypatch.setattr(
        _actions_run, "_set_run_as_finished_failed", lambda *_args: None
    )
    monkeypatch.setattr(run_in_thread, "run_in_thread", dispatch)

    async def run_scheduled():
        return await _actions_run.execute_action_for_scheduler(
            action, old_package, "run-id", {}, "artifacts"
        )

    task = threading.Thread(
        target=lambda: result.update(value=asyncio.run(run_scheduled()))
    )
    task.start()
    assert worker_started.wait(timeout=5)
    current_pool = new_pool
    allow_worker.set()
    task.join(timeout=5)

    assert result["value"] == (True, "old-generation")
    assert len(old_pool.calls) == 1
    assert old_pool.calls[0][2:] == (4, old_package)
    assert not new_pool.calls


async def _test_admitted_route_pins_process_lookup_to_old_generation(monkeypatch):
    """A request admitted before reload must retain its route generation."""
    import asyncio
    from threading import Event, Lock, Semaphore
    from types import SimpleNamespace

    from fastapi import Response
    from starlette.requests import Request

    from actions.server import _actions_process_pool as process_pool
    from actions.server import _actions_run

    started = Event()
    release = Event()
    lookups = []

    class FakeProcess:
        def __init__(self, _settings, action_package, _post_run_args):
            self.action_package = action_package
            self.key = action_package.id
            self.can_reuse = True
            self.pid = 123
            self.killed = False

        def is_alive(self):
            return not self.killed

        def kill(self):
            self.killed = True

        def retire(self):
            self.killed = True
            return True

    monkeypatch.setattr(process_pool, "ProcessHandle", FakeProcess)
    monkeypatch.setattr(
        process_pool, "_get_process_handle_key", lambda _settings, package: package.id
    )
    pool = process_pool.ActionsProcessPool.__new__(process_pool.ActionsProcessPool)
    pool._settings = SimpleNamespace(
        max_processes=1, min_processes=0, reuse_processes=True
    )
    pool._generation = 2
    pool._lock = Lock()
    pool._processes_running_semaphore = Semaphore(1)
    pool._running_processes = {}
    pool._idle_processes = {}
    pool._pending_retirements = {}
    pool._post_run_cmd_args = None
    pool.action_package_id_to_action_package = {
        "new-package": SimpleNamespace(id="new-package")
    }
    pool.actions = []
    pool._cycle_actions_iterator = iter(())
    monkeypatch.setattr(process_pool, "get_actions_process_pool", lambda: pool)

    class FakeRunner:
        def __init__(self, *args, **kwargs):
            self.action_package = args[0]
            self.action = args[1]
            self.process_pool_generation = kwargs["process_pool_generation"]

        def run_in_thread(self):
            started.set()
            assert release.wait(timeout=2)
            with pool.obtain_process_for_action(
                self.action,
                generation=self.process_pool_generation,
                action_package=self.action_package,
            ) as process:
                lookups.append((self.process_pool_generation, process.action_package))
                return "old-result"

    monkeypatch.setattr(_actions_run, "_ActionsRunner", FakeRunner)
    package = SimpleNamespace(id="old-package")
    action = SimpleNamespace(
        action_package_id="old-package",
        input_schema=json.dumps({"type": "object"}),
        output_schema=json.dumps({"type": "string"}),
    )
    fast_api, _internal, _ = _actions_run.generate_func_from_action(
        package, action, "Old Action", process_pool_generation=1
    )

    request_messages = iter(
        [{"type": "http.request", "body": b"{}", "more_body": False}]
    )

    async def receive():
        return next(request_messages)

    request = Request(
        {
            "type": "http",
            "method": "POST",
            "path": "/old",
            "headers": [],
            "query_string": b"",
            "scheme": "http",
            "server": ("127.0.0.1", 80),
            "client": ("127.0.0.1", 1),
        },
        receive=receive,
    )
    task = asyncio.create_task(fast_api(Response(), request))
    await asyncio.to_thread(started.wait, 2)
    # The pool has already advanced, but this admitted request still carries
    # generation 1 and its old package into the process lookup.
    release.set()
    assert await task == "old-result"
    assert lookups == [(1, package)]


def test_route_registration_failure_restores_routes_and_pool_generation(monkeypatch):
    from types import SimpleNamespace

    from actions.server import _server

    class FakeApp:
        def __init__(self):
            self.router = SimpleNamespace(routes=["old-route"])

    app = FakeApp()
    monkeypatch.setattr("actions.server._app.get_app", lambda: app)

    class FakeRoutes:
        def __init__(self):
            self.action_package_id_to_action_package = {"old": "old-package"}
            self.actions = ["old-action"]
            self.registered_route_names = {"old-route"}
            self.mcp_server_setup_helper = SimpleNamespace(_catalog=["old"])

        def prepare_actions(self):
            raise RuntimeError("route preparation failed")

    class FakePool:
        def __init__(self):
            self.reloads = []
            self.generation = 4

        def on_reload(self, packages, actions):
            self.reloads.append((packages, actions))
            self.generation += 1

        def restore_generation(self, generation):
            self.generation = generation

    routes = FakeRoutes()
    pool = FakePool()
    with pytest.raises(RuntimeError, match="route preparation failed"):
        _server._reload_action_generation(
            routes, pool, ["new-action"], {"new": "new-package"}
        )

    assert app.router.routes == ["old-route"]
    assert pool.reloads == []
    assert pool.generation == 4


def test_receipt_requires_identity_verification_and_lease(tmp_path):
    from actions.server._rcc_runtime_adapter import RccRuntimeError, read_receipt

    digest = "sha256:" + "1" * 64
    receipt = tmp_path / "receipt.json"
    receipt.write_text(
        json.dumps(
            {
                "artifactDigest": digest,
                "verification": {"valid": True},
                "leaseId": "lease-1",
            }
        )
    )
    assert read_receipt(receipt, digest)["leaseId"] == "lease-1"
    receipt.write_text(
        json.dumps(
            {
                "artifactDigest": digest,
                "verification": {"valid": False},
                "leaseId": "lease-1",
            }
        )
    )
    with pytest.raises(RccRuntimeError, match="verification"):
        read_receipt(receipt, digest)


def test_spec_v2_without_provider_preserves_legacy_bootstrap(monkeypatch, tmp_path):
    from actions.server._action_package_handler import ActionPackageHandler
    from actions.server._protocols import ActionResult
    from actions.server._rcc import EnvInfo

    monkeypatch.delenv("ACTIONS_RUNTIME_RCC_PROVIDER", raising=False)
    monkeypatch.delenv("ACTIONS_REAL_RCC_ARTIFACT_TEST", raising=False)
    package_dir = tmp_path / "package"
    package_dir.mkdir()
    (package_dir / "package.yaml").write_text(
        "version: 0.1\nspec-version: v2\ndependencies: {}\n"
    )

    class LegacyRcc:
        def get_package_yaml_hash(self, package_yaml, devenv):
            return "legacy-hash"

        def create_env_and_get_vars(self, datadir, package_yaml, package_hash, devenv):
            return ActionResult(True, None, EnvInfo({"PYTHON_EXE": "/legacy/python"}))

    monkeypatch.setattr("actions.server._rcc.get_rcc", lambda: LegacyRcc())
    monkeypatch.setattr(
        "actions.server._rcc_runtime_adapter.prepare_runtime",
        lambda *args, **kwargs: pytest.fail(
            "RCC artifact mode was selected without an opt-in"
        ),
    )
    assert ActionPackageHandler(
        str(package_dir), tmp_path / "data"
    ).bootstrap_environment() == (
        "legacy-hash",
        {
            "PYTHON_EXE": "/legacy/python",
            "PYTHONPATH": str(package_dir),
        },
    )


def test_process_handle_kill_waits_for_wrapper(tmp_path):
    from actions.server._rcc_runtime_adapter import RccProcessHandle

    process = subprocess.Popen(["sh", "-c", "sleep 30"])
    handle = RccProcessHandle(process, tmp_path / "receipt.json")
    handle.kill()
    assert process.poll() is not None


def test_force_killing_wrapper_preserves_failed_receipt(monkeypatch, tmp_path):
    from actions.server._rcc_runtime_adapter import RccProcessHandle

    receipt = tmp_path / "receipt.json"
    failed_receipt = {
        "status": "failed",
        "exitCode": -1,
        "reason": "child exited non-zero",
    }
    receipt.write_text(json.dumps(failed_receipt))
    original_receipt = receipt.read_bytes()

    class Process:
        pid = 123
        returncode = None

        def poll(self):
            return self.returncode

        def wait(self):
            self.returncode = -9
            return self.returncode

    monkeypatch.setattr(
        "actions.server._common.process.kill_process_and_subprocesses",
        lambda _pid: None,
    )
    handle = RccProcessHandle(Process(), receipt)

    handle.kill()

    assert receipt.read_bytes() == original_receipt
    assert json.loads(receipt.read_text()) == failed_receipt


def test_bounded_force_kill_stops_owned_wrapper_and_child(tmp_path):
    import psutil

    from actions.server._rcc_runtime_adapter import RccProcessHandle

    wrapper = subprocess.Popen(
        [
            sys.executable,
            "-c",
            "import signal, subprocess, sys, time\n"
            "child = subprocess.Popen([sys.executable, '-c', 'import time; time.sleep(30)'])\n"
            "def stop(*_):\n"
            "    if child.poll() is None:\n"
            "        child.terminate()\n"
            "    child.wait()\n"
            "    raise SystemExit(0)\n"
            "signal.signal(signal.SIGTERM, stop)\n"
            "print(child.pid, flush=True)\n"
            "time.sleep(30)\n",
        ],
        stdout=subprocess.PIPE,
        text=True,
    )
    assert wrapper.stdout is not None
    child_pid = int(wrapper.stdout.readline())
    handle = RccProcessHandle(wrapper, tmp_path / "receipt.json")
    try:
        result = handle.force_kill_until(time.monotonic() + 2)
        assert result.execution_stopped
        assert wrapper.poll() is not None
        assert result.wrapper_reaped
        assert result.descendant_reap_complete
        assert child_pid not in result.live_descendant_pids
        assert child_pid not in result.zombie_descendant_pids
    finally:
        if wrapper.poll() is None:
            wrapper.terminate()
            wrapper.wait(timeout=2)
        if psutil.pid_exists(child_pid):
            child = psutil.Process(child_pid)
            if child.status() != psutil.STATUS_ZOMBIE:
                child.terminate()
            _wait_until(lambda: not psutil.pid_exists(child_pid), timeout=2)
        wrapper.stdout.close()


@pytest.mark.skipif(sys.platform == "win32", reason="requires POSIX SIGTERM semantics")
def test_bounded_force_kill_terminates_worker_ignoring_term(tmp_path):
    from actions.server._rcc_runtime_adapter import RccProcessHandle

    wrapper = subprocess.Popen(
        [
            sys.executable,
            "-c",
            "import signal, time\n"
            "signal.signal(signal.SIGTERM, signal.SIG_IGN)\n"
            "print('ready', flush=True)\n"
            "time.sleep(30)\n",
        ],
        stdout=subprocess.PIPE,
        text=True,
    )
    assert wrapper.stdout is not None
    assert wrapper.stdout.readline().strip() == "ready"
    handle = RccProcessHandle(wrapper, tmp_path / "receipt.json")
    try:
        handle.capture_owned_processes()
        result = handle.force_kill_until(time.monotonic() + 2)
        assert result.execution_stopped
        assert result.wrapper_reaped
        assert wrapper.returncode == -9
    finally:
        if wrapper.poll() is None:
            wrapper.kill()
            wrapper.wait(timeout=2)
        wrapper.stdout.close()


def test_zombie_descendants_are_reported_without_claiming_reap():
    from actions.server._common.process import ProcessTreeCleanupResult

    result = ProcessTreeCleanupResult(
        wrapper_reaped=True,
        live_descendant_pids=(),
        zombie_descendant_pids=(12345,),
        errors=(),
    )

    assert result.execution_stopped
    assert result.zombie_descendant_pids == (12345,)
    assert not result.descendant_reap_complete


def test_descendant_snapshot_does_not_treat_missing_owner_as_empty_tree(monkeypatch):
    import psutil

    from actions.server._common.process import snapshot_process_descendants

    class MissingOwner:
        def __init__(self, _pid):
            pass

        def children(self, recursive):
            raise psutil.NoSuchProcess(123)

    monkeypatch.setattr(psutil, "Process", MissingOwner)
    with pytest.raises(psutil.NoSuchProcess):
        snapshot_process_descendants(123)


def test_pending_retirement_keeps_capacity_until_retry_completes(monkeypatch):
    from actions.server._actions_process_pool import ActionsProcessPool

    pool = ActionsProcessPool.__new__(ActionsProcessPool)
    pool._lock = threading.Lock()
    pool._pending_retirements = {}
    pool._running_processes = {}
    pool._idle_processes = {}
    pool._processes_running_semaphore = threading.BoundedSemaphore(1)
    warmups = []

    def warmup_after_removal():
        assert handle not in pool._pending_retirements
        with pool._lock:
            assert pool._count_total_processes() == 0
        warmups.append(True)

    monkeypatch.setattr(pool, "_warmup_processes", warmup_after_removal)
    assert pool._processes_running_semaphore.acquire(blocking=False)

    class Handle:
        def __init__(self):
            self.results = iter((False, True))

        def retire(self):
            assert not pool._lock.locked(), "retirement ran while pool lock was held"
            return next(self.results)

    handle = Handle()
    pool._pending_retirements[handle] = True

    pool._retry_pending_retirements()
    assert handle in pool._pending_retirements
    assert not pool._processes_running_semaphore.acquire(blocking=False)

    pool._retry_pending_retirements()
    assert handle not in pool._pending_retirements
    assert pool._processes_running_semaphore.acquire(blocking=False)
    assert warmups == [True]
    assert not pool._processes_running_semaphore.acquire(blocking=False)


@pytest.mark.parametrize(
    ("now", "deadline", "expected_timeout"),
    [
        (10.25, 10.5, 0.25),  # elapsed time is subtracted from the caller's budget
        (10.5, 10.0, 0.0),  # an expired deadline never becomes an unbounded wait
    ],
)
def test_retirement_lock_wait_uses_remaining_caller_budget(
    monkeypatch, now, deadline, expected_timeout
):
    from actions.server._actions_process_pool import ProcessHandle

    clock = {"now": now}
    monkeypatch.setattr(time, "monotonic", lambda: clock["now"])

    class RecordingLock:
        def __init__(self):
            self.timeouts = []
            self.release_calls = 0

        def acquire(self, *, timeout):
            self.timeouts.append(timeout)
            return False

        def release(self):
            self.release_calls += 1

    handle = ProcessHandle.__new__(ProcessHandle)
    lock = RecordingLock()
    handle._retirement_lock = lock
    handle._retire_locked = lambda unused_deadline: pytest.fail(
        "retirement critical section must not run when lock acquisition fails"
    )

    result = handle.retire(deadline)

    assert result.state == "pending"
    assert result.reason == "retirement lock deadline expired"
    assert lock.timeouts == [expected_timeout]
    assert lock.release_calls == 0


def test_retirement_lock_wait_does_not_turn_scheduler_delay_into_failure(monkeypatch):
    from actions.server._actions_process_pool import ProcessHandle

    clock = {"now": 20.0}
    monkeypatch.setattr(time, "monotonic", lambda: clock["now"])

    class SchedulerDelayedLock:
        def __init__(self):
            self.timeouts = []
            self.release_calls = 0

        def acquire(self, *, timeout):
            self.timeouts.append(timeout)
            clock["now"] += 0.30
            return False

        def release(self):
            self.release_calls += 1

    handle = ProcessHandle.__new__(ProcessHandle)
    lock = SchedulerDelayedLock()
    handle._retirement_lock = lock
    handle._retire_locked = lambda unused_deadline: pytest.fail(
        "retirement critical section must not run when lock acquisition fails"
    )

    result = handle.retire(20.03)

    assert clock["now"] - 20.0 >= 0.25  # old wall-clock assertion would fail here
    assert result.state == "pending"
    assert result.reason == "retirement lock deadline expired"
    assert lock.timeouts == [pytest.approx(0.03)]
    assert lock.release_calls == 0


def test_retirement_lock_wait_smoke_when_real_lock_is_held():
    from actions.server._actions_process_pool import ProcessHandle

    handle = ProcessHandle.__new__(ProcessHandle)
    handle._retirement_lock = threading.Lock()
    handle._retirement_lock.acquire()
    try:
        result = handle.retire(time.monotonic() + 0.03)
    finally:
        handle._retirement_lock.release()

    assert result.state == "pending"
    assert result.reason == "retirement lock deadline expired"


def test_active_cancellation_retirement_skips_exit_send(monkeypatch):
    from types import SimpleNamespace

    from actions.server._actions_process_pool import ProcessHandle
    from actions.server._common.process import ProcessTreeCleanupResult

    handle = ProcessHandle.__new__(ProcessHandle)
    handle._retirement_lock = threading.Lock()
    handle._retirement_complete = False
    handle._retirement_descendants = []
    handle._retirement_snapshot_failed = False
    handle._kill_called = True
    handle._exit_attempted = False
    handle._process = SimpleNamespace(pid=123, poll=lambda: -9)
    handle._rcc_wrapper = None

    class Writer:
        def write_with_deadline(self, *args):
            pytest.fail("active cancellation must not send a graceful exit frame")

    class Reader:
        def join(self, timeout=None):
            self.timeout = timeout

        def is_alive(self):
            return False

    class Socket:
        def shutdown(self, _how):
            pass

    handle._writer = Writer()
    handle._reader = Reader()
    handle._socket = Socket()
    monkeypatch.setattr(
        "actions.server._common.process.force_kill_process_tree_until",
        lambda *args, **kwargs: ProcessTreeCleanupResult(True, (), (), ()),
    )

    assert handle.retire(time.monotonic() + 1)
    assert handle._retirement_complete
    assert handle._exit_attempted


def test_unexpected_dead_worker_has_explicit_unverified_recovery(monkeypatch):
    from actions.server._actions_process_pool import ProcessHandle

    handle = ProcessHandle.__new__(ProcessHandle)
    handle._retirement_lock = threading.Lock()
    handle._retirement_complete = False
    handle._retirement_result = None
    handle._retirement_descendants = None
    handle._retirement_snapshot_failed = False
    handle._kill_called = False
    handle._process = subprocess.Popen([sys.executable, "-c", "pass"])
    handle._process.wait(timeout=2)
    handle._reader = type(
        "Reader",
        (),
        {"join": lambda self, timeout=None: None, "is_alive": lambda self: False},
    )()
    handle._socket = None

    result = handle.retire(time.monotonic() + 1)

    assert result.state == "crash_unverified"
    assert result.capacity_releasable
    assert not result.descendant_snapshot_complete


def test_unexpected_crash_classification_survives_reader_retry():
    from actions.server._actions_process_pool import ProcessHandle

    reader_alive = [True]
    handle = ProcessHandle.__new__(ProcessHandle)
    handle._retirement_lock = threading.Lock()
    handle._retirement_started = False
    handle._crash_unverified = False
    handle._retirement_complete = False
    handle._retirement_result = None
    handle._retirement_descendants = None
    handle._retirement_snapshot_failed = False
    handle._kill_called = False
    handle._process = subprocess.Popen([sys.executable, "-c", "pass"])
    handle._process.wait(timeout=2)
    handle._reader = type(
        "Reader",
        (),
        {
            "join": lambda self, timeout=None: None,
            "is_alive": lambda self: reader_alive[0],
        },
    )()
    handle._socket = None

    first = handle.retire(time.monotonic() + 1)
    reader_alive[0] = False
    second = handle.retire(time.monotonic() + 1)

    assert first.state == "pending"
    assert second.state == "crash_unverified"
    assert second.capacity_releasable
    assert not second.descendant_snapshot_complete


def test_controlled_snapshot_failure_stays_pending_after_its_force_kill(monkeypatch):
    from actions.server._actions_process_pool import ProcessHandle
    from actions.server._common.process import ProcessTreeCleanupResult

    class Process:
        pid = 123

        def __init__(self):
            self.dead = False

        def poll(self):
            return -9 if self.dead else None

        def wait(self, timeout=None):
            raise subprocess.TimeoutExpired("worker", timeout)

    class Reader:
        def join(self, timeout=None):
            pass

        def is_alive(self):
            return False

    class Writer:
        def write_with_deadline(self, *args):
            pytest.fail("a failed descendant snapshot must suppress graceful exit")

    process = Process()
    handle = ProcessHandle.__new__(ProcessHandle)
    handle._retirement_lock = threading.Lock()
    handle._retirement_started = False
    handle._retirement_complete = False
    handle._retirement_result = None
    handle._retirement_descendants = None
    handle._retirement_snapshot_failed = False
    handle._kill_called = False
    handle._exit_attempted = False
    handle._process = process
    handle._rcc_wrapper = None
    handle._reader = Reader()
    handle._writer = Writer()
    handle._socket = None

    monkeypatch.setattr(
        "actions.server._common.process.snapshot_process_descendants",
        lambda _pid: (_ for _ in ()).throw(PermissionError("snapshot denied")),
    )

    def force_kill(_process, _descendants, _deadline, *, snapshot_complete_before_call):
        assert not snapshot_complete_before_call
        process.dead = True
        return ProcessTreeCleanupResult(
            wrapper_reaped=True,
            live_descendant_pids=(),
            zombie_descendant_pids=(),
            errors=(),
            descendant_snapshot_complete=False,
        )

    monkeypatch.setattr(
        "actions.server._common.process.force_kill_process_tree_until", force_kill
    )

    first = handle.retire(time.monotonic() + 1)
    second = handle.retire(time.monotonic() + 1)

    assert first.state == "pending"
    assert second.state == "pending"
    assert not first.descendant_snapshot_complete
    assert not second.descendant_snapshot_complete


def test_rcc_wrapper_retains_failed_snapshot_after_wrapper_exit(monkeypatch):
    from actions.server._common.process import ProcessTreeCleanupResult
    from actions.server._rcc_runtime_adapter import RccProcessHandle

    class Process:
        pid = 123

        def poll(self):
            return -9

    handle = RccProcessHandle.__new__(RccProcessHandle)
    handle.process = Process()
    handle._owned_processes = []
    handle._owned_snapshot_complete = False
    handle.last_cleanup_result = None

    def force_kill(process, descendants, deadline, *, snapshot_complete_before_call):
        assert process is handle.process
        assert descendants == []
        assert not snapshot_complete_before_call
        return ProcessTreeCleanupResult(
            wrapper_reaped=True,
            live_descendant_pids=(),
            zombie_descendant_pids=(),
            errors=(),
            descendant_snapshot_complete=snapshot_complete_before_call,
        )

    monkeypatch.setattr(
        "actions.server._common.process.force_kill_process_tree_until", force_kill
    )

    first = handle.force_kill_until(time.monotonic() + 1)
    second = handle.force_kill_until(time.monotonic() + 1)

    assert not first.execution_stopped
    assert not second.execution_stopped
    assert not handle._owned_snapshot_complete


def _wait_until(predicate, timeout):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if predicate():
            return True
        time.sleep(0.01)
    return predicate()


def test_process_startup_failure_closes_listener_and_accept_future(
    monkeypatch, tmp_path
):
    from types import SimpleNamespace

    from actions.server import _actions_process_pool as process_pool

    class FakeSocket:
        def __init__(self):
            self.closed = False

        def getsockname(self):
            return ("127.0.0.1", 12345)

        def close(self):
            self.closed = True

    class FakeFuture:
        def __init__(self):
            self.cancel_called = False
            self.result_timeouts = []

        def cancel(self):
            self.cancel_called = True
            return True

        def result(self, timeout=None):
            self.result_timeouts.append(timeout)
            raise RuntimeError("accept worker stopped")

    fake_socket = FakeSocket()
    fake_future = FakeFuture()
    monkeypatch.setattr(
        process_pool, "_create_server_socket", lambda *args: fake_socket
    )
    monkeypatch.setattr(
        "actions.server._robo_utils.run_in_thread.run_in_thread",
        lambda *args, **kwargs: fake_future,
    )
    monkeypatch.setattr(
        process_pool.subprocess,
        "Popen",
        lambda *args, **kwargs: (_ for _ in ()).throw(OSError("forced Popen failure")),
    )
    monkeypatch.setattr(process_pool, "_get_process_handle_key", lambda *args: "key")
    monkeypatch.setattr(
        "actions.server._actions_run_helpers.get_action_package_cwd",
        lambda *args: tmp_path,
    )
    monkeypatch.setattr(
        "actions.server._robo_utils.process.build_subprocess_kwargs",
        lambda **kwargs: {},
    )
    monkeypatch.setattr(
        "actions.server._robo_utils.process.build_python_launch_env",
        lambda env: env,
    )
    monkeypatch.setattr(
        "actions.server._actions_run_helpers._add_preload_actions_dir_to_env_pythonpath",
        lambda env: None,
    )
    monkeypatch.setattr(
        "actions.server._rcc_runtime_adapter.load_descriptor",
        lambda env_json: object(),
    )
    monkeypatch.setattr(
        "actions.server._rcc_runtime_adapter.get_rcc_location",
        lambda: Path("/opt/rcc"),
    )
    monkeypatch.setattr(
        "actions.server._rcc_runtime_adapter.build_exec_command",
        lambda *args, **kwargs: ["/opt/rcc"],
    )
    monkeypatch.setattr(
        "actions.server._rcc_runtime_adapter.new_receipt_path",
        lambda datadir: tmp_path / "receipt.json",
    )

    settings = SimpleNamespace(datadir=tmp_path, reuse_processes=False)
    package = SimpleNamespace(
        id="package", env_json=json.dumps({"runtime": {}}), directory=str(tmp_path)
    )
    with pytest.raises(OSError, match="forced Popen failure"):
        process_pool.ProcessHandle(settings, package, None)

    assert fake_socket.closed is True
    assert fake_future.cancel_called is True
    assert fake_future.result_timeouts


def test_process_pool_releases_capacity_when_warmup_fails(monkeypatch):
    from types import SimpleNamespace

    from actions.server import _actions_process_pool as process_pool

    class FakeProcess:
        can_reuse = True
        pid = 123
        key = "key"

        def __init__(self):
            self.killed = False
            self.reaped = False

        def is_alive(self):
            return True

        def kill(self):
            self.killed = True
            self.reaped = True

        def retire(self):
            self.reaped = True
            return True

    class TrackingSemaphore:
        def __init__(self):
            self.acquired = False
            self.release_count = 0

        def acquire(self, blocking=True, timeout=None):
            assert not self.acquired
            self.acquired = True
            return True

        def release(self):
            assert fake_process.reaped
            self.acquired = False
            self.release_count += 1

    fake_process = FakeProcess()
    pool = process_pool.ActionsProcessPool.__new__(process_pool.ActionsProcessPool)
    pool._settings = SimpleNamespace(reuse_processes=False)
    pool._lock = process_pool.threading.Lock()
    pool._running_processes = {}
    pool._idle_processes = {"key": {fake_process}}
    pool._pending_retirements = {}
    semaphore = TrackingSemaphore()
    pool._processes_running_semaphore = semaphore
    pool.action_package_id_to_action_package = {
        "package": SimpleNamespace(id="package")
    }
    pool._remove_from_running_processes = (
        lambda process: pool._running_processes.clear()
    )

    def fail_warmup_after_retirement():
        assert fake_process not in pool._pending_retirements
        with pool._lock:
            assert pool._count_total_processes() == 0
        raise RuntimeError("forced warmup failure")

    pool._warmup_processes = fail_warmup_after_retirement
    monkeypatch.setattr(process_pool, "_get_process_handle_key", lambda *args: "key")
    action = SimpleNamespace(action_package_id="package", name="action")

    with pytest.raises(
        RuntimeError, match="forced warmup failure"
    ), pool.obtain_process_for_action(action):
        pass

    assert fake_process.reaped is True
    assert semaphore.release_count == 1


@pytest.mark.real_rcc
def test_real_rcc_artifact_action_vertical(tmp_path):
    import os

    if not os.environ.get("ACTIONS_REAL_RCC_ARTIFACT_TEST"):
        pytest.skip("set ACTIONS_REAL_RCC_ARTIFACT_TEST=1")
    provider = os.environ.get("ACTIONS_RUNTIME_RCC_PROVIDER")
    if not provider:
        pytest.fail(
            "ACTIONS_RUNTIME_RCC_PROVIDER must name the cache provider for real proof"
        )

    import actions.server._models as models
    from actions.server._actions_import import import_action_package
    from actions.server._actions_process_pool import ActionsProcessPool
    from actions.server._database import Database
    from actions.server._models import (
        Action,
        ActionPackage,
        Run,
        RunStatus,
        get_model_db_rules,
    )
    from actions.server._rcc_runtime_adapter import read_receipt
    from actions.server._settings import Settings

    package_dir = tmp_path / "package"
    package_dir.mkdir()
    package_yaml = package_dir / "package.yaml"
    package_yaml.write_text(
        """version: 0.1
spec-version: v2
dependencies:
  conda-forge:
    - python=3.11.11
  pypi:
    - actions-core=1.0.2
"""
    )
    action_file = package_dir / "action.py"
    action_file.write_text(
        "from actions import action\n\n@action\ndef answer() -> str:\n    return 'rcc-v18.19.3'\n"
    )
    db = Database(tmp_path / "server.db")
    with db.connect():
        db.initialize([ActionPackage, Action])
        db.create_tables(get_model_db_rules())
        models._global_db = db
        try:
            import_action_package(
                datadir=tmp_path / "data",
                action_package_dir=str(package_dir),
                disable_not_imported=False,
                skip_lint=True,
                whitelist="",
            )
            package = db.all(ActionPackage)[0]
            action = db.all(Action)[0]
            descriptor = json.loads(package.env_json)["runtime"]
            expected_artifact_digest = descriptor["artifact_digest"]
            assert expected_artifact_digest.startswith("sha256:")
            assert len(expected_artifact_digest) == len("sha256:") + 64
            assert descriptor["kind"] == "rcc"
            assert descriptor["rcc_version"] == "v18.19.3"
            for forbidden in (
                "PYTHON_EXE",
                "CONDA_PREFIX",
                "ROBOCORP_HOME",
                "holotree",
                "materialization",
            ):
                assert forbidden not in package.env_json
            settings = Settings(
                datadir=tmp_path / "data", artifacts_dir=tmp_path / "artifacts"
            )
            settings.reuse_processes = False
            settings.min_processes = 0
            settings.max_processes = 1
            pool = ActionsProcessPool(settings, {package.id: package}, [action])
            try:
                run_dir = tmp_path / "run"
                run_dir.mkdir()
                input_json = run_dir / "input.json"
                result_json = run_dir / "result.json"
                output_file = run_dir / "output.txt"
                input_json.write_text("{}")
                run = Run(
                    id="rcc-real-run",
                    status=RunStatus.NOT_RUN,
                    action_id=action.id,
                    start_time="",
                    run_time=None,
                    inputs="{}",
                    result=None,
                    error_message=None,
                    relative_artifacts_dir="",
                    numbered_id=1,
                )
                with pool.obtain_process_for_action(action) as handle:
                    assert (
                        handle.run_action(
                            run,
                            package,
                            action,
                            input_json,
                            run_dir,
                            output_file,
                            result_json,
                            {},
                            {},
                            False,
                        )
                        == 0
                    )
                    assert (
                        json.loads(result_json.read_text())["result"] == "rcc-v18.19.3"
                    )
                    receipt = handle._rcc_wrapper.receipt_file
                assert receipt.exists()
                parsed_receipt = read_receipt(receipt, expected_artifact_digest)
                assert parsed_receipt["status"] == "failed"
                assert parsed_receipt["exitCode"] == -1
                assert parsed_receipt["reason"] == "child exited non-zero"
            finally:
                pool.dispose()
        finally:
            models._global_db = None


@pytest.mark.parametrize("invalid_pythonpath", [42, ["/path"], {"path": "/path"}])
def test_bootstrap_rejects_non_string_pythonpath(
    monkeypatch, tmp_path, invalid_pythonpath
):
    from types import SimpleNamespace

    from actions.server._action_package_handler import ActionPackageHandler
    from actions.server._protocols import ActionResult
    from actions.server.vendored_deps.action_package_handling.cli_errors import (
        ActionPackageError,
    )

    monkeypatch.delenv("ACTIONS_RUNTIME_RCC_PROVIDER", raising=False)
    monkeypatch.delenv("ACTIONS_REAL_RCC_ARTIFACT_TEST", raising=False)
    package = tmp_path / "package"
    package.mkdir()
    (package / "package.yaml").write_text(
        "version: 0.1\nspec-version: v2\ndependencies: {}\n"
    )

    class InvalidEnvironmentRcc:
        def get_package_yaml_hash(self, *args):
            return "test-hash"

        def create_env_and_get_vars(self, *args):
            return ActionResult(
                True, None, SimpleNamespace(env={"PYTHONPATH": invalid_pythonpath})
            )

    monkeypatch.setattr("actions.server._rcc.get_rcc", lambda: InvalidEnvironmentRcc())
    with pytest.raises(ActionPackageError, match="PYTHONPATH must be a string"):
        ActionPackageHandler(str(package), tmp_path / "data").bootstrap_environment()


_RCC5_SELECTOR = "released-18.19.5-linux-amd64"
_RCC5_SHA256 = "1a617ad7c736fa67c605e20e5ebe3c7d54b02cd548f733e05c809cf49a48db1e"


@pytest.fixture
def approved_rcc5(monkeypatch):
    from actions.server import _rcc_runtime_adapter as adapter

    monkeypatch.setenv("ACTIONS_RUNTIME_RCC_TEST_COMPATIBILITY", _RCC5_SELECTOR)
    monkeypatch.setattr(
        adapter, "_binary_sha256", lambda _path: _RCC5_SHA256, raising=False
    )
    monkeypatch.setattr(adapter, "_rcc_platform", lambda: "linux-amd64", raising=False)
    monkeypatch.setattr(
        adapter, "_rcc_version_probe", lambda _path: (0, "v18.19.5", ""), raising=False
    )
    return adapter


def test_approved_test_only_rcc5_measures_version_without_changing_default(
    approved_rcc5,
):
    adapter = approved_rcc5
    assert adapter.RCC_VERSION == "v18.19.3"
    assert (
        adapter.verify_rcc_version(
            Path("/owned/rcc"), lambda *args: (0, "v18.19.5\n", "")
        )
        == "v18.19.5"
    )


@pytest.mark.parametrize(
    "selector", ["unknown", "candidate-097-18.19.6-linux-amd64", ""]
)
def test_unapproved_test_selector_rejected_before_publication(
    tmp_path, monkeypatch, selector
):
    from actions.server import _rcc_runtime_adapter as adapter

    monkeypatch.setenv("ACTIONS_RUNTIME_RCC_TEST_COMPATIBILITY", selector)
    calls = []
    with pytest.raises(adapter.RccRuntimeError, match="compatibility"):
        adapter.publish_artifact_details(
            tmp_path / "package.yaml",
            Path("/owned/rcc"),
            runner=lambda *args: calls.append(args) or (0, "{}", ""),
        )
    assert not calls


@pytest.mark.parametrize("version", ["", "v18.19.3", "v18.19.5\nsecret", "x" * 1025])
def test_test_only_version_rejection_does_not_leak_output(approved_rcc5, version):
    adapter = approved_rcc5
    with pytest.raises(adapter.RccRuntimeError) as error:
        adapter.verify_rcc_version(
            Path("/owned/rcc"), lambda *args: (0, version, "credential")
        )
    assert "secret" not in str(error.value)
    assert "credential" not in str(error.value)


@pytest.mark.parametrize(
    "field,value",
    [
        ("version", "v18.19.3"),
        ("runtime_kind", "other"),
        ("adapter_contract", "rcc-runtime/v2"),
        ("source_commit", "0" * 40),
        ("binary_sha256", "0" * 64),
        ("platform", "windows-amd64"),
        ("provenance_sha256", "0" * 64),
        ("approval", "QUALIFIED"),
        ("source_modified", False),
        ("source_modified", 1),
        ("required_surface", ("env-publish",)),
    ],
)
def test_descriptor_co_mutated_identity_cannot_self_certify(
    approved_rcc5, field, value
):
    adapter = approved_rcc5
    identity = adapter.verify_rcc_compatibility(
        Path("/owned/rcc"), runner=lambda *args: (0, "v18.19.5", "")
    )
    descriptor = adapter.RccRuntimeDescriptor(
        "sha256:" + "a" * 64, rcc_version="v18.19.5", rcc_compatibility=identity
    )
    payload = descriptor.to_dict()
    payload["runtime"]["rcc_compatibility"][field] = value
    # Recomputing the mutable serialized ID cannot change the committed approval.
    import hashlib

    changed = payload["runtime"]["rcc_compatibility"]
    changed.pop("compatibility_id", None)
    changed["compatibility_id"] = hashlib.sha256(
        json.dumps(changed, sort_keys=True, separators=(",", ":")).encode()
    ).hexdigest()
    with pytest.raises(adapter.RccRuntimeError, match="compatibility"):
        adapter.RccRuntimeDescriptor.from_dict(payload)


def test_new_selected_binary_does_not_execute_legacy_descriptor(approved_rcc5):
    adapter = approved_rcc5
    legacy = adapter.RccRuntimeDescriptor("sha256:" + "a" * 64)
    with pytest.raises(adapter.RccRuntimeError, match="reprepare"):
        adapter.build_exec_command(
            Path("/owned/rcc"), legacy, ["python"], receipt_file=None
        )


def test_new_descriptor_exec_rechecks_binary_bytes_and_selector(
    approved_rcc5, monkeypatch
):
    adapter = approved_rcc5
    identity = adapter.verify_rcc_compatibility(
        Path("/owned/rcc"), runner=lambda *args: (0, "v18.19.5", "")
    )
    descriptor = adapter.RccRuntimeDescriptor(
        "sha256:" + "a" * 64, rcc_version="v18.19.5", rcc_compatibility=identity
    )
    monkeypatch.setattr(
        adapter, "_subprocess_runner", lambda *args: (0, "v18.19.5", "")
    )
    assert (
        adapter.build_exec_command(
            Path("/owned/rcc"), descriptor, ["python"], receipt_file=None
        )[2]
        == "exec"
    )
    monkeypatch.setattr(adapter, "_binary_sha256", lambda _path: "0" * 64)
    with pytest.raises(adapter.RccRuntimeError, match="compatibility"):
        adapter.build_exec_command(
            Path("/owned/rcc"), descriptor, ["python"], receipt_file=None
        )
    monkeypatch.delenv("ACTIONS_RUNTIME_RCC_TEST_COMPATIBILITY")
    with pytest.raises(adapter.RccRuntimeError, match="reprepare"):
        adapter.build_exec_command(
            Path("/owned/rcc"), descriptor, ["python"], receipt_file=None
        )


def test_selected_binary_partitions_source_cache_and_measures_all_descriptors(
    approved_rcc5, tmp_path, monkeypatch
):
    adapter = approved_rcc5
    environment = tmp_path / "package.yaml"
    environment.write_text("spec-version: 2\ndependencies: []\n")
    calls = []
    digest = "sha256:" + "a" * 64

    def runner(*args):
        if args[-1] == "--version":
            return 0, "v18.19.5", ""
        calls.append(args[2])
        return (
            0,
            json.dumps(
                {
                    "artifact": digest,
                    "artifactDigest": digest,
                    "verification": {"valid": True},
                }
            ),
            "",
        )

    first = adapter.prepare_runtime(
        environment, Path("/owned/rcc"), source_generation="one", runner=runner
    )
    second = adapter.prepare_runtime(
        environment, Path("/owned/rcc"), source_generation="two", runner=runner
    )
    assert first.rcc_version == second.rcc_version == "v18.19.5"
    assert first.rcc_compatibility == second.rcc_compatibility
    assert second.preparation_class == "source-reuse"
    assert calls == ["publish", "acquire"]
    assert adapter.RccRuntimeDescriptor.from_json(second.to_json()) == second
    monkeypatch.delenv("ACTIONS_RUNTIME_RCC_TEST_COMPATIBILITY")
    calls.clear()
    third = adapter.prepare_runtime(
        environment, Path("/legacy/rcc"), source_generation="three", runner=runner
    )
    assert third.rcc_version == "v18.19.3"
    assert third.rcc_compatibility is None
    assert calls == ["publish", "acquire"]


def test_wrong_binary_or_platform_stops_before_cache_or_artifact_operation(
    approved_rcc5, tmp_path, monkeypatch
):
    adapter = approved_rcc5
    environment = tmp_path / "package.yaml"
    environment.write_text("spec-version: 2\ndependencies: []\n")
    before = dict(adapter._prepared_runtime_cache)
    calls = []
    monkeypatch.setattr(adapter, "_binary_sha256", lambda _path: "0" * 64)
    with pytest.raises(adapter.RccRuntimeError, match="compatibility"):
        adapter.prepare_runtime(
            environment,
            Path("/owned/rcc"),
            runner=lambda *args: (0, "v18.19.5", "")
            if args[-1] == "--version"
            else calls.append(args) or (0, "{}", ""),
        )
    assert not calls
    assert adapter._prepared_runtime_cache == before
    monkeypatch.setattr(adapter, "_binary_sha256", lambda _path: _RCC5_SHA256)
    monkeypatch.setattr(adapter, "_rcc_platform", lambda: "windows-amd64")
    with pytest.raises(adapter.RccRuntimeError, match="compatibility"):
        adapter.prepare_runtime(
            environment, Path("/owned/rcc"), runner=lambda *args: (0, "v18.19.5", "")
        )
    assert adapter._prepared_runtime_cache == before


@pytest.mark.parametrize(
    "selector,version,binary_sha,source,modified",
    [
        (
            _RCC5_SELECTOR,
            "v18.19.5",
            _RCC5_SHA256,
            "d1aec7d0bb897a81274423c7a6bb747233f9c263",
            True,
        ),
        (
            "candidate-727-18.19.6-linux-amd64",
            "v18.19.6",
            "be55dadc391ae4ac7dd38356755a1698c595fdf41bd4a0141f8df7cc9d29f00e",
            "727c1ff8b679cdbbe12836e53c87734a1edb0071",
            False,
        ),
    ],
)
def test_approved_test_tuples_record_measured_build_source_not_functionality(
    monkeypatch, selector, version, binary_sha, source, modified
):
    from actions.server import _rcc_runtime_adapter as adapter

    monkeypatch.setenv("ACTIONS_RUNTIME_RCC_TEST_COMPATIBILITY", selector)
    monkeypatch.setattr(
        adapter, "_binary_sha256", lambda _path: binary_sha, raising=False
    )
    monkeypatch.setattr(adapter, "_rcc_platform", lambda: "linux-amd64", raising=False)
    identity = adapter.verify_rcc_compatibility(
        Path("/owned/rcc"), runner=lambda *args: (0, version, "")
    )
    assert identity.version == version
    assert identity.source_commit == source
    assert identity.source_modified is modified
    assert identity.binary_sha256 == binary_sha
    assert identity.approval == "APPROVED_TEST_ONLY"
    assert identity.provenance_kind == "ORIGINAL_API_ASSET_OR_BUILD_RECEIPT_DATA_ONLY"
    assert identity.required_surface == (
        "env-publish",
        "env-acquire",
        "env-exec",
        "verified-artifact",
        "process-lease",
    )


def test_changed_approved_binary_cannot_reuse_source_only_cache(
    approved_rcc5, tmp_path, monkeypatch
):
    adapter = approved_rcc5
    environment = tmp_path / "package.yaml"
    environment.write_text("spec-version: 2\ndependencies: []\n")
    calls = []

    def runner(*args):
        calls.append(args[2])
        return (
            0,
            json.dumps(
                {
                    "artifact": "sha256:" + "a" * 64,
                    "artifactDigest": "sha256:" + "a" * 64,
                    "verification": {"valid": True},
                }
            ),
            "",
        )

    first = adapter.prepare_runtime(
        environment, Path("/owned/rcc5"), source_generation="one", runner=runner
    )
    monkeypatch.setenv(
        "ACTIONS_RUNTIME_RCC_TEST_COMPATIBILITY", "candidate-727-18.19.6-linux-amd64"
    )
    monkeypatch.setattr(
        adapter,
        "_binary_sha256",
        lambda _path: "be55dadc391ae4ac7dd38356755a1698c595fdf41bd4a0141f8df7cc9d29f00e",
    )
    monkeypatch.setattr(
        adapter, "_rcc_version_probe", lambda _path: (0, "v18.19.6", ""), raising=False
    )
    second = adapter.prepare_runtime(
        environment, Path("/owned/rcc6"), source_generation="two", runner=runner
    )
    assert calls == ["publish", "acquire", "publish", "acquire"]
    assert (
        first.rcc_compatibility.compatibility_id
        != second.rcc_compatibility.compatibility_id
    )
    assert second.rcc_version == "v18.19.6"
    assert second.preparation_class != "source-reuse"
    with pytest.raises(adapter.RccRuntimeError, match="reprepare"):
        adapter.prepare_runtime(
            environment, Path("/owned/rcc6"), previous_descriptor=first, runner=runner
        )


@pytest.mark.parametrize("mutation", ["version", "unknown-field", "wrong-shape"])
def test_compatibility_descriptor_namespace_remains_strict(approved_rcc5, mutation):
    adapter = approved_rcc5
    identity = adapter.verify_rcc_compatibility(
        Path("/owned/rcc"), runner=lambda *args: (0, "v18.19.5", "")
    )
    payload = adapter.RccRuntimeDescriptor(
        "sha256:" + "a" * 64, rcc_version="v18.19.5", rcc_compatibility=identity
    ).to_dict()
    if mutation == "version":
        payload["runtime"]["rcc_version"] = "v18.19.3"
    elif mutation == "unknown-field":
        payload["runtime"]["rcc_compatibility"]["operator_approval"] = True
    else:
        payload["runtime"]["rcc_compatibility"] = []
    with pytest.raises(adapter.RccRuntimeError, match="compatibility"):
        adapter.RccRuntimeDescriptor.from_dict(payload)


def test_isolated_selection_requires_explicit_binary_before_default_acquisition(
    monkeypatch,
):
    from actions.server import _download_rcc
    from actions.server import _rcc_runtime_adapter as adapter

    monkeypatch.setenv("ACTIONS_RUNTIME_RCC_TEST_COMPATIBILITY", _RCC5_SELECTOR)
    monkeypatch.delenv("ACTIONS_RUNTIME_RCC_BINARY", raising=False)
    calls = []
    monkeypatch.setattr(
        _download_rcc,
        "get_default_rcc_location",
        lambda: calls.append("download") or Path("/missing"),
    )
    with pytest.raises(adapter.RccRuntimeError, match="compatibility"):
        adapter.get_rcc_location()
    assert not calls


def test_test_only_admission_hashes_before_probe_and_never_probes_wrong_bytes(
    approved_rcc5, monkeypatch
):
    adapter = approved_rcc5
    events = []
    monkeypatch.setattr(
        adapter, "_binary_sha256", lambda _path: events.append("hash") or _RCC5_SHA256
    )
    monkeypatch.setattr(
        adapter,
        "_rcc_version_probe",
        lambda _path: events.append("probe") or (0, "v18.19.5", ""),
    )
    assert adapter.verify_rcc_compatibility(Path("/owned/rcc")).version == "v18.19.5"
    assert events == ["hash", "probe"]
    events.clear()
    monkeypatch.setattr(
        adapter, "_binary_sha256", lambda _path: events.append("hash") or "0" * 64
    )
    with pytest.raises(adapter.RccRuntimeError, match="compatibility"):
        adapter.verify_rcc_compatibility(Path("/owned/rcc"))
    assert events == ["hash"]
