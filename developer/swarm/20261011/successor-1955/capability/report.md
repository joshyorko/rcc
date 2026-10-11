# RCC #125 successor: host capability and integrity probe

## Result

This Linux x86_64 execution environment does not expose working `FICLONE` on either tested mount. The single 64 KiB attempt on each mount returned the exact error `OSError(95, "Operation not supported")` (`ENOTSUP`). The tested mounts were `/workspace` (`overlay`, rw, `metacopy=off`) and `/tmp` (`tmpfs`, rw). This result applies to these mounts on this host only.

The bounded independent-copy fallback passed on both mounts. Before mutation, source and copy SHA-256 matched (`6fc179cfd193754e6109ad043f56d146c7e7d7c3623ffceae318266286f58388`); their inodes differed. After changing the first byte of each copy, its hash became `a85ebe76235c9f14d526163d4569928efd97da952b09bf9a0ef0607071877f20`, while the retained source hash and inode remained unchanged. A symlink source was rejected with `EINVAL` before a destination was created. The source, mutated copy, and symlink are retained under the two owned scratch roots for coordinator rehashing.

## Exact host context

- Kernel: `Linux 6.18.44`, x86_64; Python 3.12.14.
- `/workspace`: `overlay[/workspace]`, rw; options include `lowerdir=/run/kata-containers/erofs-multi-layer/.../lower-0`, `upperdir=.../upper/upper`, `workdir=.../upper/work`, `index=off`, `metacopy=off`.
- `/tmp`: `tmpfs`, rw (findmnt also printed a ro view before the rw entry); `stat -f` reported `tmpfs`.
- `/sys/fs/fuse` and an empty `/sys/fs/fuse/connections` exist, and `fuse`/`fusectl` are registered in `/proc/filesystems`. `/dev/fuse` is absent. Kernel registration therefore does not provide a usable FUSE device here; lazy/FUSE workload testing is blocked by that missing device.
- `/dev` has no `loop*` device. `losetup` and `mount` executables exist, but no loop device was available; no mount or loop operation was attempted. Kernel filesystems list includes `overlay` and `erofs`, but the probe did not mount or exercise a compressed image. The mounted overlay demonstrates this workspace's overlay/COW filesystem context only; it does not establish compressed-image performance or behavior.
- This is Linux, so macOS `clonefile` is unavailable to exercise. No Windows/macOS capability claim is made.

## Reproduction and exit record

- The one-shot probe source as executed is `probe-executed.py`. It creates exactly one 64 KiB FICLONE attempt per mount, then runs the fallback only after `ENOTSUP`; it refuses to run against existing probe names/receipt. The retained raw attempt output is `probe.stdout`; its raw stderr is empty.
- The actual probe command was run with `ulimit -v 262144` and returned exit **2**. The two capability attempts and fallback operations completed. The probe's aggregate status expression incorrectly read a top-level `source_unchanged` field even in the fallback branch, so the emitted `all_checks_pass:false` and exit 2 are assessor-logic errors, not failed fallback checks. The retained receipt records every underlying comparison.
- `probe.py` contains the corrected aggregate expression for a fresh scratch environment. Do not rerun it against these retained paths: the one-per-mount probes have already been consumed and its guard correctly refuses repeat work.
- The independent read-only artifact verifier `verify_receipt.py` recomputed retained source/copy hashes and identities, checked the unchanged source and distinct inode, confirmed the mutated copies and rejected-symlink outcome, and exited **0** (`verify.stdout`; empty `verify.stderr`).
- The initial `/usr/bin/time -v` wrapper was unavailable and exited **127** (`probe.time`: `/usr/bin/time: No such file or directory`); the probe was then run directly under the 256 MiB virtual-memory limit. No RSS number is claimed. Probe execution completed in under one second by the tool's wall-clock report.
- The independently retained workspace and `/tmp` payload trees are 156,216 bytes and 131,126 bytes respectively, below the 32 MiB scratch budget. No repository source or existing cache/home was changed.

## Recommendation and boundary

For this host, a safe independent workload is the tested no-reflink copy path with hash, inode, mode, source-immutability, and symlink-rejection checks. Reflink/clonefile comparisons require a separately admitted native filesystem/platform exposing those operations; FUSE lazy workloads require an actual `/dev/fuse`; compressed-image/COW work requires an isolated image fixture and suitable backing device. These are feasibility recommendations only. This probe has no timed comparison, end-to-end workload, speedup result, or default-winner evidence.

## Receipt hashes

- Executed one-shot probe: `25577122062fb21f60b0fb1c659d3a1f5669ca3f995ef51beb472988df2f91a8` (`probe-executed.py`).
- Corrected fresh-scratch probe: `5ef15c5de710619d763b98791cae67722c02791b2eb5ab3805bcb33202043614` (`probe.py`).
- Read-only verifier: `5a9456a98bb470f5ff6635c28cb6c65526622998f7d7c8b2a2fec2203eaf5e3e` (`verify_receipt.py`).
- Raw probe receipt: `cd926004c0433e880c1564ad20234c2f9ae312a4fa0ed021e2348712949d9928` (`probe-receipt.json`).
- Source payload SHA-256 on both mounts: `6fc179cfd193754e6109ad043f56d146c7e7d7c3623ffceae318266286f58388`.

Documentation receipt
- Canonical guidance: no change
- Durable learning: this container's workspace overlay and `/tmp` tmpfs both reject `FICLONE` with `ENOTSUP`; FUSE registration exists without `/dev/fuse`
- Evidence: retained 64 KiB source/copy artifacts, one-shot probe receipt, raw output, and read-only verifier
- Stale guidance removed: none
- Remaining uncertainty: native ext4/XFS/APFS reflink support, usable FUSE hosts, compressed-image/COW behavior, and any performance effect remain untested
