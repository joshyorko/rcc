*** Settings ***
Library  OperatingSystem
Library  supporting.py
Resource  resources.robot
Suite Setup  Unpack Setup
Suite Teardown  Unpack Teardown

*** Keywords ***
Unpack Setup
  Create Directory  tmp/unpack_test
  # Create a dummy bundle with robot/ prefix
  Step    python3 -c "import zipfile, os; zf = zipfile.ZipFile('tmp/unpack_test/bundle.zip', 'w', zipfile.ZIP_DEFLATED); zf.writestr('robot/robot.yaml', 'content'); zf.writestr('robot/task.py', 'print(\"hello\")'); zf.close()"

Unpack Teardown
  Remove Directory  tmp/unpack_test  True

*** Test cases ***

Goal: Unpack valid bundle
  Step    build/rcc robot unpack --bundle tmp/unpack_test/bundle.zip --output tmp/unpack_test/out1
  Must Exist    tmp/unpack_test/out1/robot.yaml
  Must Exist    tmp/unpack_test/out1/task.py
  Use STDERR
  Must Have    OK.

Goal: Unpack to existing directory fails
  Create Directory  tmp/unpack_test/out2
  Step    build/rcc robot unpack --bundle tmp/unpack_test/bundle.zip --output tmp/unpack_test/out2    1
  Use STDERR
  Must Have    already exists

Goal: Unpack to existing directory with force succeeds
  Create Directory  tmp/unpack_test/out3
  Step    build/rcc robot unpack --bundle tmp/unpack_test/bundle.zip --output tmp/unpack_test/out3 --force
  Must Exist    tmp/unpack_test/out3/robot.yaml
  Use STDERR
  Must Have    OK.

Goal: Unpack missing bundle fails
  Step    build/rcc robot unpack --bundle tmp/unpack_test/missing.zip --output tmp/unpack_test/out4    2
  Use STDERR
  Must Have    does not exist
