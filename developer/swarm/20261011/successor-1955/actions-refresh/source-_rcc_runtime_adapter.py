"""The provisional RCC Environment Artifact runtime boundary for spec-v2 Actions.

Artifact digests remain durable execution authority. Explicit isolated binary
selections additionally bind an approved compatibility identity, never local paths.
"""

from __future__ import annotations

import hashlib
import json
import os
import platform
import re
import subprocess
import sys
import threading
import uuid
from collections.abc import Callable, Sequence
from dataclasses import asdict, dataclass, replace
from pathlib import Path
from typing import Literal
from urllib.parse import urlsplit

from actions.server._common.process import ProcessTreeCleanupResult

RCC_VERSION = "v18.19.3"
RCC_CONTRACT_VERSION = "rcc-runtime/v1"
_DIGEST_RE = re.compile(r"^sha256:[0-9a-f]{64}$")
_PROVIDER_PROFILE_RE = re.compile(r"^[a-z0-9][a-z0-9._-]{0,62}$")
_ENVIRONMENT_FIELDS = (
    "spec-version",
    "dependencies",
    "dev-dependencies",
    "post-install",
)
_prepared_runtime_cache: dict[
    tuple[Path, str, str | None, str | None], tuple[str, RccRuntimeDescriptor]
] = {}
_prepared_runtime_cache_lock = threading.Lock()


def _validate_provider_reference(provider: str | None) -> str | None:
    """Reject provider URLs that could persist credentials or argv controls."""

    if provider is None:
        return None
    if not isinstance(provider, str) or not provider:
        raise RccRuntimeError("provider", "provider reference is invalid")
    if any(ord(character) < 32 or ord(character) == 127 for character in provider):
        raise RccRuntimeError("provider", "provider reference contains control data")
    try:
        parsed = urlsplit(provider)
        _ = parsed.port  # Validate numeric port syntax.
    except ValueError:
        raise RccRuntimeError("provider", "provider reference is invalid") from None
    if (
        parsed.username is not None
        or parsed.password is not None
        or parsed.query
        or parsed.fragment
    ):
        raise RccRuntimeError(
            "provider", "credential-bearing provider URL syntax is unsupported"
        )
    if any(character.isspace() for character in provider):
        raise RccRuntimeError("provider", "provider reference contains whitespace")
    if provider == "local":
        return provider
    if parsed.scheme or parsed.netloc:
        if (
            not provider.startswith(("http://", "https://"))
            or not parsed.netloc
            or not parsed.hostname
        ):
            raise RccRuntimeError("provider", "provider must be a valid HTTP(S) URL")
        return provider
    if not _PROVIDER_PROFILE_RE.fullmatch(provider):
        raise RccRuntimeError("provider", "provider profile reference is invalid")
    return provider


class RccRuntimeError(RuntimeError):
    """A bounded failure at one RCC runtime preparation/execution phase."""

    def __init__(self, phase: str, message: str, *, retryable: bool = False):
        self.phase = phase
        self.retryable = retryable
        super().__init__(f"RCC {phase} failed: {message}")


# Acquisition admits these exact binaries for isolated tests, not production
# functionality. The required surface is an expectation for the actual gates.
_RCC_TEST_SURFACE = (
    "env-publish",
    "env-acquire",
    "env-exec",
    "verified-artifact",
    "process-lease",
)
_RCC_ACQUISITION_SHA256 = (
    "0da97bfc63a5e47de1c900e8d37ec55261d8b7ef6f68069a74167afc344c72ad"
)
_APPROVED_RCC_TEST_TUPLES = (
    (
        "released-18.19.5-linux-amd64",
        "v18.19.5",
        "d1aec7d0bb897a81274423c7a6bb747233f9c263",
        True,
        "1a617ad7c736fa67c605e20e5ebe3c7d54b02cd548f733e05c809cf49a48db1e",
    ),
    (
        "candidate-727-18.19.6-linux-amd64",
        "v18.19.6",
        "727c1ff8b679cdbbe12836e53c87734a1edb0071",
        False,
        "be55dadc391ae4ac7dd38356755a1698c595fdf41bd4a0141f8df7cc9d29f00e",
    ),
)


