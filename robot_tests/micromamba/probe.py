import os
import sys
from pathlib import Path

import requests

assert requests.__version__ == "2.32.3"
assert Path(sys.prefix).resolve() == Path(os.environ["CONDA_PREFIX"]).resolve()
assert os.environ["CONDA_SHLVL"] == "1"
if sys.argv[1:] == ["fail"]:
    print("MICROMAMBA_EXPECTED_ERROR", file=sys.stderr)
    sys.exit(23)
print("MICROMAMBA_ACCEPTANCE_OK")
