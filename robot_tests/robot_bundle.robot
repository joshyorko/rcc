*** Settings ***
Library  OperatingSystem
Library  supporting.py
Resource  resources.robot
Suite Setup  Bundle setup

*** Keywords ***
Bundle setup
  Fire And Forget   build/rcc ht delete --yes 4e67cd8
  Create Bundle Files

Create Bundle Files
  Step    python3 -c "import zipfile, os; zf = zipfile.ZipFile('tmp/robot_bundle.zip', 'w', zipfile.ZIP_DEFLATED); [(zf.write(os.path.join(r, f), os.path.relpath(os.path.join(r, f), 'robot_tests/testdata/robot_bundle')) if os.path.isfile(os.path.join(r, f)) else None) for r, _, fs in os.walk('robot_tests/testdata/robot_bundle') for f in fs]; zf.close()"
  Step    python3 -c "with open('tmp/robot_sfx.py', 'wb') as f: f.write(b'print(\"hello\")\\n'); f.write(open('tmp/robot_bundle.zip', 'rb').read())"

*** Test cases ***

Goal: Run task from plain bundle
  Must Exist    tmp/robot_bundle.zip
  Step    build/rcc robot run-from-bundle tmp/robot_bundle.zip --task test --controller citests
  Use STDOUT
  Must Have    Hello from bundle task
  Use STDERR
  Must Have    OK.

Goal: Run task from SFX bundle
  Must Exist    tmp/robot_sfx.py
  Step    build/rcc robot run-from-bundle tmp/robot_sfx.py --task test --controller citests
  Use STDOUT
  Must Have    Hello from bundle task
  Use STDERR
  Must Have    OK.

Goal: Create EXE SFX bundle
  Step    python3 -c "with open('tmp/robot_sfx.exe', 'wb') as f: f.write(b'MZ dummy header\\n'); f.write(open('tmp/robot_bundle.zip', 'rb').read())"
  Must Exist    tmp/robot_sfx.exe
  Step    chmod +x tmp/robot_sfx.exe

Goal: Run task from EXE SFX bundle
  Step    build/rcc robot run-from-bundle tmp/robot_sfx.exe --task test --controller citests
  Use STDOUT
  Must Have    Hello from bundle task
  Use STDERR
  Must Have    OK.

Goal: Run task from plain bundle and check env processing
  Step    build/rcc robot run-from-bundle tmp/robot_bundle.zip --task test --controller citests
  Use STDERR
  Must Have    Processing environment "extra"
  Use STDOUT
  Must Have    Hello from bundle task

Goal: Create bundle using rcc robot bundle command
  Step    build/rcc robot bundle --controller citests -r robot_tests/testdata/robot_bundle/robot/robot.yaml -o tmp/rcc_created_bundle.py
  Must Exist    tmp/rcc_created_bundle.py

Goal: Run task from rcc created bundle
  Step    build/rcc robot run-from-bundle tmp/rcc_created_bundle.py --task test --controller citests
  Use STDOUT
  Must Have    Hello from bundle task
  Use STDERR
  Must Have    OK.

Goal: Unpack plain bundle
  Step    build/rcc robot unpack --bundle tmp/robot_bundle.zip --output tmp/unpack_test/plain
  Must Exist    tmp/unpack_test/plain/robot.yaml
  Must Exist    tmp/unpack_test/plain/task.py
  Use STDERR
  Must Have    OK.

Goal: Unpack SFX bundle
  Step    build/rcc robot unpack --bundle tmp/robot_sfx.py --output tmp/unpack_test/sfx
  Must Exist    tmp/unpack_test/sfx/robot.yaml
  Must Exist    tmp/unpack_test/sfx/task.py
  Use STDERR
  Must Have    OK.

Goal: Unpack rcc created bundle
  Step    build/rcc robot unpack --bundle tmp/rcc_created_bundle.py --output tmp/unpack_test/rcc
  Must Exist    tmp/unpack_test/rcc/robot.yaml
  Must Exist    tmp/unpack_test/rcc/task.py
  Use STDERR
  Must Have    OK.

Goal: Unpack to existing directory fails
  Create Directory  tmp/unpack_test/fail
  Step    build/rcc robot unpack --bundle tmp/robot_bundle.zip --output tmp/unpack_test/fail    1
  Use STDERR
  Must Have    already exists

Goal: Unpack to existing directory with force succeeds
  Create Directory  tmp/unpack_test/force
  Step    build/rcc robot unpack --bundle tmp/robot_bundle.zip --output tmp/unpack_test/force --force
  Must Exist    tmp/unpack_test/force/robot.yaml
  Use STDERR
  Must Have    OK.

Goal: Unpack missing bundle fails
  Step    build/rcc robot unpack --bundle tmp/missing.zip --output tmp/unpack_test/missing    2
  Use STDERR
  Must Have    does not exist

Goal: Cleanup
  Fire And Forget    rm -f tmp/robot_bundle.zip tmp/robot_sfx.py tmp/robot_sfx.exe tmp/rcc_created_bundle.py
  Remove Directory  tmp/unpack_test  True
