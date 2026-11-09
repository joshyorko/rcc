export const title = "Recover a provider restore only after its commit record";
export function state(v) {
  if (v.phase === "recovered") return v.visible
    ? (v.committed ? "Committed restore recovered" : "Uncommitted restore exposed")
    : "Uncommitted restore discarded";
  if (v.phase === "crashed") return v.committed ? "Stopped after commit" : "Stopped before commit";
  return {
    supplied: "Backup supplied",
    validated: "Archive and closure checked",
    rejected: "Backup rejected",
    begun: "Restore transaction opened",
    pending: "Restore records pending",
    committed: "Commit record retained",
    published: "Restore available",
  }[v.phase];
}
export function step(action, from, to) {
  if (action === "Replay") return to.visible
    ? (to.committed ? "replay committed transaction" : "replay pending records [injected model fault]")
    : "discard incomplete transaction";
  return {
    Validate: to.phase === "rejected" ? "reject invalid backup" : "validate backup before publication",
    Begin: "append restore begin",
    StageRecords: "append transaction records",
    Commit: "append restore commit",
    Publish: "make restored state available",
    Crash: "process stops",
    Done: "finish",
  }[action] ?? action;
}
