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
  Step    python3 -c "import zipfile, os; zf = zipfile.ZipFile('tmp/test_bundle.zip', 'w', zipfile.ZIP_DEFLATED); [(zf.write(os.path.join(r, f), os.path.relpath(os.path.join(r, f), 'robot_tests/testdata/bundle')) if os.path.isfile(os.path.join(r, f)) else None) for r, _, fs in os.walk('robot_tests/testdata/bundle') for f in fs]; zf.close()"
  Step    python3 -c "import zipfile, os; zf = zipfile.ZipFile('tmp/multi_bundle.zip', 'w', zipfile.ZIP_DEFLATED); [(zf.write(os.path.join(r, f), os.path.relpath(os.path.join(r, f), 'robot_tests/testdata/bundle_multi')) if os.path.isfile(os.path.join(r, f)) else None) for r, _, fs in os.walk('robot_tests/testdata/bundle_multi') for f in fs]; zf.close()"
  Step    python3 -c "import zipfile; zipfile.ZipFile('tmp/empty_bundle.zip', 'w').close()"

*** Test cases ***

Goal: Create a test bundle with a single environment
  Must Exist    tmp/test_bundle.zip

Goal: Build environment from bundle
  Step    build/rcc holotree build-from-bundle tmp/test_bundle.zip --controller citests
  Use STDERR
  Must Have    Found 1 environment(s) in bundle
  Must Have    Processing environment "default"
  Must Have    Blueprint hash for "default" is
  Must Have    Built environment "default" with hash
  Must Have    Successfully built 1 environment(s) from bundle
  Must Have    OK.

Goal: Verify environment hash matches expected
  Step    build/rcc holotree hash robot_tests/testdata/bundle/envs/default/conda.yaml --controller citests
  Use STDERR
  Must Have    fbc5c1dc22abe9e3

Goal: Verify environment exists in holotree
  Step    build/rcc holotree catalogs --controller citests
  Use STDERR
  Must Have    fbc5c1dc22abe9e3

Goal: Verify environment can be used with variables command
  Step    build/rcc holotree variables robot_tests/testdata/bundle/envs/default/conda.yaml --space bundletest --controller citests
  Must Have    PYTHON_EXE=
  Must Have    CONDA_DEFAULT_ENV=rcc
  Must Have    RCC_ENVIRONMENT_HASH=

Goal: Build from bundle with JSON output
  Step    build/rcc holotree build-from-bundle tmp/test_bundle.zip --json --controller citests --silent
  Must Be Json Response
  Must Have    "bundle"
  Must Have    "total"
  Must Have    "succeeded"
  Must Have    "environments"
  Must Have    "fbc5c1dc22abe9e3"

Goal: Build from bundle with restore flag
  Step    build/rcc holotree build-from-bundle tmp/test_bundle.zip --restore --controller citests
  Use STDERR
  Must Have    Built and restored environment "default"
  Must Have    OK.

Goal: Test bundle with multiple environments
  Must Exist    tmp/multi_bundle.zip
  Step    build/rcc holotree build-from-bundle tmp/multi_bundle.zip --controller citests
  Use STDERR
  Must Have    Found 2 environment(s) in bundle
  Must Have    1/2: Processing environment
  Must Have    2/2: Processing environment
  Must Have    Successfully built 2 environment(s) from bundle

Goal: Test error handling with empty bundle
  Must Exist    tmp/empty_bundle.zip
  Step    build/rcc holotree build-from-bundle tmp/empty_bundle.zip --controller citests    3
  Use STDERR
  Must Have    No environment definitions

Goal: Create a self-extracting bundle
  Step    python3 -c "import zipfile, os; zf = zipfile.ZipFile('tmp/test_bundle.zip', 'w', zipfile.ZIP_DEFLATED); [(zf.write(os.path.join(r, f), os.path.relpath(os.path.join(r, f), 'robot_tests/testdata/bundle')) if os.path.isfile(os.path.join(r, f)) else None) for r, _, fs in os.walk('robot_tests/testdata/bundle') for f in fs]; zf.close()"
  Step    python3 -c "with open('tmp/sfx_bundle.py', 'wb') as f: f.write(b'print(\"hello\")\\n'); f.write(open('tmp/test_bundle.zip', 'rb').read())"
  Must Exist    tmp/sfx_bundle.py

Goal: Build environment from self-extracting bundle
  Step    build/rcc holotree build-from-bundle tmp/sfx_bundle.py --controller citests
  Use STDERR
  Must Have    Found 1 environment(s) in bundle
  Must Have    Successfully built 1 environment(s) from bundle
  Must Have    fbc5c1dc22abe9e3
  Must Have    OK.

Goal: Cleanup test artifacts
  Fire And Forget    rm -f tmp/test_bundle.zip tmp/multi_bundle.zip tmp/empty_bundle.zip tmp/sfx_bundle.py
  Fire And Forget    rm -rf tmp/bundle_test tmp/bundle_multi
