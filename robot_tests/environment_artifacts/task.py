import json
import os
import sys
from pathlib import Path

import yaml


proof = {
    "yamlVersion": yaml.__version__,
    "condaOffline": os.environ.get("CONDA_OFFLINE"),
    "mambaOffline": os.environ.get("MAMBA_OFFLINE"),
    "pipNoIndex": os.environ.get("PIP_NO_INDEX"),
    "uvNoIndex": os.environ.get("UV_NO_INDEX"),
}
Path(sys.argv[1]).write_text(json.dumps(proof, sort_keys=True), encoding="utf-8")
