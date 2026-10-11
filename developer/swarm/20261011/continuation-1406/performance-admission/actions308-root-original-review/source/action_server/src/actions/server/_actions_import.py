import json
import logging
import subprocess
import sys
import typing
import uuid
from pathlib import Path
from typing import Literal

from termcolor import colored

from actions.server._robo_utils.callback import Callback, OnExitContextManager
from actions.server.vendored_deps.termcolors import bold_yellow

if typing.TYPE_CHECKING:
    from actions import ActionsListActionTypedDict
    from actions.server._models import Action, ActionPackage

log = logging.getLogger(__name__)


class IHookOnActionsListCallback(typing.Protocol):
    def __call__(
        self,
        action_package: "ActionPackage",
        actions_list_result: list["ActionsListActionTypedDict"],
    ):
        ...


class IHookOnActionsList(typing.Protocol):
    def register(
        self,
        callback: IHookOnActionsListCallback,
    ) -> OnExitContextManager:
        ...

    def __call__(
        self,
        action_package: "ActionPackage",
        actions_list_result: list["ActionsListActionTypedDict"],
    ):
        ...


# Called as: hook_on_actions_list(action_package, actions_list_result)
hook_on_actions_list: IHookOnActionsList = Callback(raise_exceptions=True)


def import_action_packages(
    *,
    datadir: Path,
    action_package_dirs: list[str],
    disable_not_imported: bool,
    skip_lint: bool,
    whitelist: str,
    after_import: typing.Callable[
        [], tuple[typing.Callable[[], None], typing.Callable[[], None]]
    ]
    | None = None,
) -> None:
    """Collect a complete desired set before atomically admitting its catalog.

    Environment resolution and metadata subprocesses run before taking the DB
    writer lock. Additive imports retain omitted capabilities; synchronization
    disables them only after every selected package has been collected.
    """
    from ._errors_action_server import ActionServerValidationError
    from ._models import Action, ActionPackage, get_db

    prepared: list[tuple[ActionPackage, list[Action]]] = []
    candidate_cleanup: list[typing.Callable[[], None]] = []
    rollback_generation = None
    publish_generation = None
    try:
        for directory in action_package_dirs:
            import_action_package(
                datadir=datadir,
                action_package_dir=directory,
                disable_not_imported=disable_not_imported,
                skip_lint=skip_lint,
                whitelist=whitelist,
                _prepared=prepared,
                _candidate_cleanup=candidate_cleanup,
            )
        names = [package.name for package, _ in prepared]
        if len(set(names)) != len(names):
            raise ActionServerValidationError(
                "Selected package directories must have distinct package identities."
            )

        db = get_db()
        with db.transaction():
            for package, actions in prepared:
                _update_actions_in_db(package, actions, disable_not_imported, whitelist)
            if disable_not_imported:
                selected_ids = {
                    package.id
                    for package in db.all(ActionPackage)
                    if package.name in names
                }
                for action in db.all(Action):
                    if action.enabled and action.action_package_id not in selected_ids:
                        db.update_by_id(Action, action.id, dict(enabled=False))
            # Validate offline imports too: invalid catalogs must not persist
            # for a later --actions-sync=false restart.
            from ._api_action_routes import _ActionRoutes

            _ActionRoutes(whitelist, []).prepare_actions()
            if after_import is not None:
                publish_generation, rollback_generation = after_import()
    except BaseException:
        # Preparation can change the pool, but public routes/catalogs remain
        # untouched until commit succeeds. A commit failure restores that pool.
        if rollback_generation is not None:
            try:
                rollback_generation()
            except BaseException:
                log.exception("Unable to restore the previous process generation.")
        for cleanup in reversed(candidate_cleanup):
            cleanup()
        raise
    if publish_generation is not None:
        publish_generation()


