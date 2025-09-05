*** Settings ***
Library  OperatingSystem
Library  supporting.py
Resource  resources.robot
Suite Setup  Fullrun setup

*** Keywords ***
Fullrun setup
  Fire And Forget   build/rcc ht delete 4e67cd8

*** Test cases ***

Goal: Show rcc version information.
  Step        build/rcc version --controller citests
  Must Have   v18.

Goal: There is debug message when bundled case.
  Step        build/rcc version --controller citests --debug --bundled
  Use STDERR
  Must Have   Did not check newer version existence, since this is bundled case.

Goal: No debug message when user case.
  Step        build/rcc version --controller citests --debug
  Use STDERR
  Wont Have   this is bundled case

Goal: Show rcc license information.
  Step        build/rcc man license --controller citests
  Must Have   Apache License
  Must Have   Version 2.0
  Must Have   http://www.apache.org/licenses/LICENSE-2.0
  Must Have   Copyright 2020 Robocorp Technologies, Inc.
  Wont Have   EULA

Goal: Telemetry tracking enabled by default.
  Step        build/rcc configure identity --controller citests
  Must Have   anonymous health tracking is: enabled
  Must Exist  %{ROBOCORP_HOME}/rcc.yaml

Goal: Send telemetry data to cloud.
  Step        build/rcc feedback metric --controller citests -t test -n rcc.test -v robot.fullrun
  Use STDERR
  Must Have   OK

Goal: Telemetry tracking can be disabled.
  Step        build/rcc configure identity --controller citests --do-not-track
  Must Have   anonymous health tracking is: disabled

Goal: Show listing of rcc commands.
  Step        build/rcc --controller citests
  Use STDERR
  Must Have   rcc is environment manager
  Wont Have   missing

Goal: Show toplevel help for rcc.
  Step        build/rcc -h
  Must Have   Available Commands:

Goal: Show config help for rcc.
  Step        build/rcc config -h --controller citests
  Must Have   Available Commands:
  Must Have   credentials

Goal: List available robot templates.
  Step        build/rcc robot init -i -l --controller citests
  Must Have   extended
  Must Have   python
  Must Have   standard
  Use STDERR
  Must Have   OK.

Goal: Initialize new standard robot into tmp/fluffy folder using force.
  Step        build/rcc robot init -i --controller citests -t extended -d tmp/fluffy -f
  Use STDERR
  Must Have   OK.

Goal: Fail to initialize new standard robot into tmp/fluffy without force.
  Step        build/rcc robot init -i --controller citests -t extended -d tmp/fluffy  2
  Use STDERR
  Must Have   Error: Directory
  Must Have   fluffy is not empty
  Wont Exist  tmp/fluffy/output/environment_*_freeze.yaml

