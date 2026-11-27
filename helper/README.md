# Rattler helper boundaries

The helper uses the `conda/rattler` Git revision pinned in `Cargo.toml` and
`Cargo.lock`; registry dependencies are locked with checksums. Build with the
pinned Rust toolchain and `cargo build --release --locked`. A lockfile and
revision identify inputs, but do not independently attest source provenance or
the resulting binary. RCC requires a separately configured helper SHA-256.

Protocol 1 reads local `repodata.json` files for the native subdir and noarch.
The solver explicitly uses strict channel priority. It requires SHA-256 on
every package record, checks that archive paths resolve inside the channel,
disables link scripts and denies external symlinks. Packages requiring link
scripts may therefore be unusable. Package metadata and local channel contents
must themselves come from a trusted source; hashes in that metadata do not
authenticate it.

There is no remote channel transport or Rattler gateway integration: sharded
repodata, compressed repodata and flat remote fallback are unsupported. Proxy,
custom CA and authenticated-channel configuration are not implemented. Do not
interpret rustls dependencies as evidence of supported enterprise transport.
Unknown request fields and non-local channels fail closed. The RCC caller
currently restricts the provider to its local channel.
The optional protocol 1 `network` object accepts only `allow_https: false`;
setting it to true fails before any channel access. Omitting it preserves local
builds. This is a rejection boundary, not an HTTPS implementation.

Upstream references: [pinned solver](https://github.com/conda/rattler/blob/fa471774d9975a65febae10d8405637f89d2ba85/crates/rattler_solve/src/lib.rs),
[Rattler book networking](https://github.com/prefix-dev/rattler-book/blob/main/book/src/deep-dive-networking.md),
and [installation](https://github.com/prefix-dev/rattler-book/blob/main/book/src/ch07-install.md).
The book illustrates a separate application, not this helper's capabilities.