def import_action_package(
    *,
    datadir: Path,
    action_package_dir: str,
    disable_not_imported: bool,
    skip_lint: bool,
    whitelist: str,
    _prepared: list[tuple["ActionPackage", list["Action"]]] | None = None,
    _candidate_cleanup: list[typing.Callable[[], None]] | None = None,
):
    """
    Imports action packages based on directories given in the filesystem.

    Note that the action package is expected to be in the proper directory at
    this point (meaning that it should have been extracted under the /datadir
    if given as a .zip or a path in the filesystem in any other place when
    running in dev mode).

    Raises:
        ActionPackageError if it was not recognized as an action package.

    Note:
        This can be a slow operation as it may require building the RCC
        environment.
    """

    from actions.server._whitelist import accept_action_package

    from ._action_package_handler import ActionPackageHandler
    from ._errors_action_server import ActionServerValidationError
    from ._gen_ids import gen_uuid
    from ._models import ActionPackage, get_db
    from ._rcc_runtime_adapter import load_descriptor
    from ._robo_utils.process import build_python_launch_env

    log.debug("Importing action package from: %s", action_package_dir)

    action_package_handler = ActionPackageHandler(action_package_dir, datadir)
    action_package_name = action_package_handler.action_package_name
    package_yaml_exists = action_package_handler.package_yaml_exists
    original_package_yaml = action_package_handler.original_package_yaml
    import_path = action_package_handler.import_path

    previous_descriptor = None
    try:
        existing_package = get_db().first(
            ActionPackage,
            "SELECT * FROM action_package WHERE name = ?",
            [action_package_name],
        )
    except (KeyError, RuntimeError):
        pass
    else:
        previous_descriptor = load_descriptor(existing_package.env_json)

    if whitelist:
        if not accept_action_package(whitelist, action_package_name):
            log.info(
                f"Action package: {action_package_name} not imported because it has no match in the whitelist: {whitelist!r}"
            )
            return

    source_snapshot_owner = action_package_handler
    source_snapshot_path = None
    source_snapshot_created = False
    if action_package_handler.uses_runtime_source_snapshots:
        try:
            (
                source_snapshot_path,
                source_snapshot_created,
            ) = action_package_handler.prepare_runtime_source_snapshot()
            original_package_yaml = action_package_handler.original_package_yaml
            package_yaml_exists = action_package_handler.package_yaml_exists
            import_path = action_package_handler.import_path
            if source_snapshot_created and _candidate_cleanup is not None:
                from functools import partial

                _candidate_cleanup.append(
                    partial(
                        source_snapshot_owner.discard_runtime_source_snapshot,
                        source_snapshot_path,
                    )
                )
        except Exception as exc:
            raise ActionServerValidationError(
                f"Unable to preserve the last-good Action package source: {exc}"
            ) from exc

    try:
        condahash, use_env = action_package_handler.bootstrap_environment(
            previous_descriptor=previous_descriptor
        )
    except Exception as exc:
        if source_snapshot_path is not None:
            if source_snapshot_created:
                source_snapshot_owner.discard_runtime_source_snapshot(
                    source_snapshot_path
                )
            raise ActionServerValidationError(str(exc)) from exc
        raise

    # Ok, we bootstrapped, now, let's collect the actions.
    if source_snapshot_path is not None:
        # The worker resolves source snapshots as service-owned absolute paths.
        directory_path = import_path
    else:
        try:
            # If the directory can be made relative to the datadir, save the
            # directory as relative.
            directory_path = import_path.relative_to(datadir)
            assert import_path.samefile(directory_path)
        except (AssertionError, ValueError):
            # Otherwise use the absolute path.
            directory_path = import_path

    action_package_id = gen_uuid("action_package")

    runtime_descriptor = load_descriptor(json.dumps(use_env))
    python_exe = use_env.get("PYTHON_EXE")
    if python_exe:
        log.info(colored(f"Python interpreter path: {python_exe}", attrs=["dark"]))

    action_package = ActionPackage(
        id=action_package_id,
        name=action_package_name,
        directory=directory_path.as_posix(),
        conda_hash=condahash,
        env_json=json.dumps(use_env),
    )
    log.debug(f"Collecting actions for Action Package: {action_package_name}.")

    launch_env = {key: value for key, value in use_env.items() if key != "runtime"}
    env = build_python_launch_env(launch_env)

    try:
        # any actions version will do at this point.
        actions_library_version = _get_actions_version(
            env, import_path, "actions", runtime_descriptor=runtime_descriptor
        )
    except Exception as actions_error:
        if runtime_descriptor is not None:
            if source_snapshot_path is not None:
                if source_snapshot_created:
                    source_snapshot_owner.discard_runtime_source_snapshot(
                        source_snapshot_path
                    )
                raise ActionServerValidationError(str(actions_error)) from actions_error
            raise
        ### TODO: Remove in the future!

        found_actions = False
        # Still support robocorp.actions for now (but warn the user).
        try:
            actions_library_version = _get_actions_version(
                env,
                import_path,
                "robocorp.actions",
                runtime_descriptor=runtime_descriptor,
            )
            log.critical(
                "Important: 'robocorp.actions' is deprecated!\n"
                "Please change the 'robocorp-actions' dependency to 'actions-core'\n"
                "in your package.yaml\n"
                "(note: the public API should be the same with the exception that the\n"
                "imports should come from 'actions' instead of 'robocorp.actions).\n"
                "On future versions of the Action Server, using 'robocorp-actions' will no\n"
                "longer be supported.\n"
            )
            found_actions = True
        except Exception:
            pass

        if not found_actions:
            raise  # The actions error, not the robocorp.actions one.

        expected_version = (0, 0, 7)
        expected_version_str = ".".join(str(x) for x in expected_version)
        if actions_library_version < expected_version:
            actions_library_version_str = ".".join(
                str(x) for x in actions_library_version
            )

            if package_yaml_exists:
                raise ActionServerValidationError(
                    f"Error, the `robocorp-actions` version is: {actions_library_version_str}.\n"
                    f"Expected `robocorp-actions` version to be {expected_version_str} or higher.\n"
                    f"Please update the version in: {original_package_yaml}\n"
                )
            else:
                raise ActionServerValidationError(
                    f"Error, the `robocorp-actions` version is: {actions_library_version_str}.\n"
                    f"Expected it to be {expected_version_str} or higher.\n"
                    f"Please update the `robocorp-actions` version in your python environment (python: {sys.executable})\n"
                )

        min_version_for_encryption_with_auth_tag = (0, 2, 1)
        if actions_library_version < min_version_for_encryption_with_auth_tag:
            actions_library_version_str = ".".join(
                str(x) for x in actions_library_version
            )
            log.critical(
                f"Warning: the `robocorp-actions` version is: {actions_library_version_str}.\n"
                f"To receive encrypted secrets, robocorp-actions 0.2.1 or newer is required.\n"
                f"Please update the version in: {original_package_yaml}\n"
                "(proceeding with initalization but actions receiving encrypted secrets will\n"
                "not work properly -- on future versions of the action server, support for \n"
                "this version of robocorp-actions will be removed)."
            )

    try:
        collected: list[tuple[ActionPackage, list[Action]]] = []
        _add_actions_to_db(
            datadir,
            env,
            import_path,
            action_package,
            disable_not_imported=disable_not_imported,
            skip_lint=skip_lint,
            whitelist=whitelist,
            actions_library_version=actions_library_version,
            runtime_descriptor=runtime_descriptor,
            _prepared=collected,
        )
        source_snapshot_owner.validate_runtime_source_snapshot()
        if _prepared is not None:
            _prepared.extend(collected)
        else:
            for collected_package, collected_actions in collected:
                _update_actions_in_db(
                    collected_package,
                    collected_actions,
                    disable_not_imported,
                    whitelist,
                )
    except Exception as exc:
        if source_snapshot_path is not None:
            if source_snapshot_created:
                source_snapshot_owner.discard_runtime_source_snapshot(
                    source_snapshot_path
                )
            if not isinstance(exc, ActionServerValidationError):
                raise ActionServerValidationError(str(exc)) from exc
        raise


