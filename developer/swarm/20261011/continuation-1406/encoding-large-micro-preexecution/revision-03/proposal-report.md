# Codec runner wrapper correction proposal (not a rerun)

The single admitted execution remains immutable at `../encoding-large-micro/`. Its worker and supervisor recorded PASS, 500/500 timed controls, and numeric worker exit 0; the actual outer Python invocation exited 1. The top-level `except BaseException` catches the `SystemExit(0)` raised by `sys.exit(supervisor())`, prints it as an exception, then exits 1. Therefore the complete invocation is FAIL due to this wrapper defect. This proposal does not alter that evidence or retroactively change its exit status.

A future runner can replace the top-level block with `sys.exit(supervisor_exit_code(supervisor))`, where `supervisor_exit_code` catches `Exception`, prints the traceback, and returns 1. `SystemExit` then remains outside the handler and the actual supervisor result is preserved. The isolated implementation is in `cli_wrapper.py`; five stub-only checks in `test_cli_wrapper.py` cover zero, nonzero, ordinary exception, and subprocess numeric exits. They do not import or invoke measurement code.

Verification command:

```sh
python -B -m unittest -v /workspace/work/rcc-swarm/evidence/continuation-20261011/encoding-large-micro-preexecution/revision-03/test_cli_wrapper.py
```

Exit: 0; 5 tests passed (0.055 s). The exception traceback printed by the in-process error test is expected; its assertion and the subprocess failure exit check both passed.

Immutable run artifact checksums rechecked after proposal tests:

- `encoding_large.py`: 040feb2e0db53787e8b6aa6e2f871d73d79f177be960e07349fd1993180b5315
- `plan.json`: 3a31525d00d0403c84acf1e908c85a71d17d8aebfa915dd7011476880af265df
- `results.json`: 9546441ea67af3e5ac7122db2a08a305b9e7f19e5336278b655b4048861adf58
- `supervisor.json`: d4fe86856f4149e5f4a7b446765d48fa21e24252f11b255b66ad767d4c9bddec
- `run.log`: fee48ad5279c6f9da323f0cd53a8de2a1bbc06c8d2a6127bf06e0bcba92ed76e
- `events.jsonl`: 302c0e515295ec6f3544e30a3cd9ae2b872120d1aedc7e2b61b421f167b22f93

Proposal source hashes:

- `cli_wrapper.py`: c71f8081df0c5d2456387e51f274d6536204ce87d96d33762d9888c5148c34a6
- `test_cli_wrapper.py`: ad315dec3c18c9b1bbcfee2854c4ee21976f1c61a76681ef119b4dfb0c7eb1cb
