*** Settings ***
Library  OperatingSystem
Library  supporting.py
Resource  resources.robot

*** Test cases ***

Goal: Can see human readable catalog of robots
  Step        build/rcc holotree catalogs --controller citests
  Use STDERR
  Must Have   inside hololib
  Must Have   Age (days)
  Must Have   Idle (days)
  Must Have   ffd32af1fdf0f253
  Must Have   OK.

Goal: Can see machine readable catalog of robots
  Step        build/rcc holotree catalogs --controller citests --json
  Must Be Json Response
  Must Have   ffd32af1fdf0f253
  Must Have   "age_in_days": 0,
  Must Have   "days_since_last_use": 0,
  Must Have   "holotree":
  Must Have   "blueprint":
  Must Have   "files":
  Must Have   "directories":

Goal: Can check holotree with retries
  Step        build/rcc holotree check --retries 5 --controller citests
  Use STDERR
  Must Have   OK.

Goal: Can remove catalogs with check from hololib by ids and give warnings
  Step        build/rcc holotree remove cafebabe9000 --check 5 --controller citests
  Use STDERR
  Must Have   Warning: No catalogs given, so nothing to do. Quitting!
  Wont Have   Warning: Remember to run `rcc holotree check` after you have removed all desired catalogs!
  Must Have   OK.

Goal: Can remove catalogs from hololib by idle days and give warnings
  Step        build/rcc holotree remove --unused 90 --controller citests
  Use STDERR
  Must Have   Warning: No catalogs given, so nothing to do. Quitting!
  Must Have   Warning: Remember to run `rcc holotree check` after you have removed all desired catalogs!
  Must Have   OK.

Goal: Can remove catalogs with check from hololib by ids correctly
  Step        build/rcc holotree remove ffd32af1fdf0f253 --check 5 --controller citests
  Use STDERR
  Wont Have   Warning: No catalogs given, so nothing to do. Quitting!
  Wont Have   Warning: Remember to run `rcc holotree check` after you have removed all desired catalogs!
  Must Have   OK.