@dataclass(frozen=True)
class RccCompatibilityIdentity:
    version: str
    source_commit: str
    source_modified: bool
    binary_sha256: str
    platform: str = "linux-amd64"
    runtime_kind: str = "rcc"
    adapter_contract: str = RCC_CONTRACT_VERSION
    provenance_kind: str = "ORIGINAL_API_ASSET_OR_BUILD_RECEIPT_DATA_ONLY"
    provenance_sha256: str = _RCC_ACQUISITION_SHA256
    approval: str = "APPROVED_TEST_ONLY"
    required_surface: tuple[str, ...] = _RCC_TEST_SURFACE
    compatibility_id: str = ""

    def __post_init__(self) -> None:
        approved = any(
            (self.version, self.source_commit, self.source_modified, self.binary_sha256)
            == row[1:]
            for row in _APPROVED_RCC_TEST_TUPLES
        )
        if (
            not approved
            or not isinstance(self.source_modified, bool)
            or self.runtime_kind != "rcc"
            or self.adapter_contract != RCC_CONTRACT_VERSION
            or self.platform != "linux-amd64"
            or self.provenance_kind != "ORIGINAL_API_ASSET_OR_BUILD_RECEIPT_DATA_ONLY"
            or self.provenance_sha256 != _RCC_ACQUISITION_SHA256
            or self.approval != "APPROVED_TEST_ONLY"
            or self.required_surface != _RCC_TEST_SURFACE
        ):
            raise RccRuntimeError("compatibility", "unapproved RCC identity")
        value = asdict(self)
        value.pop("compatibility_id")
        expected = hashlib.sha256(
            json.dumps(value, sort_keys=True, separators=(",", ":")).encode()
        ).hexdigest()
        if self.compatibility_id and self.compatibility_id != expected:
            raise RccRuntimeError("compatibility", "invalid identity digest")
        object.__setattr__(self, "compatibility_id", expected)

    @classmethod
    def from_dict(cls, value: object) -> "RccCompatibilityIdentity":
        if not isinstance(value, dict) or set(value) != set(cls.__dataclass_fields__):
            raise RccRuntimeError("compatibility", "invalid identity fields")
        value = dict(value)
        surface = value.get("required_surface")
        if not isinstance(surface, (list, tuple)):
            raise RccRuntimeError("compatibility", "invalid required surface")
        value["required_surface"] = tuple(surface)
        try:
            return cls(**value)
        except (TypeError, ValueError):
            raise RccRuntimeError("compatibility", "invalid identity") from None


def approved_rcc_test_policy(selector: str) -> RccCompatibilityIdentity:
    for name, version, source, modified, digest in _APPROVED_RCC_TEST_TUPLES:
        if selector == name:
            return RccCompatibilityIdentity(version, source, modified, digest)
    raise RccRuntimeError("compatibility", "unknown or unapproved test selection")


def _rcc_platform() -> str:
    machine = platform.machine().lower()
    arch = "amd64" if machine in ("x86_64", "amd64") else machine
    return f"{sys.platform}-{arch}"


def admit_rcc_compatibility(
    version: str, binary_sha256: str, *, selector: str
) -> RccCompatibilityIdentity:
    expected = approved_rcc_test_policy(selector)
    if (
        _rcc_platform() != expected.platform
        or not isinstance(version, str)
        or len(version) > 1024
        or version
        not in (expected.version, expected.version + "\n", expected.version + "\r\n")
        or binary_sha256 != expected.binary_sha256
    ):
        raise RccRuntimeError("compatibility", "selected executable identity mismatch")
    return expected


def _binary_sha256(path: Path) -> str:
    digest = hashlib.sha256()
    try:
        with path.open("rb") as stream:
            for block in iter(lambda: stream.read(1024 * 1024), b""):
                digest.update(block)
    except OSError:
        raise RccRuntimeError(
            "compatibility", "unable to measure selected executable"
        ) from None
    return digest.hexdigest()


def _rcc_version_probe(path: Path) -> tuple[int, str, str]:
    try:
        completed = subprocess.run(
            [str(path), "--version"], capture_output=True, text=True, timeout=10
        )
    except (OSError, subprocess.TimeoutExpired, UnicodeError):
        raise RccRuntimeError(
            "compatibility", "unable to verify selected version"
        ) from None
    return completed.returncode, completed.stdout, completed.stderr


