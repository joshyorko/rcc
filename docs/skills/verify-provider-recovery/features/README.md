# Provider recovery feature map

- [Transactional restore and deferred resolution](recovery.md): archive preflight,
  commit-gated replay, interrupted recovery, and stable lazy provider selection.

This skill owns provider persistence/recovery contracts. Consumer acquisition,
materialization and build ownership belong to their environment subsystem skills;
artifact trust does not follow from provider storage authorization.
