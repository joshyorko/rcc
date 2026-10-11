# Exact-7945 embed provenance and offline-build assessment

Read-only preparation for the proposed #125 native `Filesystem.GetObject` versus indexed-pack comparison. No provider fixture was read; no source snapshot, cache, ignored embed output, build output, or network state was changed. No compile or benchmark was run.

## Immutable proposal inputs

- RCC source: commit `7945f956b428359928908a9f42e36417b113dafd`, tree `49baf943b9d64ef79af56ec8f34eebe0385477b1`.
- Read-only source projection: `source-7945/`; `source-manifest.json` SHA256 `c1bc6f5c00a96c9e1e3dc4bcf4bb39b832ef525bc2795f570af61765f17246e6`.
- `source-7945/tasks.py` SHA256 `4e6666dfedc95d3d6bbbdb012bdeff838957d9a070e3e82917bcc7bf26a404b7`.
- `source-7945/go.mod` SHA256 `eae3d866a96c296d0cdcb4e9729b4f872abf54dab331993cf717a971ac5b46d0`.
- Existing, uncompiled proposal harness SHA256 `329246f5ba513124ff4603c9fa1a7072b06f4776979206e1a5c3db6135573af4`; existing plan SHA256 `05e8771be5ca114dca293b922c0bb2468ca7f31f8a547fccdfadc9bb4273937d`. Both remain unchanged.

## Embed provenance

Exact-7945 `assets/micromamba_version.txt` contains `v2.5.0`. In `tasks.py`, Linux64 is mapped to the immutable GitHub release archive `micromamba-linux-64.tar.bz2` for release `2.5.0-2`, expected archive SHA256 `cec496f2299f9ceb5f5e23fc2ccf081fffda0c4bc87a6fdab575bf1e04f103b6`, member `bin/micromamba`. The task validates the downloaded archive against that expected hash before extracting and gzip-compressing the member. The primary micro.mamba.pm URL also receives this expected digest.

A bounded read-only search under `/workspace/work/rcc-swarm`, `/home/agent/.cache`, and `/home/agent/.robocorp` found no retained `micromamba-linux-64.tar.bz2` or equivalent archive. Existing source-tree gzip files are 7,108,897 bytes, but the asset logs only say they downloaded from the official primary URL and compressed the binary; they do not record the resolved archive digest, exact producing source commit, or a binary digest linked to a verified archive. Therefore none qualifies as exact-7945 embed provenance. In particular, the 727 gzip must not be reused absent an independent payload match to the verified archive.

The retained `root-main7945-assets` receipts bind final RCC artifact hashes to source `7945f956...`, but do not bind the embedded micromamba bytes to the official archive digest. The 7945 RCC binary itself is not a substitute for archive provenance without a separately reviewed extraction and payload comparison.

## Embed regeneration boundary after separate root admission

Work only in a fresh owned disposable copy of `source-7945`; preserve the frozen read-only projection and its manifest. Reproduce embed inputs from that copy's tracked source: zip the three template directories (`standard`, `extended`, `python`); copy `assets/*.yaml`, `*.txt`, `*.py`, `docs/*.md`, and `assets/man/*.txt` exactly; add the Linux gzip only from the official archive after checking the archive SHA, safe tar members, and `bin/micromamba` payload SHA. Use isolated generated outputs with hashes/size receipt and `GOTOOLCHAIN=local`, `GOPROXY=off`, no source or tool downloads. The ordinary `assets` task has a micromamba pre-task and may skip existing ignored files, so it must not be run against shared or prepopulated source. A root-reviewed generator that reproduces only the embed files in the disposable copy is safer and auditable.

Tracked source sizes are modest (the projection is 1,541,402 bytes); the Linux gzip alone is 7.1 MB. A disposable workspace budget of 32 MiB should hold source plus embed inputs, but compile scratch could approach that bound and must be measured under an external guard before admission. Proposed limits remain workspace 32 MiB, owned RAM/cache 1 GiB, process-tree RSS 2 GiB, per-pass 30 s, combined build/runtime 180 s, and free-space floor 2,688,548,864 bytes. Do not admit a compile until archive provenance is solved and root rechecks resources.

## Offline Go dependency inspection

Official Go tool is `/workspace/work/rcc-swarm/tools/go1.26.9/bin/go` (previously verified SHA256 `e90c4e12df236ea0c2e7536fd68da8a47fc0b585c19588bf6c1a70cfc296a0f9`). The existing module cache is `/home/agent/go/pkg/mod` (read-only inspection reported about 517,546,888 file bytes). The direct external imports in the frozen 19-package closure (`siphash`, `shlex`, `go-isatty`, `viper`, `x/sys`, `x/term`, and `yaml.v2`) have module source directories present. Several `go.mod` requirements outside that direct import set are absent from module directories; package graph resolution was not executed, so offline completeness is not established. Existing Go build cache `/home/agent/.cache/go-build` is shared and must not be modified. A later admitted invocation should use an owned GOCACHE and `GOPROXY=off`, `GOSUMDB=off`, `GOTOOLCHAIN=local`; if any required module is missing, stop without download rather than falling back to a shared cache or network.