def verify_rcc_compatibility(
    rcc_location: Path, runner: Runner | None = None
) -> RccCompatibilityIdentity | None:
    selector = os.environ.get("ACTIONS_RUNTIME_RCC_TEST_COMPATIBILITY")
    if selector is None:
        return None
    expected = approved_rcc_test_policy(selector)
    # Measure bytes before launching even the bounded version probe.
    digest = _binary_sha256(rcc_location)
    if digest != expected.binary_sha256 or _rcc_platform() != expected.platform:
        raise RccRuntimeError("compatibility", "selected executable identity mismatch")
    code, stdout, _stderr = (
        runner(str(rcc_location), "--version")
        if runner is not None
        else _rcc_version_probe(rcc_location)
    )
    if code:
        raise RccRuntimeError("compatibility", "unable to verify selected version")
    return admit_rcc_compatibility(stdout, digest, selector=selector)


@dataclass(frozen=True)
class RccRuntimeDescriptor:
    artifact_digest: str
    source_generation: str = "unknown"
    source_hash: str = "unknown"
    environment_fingerprint: str = ""
    preparation_class: str = "local"
    rcc_version: str = RCC_VERSION
    runtime_kind: str = "rcc"
    contract_version: str = RCC_CONTRACT_VERSION
    provider_reference: str | None = None
    provider_context_bound: bool = True
    rcc_compatibility: RccCompatibilityIdentity | None = None

    def __post_init__(self) -> None:
        if not _DIGEST_RE.fullmatch(self.artifact_digest):
            raise RccRuntimeError("descriptor", "invalid sha256 artifact digest")
        if self.runtime_kind != "rcc":
            raise RccRuntimeError("descriptor", "unsupported runtime kind")
        if self.contract_version != RCC_CONTRACT_VERSION:
            raise RccRuntimeError("descriptor", "unsupported adapter contract")
        if self.environment_fingerprint and not re.fullmatch(
            r"[0-9a-f]{64}", self.environment_fingerprint
        ):
            raise RccRuntimeError("descriptor", "invalid environment fingerprint")
        if self.rcc_compatibility is not None:
            if (
                not isinstance(self.rcc_compatibility, RccCompatibilityIdentity)
                or self.rcc_version != self.rcc_compatibility.version
            ):
                raise RccRuntimeError("compatibility", "descriptor identity mismatch")
        _validate_provider_reference(self.provider_reference)
        if not isinstance(self.provider_context_bound, bool):
            raise RccRuntimeError("descriptor", "invalid provider context binding")

    def to_dict(self) -> dict[str, object]:
        runtime = asdict(self)
        runtime["kind"] = runtime.pop("runtime_kind")
        if self.rcc_compatibility is None:
            runtime.pop("rcc_compatibility")
        return {"runtime": runtime}

    def to_json(self) -> str:
        return json.dumps(self.to_dict(), sort_keys=True, separators=(",", ":"))

    @classmethod
    def from_dict(cls, value: object) -> "RccRuntimeDescriptor":
        if not isinstance(value, dict) or set(value) != {"runtime"}:
            raise RccRuntimeError(
                "descriptor", "expected a versioned runtime descriptor"
            )
        runtime = value["runtime"]
        if not isinstance(runtime, dict):
            raise RccRuntimeError("descriptor", "runtime namespace is not an object")
        allowed = {f.name for f in cls.__dataclass_fields__.values()} | {"kind"}
        if set(runtime) - allowed:
            raise RccRuntimeError("descriptor", "unknown runtime fields")
        runtime = dict(runtime)
        if "rcc_compatibility" in runtime:
            runtime["rcc_compatibility"] = RccCompatibilityIdentity.from_dict(
                runtime["rcc_compatibility"]
            )
        if "provider_reference" not in runtime:
            # Persisted legacy descriptors lack trust-carrier identity.
            runtime["provider_context_bound"] = False
        if "kind" in runtime:
            runtime["runtime_kind"] = runtime.pop("kind")
        try:
            return cls(**runtime)
        except TypeError as exc:
            raise RccRuntimeError(
                "descriptor", "incomplete runtime descriptor"
            ) from exc

    @classmethod
    def from_json(cls, value: str) -> "RccRuntimeDescriptor":
        try:
            return cls.from_dict(json.loads(value))
        except RccRuntimeError:
            raise
        except (TypeError, ValueError, json.JSONDecodeError) as exc:
            raise RccRuntimeError("descriptor", "invalid descriptor JSON") from exc


