"""Stage explicitly selected source files beneath a borrowed private directory FD.

This boundary consumes the Linux confined measurement in ``source_read`` and
copies the same observed bytes into a private caller-owned tree. It does not
prove that the caller selected every relevant input, that the whole source was
an atomic snapshot, or that source is authorized/trusted. The staged inventory
is a proposal over bytes actually written and re-read, not a Package Revision.
"""

from __future__ import annotations

import hashlib
import os
import stat
import sys
from contextlib import ExitStack
from dataclasses import dataclass
from typing import Callable, Iterable

from actions.server.deployments import source_manifest, source_read

_COPY_CHUNK_BYTES = 64 * 1024


@dataclass(frozen=True)
class ProposedStagedInventory:
    """Proposed inventory of staged bytes plus their source observations.

    This value does not attest source authorization, selection completeness,
    global source snapshot consistency, admission, or compiler/runtime identity.
    """

    inventory: source_manifest.ProposedInventoryValidation
    source: source_read.MeasuredSelectedFiles


@dataclass(frozen=True)
class _CreatedDirectory:
    path: tuple[str, ...]
    parent_fd: int
    name: str
    fd: int | None
    device: int
    inode: int


@dataclass(frozen=True)
class _UnresolvedDirectory:
    path: tuple[str, ...]
    parent_fd: int
    name: str


@dataclass(frozen=True)
class _UnresolvedFile:
    path: str
    parent_fd: int
    name: str


@dataclass(frozen=True)
class _CreatedFile:
    path: str
    parts: tuple[str, ...]
    parent_fd: int
    name: str
    fd: int
    source_mode: int
    expected_size: int
    expected_sha256: str
    device: int
    inode: int
    final_observation: source_read.ObjectObservation | None


