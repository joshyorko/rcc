"""Validate a caller-supplied portable source inventory without reading files.

This pure policy boundary cannot prove how entries were acquired. In particular,
callers remain responsible for no-follow confinement, source mutation checks,
and accurate regular-file/link/hardlink/special-file classification.

Source policy version 1 requires already-NFC POSIX-relative portable paths,
explicit selected and protected inputs, and regular-file modes normalized to
0644 or 0755. Empty directories are omitted from the canonical inventory.
"""

from __future__ import annotations

import hashlib
import json
import unicodedata
from dataclasses import dataclass
from typing import Iterable, Iterator, TypeVar

from actions.server._portable_paths import (
    is_portable_path_component,
    portable_path_collision_key,
)
from actions.server.deployments.canonical import canonicalize_json

MAX_ENTRIES = 10_000
MAX_FILE_BYTES = 50 * 1024 * 1024
MAX_TOTAL_BYTES = 500 * 1024 * 1024
MAX_PATH_DEPTH = 64
MAX_PATH_BYTES = 4096
MAX_COMPONENT_BYTES = 255
SOURCE_POLICY_VERSION = 1
_T = TypeVar("_T")


@dataclass(frozen=True)
class SuppliedSourceEntry:
    """One explicit selected entry supplied by an acquisition boundary.

    File ``mode`` is the permission-only POSIX value, not a complete ``st_mode``.
    """

    path: str
    kind: str
    content: bytes | None = None
    mode: int | None = None


@dataclass(frozen=True)
class ProposedInventoryEntry:
    """Canonical file facts derived from one supplied regular-file byte string."""

    path: str
    mode: int
    size: int
    sha256: str


@dataclass(frozen=True)
class ProposedInventoryValidation:
    """Validated inventory proposal; it is not filesystem admission evidence."""

    entries: tuple[ProposedInventoryEntry, ...]
    canonical_json: bytes


def _validated_parts(path: str) -> tuple[str, ...]:
    if not isinstance(path, str) or not path or "\x00" in path:
        raise ValueError("source path must be non-empty text")
    try:
        path_bytes = path.encode("utf-8", errors="strict")
    except UnicodeEncodeError as exc:
        raise ValueError(
            "source path must contain valid Unicode scalar values"
        ) from exc
    if len(path_bytes) > MAX_PATH_BYTES:
        raise ValueError("source path exceeds the byte limit")
    if path.startswith("/") or "\\" in path:
        raise ValueError("source path must be relative POSIX text")

    parts = tuple(path.split("/"))
    if len(parts) > MAX_PATH_DEPTH:
        raise ValueError("source path exceeds the depth limit")
    for part in parts:
        if part in {"", ".", ".."}:
            raise ValueError("source path contains an empty or dot component")
        try:
            component_bytes = part.encode("utf-8", errors="strict")
        except UnicodeEncodeError as exc:
            raise ValueError(
                "source path must contain valid Unicode scalar values"
            ) from exc
        if len(component_bytes) > MAX_COMPONENT_BYTES:
            raise ValueError("source path component exceeds the byte limit")
        if not is_portable_path_component(part):
            raise ValueError("source path contains a non-portable component")
    if any(unicodedata.normalize("NFC", part) != part for part in parts):
        raise ValueError("source path component must be NFC")
    return parts


def validate_proposed_inventory(
    entries: Iterable[SuppliedSourceEntry],
    *,
    protected_input_names: Iterable[str],
) -> ProposedInventoryValidation:
    """Validate explicit entries and serialize a portable inventory proposal.

    The caller must supply the complete selected set and identify protected
    inputs explicitly. This function performs no filesystem access and makes
    no source snapshot, trust, Package Revision, or Runtime Plan claim.
    """
    declared: dict[str, str] = {}
    original_prefixes: dict[str, str] = {}
    path_kinds: dict[str, str] = {}
    files: list[ProposedInventoryEntry] = []
    total_bytes = 0

    for entry in _bounded_values(entries, "entries"):
        if not isinstance(entry, SuppliedSourceEntry):
            raise TypeError("entries must be SuppliedSourceEntry values")
        parts = _validated_parts(entry.path)
        if not isinstance(entry.kind, str) or entry.kind not in {"file", "directory"}:
            raise ValueError("source entry must be a regular file or directory")

        for index in range(1, len(parts) + 1):
            original_prefix = "/".join(parts[:index])
            collision_key = "/".join(
                portable_path_collision_key(part) for part in parts[:index]
            )
            previous = original_prefixes.get(collision_key)
            if previous is not None and previous != original_prefix:
                raise ValueError("source paths collide after portable case folding")
            original_prefixes[collision_key] = original_prefix

        if entry.path in declared:
            raise ValueError("source inventory contains a duplicate path")
        for index in range(1, len(parts)):
            ancestor = "/".join(parts[:index])
            if path_kinds.get(ancestor) == "file":
                raise ValueError("regular-file entry cannot be a directory prefix")
            path_kinds.setdefault(ancestor, "directory")
        if entry.kind == "file" and path_kinds.get(entry.path) == "directory":
            raise ValueError("regular-file entry cannot contain child paths")
        path_kinds[entry.path] = entry.kind
        declared[entry.path] = entry.kind

        if entry.kind == "directory":
            if entry.content is not None or entry.mode is not None:
                raise ValueError("directory entries cannot carry content or mode")
            continue

        if not isinstance(entry.content, bytes):
            raise ValueError("regular-file entries require supplied bytes")
        if (
            isinstance(entry.mode, bool)
            or not isinstance(entry.mode, int)
            or entry.mode < 0
            or entry.mode > 0o7777
        ):
            raise ValueError("regular-file mode must be POSIX permission bits")
        if entry.mode & 0o7000:
            raise ValueError("privileged file mode bits are forbidden")
        if len(entry.content) > MAX_FILE_BYTES:
            raise ValueError("source file exceeds the byte limit")
        total_bytes += len(entry.content)
        if total_bytes > MAX_TOTAL_BYTES:
            raise ValueError("source inventory exceeds the total byte limit")

        normalized_mode = 0o755 if entry.mode & 0o111 else 0o644
        files.append(
            ProposedInventoryEntry(
                path=entry.path,
                mode=normalized_mode,
                size=len(entry.content),
                sha256=hashlib.sha256(entry.content).hexdigest(),
            )
        )

    selected_files = {entry.path for entry in files}
    for protected_name in _bounded_values(protected_input_names, "protected inputs"):
        _validated_parts(protected_name)
        if protected_name not in selected_files:
            raise ValueError(
                "protected input must be an explicitly selected regular file"
            )

    files.sort(key=lambda entry: entry.path)
    canonical_bytes = canonicalize_json(_encode_inventory(tuple(files)))
    return ProposedInventoryValidation(tuple(files), canonical_bytes)


def _bounded_values(values: Iterable[_T], label: str) -> Iterator[_T]:
    if isinstance(values, (str, bytes)):
        raise TypeError(f"{label} must be an iterable of separate values")
    for count, value in enumerate(values):
        if count >= MAX_ENTRIES:
            raise ValueError(f"{label} exceed the entry limit")
        yield value


def _encode_inventory(entries: tuple[ProposedInventoryEntry, ...]) -> bytes:
    return json.dumps(
        {
            "sourcePolicyVersion": SOURCE_POLICY_VERSION,
            "entries": [
                {
                    "path": entry.path,
                    "mode": entry.mode,
                    "size": entry.size,
                    "sha256": entry.sha256,
                }
                for entry in entries
            ],
        },
        ensure_ascii=False,
        separators=(",", ":"),
    ).encode("utf-8")
