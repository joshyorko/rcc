import hashlib
import logging
import shutil
import stat
import uuid
from pathlib import Path
from typing import Callable

log = logging.getLogger(__name__)


def _raise_deprecated_conda(found: Path):
    from actions.server._settings import is_frozen
    from actions.server.vendored_deps.action_package_handling.cli_errors import (
        ActionPackageError,
    )

    if is_frozen():
        cmd = "action-server"
    else:
        cmd = "python -m actions.server"
    raise ActionPackageError(
        "Deprecated: The file for defining the environment is now `package.yaml`.\n"
        f"Using {found} is no longer supported.\n"
        "It's not a one to one mapping for but\n"
        f"`{cmd} package update` can be used to make most of the needed changes.\n"
        "See: https://github.com/Sema4AI/actions/blob/master/action_server/docs/guides/01-package-yaml.md for more details."
    )


class ActionPackageHandler:
    def __init__(self, action_package_dir: str, datadir: Path):
        import os

        import yaml

        from actions.server.vendored_deps.action_package_handling.cli_errors import (
            ActionPackageError,
        )

        from ._errors_action_server import ActionServerValidationError
        from ._settings import is_frozen

        self._action_package_dir = action_package_dir

        datadir = datadir.absolute()
        import_path = Path(action_package_dir).absolute()
        if not import_path.exists():
            raise ActionPackageError(
                f"Unable to import action package from directory: {import_path} "
                "(directory does not exist).",
            )
        if not import_path.is_dir():
            raise ActionPackageError(
                f"Error: expected {import_path} to be a directory."
            )

        self._datadir = datadir

        # Verify if it's actually a proper package (meaning that it has
        # the package.yaml as well as actions we can run).
        original_package_yaml = import_path / "package.yaml"

        action_package_name = ""
        package_yaml_exists = original_package_yaml.exists()
        package_yaml_contents = None
        if package_yaml_exists:
            try:
                with open(original_package_yaml, "r", encoding="utf-8") as stream:
                    package_yaml_contents = yaml.safe_load(stream)
            except Exception:
                raise ActionPackageError(
                    f"Error loading file as yaml ({original_package_yaml})."
                )
            if not isinstance(package_yaml_contents, dict):
                raise ActionPackageError(
                    f"Error: expected {original_package_yaml} to have a dictionary as top-level."
                )

            version = package_yaml_contents.get("version")
            if version is None:
                log.warn(
                    f"Expected {original_package_yaml} to have a 'version' field (without a version it's not possible to publish the action package)."
                )

            n = package_yaml_contents.get("name")
            if n:
                action_package_name = n
            else:
                log.warn(
                    f"Expected {original_package_yaml} to have a 'name' field (using the directory name as the action package name instead)."
                )

        if not action_package_name:
            action_package_name = import_path.name

        if not package_yaml_exists:
            for yaml_name in ("action-server.yaml", "conda.yaml"):
                if (import_path / yaml_name).exists():
                    _raise_deprecated_conda(import_path / yaml_name)

            if is_frozen() and not os.environ.get(
                "SEMA4AI_INTEGRATION_TEST_ACTION_SERVER_EXECUTABLE"
            ):
                raise ActionServerValidationError(
                    f"Unable to proceed because `package.yaml` is not available at: {original_package_yaml}."
                )

        self._package_yaml_exists = package_yaml_exists
        self._action_package_name = action_package_name
        self._original_package_yaml = original_package_yaml
        self._import_path = import_path
        self._package_yaml_contents = package_yaml_contents
        self._pythonpath_entries: tuple[str, ...] | None = None
        self._runtime_source_snapshot_package_yaml: Path | None = None
        self._runtime_source_snapshot_validation: Callable[[], None] | None = None

    @property
    def package_yaml_contents(self) -> dict | None:
        return self._package_yaml_contents

    @property
    def import_path(self) -> Path:
        return self._import_path

    @property
    def original_package_yaml(self) -> Path:
        return self._original_package_yaml

    @property
    def package_yaml_exists(self) -> bool:
        return self._package_yaml_exists

    @property
    def action_package_name(self) -> str:
        return self._action_package_name

    @property
    def package_root(self) -> str:
        return str(self._import_path.absolute())

    @property
    def uses_runtime_source_snapshots(self) -> bool:
        # Catalog rollback cannot preserve executable last-good behavior if a
        # worker later reopens the caller's edited directory. This applies to
        # ordinary/legacy packages as well as the RCC artifact adapter.
        return True

    def create_runtime_source_snapshot(self) -> tuple[Path, bool]:
        """Create a service-owned copy of one admitted package source generation."""
        from actions.server_integration import DEFAULT_EXCLUSION_PATTERNS

        from actions.server._errors_action_server import ActionServerValidationError
        from actions.server.package.package_exclude import PackageExcludeHandler

        source_root = self._import_path.resolve(strict=True)
        source_store = self._datadir / ".rcc-runtime-sources"
        package_key = hashlib.sha256(
            self._action_package_name.encode("utf-8")
        ).hexdigest()
        exclusions = PackageExcludeHandler()
        exclusions.fill_exclude_patterns(DEFAULT_EXCLUSION_PATTERNS)
        exclusions.exclude_patterns.extend(
            ("**/.rcc-action-version-*", "**/.rcc-action-metadata-*.json")
        )
        # Runtime state is not authored package source. In particular, a
        # datadir nested below the package must not copy databases, credentials
        # or Run artifacts into each successive source generation.
        excluded_roots: set[Path] = set()
        runtime_paths = [source_store]
        datadir = self._datadir.resolve()
        if datadir != source_root:
            runtime_paths.append(datadir)
        else:
            # --datadir=. is a supported layout. Keep the authored files, but
            # exclude the Runtime-owned entries instead of the entire root.
            runtime_paths.extend(
                datadir / name
                for name in (
                    "server.db",
                    "server.db-wal",
                    "server.db-shm",
                    "server.db-journal",
                    "workitems.db",
                    "workitems.db-wal",
                    "workitems.db-shm",
                    "workitems.db-journal",
                    "artifacts",
                    "work_item_files",
                    "secrets",
                    "env-info",
                    "rcc-receipts",
                    "tmpdir",
                    "server_log.txt",
                    "action_server.lock",
                    ".api_key",
                )
            )
        from ._settings import get_settings

        try:
            settings = get_settings()
        except AssertionError:
            settings = None
        if settings is not None and settings.datadir.resolve() == datadir:
            if settings.db_file != ":memory:":
                database_path = datadir / settings.db_file
                runtime_paths.extend(
                    Path(str(database_path) + suffix)
                    for suffix in ("", "-wal", "-shm", "-journal")
                )
            runtime_paths.extend(
                path
                for path in (settings.artifacts_dir, settings.artifact_storage_root)
                if path is not None
            )
        for path in runtime_paths:
            try:
                excluded_relative = path.resolve().relative_to(source_root)
            except ValueError:
                continue
            if excluded_relative != Path("."):
                excluded_roots.add(excluded_relative)

        def is_runtime_state(relative: Path) -> bool:
            return any(
                relative == excluded or excluded in relative.parents
                for excluded in excluded_roots
            )

        def source_files(root: Path) -> list[tuple[Path, str]]:
            files = sorted(
                (
                    (path, Path(relative).as_posix())
                    for path, relative in exclusions.collect_files_excluding_patterns(
                        root
                    )
                    if not is_runtime_state(Path(relative))
                ),
                key=lambda item: item[1],
            )
            for path, _ in files:
                if path.is_symlink():
                    resolved = path.resolve(strict=True)
                    if not resolved.is_relative_to(root) or not resolved.is_file():
                        raise ActionServerValidationError(
                            "RCC source snapshots require package file links to remain inside the package"
                        )
            for path in root.rglob("*"):
                if (
                    not is_runtime_state(path.relative_to(root))
                    and path.is_symlink()
                    and path.is_dir()
                ):
                    raise ActionServerValidationError(
                        "RCC source snapshots do not support directory symlinks"
                    )
            return files

        def signature(root: Path) -> str:
            digest = hashlib.sha256()
            for path, relative in source_files(root):
                relative_bytes = relative.encode("utf-8")
                payload = path.read_bytes()
                mode = stat.S_IMODE(path.stat().st_mode)
                digest.update(len(relative_bytes).to_bytes(8, "big"))
                digest.update(relative_bytes)
                digest.update(mode.to_bytes(4, "big"))
                digest.update(len(payload).to_bytes(8, "big"))
                digest.update(payload)
            return digest.hexdigest()

        files_before = source_files(source_root)
        if self.package_yaml_exists and not any(
            relative == "package.yaml" for _, relative in files_before
        ):
            raise ActionServerValidationError(
                "RCC source snapshot is missing package.yaml"
            )
        source_signature = signature(source_root)
        # Keep both complete identities in one digest. Two nested SHA-256
        # directory names exhaust CreateProcess's cwd budget on ordinary
        # Windows datadirs, even when Python can copy the files successfully.
        generation_key = hashlib.sha256(
            f"{package_key}:{source_signature}".encode("ascii")
        ).hexdigest()
        destination = source_store / generation_key

        def validate_selected_snapshot() -> None:
            if signature(destination) != source_signature:
                raise ActionServerValidationError(
                    "Package source snapshot changed during environment or metadata collection"
                )

        self._runtime_source_snapshot_validation = validate_selected_snapshot
        if destination.is_dir():
            if signature(destination) != source_signature:
                raise ActionServerValidationError(
                    "existing RCC source snapshot does not match its identity"
                )
            return destination, False

        source_store.mkdir(parents=True, exist_ok=True, mode=0o700)
        staging = source_store / f".staging-{uuid.uuid4().hex}"
        staging.mkdir(mode=0o700)
        try:
            for source, relative in files_before:
                target = staging / relative
                target.parent.mkdir(parents=True, exist_ok=True)
                shutil.copy2(source, target)
            if signature(source_root) != source_signature:
                raise ActionServerValidationError(
                    "RCC package source changed while its generation was being snapshotted"
                )
            if signature(staging) != source_signature:
                raise ActionServerValidationError(
                    "RCC source snapshot differs from the selected package generation"
                )
            try:
                staging.rename(destination)
            except FileExistsError:
                if signature(destination) != source_signature:
                    raise ActionServerValidationError(
                        "concurrent RCC source snapshot has a different identity"
                    )
                shutil.rmtree(staging)
                return destination, False
            return destination, True
        except BaseException:
            shutil.rmtree(staging, ignore_errors=True)
            raise

    def use_runtime_source_snapshot(self, snapshot: Path) -> None:
        """Point metadata collection and the persisted ActionPackage at a snapshot."""
        import yaml

        snapshot_package_yaml = snapshot / "package.yaml"
        try:
            snapshot_yaml = (
                yaml.safe_load(snapshot_package_yaml.read_text(encoding="utf-8"))
                if snapshot_package_yaml.exists()
                else None
            )
        except (OSError, yaml.YAMLError) as exc:
            from actions.server._errors_action_server import ActionServerValidationError

            raise ActionServerValidationError(
                "RCC source snapshot has an unreadable package.yaml"
            ) from exc
        if snapshot_yaml != self._package_yaml_contents:
            from actions.server._errors_action_server import ActionServerValidationError

            raise ActionServerValidationError(
                "package.yaml changed while creating the RCC source snapshot"
            )
        self._runtime_source_snapshot_package_yaml = (
            snapshot_package_yaml if self.package_yaml_exists else None
        )
        self._action_package_dir = str(snapshot)
        self._import_path = snapshot
        self._pythonpath_entries = None

    def prepare_runtime_source_snapshot(self) -> tuple[Path, bool]:
        """Create and validate a snapshot, discarding only a failed new candidate."""
        snapshot, created = self.create_runtime_source_snapshot()
        try:
            self.use_runtime_source_snapshot(snapshot)
        except BaseException:
            if created:
                self.discard_runtime_source_snapshot(snapshot)
            raise
        return snapshot, created

    def validate_runtime_source_snapshot(self) -> None:
        """Reject metadata collection which altered the selected source bytes."""
        if self._runtime_source_snapshot_validation is not None:
            self._runtime_source_snapshot_validation()

    def discard_runtime_source_snapshot(self, snapshot: Path) -> None:
        """Remove a newly created candidate after its import transaction fails."""
        import shutil

        source_store = (self._datadir / ".rcc-runtime-sources").resolve()
        candidate = snapshot.resolve()
        try:
            candidate.relative_to(source_store)
        except ValueError:
            return
        shutil.rmtree(candidate, ignore_errors=True)

    def get_pythonpath_entries(self) -> tuple[str, ...]:
        """
        Returns:
            Tuple with the entries of the pythonpath (as the user entered them in the package.yaml,
            usually relative to the package.yaml file, although absolute paths are also accepted).
        """
        if self._pythonpath_entries is not None:
            return self._pythonpath_entries

        from actions.server._errors_action_server import ActionServerValidationError

        pythonpath_entries: list[str] = []
        if not self._package_yaml_contents:
            pythonpath_entries.append(".")
        else:
            # Extract the PYTHONPATH from the package.yaml contents.
            pythonpath_in_yaml = self._package_yaml_contents.get("pythonpath")
            if not pythonpath_in_yaml:
                pythonpath_entries.append(".")
            else:
                # Validate that the pythonpath is a list of strings.
                if not isinstance(pythonpath_in_yaml, list):
                    raise ActionServerValidationError(
                        f"The 'pythonpath' field in package.yaml must be a list of strings. Found: {pythonpath_in_yaml!r} (type: {type(pythonpath_in_yaml)})"
                    )

                for p in pythonpath_in_yaml:
                    if not isinstance(p, str):
                        raise ActionServerValidationError(
                            f"The 'pythonpath' field in package.yaml must be a list of strings. Found list with item: {p!r} (type: {type(p)})"
                        )

                    pythonpath_entries.append(p)

        self._pythonpath_entries = tuple(pythonpath_entries)
        return self._pythonpath_entries

    def bootstrap_environment(
        self, devenv: bool = False, previous_descriptor=None
    ) -> tuple[str, dict]:
        """
        Args:
            devenv: Whether the environment is being bootstrapped for the dev
                environment.

        Returns:
            Tuple of condahash and the environment variables.

        Raises:
            ActionPackageError: If it was not possible to bootstrap the environment.
        """
        import os

        from actions.server._errors_action_server import ActionServerValidationError
        from actions.server.vendored_deps.action_package_handling.cli_errors import (
            ActionPackageError,
        )
        from actions.server.vendored_deps.termcolors import bold_yellow

        use_env: dict[str, object]
        if not self.package_yaml_exists:
            log.info(
                """Adding action without a managed environment (package.yaml unavailable).
    Note: no virtual environment will be used for the imported actions, they'll be run in the same environment used to run the action server."""
            )
            condahash = "<unmanaged>"
            use_env = {}
        else:
            from ._rcc import get_rcc

            if self._package_yaml_contents:
                # Verify the version of the package.yaml file.
                spec_version = self._package_yaml_contents.get("spec-version")
                if not spec_version:
                    log.warn(
                        bold_yellow(
                            "The `spec-version` field is missing from `package.yaml`.\n"
                            "It's recommended to update your package.yaml file to include the `spec-version` field.\n"
                            "See: https://github.com/Sema4AI/actions/blob/master/action_server/docs/guides/01-package-yaml.md for more details."
                        )
                    )

                elif spec_version not in ("v1", "v2"):
                    raise ActionServerValidationError(
                        f"This version of the Action Server only supports `spec-version` `v1` or `v2`. Found: `{spec_version}`."
                    )

            log.info(
                "Action package seems ok. "
                "Bootstrapping RCC environment (please wait, this can take a long time)."
            )
            spec_version = (
                self._package_yaml_contents.get("spec-version")
                if self._package_yaml_contents
                else None
            )
            artifact_mode = os.environ.get(
                "ACTIONS_RUNTIME_RCC_PROVIDER"
            ) or os.environ.get("ACTIONS_REAL_RCC_ARTIFACT_TEST")
            if spec_version == "v2" and not devenv and artifact_mode:
                from ._rcc_runtime_adapter import (
                    compute_source_generation,
                    get_rcc_location,
                    prepare_runtime,
                )

                environment_yaml = (
                    self._runtime_source_snapshot_package_yaml
                    or self._original_package_yaml
                )
                descriptor = prepare_runtime(
                    environment_yaml,
                    get_rcc_location(),
                    source_generation=compute_source_generation(self._import_path),
                    provider=os.environ.get("ACTIONS_RUNTIME_RCC_PROVIDER"),
                    previous_descriptor=previous_descriptor,
                    environment_identity=(
                        self._original_package_yaml
                        if self._runtime_source_snapshot_package_yaml is not None
                        else None
                    ),
                )
                condahash = descriptor.artifact_digest
                use_env = descriptor.to_dict()
            else:
                rcc = get_rcc()
                environment_yaml = (
                    self._runtime_source_snapshot_package_yaml
                    or self._original_package_yaml
                )
                condahash = rcc.get_package_yaml_hash(environment_yaml, devenv)

                env_info = rcc.create_env_and_get_vars(
                    self._datadir, environment_yaml, condahash, devenv
                )
                if not env_info.success:
                    raise ActionPackageError(
                        f"It was not possible to bootstrap the RCC environment. "
                        f"Error: {env_info.message}"
                    )
                if not env_info.result:
                    raise ActionPackageError(
                        "It was not possible to get the environment when "
                        "bootstrapping RCC environment."
                    )
                use_env = dict(env_info.result.env)

        pythonpath_entries = self.get_pythonpath_entries()

        if pythonpath_entries != ["."]:
            if self._package_yaml_contents:
                spec_version = self._package_yaml_contents.get("spec-version")
                if not spec_version:
                    log.critical(
                        "The `pythonpath` entries in `package.yaml` are only supported in `spec-version: v2`.\n"
                        f"Please update {self._original_package_yaml}\n  to include the `spec-version` field.\n"
                        "See: https://github.com/Sema4AI/actions/blob/master/action_server/docs/guides/01-package-yaml.md for more details."
                    )

        package_root = self.package_root
        abspath_entries = []
        for p in pythonpath_entries:
            original_root = self._original_package_yaml.parent
            original_entry = (original_root / p).resolve()
            try:
                relative = original_entry.relative_to(original_root.resolve())
            except ValueError:
                # Explicit external imports retain their original location;
                # they are not part of the package's source snapshot guarantee.
                entry = str(original_entry)
            else:
                entry = str(Path(package_root) / relative)
            if not os.path.exists(entry):
                log.critical(
                    f"The pythonpath entry: {p} does not exist in the filesystem (in {package_root}/package.yaml)."
                )
            abspath_entries.append(entry)

        pythonpath = os.pathsep.join(abspath_entries)

        if "PYTHONPATH" in use_env:
            existing_pythonpath = use_env["PYTHONPATH"]
            if not isinstance(existing_pythonpath, str):
                raise ActionPackageError("Environment PYTHONPATH must be a string.")
            use_env["PYTHONPATH"] = existing_pythonpath + os.pathsep + pythonpath
        else:
            use_env["PYTHONPATH"] = pythonpath

        return condahash, use_env