def stage_selected_files(
    source_root_fd: int,
    destination_fd: int,
    selected_file_names: Iterable[str],
    *,
    protected_input_names: Iterable[str],
) -> ProposedStagedInventory:
    """Measure and stage selected regular files without taking FD ownership.

    ``destination_fd`` must identify an empty, owner-private directory that the
    caller created for this operation, with exclusive write access for the
    duration of staging. Entries are created descriptor-relative and
    exclusively; destination contents are never overwritten. Only files and
    parent directories whose no-follow identities were observed by this call
    are eligible for failure cleanup. Directory creation and the following
    identity observation are not atomic, so hostile concurrent writers sharing
    the caller's effective UID are outside this boundary.
    The function requires the same Linux O_PATH and trusted ``/proc/self/fd``
    support as ``source_read`` and has no path-based fallback.
    """
    _require_staging_features()
    get_effective_uid = _required_linux_geteuid()
    set_file_mode = _required_linux_fchmod()
    selected = tuple(source_read._validated_names(selected_file_names, "entries"))
    protected = tuple(
        source_read._validated_names(protected_input_names, "protected inputs")
    )
    # Reject unsafe names and incomplete protected-input declarations before
    # inspecting source bytes or creating destination entries.
    source_manifest.validate_proposed_inventory(
        (
            source_manifest.SuppliedSourceEntry(name, "file", b"", 0o644)
            for name in selected
        ),
        protected_input_names=protected,
    )

    with ExitStack() as handles:
        owned_destination = source_read._own_fd(handles, os.dup(destination_fd))
        destination_stat = os.fstat(owned_destination)
        if stat.S_IFMT(destination_stat.st_mode) != stat.S_IFDIR:
            raise ValueError("staging destination descriptor must identify a directory")
        if (
            destination_stat.st_uid != get_effective_uid()
            or stat.S_IMODE(destination_stat.st_mode) & 0o700 != 0o700
            or stat.S_IMODE(destination_stat.st_mode) & 0o077
        ):
            raise ValueError("staging destination must be owner-private")
        try:
            with os.scandir(owned_destination) as entries:
                if next(entries, None) is not None:
                    raise ValueError("staging destination must be empty")
        except OSError as exc:
            raise ValueError(
                "staging destination descriptor must permit directory listing"
            ) from exc
        destination_identity = (
            destination_stat.st_dev,
            destination_stat.st_ino,
            stat.S_IFMT(destination_stat.st_mode),
            stat.S_IMODE(destination_stat.st_mode),
        )

        measured = source_read.read_selected_files(
            source_root_fd,
            selected,
            protected_input_names=protected,
        )
        measured_by_path = {entry.path: entry for entry in measured.inventory.entries}
        measured_files = {entry.path: entry for entry in measured.files}
        total_bytes = 0
        created_directories: list[_CreatedDirectory] = []
        unresolved_directories: list[_UnresolvedDirectory] = []
        created_files: list[_CreatedFile] = []
        unresolved_files: list[_UnresolvedFile] = []
        directory_fds: dict[tuple[str, ...], int] = {(): owned_destination}

        try:
            source_root = source_read._own_fd(handles, os.dup(source_root_fd))
            if source_read._observation(os.fstat(source_root)) != measured.root:
                raise ValueError("source root changed after measurement")
            proc_fd = source_read._open_proc_directory(handles)

            for path in selected:
                item = measured_by_path[path]
                observed_file = measured_files[path]
                parts = source_manifest._validated_parts(path)
                with source_read._opened_path(source_root, proc_fd, parts) as opened:
                    if (
                        opened.object != observed_file.object
                        or opened.directories != observed_file.directories
                    ):
                        raise ValueError("selected source changed after measurement")
                    parent_fd = _ensure_parent_directories(
                        parts[:-1],
                        directory_fds,
                        created_directories,
                        unresolved_directories,
                        handles,
                    )
                    destination_file = _create_file(parts[-1], parent_fd, handles)
                    pending_file = _UnresolvedFile(path, parent_fd, parts[-1])
                    unresolved_files.append(pending_file)
                    created_at_open_stat = os.fstat(destination_file)
                    created_files.append(
                        _CreatedFile(
                            path,
                            parts,
                            parent_fd,
                            parts[-1],
                            destination_file,
                            item.mode,
                            item.size,
                            item.sha256,
                            created_at_open_stat.st_dev,
                            created_at_open_stat.st_ino,
                            None,
                        )
                    )
                    unresolved_files.remove(pending_file)
                    created_at_open = source_read._observation(created_at_open_stat)
                    if (
                        created_at_open.file_type != stat.S_IFREG
                        or created_at_open.links != 1
                    ):
                        raise ValueError("staged file is not a private regular file")
                    digest, size = _copy_source_bytes(
                        opened.fd,
                        destination_file,
                        item.size,
                        item.sha256,
                        total_bytes,
                    )
                    total_bytes += size
                    set_file_mode(destination_file, item.mode)
                    os.fsync(destination_file)
                    staged_observation = source_read._observation(
                        os.fstat(destination_file)
                    )
                    if (
                        staged_observation.file_type != stat.S_IFREG
                        or staged_observation.links != 1
                        or staged_observation.size != size
                        or staged_observation.mode != item.mode
                    ):
                        raise ValueError("staged source file changed during writing")
                    created_files[-1] = _CreatedFile(
                        path,
                        parts,
                        parent_fd,
                        parts[-1],
                        destination_file,
                        item.mode,
                        size,
                        digest,
                        created_at_open.device,
                        created_at_open.inode,
                        staged_observation,
                    )

            staged_inventory = _verify_staged_tree(
                owned_destination,
                destination_identity,
                directory_fds,
                created_directories,
                created_files,
            )
            if staged_inventory != measured.inventory:
                raise ValueError("staged bytes differ from measured source inventory")
            _verify_source_observations(source_root, proc_fd, measured)
            _verify_staged_bindings(
                owned_destination,
                destination_identity,
                directory_fds,
                created_directories,
                created_files,
            )
            return ProposedStagedInventory(staged_inventory, measured)
        except BaseException as error:
            try:
                unresolved = _remove_owned_entries(
                    created_files,
                    created_directories,
                    unresolved_directories,
                    unresolved_files,
                )
            except BaseException as cleanup_error:
                error.add_note(
                    "staging cleanup itself failed while handling the original "
                    f"error: {cleanup_error!r}"
                )
            else:
                for path in unresolved:
                    error.add_note(
                        "staging cleanup could not verify or remove task-created "
                        f"entry {path!r}; cleanup outcome is unknown"
                    )
            raise


def _require_staging_features() -> None:
    source_read._require_linux_descriptors()
    if (
        sys.platform != "linux"
        or os.scandir not in os.supports_fd
        or any(
            operation not in os.supports_dir_fd
            for operation in (os.mkdir, os.open, os.stat, os.unlink, os.rmdir)
        )
    ):
        raise NotImplementedError(
            "Linux descriptor-relative private staging support is required"
        )
    source_read._required_linux_flag("O_DIRECTORY")
    source_read._required_linux_flag("O_NOFOLLOW")
    source_read._required_linux_flag("O_CLOEXEC")
    source_read._required_linux_flag("O_CREAT")
    source_read._required_linux_flag("O_EXCL")
    source_read._required_linux_flag("O_RDWR")


