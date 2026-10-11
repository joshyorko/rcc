# Independent review: nine release asset ZIPs

Reviewed the nine preserved review-only artifact ZIPs for source `7945f956b428359928908a9f42e36417b113dafd`, version `v18.19.6`, workflow run `38147141244`. The expected shared manifest SHA-256 is `26a47203ec1375e2c60d362c5aa55ba0aa65e334c1b24bc07b16f973a585f62c`. No files were downloaded, binaries executed, source edited, or cache modified.

## Result

All nine artifact API records match exact artifact name, ID, run, source head, expiry state, ZIP byte size, and recomputed ZIP SHA-256. Each ZIP contains exactly five expected regular-file members: the single asset, shared `asset-manifest.json`, `SHA256SUMS`, `provenance.json`, and `review-identity.json`. Paths are safe, no symlink entries are present, raw members are within 24 MiB, and all ZIP member bytes match the separately preserved extracted copies.

The shared manifest is identical across all nine assets and binds source/version/run plus the exact nine-name set and each asset’s SHA-256/size. The checksum file has exactly those nine asset paths with matching hashes. Provenance and review identity bind to the same manifest. The index contains exactly one `v18.19.6` entry and its four release URLs point to the expected Windows, Linux, macOS Intel, and macOS ARM64 assets. All eight binary assets pass official Go 1.26.9 `go version -m`: compiler version, GOOS/GOARCH, source revision `7945f956b428359928908a9f42e36417b113dafd`, clean VCS state, and CGO disabled match expected metadata. The four RCC CLI assets byte-match their exact native executed binaries from the separately reviewed native-artifact proof.

| Asset | Artifact ID | ZIP bytes | ZIP SHA-256 | Asset bytes | Asset SHA-256 | Result |
|---|---:|---:|---|---:|---|---|
| index.json | 11706315811 | 13981 | `sha256:c28b9758575c3cae1187b1169cf9cf7885e8d92c30463826c3b4c3bce47f5f22` | 8949 | `0bfdecc19330f76cab7a75800b6d7c882767679d6629ece6dc254e0798d1336c` | pass |
| rcc-linux64 | 11706495506 | 22398033 | `sha256:f39e6347088725e1f126e344ab8b3c1a849e04ee11aa9e77507b0389aa2f2c9f` | 22392994 | `5a6ae55f33e041e376ca4538c592e7c214b9da30cc6c1cf32cd62a0f65077e21` | pass |
| rcc-macos64 | 11705592029 | 22233967 | `sha256:075280bfba61c89ffed37385bcc11ece3dfa61eecd26ff9170226bd49e73a6a6` | 22228928 | `771b06ca9352b84df08507bccb759a115e354a898a78c678127dbdb2d2a1e60c` | pass |
| rcc-macosarm64 | 11706480506 | 20974858 | `sha256:fee0614f1b74bcf1f0b61a9bd2b0a3931097e41c25ae5d3dffb35d12a0d612bb` | 20969810 | `4a953e052041082345af2ea120b60dac2f38b2018b6da54c3b23ccd09a5f6001` | pass |
| rcc-windows64.exe | 11705946257 | 20122561 | `sha256:21ef08ea1c6ca560dd858c64f0384914866ae5d09d094ccfb3682769da7f7732` | 20117504 | `cef95bf88055c4a587b06c38fcef5a7edb48cad63202f7bb3238ad7fe03cb895` | pass |
| rccremote-linux64 | 11705419997 | 9356386 | `sha256:688debb3d915883c8272f334a4637aaaac1f6ebc895abe5fc8ad87b21d7f2fff` | 9351330 | `4e3522c042e34d1702604c40ecf506a228f91116ae12c26290424fef2aff62af` | pass |
| rccremote-macos64 | 11706300817 | 9548096 | `sha256:8294f11d36c3c891c4446b40f6d6c6891f3026627ad5671c66e2910d16ed4266` | 9543040 | `ca43a5ae6d4313803299864bdda995c99704ec0d44a497fa2e0bcc82546d50f0` | pass |
| rccremote-macosarm64 | 11706210955 | 8891643 | `sha256:957e0736ec65b8e8be6fc3c74d6f1f577ed0bdced69247e57826693e2b9d5973` | 8886578 | `9916ffbb8a2e071ce5a25bcf3877c90c0e1696ae43e12042989575122583c1e5` | pass |
| rccremote-windows64.exe | 11705946259 | 9677266 | `sha256:8578d4ddabae0cc15ee2d826eca03e9f39b64f8cd037e8fdb39ebdc8016b4084` | 9672192 | `13b72d0e322fce53a4d7f50703c3006c3600e9bdfd4ab35d82240b0142b49602` | pass |

Official inspection tool: `go version go1.26.9 linux/amd64`, SHA-256 `e90c4e12df236ea0c2e7536fd68da8a47fc0b585c19588bf6c1a70cfc296a0f9`. Raw `go version -m` stdout/exit records are in `official-go9-metadata.log`; parsed per-asset results are in `verification.json`.

## Limits

This validates review artifacts only. It does not validate a tagged source, publish a release, test a live installation, establish strict trust behavior, or clear the security dashboard. The four CLI/native byte matches refer to exact source/run-bound artifacts; no foreign executable was run.