def _get_actions_version(
    env,
    cwd,
    libname: Literal["robocorp.actions"] | Literal["actions"],
    runtime_descriptor=None,
) -> tuple[int, ...]:
    if runtime_descriptor is None:
        from actions.server._settings import get_python_exe_from_env

        python = get_python_exe_from_env(env)
        cmdline: list[str] = [
            python,
            "-c",
            f"import {libname};print({libname}.__version__)",
        ]
    else:
        from ._rcc_runtime_adapter import build_exec_command, get_rcc_location

        python = "RCC Environment Artifact"
        version_file = Path(cwd) / f".rcc-action-version-{uuid.uuid4().hex}"
        version_code = (
            f"import {libname}; from pathlib import Path; "
            f"Path({str(version_file)!r}).write_text({libname}.__version__)"
        )
        cmdline = build_exec_command(
            get_rcc_location(),
            runtime_descriptor,
            ["python", "-c", version_code],
            receipt_file=None,
        )
    msg = f"""Unable to get {libname} version.

This usually means that `{libname}` is not installed in the python
environment (make sure that `{libname.replace('.', '-')}`
is defined in your `package.yaml`).

Python executable being used:
{python}
"""

    try:
        output = subprocess.check_output(cmdline, env=env, cwd=cwd)
    except Exception:
        raise RuntimeError(msg)
    if runtime_descriptor is not None:
        try:
            str_output = version_file.read_text(encoding="utf-8")
        finally:
            version_file.unlink(missing_ok=True)
    else:
        str_output = output.decode("utf-8", "replace")
    if runtime_descriptor is not None:
        decoded_results = []
        decoder = json.JSONDecoder()
        for index, character in enumerate(str_output):
            if character != "{":
                continue
            try:
                result, _end = decoder.raw_decode(str_output[index:])
            except json.JSONDecodeError:
                continue
            if isinstance(result, dict):
                decoded_results.append(result)
        for result in reversed(decoded_results):
            for key in ("stdout", "output", "result"):
                if isinstance(result.get(key), str):
                    str_output = result[key]
                    break
    try:
        return tuple(int(x) for x in str_output.strip().split("."))
    except Exception:
        raise RuntimeError(msg)