def _required_linux_geteuid() -> Callable[[], int]:
    if sys.platform != "linux":
        raise NotImplementedError("Linux staging support is required")
    operation = getattr(os, "geteuid", None)
    if not callable(operation):
        raise NotImplementedError("Linux effective-user identification is required")
    return operation


def _required_linux_fchmod() -> Callable[[int, int], None]:
    if sys.platform != "linux":
        raise NotImplementedError("Linux staging support is required")
    operation = getattr(os, "fchmod", None)
    if not callable(operation):
        raise NotImplementedError("Linux descriptor-based mode setting is required")
    return operation


def _ensure_parent_directories(
    parts: tuple[str, ...],
    directory_fds: dict[tuple[str, ...], int],
    created_directories: list[_CreatedDirectory],
    unresolved_directories: list[_UnresolvedDirectory],
    handles: ExitStack,
) -> int:
    path: tuple[str, ...] = ()
    for part in parts:
        parent_fd = directory_fds[path]
        path = (*path, part)
        if path not in directory_fds:
            os.mkdir(part, mode=0o700, dir_fd=parent_fd)
            pending = _UnresolvedDirectory(path, parent_fd, part)
            unresolved_directories.append(pending)
            named = os.stat(part, dir_fd=parent_fd, follow_symlinks=False)
            if stat.S_IFMT(named.st_mode) != stat.S_IFDIR:
                raise ValueError("created staging parent is not a directory")
            created = _CreatedDirectory(
                path, parent_fd, part, None, named.st_dev, named.st_ino
            )
            created_directories.append(created)
            unresolved_directories.remove(pending)
            fd = source_read._own_fd(
                handles,
                os.open(
                    part,
                    os.O_RDONLY
                    | source_read._required_linux_flag("O_DIRECTORY")
                    | source_read._required_linux_flag("O_NOFOLLOW")
                    | source_read._required_linux_flag("O_CLOEXEC"),
                    dir_fd=parent_fd,
                ),
            )
            observed = source_read._observation(os.fstat(fd))
            rebound = os.stat(part, dir_fd=parent_fd, follow_symlinks=False)
            identity = (created.device, created.inode)
            if (
                observed.file_type != stat.S_IFDIR
                or (observed.device, observed.inode) != identity
                or stat.S_IFMT(rebound.st_mode) != stat.S_IFDIR
                or (rebound.st_dev, rebound.st_ino) != identity
            ):
                raise ValueError("created staging parent binding changed")
            directory_fds[path] = fd
            created_directories[-1] = _CreatedDirectory(
                path, parent_fd, part, fd, observed.device, observed.inode
            )
        else:
            fd = directory_fds[path]
            named = os.stat(part, dir_fd=parent_fd, follow_symlinks=False)
            if stat.S_IFMT(named.st_mode) != stat.S_IFDIR or (
                named.st_dev,
                named.st_ino,
            ) != (os.fstat(fd).st_dev, os.fstat(fd).st_ino):
                raise ValueError("staging directory name binding changed")
    return directory_fds[parts]


def _create_file(name: str, parent_fd: int, handles: ExitStack) -> int:
    flags = (
        source_read._required_linux_flag("O_RDWR")
        | source_read._required_linux_flag("O_CREAT")
        | source_read._required_linux_flag("O_EXCL")
        | source_read._required_linux_flag("O_NOFOLLOW")
        | source_read._required_linux_flag("O_CLOEXEC")
    )
    return source_read._own_fd(
        handles,
        os.open(name, flags, 0o600, dir_fd=parent_fd),
    )