@dataclass(frozen=True)
class RccPublishedArtifactDetails:
    """The two canonical identities returned by one RCC publish operation."""

    specification_digest: str
    artifact_digest: str

    def __post_init__(self) -> None:
        if not isinstance(self.specification_digest, str) or not _DIGEST_RE.fullmatch(
            self.specification_digest
        ):
            raise RccRuntimeError("publish", "invalid sha256 specification digest")
        if not isinstance(self.artifact_digest, str) or not _DIGEST_RE.fullmatch(
            self.artifact_digest
        ):
            raise RccRuntimeError("publish", "invalid sha256 artifact digest")


def parse_artifact_digest(payload: object) -> str:
    """Extract exactly one canonical digest from an RCC JSON result."""

    candidates: list[object] = []
    if isinstance(payload, dict):
        for key in ("artifact", "artifact_digest", "artifactDigest", "digest"):
            if key in payload:
                candidates.append(payload[key])
        artifact = payload.get("artifact")
        if isinstance(artifact, dict) and "digest" in artifact:
            candidates.append(artifact["digest"])
        environment_artifact = payload.get("environment_artifact")
        if isinstance(environment_artifact, dict) and "digest" in environment_artifact:
            candidates.append(environment_artifact["digest"])
    values = []
    for candidate in candidates:
        if isinstance(candidate, str) and _DIGEST_RE.fullmatch(candidate):
            values.append(candidate)
        elif not isinstance(candidate, dict):
            raise RccRuntimeError("artifact", "missing or malformed artifact identity")
    values = list(dict.fromkeys(values))
    if len(values) != 1 or len(candidates) == 0:
        raise RccRuntimeError("artifact", "missing or malformed artifact identity")
    return values[0]


def parse_published_artifact_details(payload: object) -> RccPublishedArtifactDetails:
    """Require RCC's canonical publish fields and reject identity conflicts."""

    if not isinstance(payload, dict):
        raise RccRuntimeError("publish", "RCC publish details are not an object")

    specification_digest = payload.get("specificationDigest")
    artifact_digest = payload.get("artifactDigest")
    if not isinstance(specification_digest, str) or not _DIGEST_RE.fullmatch(
        specification_digest
    ):
        raise RccRuntimeError("publish", "missing or malformed specification digest")
    if not isinstance(artifact_digest, str) or not _DIGEST_RE.fullmatch(
        artifact_digest
    ):
        raise RccRuntimeError("publish", "missing or malformed artifact digest")

    # RCC v18.19.3 emits these camel-case fields. If older artifact aliases are
    # also present, require them to identify the same artifact.
    artifact_values: list[object] = []
    for key in ("artifact_digest", "digest"):
        if key in payload:
            artifact_values.append(payload[key])
    if "artifact" in payload:
        value = payload["artifact"]
        if isinstance(value, dict) and "digest" in value:
            artifact_values.append(value["digest"])
        else:
            artifact_values.append(value)
    if "environment_artifact" in payload:
        value = payload["environment_artifact"]
        if isinstance(value, dict) and "digest" in value:
            artifact_values.append(value["digest"])
        else:
            artifact_values.append(value)
    if any(
        not isinstance(value, str)
        or not _DIGEST_RE.fullmatch(value)
        or value != artifact_digest
        for value in artifact_values
    ):
        raise RccRuntimeError("publish", "conflicting or malformed artifact identities")

    if (
        "specification_digest" in payload
        and payload["specification_digest"] != specification_digest
    ):
        raise RccRuntimeError("publish", "conflicting specification identities")

    return RccPublishedArtifactDetails(
        specification_digest=specification_digest,
        artifact_digest=artifact_digest,
    )


Runner = Callable[..., tuple[int, str, str]]