def _add_actions_to_db(
    datadir: Path,
    env: dict,
    import_path: Path,
    action_package: "ActionPackage",
    disable_not_imported: bool,
    skip_lint: bool,
    whitelist: str,
    actions_library_version: tuple[int, ...],
    runtime_descriptor=None,
    _prepared: list[tuple["ActionPackage", list["Action"]]] | None = None,
):
    from actions.server_integration import format_lint_results

    from actions.server._errors_action_server import ActionServerValidationError
    from actions.server._gen_ids import gen_uuid
    from actions.server._models import Action

    if runtime_descriptor is None:
        from actions.server._settings import get_python_exe_from_env

        python = get_python_exe_from_env(env)
        command_prefix = [python]
    else:
        from ._rcc_runtime_adapter import build_exec_command, get_rcc_location

        command_prefix = build_exec_command(
            get_rcc_location(),
            runtime_descriptor,
            ["python"],
            receipt_file=None,
        )[:-1]

    if actions_library_version > (1, 0, 1):
        command = "metadata"
    else:
        command = "list"

    if skip_lint:
        code = f"""
try:
    from actions import cli
except:
    from robocorp.actions import cli

cli.main(["{command}", "--skip-lint"])
"""
    else:
        code = f"""
try:
    from actions import cli
except:
    from robocorp.actions import cli

cli.main(["{command}"])
"""
    if runtime_descriptor is not None:
        code = code.replace(
            f'cli.main(["{command}"',
            f'cli.main(["{command}", {str(import_path)!r}',
        )
        metadata_file = import_path / f".rcc-action-metadata-{uuid.uuid4().hex}.json"
        code = (
            "import contextlib\n"
            f"with open({str(metadata_file)!r}, 'w', encoding='utf-8') as _out, "
            "contextlib.redirect_stdout(_out):\n"
            + "    "
            + code.replace("\n", "\n    ")
        )
    if runtime_descriptor is None:
        cmdline = [*command_prefix, "-c", code]
    else:
        cmdline = [*command_prefix, "python", "-c", code]

    popen = subprocess.Popen(
        cmdline,
        env=env,
        cwd=str(import_path),
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        shell=False,
    )
    stdout, stderr = popen.communicate()
    if runtime_descriptor is not None:
        try:
            stdout = metadata_file.read_bytes()
        finally:
            metadata_file.unlink(missing_ok=True)
    if popen.poll() != 0:
        if popen.poll() == 1:
            # Let's see if we have linting issues.
            try:
                loaded_from_stdout = json.loads(stdout)
            except Exception:
                pass  # Ok, just go with the "regular" error.
            else:
                # We loaded some json from the stdout. Check if there
                # were linting errors.
                if isinstance(loaded_from_stdout, dict):
                    lint_result = loaded_from_stdout.get("lint_result")
                    if isinstance(lint_result, dict):
                        formatted_lint_result = format_lint_results(lint_result)
                        if formatted_lint_result is not None:
                            raise ActionServerValidationError(
                                formatted_lint_result.message
                            )

        raise RuntimeError(
            f"It was not possible to list the actions.\n"
            f"cmdline: {subprocess.list2cmdline(cmdline)}\n"
            f"cwd: {import_path}\n"
            f"stdout:{stdout.decode('utf-8', 'replace')}\n"
            f"stderr:{stderr.decode('utf-8', 'replace')}"
        )

    # If it didn't fail the import, consider as warning (and thus print in yellow).
    decoded_stderr = stderr.decode("utf-8", "replace").strip()
    if decoded_stderr:
        log.critical(bold_yellow(f"{decoded_stderr}\n"))

    try:
        actions_list_result = json.loads(stdout)
    except json.JSONDecodeError:
        raise RuntimeError(
            f"It was not possible to load as json the contents >>{stdout!r}<<"
        )
    else:
        if command == "metadata":
            metadata_result = actions_list_result
            if not isinstance(metadata_result, dict):
                raise RuntimeError(
                    f"Expected actions metadata to provide dictionary. Found: >>{stdout!r}<<"
                )
            actions_list_result = metadata_result.get("actions")
            if not isinstance(actions_list_result, list):
                raise RuntimeError(
                    f"Expected actions metadata to provide dictionary with 'actions' key. Found: >>{stdout!r}<<"
                )
        elif command == "list":
            if not isinstance(actions_list_result, list):
                raise RuntimeError(
                    f"Expected actions list to provide a list. Found: >>{stdout!r}<<"
                )

        hook_on_actions_list(
            action_package,
            typing.cast(list["ActionsListActionTypedDict"], actions_list_result),
        )

        actions = []
        for action_fields in actions_list_result:
            action_name = action_fields["name"]
            filepath = Path(action_fields["file"]).absolute()
            try:
                filepath = filepath.relative_to(import_path)
            except ValueError:
                pass

            managed_params_str: str = ""
            if action_fields.get("managed_params_schema"):
                managed_params_str = json.dumps(action_fields["managed_params_schema"])

            options = action_fields.get("options") or {}
            actions.append(
                Action(
                    id=gen_uuid("action"),
                    action_package_id=action_package.id,
                    name=action_name,
                    docs=action_fields["docs"],
                    file=filepath.as_posix(),
                    lineno=action_fields["line"],
                    input_schema=json.dumps(action_fields["input_schema"]),
                    output_schema=json.dumps(action_fields["output_schema"]),
                    enabled=True,
                    is_consequential=options.get("is_consequential", None),
                    managed_params_schema=managed_params_str,
                    options=json.dumps(options),
                )
            )

    names = [action.name for action in actions]
    if len(set(names)) != len(names):
        raise ActionServerValidationError(
            f"Duplicate action name in package {action_package.name!r}"
        )
    if _prepared is not None:
        _prepared.append((action_package, actions))
    else:
        _update_actions_in_db(action_package, actions, disable_not_imported, whitelist)


