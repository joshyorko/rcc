# Large-object external codec micro: execution receipt

Experiment: one admitted invocation of revision 2 only. This is an external CPython component measurement on the frozen provider corpus. It did not execute RCC, Actions, materialization, or a production-format path. The fixture was produced historically by Actions run a709 using stable RCC3; main727 is not its producer.

## Invocation outcome

- Plan SHA-256: `3a31525d00d0403c84acf1e908c85a71d17d8aebfa915dd7011476880af265df`
- Runner SHA-256: `040feb2e0db53787e8b6aa6e2f871d73d79f177be960e07349fd1993180b5315`
- Results SHA-256: `9546441ea67af3e5ac7122db2a08a305b9e7f19e5336278b655b4048861adf58`
- Supervisor receipt SHA-256: `d4fe86856f4149e5f4a7b446765d48fa21e24252f11b255b66ad767d4c9bddec`
- Event log SHA-256: `302c0e515295ec6f3544e30a3cd9ae2b872120d1aedc7e2b61b421f167b22f93`
- Sole command: `env PYTHONDONTWRITEBYTECODE=1 /opt/codex/runtimes/codex-primary-runtime/dependencies/python/bin/python /workspace/work/rcc-swarm/evidence/continuation-20261011/encoding-large-micro/encoding_large.py`
- Worker: exit 0, status PASS, 500 verified timed calls; supervisor: numeric worker exit 0, status PASS, no failure, elapsed 38.863249 s.
- The outer Python launcher printed `SystemExit: 0` as a traceback because its `except BaseException` catches the normal `sys.exit(supervisor())`. That control path then exits 1. This wrapper defect is preserved and was not rerun; the worker and supervisor receipts remain PASS.

Raw command-result transcript (as returned by the execution tool):

```text
Traceback (most recent call last):
  File "/workspace/work/rcc-swarm/evidence/continuation-20261011/encoding-large-micro/encoding_large.py", line 629, in <module>
    sys.exit(supervisor())
SystemExit: 0
{"elapsed_seconds": 38.86324851500012, "failure": null, "numeric_worker_exit": 0, "status": "PASS"}
```

## Frozen input and limits

All 20 indexed objects whose logical size exceeds 1 MiB were selected in lexicographic stored-digest order: 45,517,701 stored bytes and 102,457,064 logical bytes (65.524% of corpus stored bytes; 60.060% of corpus logical bytes). Each of five phases processed each object once per repetition for five repetitions (100 calls per phase, 500 total). Per-call wall and process CPU timers cover only the codec call; reads, original gzip validation, frame preparation, hashes, and round trips are outside those timers. The runner retained only frame digests/sizes, not encoded payload frames.

Limits and observed values: total elapsed 38.863 s (<120 s); observed maximum sampled process-tree RSS 150,142,976 B (<536,870,912 B); filesystem free 2,705,887,232 B at first observation to 2,705,289,216 B at final observation (floor plus margin 2,688,548,864 B); allocated evidence before supervisor receipt 655,360 B, predicted final 765,952 B (<1,048,576 B). RSS is a periodic ps-observed peak, not an absolute transient maximum.

Untimed preparation calls counted: `gzip_frame_encode_before_decode_timing`=100, `gzip_roundtrip_decode_after_encode`=100, `original_gzip_decode_for_input_validation`=540, `zstd_frame_encode_before_decode_timing`=100, `zstd_roundtrip_decode_after_encode`=100. Selected source object/index/manifest metadata and hashes matched before and after; all 20 selected original stored and logical identities verified.

## Per-repetition and weighted results

Each cell lists the five per-repetition sums in seconds (rep0 through rep4), followed by median seconds and median wall/CPU nanoseconds per logical byte. Dividing total timed phase cost by total logical bytes weights the result by this corpus; no unweighted per-object average is used.

| Phase | Repetition wall seconds | Median wall s | Median CPU s | Wall ns/logical byte | CPU ns/logical byte | Input bytes/rep | Output bytes/rep |
|---|---:|---:|---:|---:|---:|---:|---:|
| legacy-gzip-decode | 0.409078, 0.380908, 0.374957, 0.390128, 0.385237 | 0.385237 | 0.385136 | 3.7600 | 3.7590 | 45,517,701 | 102,457,064 |
| gzip-level1-encode | 1.159769, 1.196861, 1.202938, 1.190254, 1.250185 | 1.196861 | 1.194971 | 11.6816 | 11.6631 | 102,457,064 | 43,769,827 |
| zstd-level3-encode | 0.402469, 0.414903, 0.416608, 0.404212, 0.414601 | 0.414601 | 0.414026 | 4.0466 | 4.0410 | 102,457,064 | 39,419,371 |
| gzip-level1-decode | 0.410017, 0.397430, 0.389646, 0.393095, 0.382116 | 0.393095 | 0.392880 | 3.8367 | 3.8346 | 43,769,827 | 102,457,064 |
| zstd-level3-decode | 0.117979, 0.127289, 0.122543, 0.118855, 0.119449 | 0.119449 | 0.119423 | 1.1658 | 1.1656 | 39,419,371 | 102,457,064 |