def _copy_source_bytes(
    source_fd: int,
    destination_fd: int,
    expected_size: int,
    expected_sha256: str,
    total_bytes: int,
) -> tuple[str, int]:
    if expected_size > source_manifest.MAX_FILE_BYTES:
        raise ValueError("source file exceeds the byte limit")
    if expected_size > source_manifest.MAX_TOTAL_BYTES - total_bytes:
        raise ValueError("source inventory exceeds the total byte limit")
    digest = hashlib.sha256()
    size = 0
    while True:
        allowance = min(
            _COPY_CHUNK_BYTES,
            source_manifest.MAX_FILE_BYTES - size,
            source_manifest.MAX_TOTAL_BYTES - total_bytes - size,
        )
        chunk = os.read(source_fd, allowance + 1)
        if not chunk:
            break
        size += len(chunk)
        if size > source_manifest.MAX_FILE_BYTES:
            raise ValueError("source file exceeds the byte limit")
        if total_bytes + size > source_manifest.MAX_TOTAL_BYTES:
            raise ValueError("source inventory exceeds the total byte limit")
        _write_all(destination_fd, chunk)
        digest.update(chunk)
    actual_digest = digest.hexdigest()
    if size != expected_size or actual_digest != expected_sha256:
        raise ValueError("selected source bytes changed after measurement")
    return actual_digest, size


def _write_all(fd: int, content: bytes) -> None:
    view = memoryview(content)
    while view:
        written = os.write(fd, view)
        if written <= 0:
            raise OSError("staging write made no progress")
        view = view[written:]


def _verify_source_observations(
    root_fd: int,
    proc_fd: int,
    measured: source_read.MeasuredSelectedFiles,
) -> None:
    for item in measured.files:
        parts = source_manifest._validated_parts(item.path)
        with source_read._opened_path(root_fd, proc_fd, parts) as opened:
            if opened.object != item.object or opened.directories != item.directories:
                raise ValueError("selected source changed during staging")
    if source_read._observation(os.fstat(root_fd)) != measured.root:
        raise ValueError("source root changed during staging")


def _verify_staged_tree(
    destination_fd: int,
    destination_identity: tuple[int, int, int, int],
    directory_fds: dict[tuple[str, ...], int],
    created_directories: list[_CreatedDirectory],
    created_files: list[_CreatedFile],
) -> source_manifest.ProposedInventoryValidation:
    _verify_staged_bindings(
        destination_fd,
        destination_identity,
        directory_fds,
        created_directories,
        created_files,
    )

    inventory_entries: list[source_manifest.ProposedInventoryEntry] = []
    for item in created_files:
        if item.final_observation is None:
            raise ValueError("staged file did not reach verification state")
        before = source_read._observation(os.fstat(item.fd))
        named = os.stat(item.name, dir_fd=item.parent_fd, follow_symlinks=False)
        if (
            before != item.final_observation
            or source_read._observation(named) != item.final_observation
            or before.file_type != stat.S_IFREG
            or before.links != 1
        ):
            raise ValueError("staged file binding changed before verification")
        os.lseek(item.fd, 0, os.SEEK_SET)
        digest = hashlib.sha256()
        size = 0
        while True:
            allowance = min(
                _COPY_CHUNK_BYTES,
                source_manifest.MAX_FILE_BYTES - size,
                source_manifest.MAX_TOTAL_BYTES - size,
            )
            chunk = os.read(item.fd, allowance + 1)
            if not chunk:
                break
            size += len(chunk)
            if size > item.expected_size or size > source_manifest.MAX_FILE_BYTES:
                raise ValueError("staged file exceeds its measured size")
            digest.update(chunk)
        after = source_read._observation(os.fstat(item.fd))
        named_after = os.stat(item.name, dir_fd=item.parent_fd, follow_symlinks=False)
        actual_digest = digest.hexdigest()
        if (
            after != before
            or source_read._observation(named_after) != after
            or size != item.expected_size
            or actual_digest != item.expected_sha256
        ):
            raise ValueError("staged source bytes changed during verification")
        inventory_entries.append(
            source_manifest.ProposedInventoryEntry(
                item.path,
                item.source_mode,
                size,
                actual_digest,
            )
        )

    entries = tuple(sorted(inventory_entries, key=lambda entry: entry.path))
    return source_manifest.ProposedInventoryValidation(
        entries,
        source_manifest.canonicalize_json(source_manifest._encode_inventory(entries)),
    )


