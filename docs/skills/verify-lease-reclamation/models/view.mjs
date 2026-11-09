export const title = "RCC lease protection and reclamation";
export const colors = { protected: "teal", reclaim: "orange", complete: "blue" };
export const state = (v) => {
  if (v.phase === "ready") {
    if (v.dryRun) return "Dry run: storage preserved";
    return ({ none: "Ready without lease", active: "Active owner protects environment",
      ambiguous: "Unknown owner protects environment", stale: "Owner exited; stale lease remains" })[v.lease];
  }
  return ({ "path-removed": "Environment removed; ready record remains",
    "ready-record-removed": "Ready record removed; live root protects content",
    retired: "Root retired; content eligible", reclaimed: "Unreferenced content reclaimed" })[v.phase];
};
export const group = (v) => v.phase === "ready" ? "protected" : v.phase === "reclaimed" ? "complete" : "reclaim";
export const step = (action) => ({
  AcquireLease: "execution obtains a lease",
  IdentityUnavailable: "owner identity cannot be established",
  IdentityRestored: "owner identity matches again",
  OwnerExits: "owner exits or its process identity changes",
  ReleaseLease: "lease holder releases the lease",
  ReconcileStale: "reconciliation removes a positively stale lease",
  EvictPath: "collection removes an eligible environment",
  RemoveReadyRecord: "collection removes the ready record",
  RetireRoot: "collection retires the reference root",
  ReclaimContent: "pressure collection reclaims eligible content",
  Done: null,
})[action];
