# Signed offline environment archives

`rcc env export` can include validated detached trust attachments in a canonical `.rcca` archive. The provider supplies the immutable environment artifact; an explicitly selected trust carrier supplies provenance, RCC object inventory (historical `sbom` attachment), signature, revocation, and v2 metadata files.

```sh
rcc env export \
  --artifact sha256:<artifact-digest> \
  --provider <provider> \
  --trust-carrier /path/to/publisher-trust \
  --trust-carrier-type filesystem \
  --include-trust-attachments \
  --output environment.rcca
```

Export fails if attachment inclusion is requested without a carrier, a carrier is selected without opting into inclusion, or the selected carrier has no attachments for the artifact. The source attachments are bounded and validated as canonical trust data. The command reports the included kinds. Changing detached evidence can change the archive bytes; it does not change the artifact digest.

In a fresh offline home, `--consume-archive-trust` explicitly selects only the evidence carried by that archive for acquisition. RCC stores it in a carrier directory scoped to the archive bytes and returns that path as `trustCarrierPath` for later `env exec`. Trust roots and metadata rollback state remain deployment-owned inputs:

```sh
acquire_result=$(rcc env acquire \
  --archive environment.rcca \
  --artifact sha256:<expected-artifact-digest> \
  --consume-archive-trust \
  --trust-roots /deployment/trust/artifact-roots.json \
  --metadata-trust-roots /deployment/trust/metadata-roots.json \
  --metadata-state /var/lib/rcc/metadata-state \
  --strict-remote --json) || exit $?
trust_carrier=$(printf '%s' "$acquire_result" | jq -r .trustCarrierPath)

rcc env exec \
  --artifact sha256:<artifact-digest> \
  --trust-carrier "$trust_carrier" \
  --trust-carrier-type filesystem \
  --trust-roots /deployment/trust/artifact-roots.json \
  --metadata-trust-roots /deployment/trust/metadata-roots.json \
  --metadata-state /var/lib/rcc/metadata-state \
  --strict-remote --json -- python task.py
```

Archive trust is not trusted by its presence: strict verification still requires deployment-owned signer roots. The archive does not provide or replace trust roots, metadata roots, or the metadata state journal. Keep the journal outside the archive and outside disposable execution state; do not restore or rewind it from an archive. The journal can advance after authenticated metadata admission even if a later artifact or revocation policy rejects the request. Never rewind the journal to retry denied evidence. Malformed, missing, wrong-authority, expired, revoked, or rolled-back evidence fails before execution.

When `--artifact` is supplied with `--archive`, RCC checks it against the canonical archive manifest identity before importing archive objects. Matching values preserve callers that use the expected digest as a separate identity assertion; mismatches fail without bulk import.

An external detached carrier remains an alternative: omit `--consume-archive-trust` and select it with `--trust-carrier` and `--trust-carrier-type`. The two sources cannot be combined in one acquire request. `--provider` may be omitted for a complete archive; an offline run should omit it so no provider or evidence fetch can occur. Archive paths, member sizes, attachment sizes, canonical encodings, and the existing 256 MiB archive bound remain enforced by RCC.
