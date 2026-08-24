import json
import os
import _sqlite3
import sqlite3
import sys
from pathlib import Path

import yaml


proof = {
    "yamlVersion": yaml.__version__,
    "condaOffline": os.environ.get("CONDA_OFFLINE"),
    "mambaOffline": os.environ.get("MAMBA_OFFLINE"),
    "pipNoIndex": os.environ.get("PIP_NO_INDEX"),
    "uvNoIndex": os.environ.get("UV_NO_INDEX"),
    "nativeImport": "sqlite3",
    "nativeExtension": _sqlite3.__file__,
    "sqliteVersion": sqlite3.sqlite_version,
}
encoded = json.dumps(proof, sort_keys=True)
if len(sys.argv) > 1:
    Path(sys.argv[1]).write_text(encoded, encoding="utf-8")
else:
    print(encoded)
