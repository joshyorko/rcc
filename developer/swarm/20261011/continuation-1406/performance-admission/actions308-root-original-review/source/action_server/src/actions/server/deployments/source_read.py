"""Linux no-follow measurement of explicitly selected files below a borrowed FD.

The caller owns verification and authorization of the root directory descriptor.
Opened-object and parent/name comparisons reject observed mutations, but do not
establish an atomic tree snapshot, original root pathname, or selected-set
completeness. This boundary requires trusted Linux kernel procfs at
``/proc/self/fd`` to reopen owned, classified O_PATH descriptors. No source or
staging publication occurs here.
"""

from __future__ import annotations

import os
import stat
import sys
from contextlib import ExitStack, contextmanager
from dataclasses import dataclass
from typing import Iterable, Iterator

from actions.server.deployments import source_manifest

_READ_CHUNK_BYTES = 64 * 1024


@dataclass(frozen=True)
class ObjectObservation:
    """Metadata observed on an opened object, without an authorization claim."""

    device: int
    inode: int
    file_type: int
    mode: int
    size: int
    mtime_ns: int
    ctime_ns: int
    links: int


@dataclass(frozen=True)
class MeasuredFile:
    """Selected file and its root-to-parent directory observations."""

    path: str
    object: ObjectObservation
    directories: tuple[ObjectObservation, ...]


@dataclass(frozen=True)
class MeasuredSelectedFiles:
    """Bounded selected-file evidence and its policy-versioned proposal."""

    inventory: source_manifest.ProposedInventoryValidation
    root: ObjectObservation
    files: tuple[MeasuredFile, ...]


@dataclass(frozen=True)
class _OpenedPath:
    fd: int
    pinned_fd: int
    object: ObjectObservation
    directories: tuple[ObjectObservation, ...]


def read_selected_files(
    root_fd: int,
    selected_file_names: Iterable[str],
    *,
    protected_input_names: Iterable[str],
) -> MeasuredSelectedFiles:
    """Measure explicit portable files using a caller-verified Linux directory FD.

    Own only a duplicate of ``root_fd`` and traversal handles. Validate the entire
    name/protection proposal before reading content; recheck opened objects and
    their parent/name bindings during each read and in a final selected-path pass.
    Trusted Linux kernel procfs is required for reopening classified O_PATH
    leaves; there is no ordinary-path fallback. These observations cannot prove
    global atomic coherence against a writer.
    """
    _require_linux_descriptors()
    selected = tuple(_validated_names(selected_file_names, "entries"))
    protected = tuple(_validated_names(protected_input_names, "protected inputs"))
    source_manifest.validate_proposed_inventory(
        (
            source_manifest.SuppliedSourceEntry(name, "file", b"", 0o644)
            for name in selected
        ),
        protected_input_names=protected,
    )

    with ExitStack() as handles:
        owned_root = _own_fd(handles, os.dup(root_fd))
        root = _observation(os.fstat(owned_root))
        if root.file_type != stat.S_IFDIR:
            raise ValueError("source root descriptor must identify a directory")
        proc_fd = _open_proc_directory(handles)
        measured: list[MeasuredFile] = []
        total_bytes = 0

        def measured_entries() -> Iterator[source_manifest.SuppliedSourceEntry]:
            nonlocal total_bytes
            for name in selected:
                parts = source_manifest._validated_parts(name)
                with _opened_path(owned_root, proc_fd, parts) as opened:
                    content = _read_bytes(opened.fd, opened.object, total_bytes)
                total_bytes += len(content)
                measured.append(MeasuredFile(name, opened.object, opened.directories))
                yield source_manifest.SuppliedSourceEntry(
                    name, "file", content, opened.object.mode
                )

        inventory = source_manifest.validate_proposed_inventory(
            measured_entries(), protected_input_names=protected
        )
        for item in measured:
            parts = source_manifest._validated_parts(item.path)
            with _opened_path(owned_root, proc_fd, parts) as opened:
                if (
                    opened.object != item.object
                    or opened.directories != item.directories
                ):
                    raise ValueError("selected source object changed after measurement")
        if _observation(os.fstat(owned_root)) != root:
            raise ValueError("source root directory changed during measurement")
        return MeasuredSelectedFiles(inventory, root, tuple(measured))


def _require_linux_descriptors() -> None:
    if (
        sys.platform != "linux"
        or os.open not in os.supports_dir_fd
        or os.stat not in os.supports_dir_fd
        or os.stat not in os.supports_follow_symlinks
    ):
        raise NotImplementedError(
            "Linux no-follow directory descriptor support is required"
        )
    for name in ("O_DIRECTORY", "O_NOFOLLOW", "O_NONBLOCK", "O_CLOEXEC", "O_PATH"):
        _required_linux_flag(name)


def _validated_names(names: Iterable[str], label: str) -> Iterator[str]:
    for name in source_manifest._bounded_values(names, label):
        source_manifest._validated_parts(name)
        yield name


def _required_linux_flag(name: str) -> int:
    value = getattr(os, name, None)
    if isinstance(value, bool) or not isinstance(value, int) or value <= 0:
        raise NotImplementedError(
            "Linux no-follow directory descriptor support is required"
        )
    return value