def environment_spec_fingerprint(environment: Path) -> str:
    """Return a deterministic fingerprint of package environment inputs."""

    try:
        import yaml

        package = yaml.safe_load(environment.read_text(encoding="utf-8"))
    except (OSError, TypeError, ValueError) as exc:
        raise RccRuntimeError("resolve", "unable to read package environment") from exc
    if not isinstance(package, dict):
        raise RccRuntimeError("resolve", "package environment must be a mapping")
    normalized = {key: package[key] for key in _ENVIRONMENT_FIELDS if key in package}
    encoded = json.dumps(normalized, sort_keys=True, separators=(",", ":"), default=str)
    return hashlib.sha256(encoded.encode("utf-8")).hexdigest()


def classify_environment_change(
    previous: Path, current: Path
) -> Literal["environment", "source"]:
    """Classify package changes using only the normalized environment inputs."""

    if environment_spec_fingerprint(previous) != environment_spec_fingerprint(current):
        return "environment"
    return "source"


def compute_source_generation(package_dir: Path) -> str:
    """Hash package source files without making local paths part of identity."""

    digest = hashlib.sha256()
    for path in sorted(package_dir.rglob("*")):
        if not path.is_file() or path.name == "package.yaml":
            continue
        if "__pycache__" in path.parts or path.name.endswith(".pyc"):
            continue
        relative = path.relative_to(package_dir).as_posix().encode("utf-8")
        digest.update(len(relative).to_bytes(8, "big"))
        digest.update(relative)
        content = path.read_bytes()
        digest.update(len(content).to_bytes(8, "big"))
        digest.update(content)
    return digest.hexdigest()


def _subprocess_runner(*args: str) -> tuple[int, str, str]:
    try:
        completed = subprocess.run(
            list(args),
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
            timeout=float(os.environ.get("ACTIONS_RUNTIME_RCC_TIMEOUT", "900")),
        )
    except subprocess.TimeoutExpired as exc:
        raise RccRuntimeError("rcc", "command timed out") from exc
    except OSError as exc:
        raise RccRuntimeError("rcc", str(exc)) from exc
    return completed.returncode, completed.stdout, completed.stderr


def _run_json(
    phase: str, args: Sequence[str], runner: Runner = _subprocess_runner
) -> dict:
    code, stdout, stderr = runner(*args)
    if code:
        output_lines = (stderr or stdout).strip().splitlines()
        detail = next(
            (
                line.strip()
                for line in reversed(output_lines)
                if line.strip()
                and not line.strip().startswith("[rcc] exit status will be:")
                and not line.strip().startswith("Note: Now running rcc")
            ),
            "command failed",
        )
        raise RccRuntimeError(phase, detail[:400])
    try:
        loaded = json.loads(stdout)
    except (TypeError, ValueError, json.JSONDecodeError) as exc:
        raise RccRuntimeError(phase, "RCC did not return a JSON object") from exc
    if not isinstance(loaded, dict):
        raise RccRuntimeError(phase, "RCC returned a non-object JSON result")
    return loaded


def _publish_artifact_payload(
    environment: Path,
    rcc_location: Path,
    *,
    provider: str | None,
    runner: Runner,
) -> dict:
    verify_rcc_compatibility(rcc_location)
    provider = _validate_provider_reference(provider)
    args = [
        str(rcc_location),
        "env",
        "publish",
        "--environment",
        str(environment),
        "--json",
    ]
    if provider:
        args.extend(["--provider", provider])
    return _run_json("publish", args, runner)


def publish_artifact(
    environment: Path,
    rcc_location: Path,
    *,
    provider: str | None = None,
    runner: Runner = _subprocess_runner,
) -> str:
    return parse_artifact_digest(
        _publish_artifact_payload(
            environment, rcc_location, provider=provider, runner=runner
        )
    )


def publish_artifact_details(
    environment: Path,
    rcc_location: Path,
    *,
    provider: str | None = None,
    runner: Runner = _subprocess_runner,
) -> RccPublishedArtifactDetails:
    """Publish once and return RCC's unmodified specification/artifact digests."""

    return parse_published_artifact_details(
        _publish_artifact_payload(
            environment, rcc_location, provider=provider, runner=runner
        )
    )


