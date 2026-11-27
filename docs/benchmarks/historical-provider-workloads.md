# Historical Conda workloads

The medium, large and heavy inputs in `robot_tests/profile_conda_*.yaml` are byte-for-byte copies from [PR #64 at 2a3e1a3](https://github.com/joshyorko/rcc/tree/2a3e1a38e4343f44a57f97a496fad0c6700d2a2d/robot_tests). Their comments give estimates. Use the installed prefix records for actual counts.

On native Linux amd64, freeze the official conda-forge `linux-64` and `noarch` repodata in a directory containing `SUBDIR/repodata.json.zst`, `SUBDIR/headers` and a `SHA256SUMS` file. Place the PyPI Robot Framework 6.1.1 wheel in the adjacent `wheels/` directory. The harness checks its published SHA-256. The following commands use the release-pinned micromamba 2.10.0, the released RCC v18.19.12 executable and the candidate built with native defaults.

```sh
python3 scripts/seed_historical_profile.py --fixture robot_tests/profile_conda_large.yaml \
  --repodata "$INPUTS/repodata" --micromamba "$MICROMAMBA" --cache "$INPUTS/archives" \
  --public-sharded-seed --pip-version 25.0.1 --output "$INPUTS/large-seed"
unshare --user --map-root-user --net sh -c 'ip link set lo up && exec "$@"' sh \
  python3 scripts/profile_historical.py --fixture large --baseline "$RELEASED_RCC" \
  --candidate "$CANDIDATE_RCC" --cache "$INPUTS/large-seed/records" \
  --wheel-dir "$INPUTS/wheels" --runs 5 --output "$RESULTS/large"
```

The sharded seed selects candidate records, checks their hashes and re-solves using the original frozen metadata. The timed providers see the same reduced channel of verified archives. The historical YAML leaves the pip installer version open. All timed manifests select the same interpreter-local pip and keep the original task dependencies. Use `--pin-records` to constrain every Conda version and build. The provider still executes a solver in that condition.

Each arm starts in a separate private home. A seeded run copies only that provider's package cache into a fresh home. A warm replay uses the preceding ready Holotree. Raw process output, cold and warm inventories, native import results, RCC progress timestamps and CPU profiles stay beside each run. Linux `wait4` reports CPU and peak RSS of the command and its waited descendants. RCC's CPU profile covers its Go process. The historical gzip and zstd results have a different workload and measurement boundary.


The [large Linux amd64 receipt](historical-large-linux-amd64.json) records all three arms and links the [raw CPU profiles, installed inventories and reports](historical-large-linux-amd64-cpu.zip). All 260 Conda name/version/build/digest tuples matched. The UCX post-link script is present in every prefix. Rattler disables execution of Conda link scripts; applications relying on their side effects require separate qualification.

The [heavy Linux amd64 receipt](historical-heavy-linux-amd64.json) covers the released baseline. The ML imports passed with 273 installed Conda records. The serial low-priority run was interrupted under sustained workstation pressure; candidate heavy timings and five-repeat historical ranges remain open.
