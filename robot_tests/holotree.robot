*** Settings ***
Library  OperatingSystem
Library  supporting.py
Resource  resources.robot
Suite Setup  Holotree setup

*** Keywords ***
Holotree setup
  Fire And Forget   build/rcc ht delete 4e67cd8

*** Test cases ***

Goal: Initialize new standard robot into tmp/holotin folder using force.
  Step        build/rcc robot init --controller citests -t extended -d tmp/holotin -f
  Use STDERR
  Must Have   OK.

Goal: See variables from specific environment without robot.yaml knowledge
  Step        build/rcc holotree variables --space jam --controller citests conda/testdata/conda.yaml
  Must Have   ROBOCORP_HOME=
  Must Have   PYTHON_EXE=
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

Goal: See variables from specific environment with robot.yaml but without task
  Step        build/rcc holotree variables --space holotin --controller citests -r tmp/holotin/robot.yaml
  Must Have   ROBOCORP_HOME=
  Must Have   PYTHON_EXE=
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

Goal: See variables from specific environment without robot.yaml knowledge in JSON form
  Step        build/rcc holotree variables --space jam --controller citests --json conda/testdata/conda.yaml
  Must Be Json Response

Goal: See variables from specific environment with robot.yaml knowledge
  Step        build/rcc holotree variables --space jam --controller citests conda/testdata/conda.yaml --config tmp/alternative.yaml -r tmp/holotin/robot.yaml -e tmp/holotin/devdata/env.json
  Must Have   ROBOCORP_HOME=
  Must Have   PYTHON_EXE=
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
  Use STDERR
  Wont Have   (virtual)

Goal: See variables from specific environment with robot.yaml knowledge in JSON form
  Step        build/rcc holotree variables --space jam --controller citests --json conda/testdata/conda.yaml --config tmp/alternative.yaml -r tmp/holotin/robot.yaml -e tmp/holotin/devdata/env.json
  Must Be Json Response
  Use STDERR
  Wont Have   (virtual)

Goal: Liveonly works and uses virtual holotree
  Step        build/rcc holotree vars --liveonly --space jam --controller citests robot_tests/certificates.yaml --config tmp/alternative.yaml --timeline
  Must Have   ROBOCORP_HOME=
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
  Wont Have   PYTHON_EXE=
  Wont Have   PYTHONPATH=
  Wont Have   ROBOT_ROOT=
  Wont Have   ROBOT_ARTIFACTS=
  Wont Have   RC_API_SECRET_HOST=
  Wont Have   RC_API_WORKITEM_HOST=
  Wont Have   RC_API_SECRET_TOKEN=
  Wont Have   RC_API_WORKITEM_TOKEN=
  Wont Have   RC_WORKSPACE_ID=
  Use STDERR
  Must Have   (virtual)

Goal: Do quick cleanup on environments
  Step        build/rcc config cleanup --controller citests --quick
  Must Exist  %{ROBOCORP_HOME}/micromamba/
  Wont Exist  %{ROBOCORP_HOME}/pkgs/
  Wont Exist  %{ROBOCORP_HOME}/pipcache/
  Wont Exist  %{ROBOCORP_HOME}/templates/
  Use STDERR
  Must Have   OK

Goal: Liveonly works and uses virtual holotree and can give output in JSON form
  Step        build/rcc ht vars --liveonly --space jam --controller citests --json robot_tests/certificates.yaml --config tmp/alternative.yaml --timeline
  Must Be Json Response
  Use STDERR
  Must Have   (virtual)
