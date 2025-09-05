*** Settings ***
Library  OperatingSystem
Library  supporting.py
Resource  resources.robot

*** Test cases ***

Goal: Initially there are no profiles
  Step        build/rcc configuration switch  2
  Use STDERR
  Must Have   No profiles found, you must first import some.

Goal: Can import profiles into rcc
  Step        build/rcc configuration import --filename robot_tests/profile_alpha.yaml
  Use STDERR
  Must Have   OK.

  Step        build/rcc configuration import --filename robot_tests/profile_beta.yaml
  Use STDERR
  Must Have   OK.

Goal: Can see imported profiles
  Step        build/rcc configuration switch
  Must Have   Alpha: Alpha settings
  Must Have   Beta: Beta settings
  Wont Have   Gamma: Gamma settings
  Must Have   Currently active profile is: default
  Use STDERR
  Must Have   OK.

Goal: Can see imported profiles as json
  Step        build/rcc configuration switch --json
  Must Be Json Response
  Must Have   "current"
  Must Have   "profiles"
  Must Have   "Alpha settings"
  Must Have   "Beta settings"

Goal: Can switch to Alpha profile
  Step        build/rcc configuration switch --profile alpha
  Use STDERR
  Must Have   OK.

  Step        build/rcc configuration switch
  Use STDOUT
  Must Have   Currently active profile is: Alpha
  Use STDERR
  Must Have   OK.

Goal: Quick diagnostics can show alpha profile information
  Step        build/rcc configuration diagnostics --quick --json
  Must Be Json Response
  Must Have   "config-micromambarc-used": "false"
  Must Have   "config-piprc-used": "false"
  Must Have   "config-settings-yaml-used": "true"
  Must Have   "config-ssl-no-revoke": "false"
  Must Have   "config-ssl-verify": "true"
  Must Have   "config-no-proxy": ""
  Must Have   "config-https-proxy": ""
  Must Have   "config-http-proxy": ""

Goal: Can switch to Beta profile
  Step        build/rcc configuration switch --profile Beta
  Use STDERR
  Must Have   OK.

  Step        build/rcc configuration switch
  Use STDOUT
  Must Have   Currently active profile is: Beta
  Use STDERR
  Must Have   OK.

Goal: Quick diagnostics can show beta profile information
  Step        build/rcc configuration diagnostics --quick --json
  Must Be Json Response
  Must Have   "config-micromambarc-used": "true"
  Must Have   "config-piprc-used": "true"
  Must Have   "config-settings-yaml-used": "true"
  Must Have   "config-ssl-no-revoke": "true"
  Must Have   "config-ssl-verify": "false"
  Must Have   "config-legacy-renegotiation-allowed": "true"
  Must Have   "config-no-proxy": "noproxy.betaputkinen.net"
  Must Have   "config-https-proxy": "http://bad.betaputkinen.net:1234/"
  Must Have   "config-http-proxy": "http://bad.betaputkinen.net:2345/"

Goal: Can import and switch to Gamma profile immediately
  Step        build/rcc configuration import --filename robot_tests/profile_gamma.yaml --switch
  Use STDERR
  Must Have   OK.

  Step        build/rcc configuration switch
  Use STDOUT
  Must Have   Alpha: Alpha settings
  Must Have   Beta: Beta settings
  Must Have   Gamma: Gamma settings
  Must Have   Currently active profile is: Gamma
  Use STDERR
  Must Have   OK.

Goal: Can remove profile while it is still used
  Step        build/rcc configuration remove --profile Gamma
  Use STDERR
  Must Have   OK.

  Step        build/rcc configuration switch
  Use STDOUT
  Must Have   Alpha: Alpha settings
  Must Have   Beta: Beta settings
  Wont Have   Gamma: Gamma settings
  Must Have   Currently active profile is: Gamma
  Use STDERR
  Must Have   OK.

Goal: Can switch to no profile
  Step        build/rcc configuration switch --noprofile
  Use STDERR
  Must Have   OK.

  Step        build/rcc configuration switch
  Use STDOUT
  Must Have   Currently active profile is: default
  Use STDERR
  Must Have   OK.

Goal: Can export profile
  Step        build/rcc configuration export --profile Alpha --filename tmp/exported_alpha.yaml
  Use STDERR
  Must Have   OK.
