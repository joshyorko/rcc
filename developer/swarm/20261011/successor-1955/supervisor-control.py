#!/usr/bin/env python3
"""Owned control: a child deliberately enters a different process group."""
import subprocess, sys

child = subprocess.Popen([sys.executable, '-c',
    'import os,time; os.setpgid(0,0); payload=bytearray(10*1024*1024); time.sleep(10)'])
try:
    child.wait()
finally:
    if child.poll() is None:
        child.terminate()
        child.wait(timeout=2)
