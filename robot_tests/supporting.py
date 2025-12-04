import json
import logging
import subprocess
import sys
from pathlib import Path

log = logging.getLogger(__name__)


def fix_command(command):
    if sys.platform == "win32":
        if command.strip().startswith("build/rcc"):
            command = command.replace("build/rcc", ".\\build\\rcc.exe", 1)
    return command


def get_cwd():
    import os

    cwd = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
    detail = (
        "(rcc doesn't seem to be built, please run `inv local` before running tests)"
    )
    assert "build" in os.listdir(cwd), f"Missing build directory in: {cwd!r} {detail}"

    build_dir = os.path.join(cwd, "build")
    if sys.platform == "win32":
        assert "rcc.exe" in os.listdir(
            build_dir
        ), f"Missing rcc.exe in: {build_dir!r} {detail}"
    else:
        assert "rcc" in os.listdir(build_dir), f"Missing rcc in: {build_dir!r} {detail}"
    return cwd


def log_command(command: str, cwd: str):
    msg = f"Running command: {command!r} cwd: {cwd!r}"
    log.info(msg)


def capture_flat_output(command):
    command = fix_command(command)
    cwd = get_cwd()
    log_command(command, cwd)

    task = subprocess.Popen(
        command,
        shell=True,
        stderr=subprocess.PIPE,
        stdout=subprocess.PIPE,
        cwd=cwd,
    )
    out, _ = task.communicate()
    assert (
        task.returncode == 0
    ), f"Unexpected exit code {task.returncode} from {command!r}"
    return out.decode().strip()


def run_and_return_code_output_error(
    command,
    env: dict[str, str] | None = None,
    cwd: str | None = None,
    check: bool = False,
    stdin_contents: str | None = None,
) -> tuple[int, str, str]:
    command = fix_command(command)
    cwd = get_cwd() if cwd is None else cwd
    log_command(command, cwd)

    task = subprocess.Popen(
        command,
        shell=True,
        stderr=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stdin=subprocess.PIPE if stdin_contents else None,
        cwd=cwd,
        env=env,
    )
    out, err = task.communicate(
        input=stdin_contents.encode("utf-8") if stdin_contents else None
    )
    if check:
        assert (
            task.returncode == 0
        ), f"Unexpected exit code {task.returncode} from {command!r}"
    return task.returncode, out.decode(), err.decode()


def parse_json(content):
    parsed = json.loads(content)
    assert isinstance(parsed, (list, dict)), f"Expecting list or dict; got {parsed!r}"
    return parsed


def run_with_env(
    command, json_env_output: str, fail: bool = False
) -> tuple[int, str, str]:
    import os

    env_lst = parse_json(json_env_output)
    env = os.environ.copy()
    env.update({entry["key"]: entry["value"] for entry in env_lst})
    ret, out, err = run_and_return_code_output_error(command, env=env)
    if fail:
        assert (
            ret != 0
        ), f"Expected non-zero exit code; got {ret!r} with output: {out!r} and error: {err!r}"
    else:
        assert (
            ret == 0
        ), f"Unexpected exit code {ret!r} from {command!r} with output: {out!r} and error: {err!r}"

    return ret, out, err


def normalize_output(output: str) -> str:
    return output.strip().replace("\r\n", "\n").replace("\r", "\n")


def run_and_check_expected_output(
    command: str,
    expected_output: str,
    expected_file: str,
    use_stream: str = "stdout",
    pass_file_as_stdin: Path | None = None,
):
    stdin_contents = pass_file_as_stdin.read_text() if pass_file_as_stdin else None

    ret, out, err = run_and_return_code_output_error(
        command, stdin_contents=stdin_contents
    )
    assert (
        ret == 0
    ), f"Unexpected exit code {ret!r} from {command!r} with output: {out!r} and error: {err!r}"

    log.info(f"Output: {out!r}")
    log.info(f"Error: {err!r}")

    if use_stream == "stdout":
        contents = out
    else:
        contents = err

    f = Path(expected_file)
    if not f.exists():
        f.parent.mkdir(parents=True, exist_ok=True)
        f.write_text(normalize_output(contents))
        raise Exception(f"Expected file {expected_file!r} not found; created it")
    else:
        expected_output = f.read_text()
        if normalize_output(contents) != normalize_output(expected_output):
            raise Exception(
                f"Output does not match expected output in {expected_file!r}\n"
                f"Expected: {expected_output!r}\n"
                f"Actual: {contents!r}"
            )


def run_and_stream_output(command: str, title: str = "UI Output") -> tuple[int, str, str]:
    """
    Run a command and stream its output to the terminal in real-time.
    This allows you to see spinners, progress bars, and dashboard UI as they render.
    Also captures and returns the output for assertions.

    Args:
        command: The command to run
        title: A title to display before the output

    Returns:
        tuple of (return_code, stdout, stderr)
    """
    import os
    import select
    import io

    command = fix_command(command)
    cwd = get_cwd()
    log_command(command, cwd)

    # Print header
    print(f"\n{'='*70}", flush=True)
    print(f"  {title}", flush=True)
    print(f"  Command: {command}", flush=True)
    print(f"{'='*70}", flush=True)
    print(f"--- OUTPUT (stdout + stderr interleaved) ---", flush=True)

    # Run with output going directly to terminal AND captured
    # Use unbuffered mode and inherit terminal for proper ANSI rendering
    task = subprocess.Popen(
        command,
        shell=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        cwd=cwd,
        bufsize=0,  # Unbuffered
    )

    stdout_data = io.StringIO()
    stderr_data = io.StringIO()

    # Read from both streams and print in real-time
    import threading

    def read_stream(stream, storage, prefix=""):
        """Read from stream and print + store"""
        while True:
            line = stream.readline()
            if not line:
                break
            decoded = line.decode('utf-8', errors='replace')
            storage.write(decoded)
            # Print to terminal with ANSI codes preserved
            sys.stdout.write(decoded)
            sys.stdout.flush()

    # Start threads to read both streams
    stdout_thread = threading.Thread(target=read_stream, args=(task.stdout, stdout_data))
    stderr_thread = threading.Thread(target=read_stream, args=(task.stderr, stderr_data))

    stdout_thread.start()
    stderr_thread.start()

    # Wait for completion
    stdout_thread.join()
    stderr_thread.join()
    task.wait()

    print(f"\n{'='*70}", flush=True)
    print(f"  Exit code: {task.returncode}", flush=True)
    print(f"{'='*70}\n", flush=True)

    return task.returncode, stdout_data.getvalue(), stderr_data.getvalue()


def run_ui_sample(command: str, title: str = "UI Sample") -> tuple[int, str, str]:
    """
    Convenience function to run a command and display its UI output.
    Streams output in real-time so you can see progress indicators,
    spinners, and dashboards as they render.

    Args:
        command: The RCC command to run
        title: Description of what this UI sample shows

    Returns:
        tuple of (return_code, stdout, stderr)
    """
    return run_and_stream_output(command, title)
