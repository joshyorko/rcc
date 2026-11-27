"""Measure fresh-home and reused-home RCC CLI startup on native runners."""
import argparse
import ctypes
import hashlib
import json
import os
from pathlib import Path
import platform
import subprocess
import threading
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


def measure(binary, home):
    env = dict(os.environ, ROBOCORP_HOME=str(home))
    cmd = [str(binary), "version"]
    system = platform.system()
    with tempfile.TemporaryFile(mode="w+") as output, tempfile.TemporaryFile(mode="w+") as errors:
        start = time.perf_counter()
        with subprocess.Popen(cmd, env=env, stdout=output, stderr=errors) as process:
            timer = threading.Timer(30, process.kill)
            timer.start()
            try:
                if system == "Windows":
                    process.wait()
                    elapsed = time.perf_counter() - start
                    peak = windows_peak(process._handle)
                else:
                    _, status, usage = os.wait4(process.pid, 0)
                    elapsed = time.perf_counter() - start
                    process.returncode = os.waitstatus_to_exitcode(status)
                    peak = usage.ru_maxrss * (1 if system == "Darwin" else 1024)
            finally:
                timer.cancel()
                timer.join()
            if process.returncode:
                errors.seek(0)
                raise RuntimeError(errors.read())
        output.seek(0)
        stdout = output.read()
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
        for i in range(5):
            order = list(binaries) if i % 2 == 0 else list(reversed(binaries))
            for role in order:
                home = Path(directory) / f"{i}-{role}"
                results[role]["samples"].append({phase: measure(binaries[role], home)
                                                 for phase in ("fresh", "warm")})
    print(json.dumps(dict(platform=platform.platform(), arch=platform.machine(),
                          command="version", binaries=results)))


if __name__ == "__main__":
    main()