def acquire_artifact(
    artifact_digest: str,
    rcc_location: Path,
    *,
    provider: str | None = None,
    runner: Runner = _subprocess_runner,
) -> dict:
    verify_rcc_compatibility(rcc_location)
    provider = _validate_provider_reference(provider)
    if not _DIGEST_RE.fullmatch(artifact_digest):
        raise RccRuntimeError("acquire", "invalid artifact digest")
    args = [
        str(rcc_location),
        "env",
        "acquire",
        "--artifact",
        artifact_digest,
        "--json",
        "--permissive-local",
    ]
    if provider:
        args.extend(["--provider", provider])
    try:
        result = _run_json("acquire", args, runner)
    except RccRuntimeError as exc:
        if "not materialized" in str(exc).casefold():
            raise RccRuntimeError("acquire", str(exc), retryable=True) from exc
        raise
    returned = parse_artifact_digest(result)
    if returned != artifact_digest:
        raise RccRuntimeError("acquire", "RCC returned a different artifact identity")
    verification = result.get("verification")
    if not isinstance(verification, dict) or verification.get("valid") is not True:
        raise RccRuntimeError("acquire", "artifact verification is not valid")
    return result


def prepare_runtime(
    environment: Path,
    rcc_location: Path,
    *,
    environment_identity: Path | None = None,
    source_generation: str = "unknown",
    provider: str | None = None,
    previous_descriptor: RccRuntimeDescriptor | None = None,
    runner: Runner = _subprocess_runner,
) -> RccRuntimeDescriptor:
    environment = environment.resolve()
    cache_identity = (environment_identity or environment).resolve()
    provider = _validate_provider_reference(provider)
    environment_fingerprint = environment_spec_fingerprint(environment)
    compatibility = verify_rcc_compatibility(rcc_location)
    if (
        previous_descriptor is not None
        and previous_descriptor.rcc_compatibility != compatibility
    ):
        raise RccRuntimeError(
            "compatibility", "descriptor selection changed; reprepare required"
        )
    cache_key = (
        cache_identity,
        environment_fingerprint,
        provider,
        compatibility.compatibility_id if compatibility else None,
    )
    source_hash = source_generation
    if source_hash == "unknown":
        source_hash = hashlib.sha256(environment.read_bytes()).hexdigest()
    with _prepared_runtime_cache_lock:
        cached = _prepared_runtime_cache.get(cache_key)
    if (
        cached is not None
        and source_generation != "unknown"
        and source_generation != cached[1].source_generation
    ):
        cached_descriptor = cached[1]
        descriptor = RccRuntimeDescriptor(
            artifact_digest=cached_descriptor.artifact_digest,
            source_generation=source_generation,
            source_hash=source_hash,
            environment_fingerprint=environment_fingerprint,
            preparation_class="source-reuse",
            rcc_version=cached_descriptor.rcc_version,
            rcc_compatibility=compatibility,
            runtime_kind=cached_descriptor.runtime_kind,
            contract_version=cached_descriptor.contract_version,
            provider_reference=provider,
        )
        with _prepared_runtime_cache_lock:
            _prepared_runtime_cache[cache_key] = (environment_fingerprint, descriptor)
        return descriptor
    if (
        previous_descriptor is not None
        and previous_descriptor.environment_fingerprint == environment_fingerprint
    ):
        try:
            acquire_artifact(
                previous_descriptor.artifact_digest,
                rcc_location,
                provider=provider,
                runner=runner,
            )
        except RccRuntimeError as exc:
            if not exc.retryable:
                raise
            # The durable descriptor is an identity hint, not an activation
            # path.  If RCC cannot materialize that identity, publish a new
            # artifact and validate its exact identity before using it.
            digest = publish_artifact(
                environment, rcc_location, provider=provider, runner=runner
            )
            acquire_artifact(digest, rcc_location, provider=provider, runner=runner)
            descriptor = RccRuntimeDescriptor(
                artifact_digest=digest,
                source_generation=source_generation,
                source_hash=source_hash,
                environment_fingerprint=environment_fingerprint,
                preparation_class="rebuild",
                rcc_version=compatibility.version if compatibility else RCC_VERSION,
                rcc_compatibility=compatibility,
                provider_reference=provider,
            )
        else:
            descriptor = RccRuntimeDescriptor(
                artifact_digest=previous_descriptor.artifact_digest,
                source_generation=source_generation,
                source_hash=source_hash,
                environment_fingerprint=environment_fingerprint,
                preparation_class="warm-reuse",
                rcc_version=compatibility.version if compatibility else RCC_VERSION,
                rcc_compatibility=compatibility,
                provider_reference=provider,
            )
        with _prepared_runtime_cache_lock:
            _prepared_runtime_cache[cache_key] = (environment_fingerprint, descriptor)
        return descriptor
    if cached is not None:
        _, descriptor = cached
        try:
            acquire_artifact(
                descriptor.artifact_digest,
                rcc_location,
                provider=provider,
                runner=runner,
            )
        except RccRuntimeError as exc:
            if not exc.retryable:
                raise
            digest = publish_artifact(
                environment, rcc_location, provider=provider, runner=runner
            )
            acquire_artifact(digest, rcc_location, provider=provider, runner=runner)
            descriptor = RccRuntimeDescriptor(
                artifact_digest=digest,
                source_generation=source_generation,
                source_hash=source_hash,
                environment_fingerprint=environment_fingerprint,
                preparation_class="rebuild",
                rcc_version=compatibility.version if compatibility else RCC_VERSION,
                rcc_compatibility=compatibility,
                provider_reference=provider,
            )
            with _prepared_runtime_cache_lock:
                _prepared_runtime_cache[cache_key] = (
                    environment_fingerprint,
                    descriptor,
                )
            return descriptor
        return RccRuntimeDescriptor(
            artifact_digest=descriptor.artifact_digest,
            source_generation=source_generation,
            source_hash=source_hash,
            environment_fingerprint=environment_fingerprint,
            preparation_class=descriptor.preparation_class,
            rcc_version=descriptor.rcc_version,
            rcc_compatibility=compatibility,
            runtime_kind=descriptor.runtime_kind,
            contract_version=descriptor.contract_version,
            provider_reference=provider,
        )

    digest = publish_artifact(
        environment, rcc_location, provider=provider, runner=runner
    )
    acquire_artifact(digest, rcc_location, provider=provider, runner=runner)
    descriptor = RccRuntimeDescriptor(
        artifact_digest=digest,
        source_generation=source_generation,
        source_hash=source_hash,
        environment_fingerprint=environment_fingerprint,
        rcc_version=compatibility.version if compatibility else RCC_VERSION,
        rcc_compatibility=compatibility,
        provider_reference=provider,
    )
    with _prepared_runtime_cache_lock:
        _prepared_runtime_cache[cache_key] = (environment_fingerprint, descriptor)
    return descriptor