def _update_actions_in_db(
    action_package: "ActionPackage",
    actions: list["Action"],
    disable_not_imported: bool,
    whitelist: str = "",
) -> None:
    from dataclasses import asdict

    from ._models import Action, ActionPackage, get_db
    from ._whitelist import accept_action

    db = get_db()

    def selected_actions(retained: set[str]) -> list[Action]:
        # Additive imports retain already enabled capabilities. Refresh their
        # metadata from the new source too, even if this import only selects a
        # different action. Never admit an unselected new or disabled action.
        return [
            action
            for action in actions
            if not whitelist
            or action.name in retained
            or accept_action(whitelist, action_package.name, action.name)
        ]

    try:
        existing_action_package = db.first(
            ActionPackage,
            "SELECT * FROM action_package WHERE name = ?",
            [action_package.name],
        )
    except KeyError:
        # ok, insert as new one
        with db.transaction():
            log.info("Found new action package: %s", action_package.name)
            db.insert(action_package)
            for action in selected_actions(set()):
                log.info("Found new action: %s", action.name)
                db.insert(action)

    else:
        # We already have an existing action package with the same name. This
        # means we'll have to update it instead of adding a new one.
        new_action_package_as_dict = asdict(action_package)
        # The id should not be updated.
        del new_action_package_as_dict["id"]

        existing_actions = db.select(
            Action,
            "SELECT * FROM action WHERE action_package_id = ?",
            [existing_action_package.id],
        )

        retained_names: set[str] = set()
        if not disable_not_imported:
            candidate_names = {action.name for action in actions}
            omitted_names = sorted(
                action.name
                for action in existing_actions
                if action.enabled and action.name not in candidate_names
            )
            if omitted_names:
                from ._errors_action_server import ActionServerValidationError

                raise ActionServerValidationError(
                    f"Additive import of {action_package.name!r} would remove "
                    f"enabled actions: {', '.join(omitted_names)}. "
                    "The previous package remains admitted. Use start with "
                    "--actions-sync=true and all desired --dir entries to "
                    "explicitly reconcile removals."
                )
            retained_names = {
                action.name for action in existing_actions if action.enabled
            }

        actions = selected_actions(retained_names)

        existing_action_name_to_action = {}
        for action in existing_actions:
            existing_action_name_to_action[action.name] = action

        seen_action_ids = set()
        with db.transaction():
            log.debug("Updating action package: %s", action_package.name)
            db.update_by_id(
                ActionPackage,
                existing_action_package.id,
                new_action_package_as_dict,
            )
            if not actions:
                log.info("Found no actions in: %s", action_package.name)

            for action in actions:
                action.action_package_id = existing_action_package.id
                existing_action = existing_action_name_to_action.get(action.name)
                if existing_action is not None:
                    # This is an existing action, we need to update it.
                    new_action_as_dict = asdict(action)
                    del new_action_as_dict["id"]
                    log.debug("Updating action: %s", action.name)
                    db.update_by_id(Action, existing_action.id, new_action_as_dict)
                    seen_action_ids.add(existing_action.id)
                else:
                    # This is a new action, insert it.
                    log.info("Found new action: %s", action.name)
                    db.insert(action)
                    seen_action_ids.add(action.id)

            if disable_not_imported:
                for action in existing_actions:
                    if action.id not in seen_action_ids:
                        log.info("Disabling action: %s", action.name)
                        db.update_by_id(Action, action.id, dict(enabled=False))
