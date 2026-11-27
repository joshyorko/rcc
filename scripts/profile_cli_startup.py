"""Measure fresh-home and reused-home RCC CLI startup on native runners."""
import argparse
import ctypes
import hashlib
import json
import os
from pathlib import Path
import platform
import subprocess
import signal
import tempfile
import time


def windows_peak(handle):
    from ctypes import wintypes
    class Counters(ctypes.Structure):
        _fields_ = [("cb", wintypes.DWORD), ("PageFaultCount", wintypes.DWORD)] + [
            (field, ctypes.c_size_t) for field in (
                "PeakWorkingSetSize", "WorkingSetSize", "QuotaPeakPagedPoolUsage",
                "QuotaPagedPoolUsage", "QuotaPeakNonPagedPoolUsage", "QuotaNonPagedPoolUsage",
                "PagefileUsage", "PeakPagefileUsage")]
    info = Counters()
    info.cb = ctypes.sizeof(info)
    get_info = ctypes.WinDLL("psapi", use_last_error=True).GetProcessMemoryInfo
    get_info.argtypes = [wintypes.HANDLE, ctypes.POINTER(Counters), wintypes.DWORD]
    if not get_info(int(handle), ctypes.byref(info), info.cb):
        raise ctypes.WinError(ctypes.get_last_error())
    return info.PeakWorkingSetSize


def measure(binary, home, runner):
    env = dict(os.environ, ROBOCORP_HOME=str(home))
    cmd = [str(binary), "version"]
    system = platform.system()
    if runner:
        stats = home.with_suffix(".stats")
        cmd = [str(runner), str(binary), str(stats)]
    start = time.perf_counter()
    with subprocess.Popen(cmd, env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                          text=True, start_new_session=bool(runner)) as process:
        try:
            stdout, stderr = process.communicate(timeout=30)
        except subprocess.TimeoutExpired:
            if runner:
                os.killpg(process.pid, signal.SIGKILL)
            else:
                process.kill()
            process.communicate()
            raise
        elapsed = time.perf_counter() - start
        if process.returncode:
            raise RuntimeError(stderr)
        if runner:
            seconds, rss = stats.read_text().split()
            elapsed = float(seconds)
            peak = int(rss) * (1 if system == "Darwin" else 1024)
        else:
            peak = windows_peak(process._handle)
    if peak <= 0 or not stdout.strip():
        raise ValueError("Missing native peak RSS or RCC version output")
    return dict(seconds=elapsed, peak_rss_bytes=peak, version=stdout.strip())


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("baseline", type=Path)
    parser.add_argument("candidate", type=Path)
    args = parser.parse_args()
    binaries = {role: path.resolve(strict=True) for role, path in vars(args).items()}
    results = {role: {"binary_sha256": hashlib.sha256(path.read_bytes()).hexdigest(), "samples": []}
               for role, path in binaries.items()}
    with tempfile.TemporaryDirectory() as directory:
        runner = None
        if platform.system() != "Windows":
            runner = Path(directory) / "measure-child"
            subprocess.run(["cc", "-O2", str(Path(__file__).with_name("profile_cli_child.c")),
                            "-o", str(runner)], check=True)
        for i in range(5):
            order = list(binaries) if i % 2 == 0 else list(reversed(binaries))
            for role in order:
                home = Path(directory) / f"{i}-{role}"
                results[role]["samples"].append({phase: measure(binaries[role], home, runner)
                                                 for phase in ("fresh", "warm")})
    print(json.dumps(dict(platform=platform.platform(), arch=platform.machine(),
                          command="version", binaries=results)))


if __name__ == "__main__":
    main()
