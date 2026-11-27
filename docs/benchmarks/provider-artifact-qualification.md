# Provider artifact qualification

`scripts/qualify_provider_artifact.py` publishes a native provider environment,
exports a signed archive, and acquires and executes it in a separate home with
its provider stopped. It repeats acquisition in the warm consumer home. The
script uses public RFC 8032 test keys and generates short-lived test metadata;
these roots are not production trust policy. Temporary homes, keys, and archive
are removed when the run exits.

Run on Linux with an RCC containing its native provider defaults, a provider
plan, and a prepared uv and managed Python cache:

```sh
python3 scripts/qualify_provider_artifact.py \
  --rcc /path/to/rcc --environment /path/to/package.yaml \
  --cache-home /path/to/prepared-home --scratch-parent /var/tmp
```

The script prints binary and archive hashes and acquisition receipts. This is a
local synthetic qualification, not remote-service or production acceptance.

On Linux amd64 the released-baseline-compatible candidate executable
`2b46ff375a3a7a55ec2cbe30f88a2a083a0cb4e83c2eb108db8d71ddb5c4c24e`
published artifact
`sha256:92c91db14ead3ab0589885c3538377fff9c32a22962bf02c8c57a7767d8e31fa`.
The signed archive SHA-256 was
`8346e76669e71e1c2520c87c7a1216e5b8015ec7db690c99df53162e5b648af6`.
Inside a loopback-only network namespace, the separate RCC-only consumer
verified the trust attachments, executed Python and reused the same local
materialization. The run used tmpfs. Earlier disk-backed tests encountered
a missing `libcrypt.so.1` or an object PUT ending in EOF. The cause of the
EOF remains unresolved.

The native CI job uses the assembled RCC executable for its target. Each
mixed environment is published, signed and checked in a separate private
consumer home. The consumer launch validates the wheel, native zlib and
resolved interpreter path. The fixture stops the publisher's HTTP server
before acquisition and sets offline variables. The hosted tests do not
enforce network isolation at the OS level.