Goal: Run task in place in debug mode and with timeline.
  Step        build/rcc task run --task "Run Example task" --controller citests -r tmp/fluffy/robot.yaml --debug --timeline
  Must Have   1 task, 1 passed, 0 failed
  Use STDERR
  Must Have   Progress: 01/15
  Must Have   Progress: 02/15
  Must Have   Progress: 15/15
  Must Have   rpaframework
  Must Have   PID #
  Must Have   [N]
  Must Have   [D]
  Wont Have   [T]
  Wont Have   Running against old environment
  Wont Have   WARNING
  Wont Have   NOT pristine
  Must Have   Installation plan is:
  Must Have   Command line is: [
  Must Have   rcc timeline
  Must Have   robot execution (simple=false).
  Must Have   Now.
  Must Have   Wanted
  Must Have   Available
  Must Have   Version
  Must Have   Origin
  Must Have   Status
  Must Have   point of view, "actual main robot run" was SUCCESS.
  Must Have   OK.
  Must Exist  tmp/fluffy/output/environment_*_freeze.yaml
  Must Exist  %{ROBOCORP_HOME}/wheels/
  Must Exist  %{ROBOCORP_HOME}/pipcache/
  Step        build/rcc holotree check --controller citests

Goal: Run task in clean temporary directory.
  Step        build/rcc task testrun --task "Run Example task" --controller citests -r tmp/fluffy/robot.yaml
  Must Have   1 task, 1 passed, 0 failed
  Use STDERR
  Must Have   rpaframework
  Must Have   Progress: 01/15
  Wont Have   Progress: 03/15
  Wont Have   Progress: 05/15
  Wont Have   Progress: 07/15
  Wont Have   Progress: 09/15
  Must Have   Progress: 14/15
  Must Have   Progress: 15/15
  Must Have   point of view, "actual main robot run" was SUCCESS.
  Must Have   OK.

Goal: Merge two different conda.yaml files with conflict fails
  Step        build/rcc holotree vars --controller citests conda/testdata/conda.yaml conda/testdata/other.yaml  5
  Use STDERR
  Must Have   robotframework=3.1 vs. robotframework=3.2

Goal: Merge two different conda.yaml files without conflict passes
  Step        build/rcc holotree vars --controller citests conda/testdata/third.yaml conda/testdata/other.yaml --silent
  Must Have   RCC_ENVIRONMENT_HASH=ffd32af1fdf0f253
  Must Have   4e67cd8_9fcd2534

Goal: Can list environments as JSON
  Step        build/rcc holotree list --controller citests --json
  Must Have   4e67cd8_9fcd2534
  Must Have   ffd32af1fdf0f253
  Must Be Json Response

Goal: See variables from specific environment without robot.yaml knowledge
  Step        build/rcc holotree variables --controller citests conda/testdata/conda.yaml
  Must Have   ROBOCORP_HOME=
  Must Have   PYTHON_EXE=
  Must Have   RCC_EXE=
  Must Have   CONDA_DEFAULT_ENV=rcc
  Must Have   CONDA_PREFIX=
  Must Have   CONDA_PROMPT_MODIFIER=(rcc)
  Must Have   CONDA_SHLVL=1
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
  Must Have   RCC_ENVIRONMENT_HASH=786fd9dca1e1f1db
  Step        build/rcc holotree check --controller citests

Goal: See variables from specific environment with robot.yaml but without task
  Step        build/rcc holotree variables --controller citests -r tmp/fluffy/robot.yaml
  Must Have   ROBOCORP_HOME=
  Must Have   PYTHON_EXE=
  Must Have   RCC_EXE=
  Must Have   CONDA_DEFAULT_ENV=rcc
  Must Have   CONDA_PREFIX=
  Must Have   CONDA_PROMPT_MODIFIER=(rcc)
  Must Have   CONDA_SHLVL=1
  Must Have   PATH=
  Must Have   PYTHONHOME=
  Must Have   PYTHONEXECUTABLE=
  Must Have   PYTHONNOUSERSITE=1
  Must Have   TEMP=
  Must Have   TMP=
  Must Have   RCC_ENVIRONMENT_HASH=1cdd0b852854fe5b
  Must Have   RCC_INSTALLATION_ID=
  Must Have   RCC_TRACKING_ALLOWED=
  Must Have   PYTHONPATH=
  Must Have   ROBOT_ROOT=
  Must Have   ROBOT_ARTIFACTS=
  Step        build/rcc holotree check --controller citests

Goal: See variables from specific environment with warranty voided
  Step        build/rcc holotree variables --controller citests -r tmp/fluffy/robot.yaml --warranty-voided --anything I_know_what_Im_doing
  Must Have   ROBOCORP_HOME=
  Must Have   PYTHON_EXE=
  Must Have   RCC_EXE=
  Must Have   CONDA_DEFAULT_ENV=rcc
  Must Have   CONDA_PREFIX=
  Must Have   CONDA_PROMPT_MODIFIER=(rcc)
  Must Have   CONDA_SHLVL=1
  Must Have   PATH=
  Must Have   PYTHONHOME=
  Must Have   PYTHONEXECUTABLE=
  Must Have   PYTHONNOUSERSITE=1
  Must Have   TEMP=
  Must Have   TMP=
  Must Have   RCC_ENVIRONMENT_HASH=
  Must Have   RCC_INSTALLATION_ID=
  Must Have   RCC_TRACKING_ALLOWED=
  Must Have   PYTHONPATH=
  Must Have   ROBOT_ROOT=
  Must Have   ROBOT_ARTIFACTS=
  Use STDERR
  Wont Have   Progress: 01/15
  Wont Have   Progress: 02/15
  Wont Have   Progress: 15/15
  Must Have   Warning: Note that 'rcc' is running in 'warranty voided' mode.

Goal: See variables from specific environment without robot.yaml knowledge in JSON form
  Step        build/rcc holotree variables --controller citests --json conda/testdata/conda.yaml
  Must Be Json Response

Goal: See variables from specific environment with robot.yaml knowledge
  Step        build/rcc holotree variables --controller citests conda/testdata/conda.yaml --config tmp/alternative.yaml -r tmp/fluffy/robot.yaml -e tmp/fluffy/devdata/env.json
  Must Have   ROBOCORP_HOME=
  Must Have   PYTHON_EXE=
  Must Have   RCC_EXE=
  Must Have   CONDA_DEFAULT_ENV=rcc
  Must Have   CONDA_PREFIX=
  Must Have   CONDA_PROMPT_MODIFIER=(rcc)
  Must Have   CONDA_SHLVL=1
  Must Have   PATH=
  Must Have   PYTHONPATH=
  Must Have   PYTHONHOME=
  Must Have   PYTHONEXECUTABLE=
  Must Have   PYTHONNOUSERSITE=1
  Must Have   TEMP=
  Must Have   TMP=
  Must Have   RCC_ENVIRONMENT_HASH=
  Must Have   RCC_INSTALLATION_ID=
  Must Have   RCC_TRACKING_ALLOWED=
  Must Have   ROBOT_ROOT=
  Must Have   ROBOT_ARTIFACTS=
  Wont Have   RC_API_SECRET_HOST=
  Wont Have   RC_API_WORKITEM_HOST=
  Wont Have   RC_API_SECRET_TOKEN=
  Wont Have   RC_API_WORKITEM_TOKEN=
  Wont Have   RC_WORKSPACE_ID=
  Step        build/rcc holotree check --controller citests

Goal: See variables from specific environment with robot.yaml knowledge in JSON form
  Step        build/rcc holotree variables --controller citests --json conda/testdata/conda.yaml --config tmp/alternative.yaml -r tmp/fluffy/robot.yaml -e tmp/fluffy/devdata/env.json
  Must Be Json Response

Goal: See diagnostics as valid JSON form
  Step        build/rcc configure diagnostics --json
  Must Be Json Response

Goal: Simulate issue report sending with dryrun
  Step        build/rcc feedback issue --controller citests --dryrun --report robot_tests/report.json --attachments robot_tests/conda.yaml
  Must Have   "report":
  Must Have   "zipfile":
  Must Have   "installationId":
  Must Have   "platform":
  Must Be Json Response
  Use STDERR
  Must Have   OK
