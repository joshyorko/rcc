# Rattler helper boundaries

The helper uses the `conda/rattler` Git revision pinned in `Cargo.toml` and
`Cargo.lock`; registry dependencies are locked with checksums. Build with the
pinned Rust toolchain and `cargo build --release --locked`. A lockfile and
revision identify inputs, but do not independently attest source provenance or
the resulting binary. Release builds verify the embedded default against its embedded digest. Development builds that load a sidecar require a separately configured helper SHA-256.

Protocol 1 reads local `repodata.json` files for the native subdir and noarch.
The solver explicitly uses strict channel priority. It requires SHA-256 on
every package record, checks that archive paths resolve inside the channel,
disables link scripts and denies external symlinks. Packages requiring link
scripts may therefore be unusable. Package metadata and local channel contents
must themselves come from a trusted source; hashes in that metadata do not
authenticate it.

Protocol 1 can query exclusively HTTPS channels when `network.allow_https` is
true. The gateway enables sharded repodata and recursive dependency queries.
The HTTP client disables ambient proxies and redirects, uses rustls, and can add
certificates from `network.ca_bundle_pem`. Selected archives must have SHA-256
and belong to their originating HTTPS channel. RCC resolves the profile proxy
and bypass rules for that origin. A username and password in the configured
HTTPS channel alias select Basic authentication. RCC passes credentials on the
helper's stdin and removes them from the channel URL. The helper attaches the
header only to requests inside the configured channel paths. Mixed local and
remote channels and custom revocation settings remain unsupported. Metadata
remains trusted input. The Linux synthetic fixture verifies proxy CONNECT,
authenticated metadata and payload requests, and rejects an untrusted CA.

Upstream references: [pinned solver](https://github.com/conda/rattler/blob/fa471774d9975a65febae10d8405637f89d2ba85/crates/rattler_solve/src/lib.rs),
[Rattler book networking](https://github.com/prefix-dev/rattler-book/blob/main/book/src/deep-dive-networking.md),
and [installation](https://github.com/prefix-dev/rattler-book/blob/main/book/src/ch07-install.md).
The book illustrates a separate application, not this helper's capabilities.