def build_exec_command(
    rcc_location: Path,
    descriptor: RccRuntimeDescriptor,
    command: Sequence[str],
    *,
    receipt_file: Path | None,
    json_output: bool = True,
) -> list[str]:
    if not descriptor.provider_context_bound:
        raise RccRuntimeError(
            "exec", "descriptor lacks provider trust context; reprepare required"
        )
    compatibility = verify_rcc_compatibility(rcc_location)
    if descriptor.rcc_compatibility != compatibility:
        raise RccRuntimeError(
            "compatibility", "descriptor selection changed; reprepare required"
        )
    args = [
        str(rcc_location),
        "env",
        "exec",
        "--artifact",
        descriptor.artifact_digest,
        "--permissive-local",
    ]
    if descriptor.provider_reference:
        args.extend(["--provider", descriptor.provider_reference])
    if receipt_file is None and json_output:
        args.append("--json")
    elif receipt_file is not None:
        args.extend(["--inherit-streams", "--receipt-file", str(receipt_file)])
    args.extend(["--", *map(str, command)])
    return args


def load_descriptor(env_json: str) -> RccRuntimeDescriptor | None:
    try:
        value = json.loads(env_json)
    except (TypeError, ValueError, json.JSONDecodeError):
        return None
    if isinstance(value, dict) and "runtime" in value:
        return RccRuntimeDescriptor.from_dict({"runtime": value["runtime"]})
    return None