Encoded frame totals over these 20 objects: original gzip 45,517,701 B; external gzip level 1 43,769,827 B (42.72% of logical; 96.16% of original stored); external zstd level 3 39,419,371 B (38.47% of logical; 86.60% of original stored). Zstd external output is 90.06% of external gzip output. All are distinct stored-frame digests; no frame was persisted or substituted into the provider.

## Per-object encoded output sizes

| Legacy logical SHA-256 | Original stored B | External gzip B | External gzip SHA-256 | External zstd B | External zstd SHA-256 |
|---|---:|---:|---|---:|---|
| `5ede1e96ce191fbcf740817d511f72beda30ee3783dca395023067b1184fe80b` | 2,924,731 | 2,771,640 | `18c5458bc12fcd75c0a07d73b56516c0ed9b259ecd970bd782efca9f9d03070b` | 2,524,957 | `ee8050e2ccb85a8ada02be83e8040c3676ec23b9ac90c6b71aba1d1ae55642b2` |
| `9f9fc1f5ab856181716b7dff697daf16aa5b0f47f7add572c491ac31a10ef8f4` | 566,453 | 540,907 | `38ae5798e8a8972d15b4021155db1a2af2b7f473dafdbd0e9e4fa5eb487ce603` | 513,190 | `c08231a880b22a10a94050026d97b33dfbcdfee9ca957d43467222ebcc78f568` |
| `7432dd221132c301970e9f526a9654b10cbd53bb92b1f0cb3d6d899877ff8c53` | 909,604 | 875,138 | `a67c3821e61187933c7b2639d67bf16eaf0152160e3ad011f44bac26c43a8546` | 852,804 | `6e62d90c13832d568955e1a3c62a05a0de2b2c390412f5fd9dbf1a9dff9a8b54` |
| `a812cd8dcd4ae107c42d9fc374a4b910bdfc0ddde19aebfe3e3f02cca93fd100` | 1,035,796 | 982,929 | `ff80cb91cfa620bbb35083810ca0c0a77b74a46820b65c04a9cfe90d0f7788b8` | 827,496 | `b78d92ce0e353e35e77e415984a54d83af4679cf24c4da2fbb424d0bd0cda4a3` |
| `aa9bb0d7b261b8266ababd9e19f5bf2f505d9a84ba360ce16502297e8a033765` | 875,725 | 849,490 | `d79de688f524927d532d1ad024fb1689ea0646d8bd76ed19aac442a997268855` | 770,778 | `4a965dc4df81248286e6d6b2900f725a2d5ee6ee997c9e8bf729cc9bc7aea8a5` |
| `0d91512e2f1cd19642df9ecc9e5939b06504ed5e8e3989f1d349df4a902ae89d` | 582,916 | 467,643 | `dcb42cd722ce18279a1cf808d77b597f8bf32f1321beb9384c8b6f6791f3d6e0` | 530,439 | `95f6334b3d4f9f11bd123dbe13ccc54b102e930db83d2f08a5bf8811e1b18ee4` |
| `f62ea9da9ed6289bfe868cd6845968a2c854d1427f8548d52cae02a42b4f0356` | 1,229,362 | 1,211,828 | `8618cf3fdbb2c66db0d42cd7b9d02fb1e4b56536fe48f8e0daa63e5149abcc39` | 1,009,172 | `e2138690679bfc42bc1733e1f405e47858014828f92bd796d158abd2d007aecb` |
| `34324e0835be6e103844d7c416bec42834cf803d3ba8d9519df15ced19914dfe` | 809,275 | 786,017 | `322ec596c0cb2a6099eaacae601e1be3eab4f812cf4b054de10dce3cb9cdd487` | 707,877 | `241554ea69dd42640cbc494a72ac5e0cdb888f4ace536d10d9d382900589a670` |
| `7b7a760153f880c29f49bef084aad6968de3cc3ad6303c7398f5e697662c41cc` | 153,332 | 169,959 | `6f574fcb526604f07d4bacc6a8064aae89c63054797f8f1fd3afc5831605fac2` | 131,964 | `4d0d0cd1f595917b7bc280e77ca1e26e775383609e70be1ab260e5d11423974b` |
| `9b0cd0068df0f7d98b1ec5874845b034d1f5b2adc724508f6857d9dc18c77178` | 983,075 | 930,896 | `aacab1fc680a8b16cc6d295f9c30865665a5f1cd4731f86f93c511bce51cbf87` | 877,506 | `9b8e902b7651d0c569b52d0766cd8b87694823fab69ffff86b08c9e836809ee4` |
| `e6ed1ed462080e0bea796247125c1a8624fb3134be3646c51c8942f38505e53b` | 11,038,953 | 10,659,344 | `467132dd826e38585de7c9c9be1964bcdedd95577903f5046bdbc66c3fbea61b` | 9,413,377 | `f3198942f7e97720d9a94f8148c530bf4814e8583fbda83225bf7991ca4da606` |
| `67694582bb07ec2852df4d092b44dded578f3d1b38134967385c495f18c79c6e` | 778,021 | 747,830 | `7d8ba205903ec333c00a2d987293cb85da5d5566bbea6ae85a6ccccc66b094a7` | 713,640 | `61e89a316a15c6cc2585c7e9335125f78ae5316959b94ca89c31835df29d3e31` |
| `36c57dc318a720cff9acde104f633fd4b8eef0f5a4cba0dbbc3869cd026aec41` | 535,030 | 509,855 | `6c058308a15334b026a1e286ad8bbcece78b1082cd8dcc3d987f26c18fdfd85c` | 473,177 | `2ff8fe5c7df9e71438cf77bbe0c5a054558deb12ccd21a11d7ba6daceca8ecdd` |
| `5a7331d26863a2e958df6239ad0b6a6dbaa1eb6f3f42ae7af3fca609f6a605c3` | 1,021,730 | 981,749 | `e8ceabaaa8c5ffb105a2214de1256406b9a372c02fbb2c2e8a33bd0d25c09aa4` | 962,391 | `09aafdc6f35443b500f7654081cd0633ba27a2a729b347ec6dc636bcee29d777` |
| `67129e428f47e2f67c33cb2e85a92a6d2ff0014d587407cf7a8e5dba0c68c6ce` | 679,607 | 657,603 | `a2ee36001aff94fab922760f4a62d87d3627ac1b683811e8827cffe0a9461440` | 610,642 | `e9a54202ec4ce4b2fd04bd61a7a7d52b9792304789e5d0c09a870eb99fcd32db` |
| `ba0d021a166865d2265246961bec0152ff124de910c5cc39f1156ce3fa7c69dc` | 2,086,645 | 2,059,808 | `46dd33a5dfa5fc929807d91e091923c40b4e854e46ef989da02fed71958b2ec1` | 2,064,828 | `1cc6602a20e739702a48daa5645386577c431cc9e1c0e0e96fb0ed10a655d1b7` |
| `74519a830fae91abc48662768dbd326692e919b37655c798b1c9da71a344f809` | 11,097,301 | 10,712,129 | `c8cb92111917541ba76e77b2b2df6539ae636a0ccc38b6c2d3a83be73db6ae73` | 9,463,007 | `db1d4190a19927d3ec3df2045714586468a0e6a09ffeed0ed10b73cadc469d79` |
| `b0e2bad8c117a3ad7d344d7924272d12b8e363956efb91b5074e43d4e201a08d` | 563,030 | 541,058 | `c6b1893a3bc92abb67c48e6bda52388791c6f4eb3b7cd143d4f23e4bcda24761` | 487,569 | `465d4974dc2ab0911895946a8f3dd753d5f69b4b489386f6f03a36b63824701d` |
| `1e5d23fa66915204b9b67202a46993844d81cda56f6ad52cead2def37617ee29` | 5,322,030 | 5,101,191 | `5141bc6646e83d9d83f0fc24f31b286c67e5e4ef14ca0a654b271d85bbcd255d` | 4,488,764 | `5db0995d022cfa94ca89a822d7893c8a06d636974e0bf8d45a5c646f238482bf` |
| `579a71f6f44e9f8c66ad15a6d04faf770f6bbd72c6d9bf6259516742ce394ef1` | 2,325,085 | 2,212,813 | `020e87e8d8450438c84b9ed80ce6cade32fd1837470361cf822eb0fe239eb545` | 1,995,793 | `bd5e2edb03fb2df532ac1e3dedce4377cda65abda31e0b156c475beee4ef1431` |

## Interpretation and limitations

On this selected corpus, external Python zstd level 3 had lower per-repetition encode/decode wall time and smaller frames than external CPython gzip level 1. The measured original-gzip decode and external-gzip decode controls are close in this harness. The measurements do not establish Go encoder equivalence, an RCC runtime gain, a provider/HTTP gain, an Actions speedup, or a production codec winner. They exclude filesystem materialization, native consumers, transport, cache state transitions, and artifact migration. Results are specific to this CPython/zlib/zstandard/libzstd toolchain and this warm local filesystem condition.

No retry occurred. The only execution was admitted plan revision 2. Previous revision 1 and all earlier static-check logs remain archived separately.