def _verify_staged_bindings(
    destination_fd: int,
    destination_identity: tuple[int, int, int, int],
    directory_fds: dict[tuple[str, ...], int],
    created_directories: list[_CreatedDirectory],
    created_files: list[_CreatedFile],
) -> None:
    _verify_destination_identity(destination_fd, destination_identity)
    expected_children: dict[tuple[str, ...], set[str]] = {(): set()}
    for created_file in created_files:
        parent = created_file.parts[:-1]
        expected_children.setdefault(parent, set()).add(created_file.name)
        for depth in range(1, len(parent) + 1):
            directory_prefix = parent[:depth]
            expected_children.setdefault(directory_prefix, set())
            expected_children.setdefault(directory_prefix[:-1], set()).add(
                directory_prefix[-1]
            )
    for directory_path, directory_fd in directory_fds.items():
        if not _directory_entries_equal(
            directory_fd, expected_children.get(directory_path, set())
        ):
            raise ValueError("staging directory entries changed during copy")
    for created_directory in created_directories:
        if created_directory.fd is None:
            raise ValueError("created staging directory was not opened")
        opened_dir_stat = os.fstat(created_directory.fd)
        named = os.stat(
            created_directory.name,
            dir_fd=created_directory.parent_fd,
            follow_symlinks=False,
        )
        if (
            stat.S_IFMT(opened_dir_stat.st_mode) != stat.S_IFDIR
            or stat.S_IFMT(named.st_mode) != stat.S_IFDIR
            or (opened_dir_stat.st_dev, opened_dir_stat.st_ino)
            != (created_directory.device, created_directory.inode)
            or (named.st_dev, named.st_ino)
            != (created_directory.device, created_directory.inode)
        ):
            raise ValueError("staging directory binding changed")
    for created_file in created_files:
        expected = created_file.final_observation
        if expected is None:
            raise ValueError("staged file did not reach verification state")
        opened_file_observation = source_read._observation(os.fstat(created_file.fd))
        named = os.stat(
            created_file.name,
            dir_fd=created_file.parent_fd,
            follow_symlinks=False,
        )
        if (
            opened_file_observation != expected
            or source_read._observation(named) != expected
        ):
            raise ValueError("staged file binding changed")


def _verify_destination_identity(
    destination_fd: int, expected: tuple[int, int, int, int]
) -> None:
    observed = os.fstat(destination_fd)
    actual = (
        observed.st_dev,
        observed.st_ino,
        stat.S_IFMT(observed.st_mode),
        stat.S_IMODE(observed.st_mode),
    )
    if actual != expected or actual[2] != stat.S_IFDIR:
        raise ValueError("staging destination changed during copy")


def _directory_entries_equal(fd: int, expected: set[str]) -> bool:
    found: set[str] = set()
    with os.scandir(fd) as entries:
        for entry in entries:
            if entry.name not in expected or entry.name in found:
                return False
            found.add(entry.name)
            if len(found) > source_manifest.MAX_ENTRIES:
                return False
    return found == expected


def _remove_owned_entries(
    created_files: list[_CreatedFile],
    created_directories: list[_CreatedDirectory],
    unresolved_directories: list[_UnresolvedDirectory],
    unresolved_files: list[_UnresolvedFile],
) -> list[str]:
    unresolved = ["/".join(item.path) for item in unresolved_directories]
    unresolved.extend(item.path for item in unresolved_files)
    for created_file in reversed(created_files):
        try:
            current = os.stat(
                created_file.name,
                dir_fd=created_file.parent_fd,
                follow_symlinks=False,
            )
        except OSError:
            unresolved.append(created_file.path)
            continue
        if stat.S_IFMT(current.st_mode) == stat.S_IFREG and (
            current.st_dev,
            current.st_ino,
        ) == (created_file.device, created_file.inode):
            try:
                os.unlink(created_file.name, dir_fd=created_file.parent_fd)
            except OSError:
                unresolved.append(created_file.path)
                continue
        else:
            unresolved.append(created_file.path)
    for created_directory in reversed(created_directories):
        try:
            current = os.stat(
                created_directory.name,
                dir_fd=created_directory.parent_fd,
                follow_symlinks=False,
            )
        except OSError:
            unresolved.append("/".join(created_directory.path))
            continue
        if stat.S_IFMT(current.st_mode) != stat.S_IFDIR or (
            current.st_dev,
            current.st_ino,
        ) != (created_directory.device, created_directory.inode):
            unresolved.append("/".join(created_directory.path))
            continue
        try:
            os.rmdir(created_directory.name, dir_fd=created_directory.parent_fd)
        except OSError:
            # A concurrent/unowned entry makes a created directory non-empty;
            # leave it in place rather than deleting contents we did not make.
            unresolved.append("/".join(created_directory.path))
            continue
    return unresolved
