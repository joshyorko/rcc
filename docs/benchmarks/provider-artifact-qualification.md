# Provider artifact qualification

`scripts/qualify_provider_artifact.py` publishes a native provider environment,
exports a signed archive, and acquires and executes it in a separate home with
its provider stopped. It repeats acquisition in the warm consumer home. The
script uses public RFC 8032 test keys and generates short-lived test metadata;
these roots are not production trust policy. Temporary homes, keys, and archive
are removed when the run exits.

Run on Linux with a built RCC and its adjacent `rcc-rattler-helper`, a provider
plan, and a prepared uv and managed Python cache:

```sh
python3 scripts/qualify_provider_artifact.py \
  --rcc /path/to/rcc --environment /path/to/package.yaml \
  --cache-home /path/to/prepared-home --scratch-parent /var/tmp
```

The script prints binary and archive hashes and acquisition receipts. This is a
local synthetic qualification, not remote-service or production acceptance.

The 2026-11-26 Linux run published and exported an artifact but did not qualify:
strict acquisition rejected its `libcrypt.so.1` system-library requirement on
this host. Execution and warm acquisition were not reached. Run on a compatible
worker before treating this harness as qualified.
