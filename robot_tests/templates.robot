*** Settings ***
Library         OperatingSystem
Library         supporting.py
Resource        resources.robot

*** Test cases ***

Goal: Initialize new standard robot.
  Step        build/rcc robot init -i --controller citests -t standard -d tmp/standardi -f
  Use STDERR
  Must Have   OK.

Goal: Standard robot has correct hash.
  Step        build/rcc holotree hash --silent --controller citests tmp/standardi/conda.yaml
  Must Have   1cdd0b852854fe5b

Goal: Running standard robot is succesful.
  Step        build/rcc task run --space templates --controller citests --robot tmp/standardi/robot.yaml
  Use STDERR
  Must Have   point of view, "actual main robot run" was SUCCESS.
  Must Have   OK.

Goal: Initialize new python robot.
  Step        build/rcc robot init -i --controller citests -t python -d tmp/pythoni -f
  Use STDERR
  Must Have   OK.

Goal: Python robot has correct hash.
  Step        build/rcc holotree hash --silent --controller citests tmp/pythoni/conda.yaml
  Must Have   1cdd0b852854fe5b

Goal: Running python robot is succesful.
  Step        build/rcc task run --space templates --controller citests --robot tmp/pythoni/robot.yaml
  Use STDERR
  Must Have   point of view, "actual main robot run" was SUCCESS.
  Must Have   OK.

Goal: Initialize new extended robot.
  Step        build/rcc robot init -i --controller citests -t extended -d tmp/extendedi -f
  Use STDERR
  Must Have   OK.

Goal: Extended robot has correct hash.
  Step        build/rcc holotree hash --silent --controller citests tmp/extendedi/conda.yaml
  Must Have   1cdd0b852854fe5b

Goal: Running extended robot is succesful. (Run All Tasks)
  Step        build/rcc task run --space templates --task "Run All Tasks" --controller citests --robot tmp/extendedi/robot.yaml
  Use STDERR
  Must Have   point of view, "actual main robot run" was SUCCESS.
  Must Have   OK.

Goal: Running extended robot is succesful. (Run Example Task)
  Step        build/rcc task run --space templates --task "Run Example Task" --controller citests --robot tmp/extendedi/robot.yaml
  Use STDERR
  Must Have   point of view, "actual main robot run" was SUCCESS.
  Must Have   OK.

Goal: Correct holotree spaces were created.
  Step        build/rcc holotree list
  Use STDERR
  Must Have   rcc.citests
  Must Have   templates
  Wont Have   rcc.user

Goal: Can get plan for used environment.
  Step        build/rcc holotree plan 4e67cd8_c6880905
  Must Have   micromamba plan
  Must Have   pip plan
  Must Have   post install plan
  Must Have   activation plan
  Must Have   installation plan complete
  Use STDERR
  Must Have   OK.

Goal: Holotree is still correct.
  Step        build/rcc holotree check --controller citests
  Use STDERR
  Must Have   OK.