def _own_fd(handles: ExitStack, fd: int) -> int:
    handles.callback(os.close, fd)
    return fd


def _open_proc_directory(handles: ExitStack) -> int:
    try:
        return _own_fd(
            handles,
            os.open(
                "/proc/self/fd",
                os.O_RDONLY
                | _required_linux_flag("O_DIRECTORY")
                | _required_linux_flag("O_NOFOLLOW")
                | _required_linux_flag("O_CLOEXEC"),
            ),
        )
    except OSError as exc:
        raise NotImplementedError(
            "trusted Linux /proc/self/fd support is required"
        ) from exc


def _observation(value: os.stat_result) -> ObjectObservation:
    return ObjectObservation(
        value.st_dev,
        value.st_ino,
        stat.S_IFMT(value.st_mode),
        stat.S_IMODE(value.st_mode),
        value.st_size,
        value.st_mtime_ns,
        value.st_ctime_ns,
        value.st_nlink,
    )


@contextmanager
def _opened_path(
    root_fd: int, proc_fd: int, parts: tuple[str, ...]
) -> Iterator[_OpenedPath]:
    with ExitStack() as handles:
        directory_handles = [root_fd]
        directories = [_observation(os.fstat(root_fd))]
        directory_flags = (
            os.O_RDONLY
            | _required_linux_flag("O_DIRECTORY")
            | _required_linux_flag("O_NOFOLLOW")
            | _required_linux_flag("O_CLOEXEC")
        )
        for part in parts[:-1]:
            fd = _own_fd(
                handles, os.open(part, directory_flags, dir_fd=directory_handles[-1])
            )
            observed = _observation(os.fstat(fd))
            if observed.file_type != stat.S_IFDIR:
                raise ValueError("source parent must be an opened directory")
            directory_handles.append(fd)
            directories.append(observed)

        pinned_fd = _own_fd(
            handles,
            os.open(
                parts[-1],
                _required_linux_flag("O_PATH")
                | _required_linux_flag("O_NOFOLLOW")
                | _required_linux_flag("O_CLOEXEC"),
                dir_fd=directory_handles[-1],
            ),
        )
        observed_file = _observation(os.fstat(pinned_fd))
        if observed_file.file_type != stat.S_IFREG or observed_file.links != 1:
            raise ValueError("selected source must be a regular file without hardlinks")
        if observed_file.mode & 0o7000:
            raise ValueError("privileged source file mode bits are forbidden")
        pinned = _OpenedPath(pinned_fd, pinned_fd, observed_file, tuple(directories))
        _verify_bindings(pinned, directory_handles, parts)
        try:
            fd = _own_fd(
                handles,
                os.open(
                    str(pinned_fd),
                    os.O_RDONLY
                    | _required_linux_flag("O_NONBLOCK")
                    | _required_linux_flag("O_CLOEXEC"),
                    dir_fd=proc_fd,
                ),
            )
        except OSError as exc:
            raise NotImplementedError(
                "trusted Linux procfd reopening is required"
            ) from exc
        opened = _OpenedPath(fd, pinned_fd, observed_file, tuple(directories))
        _verify_bindings(opened, directory_handles, parts)
        yield opened
        _verify_bindings(opened, directory_handles, parts)


def _verify_bindings(
    opened: _OpenedPath, directory_handles: list[int], parts: tuple[str, ...]
) -> None:
    for index, (fd, expected) in enumerate(zip(directory_handles, opened.directories)):
        if _observation(os.fstat(fd)) != expected:
            raise ValueError("source directory changed during measurement")
        if index:
            named = os.stat(
                parts[index - 1],
                dir_fd=directory_handles[index - 1],
                follow_symlinks=False,
            )
            if _observation(named) != expected:
                raise ValueError("source directory name binding changed")
    if (
        _observation(os.fstat(opened.fd)) != opened.object
        or _observation(os.fstat(opened.pinned_fd)) != opened.object
    ):
        raise ValueError("selected source file changed during measurement")
    named_file = os.stat(parts[-1], dir_fd=directory_handles[-1], follow_symlinks=False)
    if _observation(named_file) != opened.object:
        raise ValueError("selected source file name binding changed")


def _read_bytes(fd: int, observed: ObjectObservation, total_bytes: int) -> bytes:
    if observed.size > source_manifest.MAX_FILE_BYTES:
        raise ValueError("source file exceeds the byte limit")
    if observed.size > source_manifest.MAX_TOTAL_BYTES - total_bytes:
        raise ValueError("source inventory exceeds the total byte limit")
    content = bytearray()
    while True:
        allowance = min(
            _READ_CHUNK_BYTES,
            source_manifest.MAX_FILE_BYTES - len(content),
            source_manifest.MAX_TOTAL_BYTES - total_bytes - len(content),
        )
        chunk = os.read(fd, allowance + 1)
        if not chunk:
            break
        if len(content) + len(chunk) > source_manifest.MAX_FILE_BYTES:
            raise ValueError("source file exceeds the byte limit")
        if total_bytes + len(content) + len(chunk) > source_manifest.MAX_TOTAL_BYTES:
            raise ValueError("source inventory exceeds the total byte limit")
        content.extend(chunk)
    if len(content) != observed.size:
        raise ValueError("selected source file size changed during measurement")
    return bytes(content)
