*** Settings ***
Library  OperatingSystem
Library  supporting.py
Resource  resources.robot
Suite Setup  UV Native Setup

*** Keywords ***
UV Native Setup
  Fire And Forget   build/rcc ht delete 4e67cd8

*** Test cases ***

Goal: Build uv-native environment and see correct variables
  Step        build/rcc ht vars --space uvnative --controller citests robot_tests/uv_native/conda.yaml
  Must Have   PYTHON_EXE=
  Must Have   CONDA_PREFIX=
  Must Have   PATH=
  Must Have   PYTHONHOME=
  Must Have   PYTHONEXECUTABLE=
  Must Have   PYTHONNOUSERSITE=1
  Must Have   TEMP=
  Must Have   TMP=
  Must Have   RCC_ENVIRONMENT_HASH=
  Must Have   RCC_INSTALLATION_ID=
  Must Have   RCC_TRACKING_ALLOWED=
  Wont Have   PYTHONPATH=
  Wont Have   ROBOT_ROOT=
  Wont Have   ROBOT_ARTIFACTS=
  Use STDERR
  Must Have   Running uv-native phase
  Must Have   environment creation" was SUCCESS
  Wont Have   micromamba
  Wont Have   No such file or directory

Goal: Holotree integrity check passes after uv-native build
  Step        build/rcc holotree check --controller citests
  Use STDERR
  Must Have   OK

Goal: Run task that imports pip package in uv-native environment
  Step        build/rcc task run --task "Verify UV Native" --space uvnative --controller citests -r robot_tests/uv_native/robot.yaml
  Must Have   UV_NATIVE_OK
  Must Have   requests
  Use STDERR
  Must Have   environment creation" was SUCCESS
  Must Have   OK.
  Wont Have   micromamba
  Wont Have   No such file or directory
  Wont Have   ModuleNotFoundError

Goal: Cached restore skips build phases on second run
  Step        build/rcc ht vars --space uvnative --controller citests robot_tests/uv_native/conda.yaml
  Use STDERR
  Must Have   Skipping uv-native phase, layer exists.
  Must Have   Skipping pip phase, layer exists.
  Must Have   environment creation" was SUCCESS
  Wont Have   micromamba

Goal: Task execution works after cached restore
  Step        build/rcc task run --task "Verify UV Native" --space uvnative --controller citests -r robot_tests/uv_native/robot.yaml
  Must Have   UV_NATIVE_OK
  Must Have   requests
  Use STDERR
  Must Have   OK.
  Wont Have   ModuleNotFoundError

Goal: JSON output works for uv-native environment
  Step        build/rcc ht vars --space uvnative --controller citests --json robot_tests/uv_native/conda.yaml
  Must Be Json Response

Goal: Holotree still valid after full test cycle
  Step        build/rcc holotree check --controller citests
  Use STDERR
  Must Have   OK
