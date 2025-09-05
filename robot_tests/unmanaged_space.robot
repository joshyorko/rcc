*** Settings ***
Library  OperatingSystem
Library  supporting.py
Resource  resources.robot
Suite Setup  Holotree setup

*** Keywords ***
Holotree setup
  Fire And Forget   build/rcc ht delete 4e67cd8

*** Test cases ***

Goal: See variables from specific unamanged space
  Step        build/rcc holotree variables --unmanaged --space python39 --controller citests robot_tests/python3913.yaml
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
  Use STDERR
  Must Have   This is unmanaged holotree space
  Must Have   Progress: 01/15
  Must Have   Progress: 02/15
  Must Have   Progress: 04/15
  Must Have   Progress: 05/15
  Must Have   Progress: 06/15
  Must Have   Progress: 14/15
  Must Have   Progress: 15/15

Goal: Wont allow use of unmanaged space with incompatible conda.yaml
  Step        build/rcc holotree variables --debug --unmanaged --space python39 --controller citests robot_tests/python375.yaml    6
  Wont Have   ROBOCORP_HOME=
  Wont Have   PYTHON_EXE=
  Wont Have   RCC_ENVIRONMENT_HASH=
  Wont Have   RCC_INSTALLATION_ID=
  Use STDERR
  Must Have   This is unmanaged holotree space
  Must Have   Progress: 01/15
  Must Have   Progress: 02/15
  Must Have   Progress: 15/15

  Wont Have   Progress: 04/15
  Wont Have   Progress: 05/15
  Wont Have   Progress: 06/15
  Wont Have   Progress: 14/15

  Must Have   Existing unmanaged space fingerprint
  Must Have   does not match requested one
  Must Have   Quitting!

Goal: Allows different unmanaged space for different conda.yaml
  Step        build/rcc holotree variables --unmanaged --space python37 --controller citests robot_tests/python375.yaml
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
  Use STDERR
  Must Have   This is unmanaged holotree space
  Must Have   Progress: 01/15
  Must Have   Progress: 02/15
  Must Have   Progress: 04/15
  Must Have   Progress: 05/15
  Must Have   Progress: 06/15
  Must Have   Progress: 14/15
  Must Have   Progress: 15/15

Goal: Wont allow use of unmanaged space with incompatible conda.yaml when two unmanaged spaces exists
  Step        build/rcc holotree variables --debug --unmanaged --space python37 --controller citests robot_tests/python3913.yaml    6
  Use STDERR
  Must Have   This is unmanaged holotree space
  Must Have   Progress: 01/15
  Must Have   Progress: 02/15
  Must Have   Progress: 15/15

  Wont Have   Progress: 05/15
  Wont Have   Progress: 14/15

  Must Have   Existing unmanaged space fingerprint
  Must Have   does not match requested one
  Must Have   Quitting!

Goal: See variables from specific environment without robot.yaml knowledge in JSON form
  Step        build/rcc holotree variables --unmanaged --space python39 --controller citests --json robot_tests/python3913.yaml
  Must Be Json Response
  Use STDERR
  Must Have   This is unmanaged holotree space

Goal: Can see unmanaged spaces in listings
  Step        build/rcc holotree list --controller citests
  Use STDERR
  Must Have   UNMNGED_
  Must Have   python37
  Must Have   python39

Goal: Can delete all unmanaged spaces with one command
  Step        build/rcc holotree delete --controller citests UNMNGED_
  Use STDERR
  Must Have   Removing UNMNGED_

Goal: After deleted, cannot see unmanaged spaces in listings
  Step        build/rcc holotree list --controller citests
  Use STDERR
  Wont Have   UNMNGED_
  Wont Have   python37
  Wont Have   python39