class RccProcessHandle:
    """Own and reap one RCC env-exec wrapper."""

    def __init__(self, process: subprocess.Popen, receipt_file: Path):
        self.process = process
        self.receipt_file = receipt_file
        self._owned_processes: list[object] | None = None
        self._owned_snapshot_complete = False
        self.last_cleanup_result: ProcessTreeCleanupResult | None = None

    @property
    def pid(self) -> int:
        return self.process.pid

    def kill(self) -> None:
        if self.process.poll() is None:
            from actions.server._common.process import kill_process_and_subprocesses

            kill_process_and_subprocesses(self.process.pid)
        self.process.wait()

    def wait(self, timeout: float | None = None) -> int:
        return self.process.wait(timeout=timeout)

    def capture_owned_processes(self):
        """Snapshot wrapper descendants before terminal shutdown can reparent them."""
        from actions.server._common.process import snapshot_process_descendants

        self._owned_processes = snapshot_process_descendants(self.process.pid)
        self._owned_snapshot_complete = True
        return self._owned_processes

    def force_kill_until(self, deadline: float) -> ProcessTreeCleanupResult:
        """Force-stop the captured wrapper tree and wait within one deadline."""
        snapshot_error = None
        from actions.server._common.process import force_kill_process_tree_until

        if self._owned_processes is None:
            try:
                self.capture_owned_processes()
            except Exception as exc:
                self._owned_processes = []
                snapshot_error = f"descendant snapshot unavailable: {exc}"
        cleanup_result = force_kill_process_tree_until(
            self.process,
            self._owned_processes,
            deadline,
            snapshot_complete_before_call=self._owned_snapshot_complete,
        )
        if snapshot_error:
            cleanup_result = replace(
                cleanup_result,
                descendant_snapshot_complete=False,
                errors=(*cleanup_result.errors, snapshot_error),
            )
        self.last_cleanup_result = cleanup_result
        self._owned_snapshot_complete = cleanup_result.descendant_snapshot_complete
        return cleanup_result


def new_receipt_path(datadir: Path) -> Path:
    directory = datadir / "rcc-receipts"
    directory.mkdir(parents=True, exist_ok=True)
    return directory / f"{uuid.uuid4().hex}.json"


def get_rcc_location() -> Path:
    override = os.environ.get("ACTIONS_RUNTIME_RCC_BINARY")
    selector = os.environ.get("ACTIONS_RUNTIME_RCC_TEST_COMPATIBILITY")
    if selector is not None:
        approved_rcc_test_policy(selector)
        if not override:
            raise RccRuntimeError(
                "compatibility", "isolated selection requires an explicit executable"
            )
    if override:
        path = Path(override)
        if not path.is_file() or not os.access(path, os.X_OK):
            raise RccRuntimeError("rcc", "ACTIONS_RUNTIME_RCC_BINARY is not executable")
        verify_rcc_version(path)
        return path
    from actions.server._download_rcc import get_default_rcc_location

    path = get_default_rcc_location()
    if not path.is_file() or not os.access(path, os.X_OK):
        raise RccRuntimeError("rcc", f"RCC executable unavailable for {RCC_VERSION}")
    verify_rcc_version(path)
    return path


def verify_rcc_version(rcc_location: Path, runner: Runner = _subprocess_runner) -> str:
    """Require the selected executable to be the released RCC contract."""

    if os.environ.get("ACTIONS_RUNTIME_RCC_TEST_COMPATIBILITY") is not None:
        compatibility = verify_rcc_compatibility(
            rcc_location, runner=None if runner is _subprocess_runner else runner
        )
        assert compatibility is not None
        return compatibility.version
    code, stdout, stderr = runner(str(rcc_location), "--version")
    if code:
        raise RccRuntimeError("rcc", "unable to verify RCC version")
    version = stdout.strip()
    if version != RCC_VERSION:
        raise RccRuntimeError("rcc", f"unsupported RCC version {version!r}")
    return version


def read_receipt(receipt_file: Path, artifact_digest: str) -> dict:
    try:
        receipt = json.loads(receipt_file.read_text(encoding="utf-8"))
    except (OSError, ValueError, json.JSONDecodeError) as exc:
        raise RccRuntimeError("receipt", "missing or malformed RCC receipt") from exc
    if (
        not isinstance(receipt, dict)
        or receipt.get("artifactDigest") != artifact_digest
    ):
        raise RccRuntimeError("receipt", "receipt artifact identity mismatch")
    verification = receipt.get("verification")
    if not isinstance(verification, dict) or verification.get("valid") is not True:
        raise RccRuntimeError("receipt", "receipt verification is not valid")
    if not isinstance(receipt.get("leaseId"), str) or not receipt["leaseId"]:
        raise RccRuntimeError("receipt", "receipt lease identity is missing")
    return receipt
