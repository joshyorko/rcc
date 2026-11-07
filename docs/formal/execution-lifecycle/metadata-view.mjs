export const title = "Authenticated metadata admission across restarts";
export function state(v) {
  if (v.phase === "permitted" && !v.offered[2]) return "Execution permitted from forged metadata";
  if (v.phase === "permitted" && v.offered[1] < v.highestCommitted) return "Execution permitted after rollback";
  return {
    idle: "Ready for next admission",
    fetched: "Carrier snapshot received",
    authenticated: "Authority checked",
    persisted: "Checkpoint retained",
    denied: "Admission denied",
    permitted: "Execution permitted",
  }[v.phase];
}
export function step(action, from, to) {
  if (action === "Restart" && to.checkpoint[1] < from.checkpoint[1]) return "restart loses checkpoint [injected fault]";
  if (action === "Authenticate" && !from.offered[2] && to.phase === "authenticated") return "skip signature verification [injected fault]";
  return {
    Fetch: "carrier supplies a snapshot",
    Authenticate: "verify independent authority",
    Commit: "compare sequence and flush checkpoint",
    RejectRollback: "reject rollback or equivocation",
    StorageFailure: "storage fails closed",
    Decide: "apply authenticated revocations",
    Restart: "restart with retained checkpoint",
  }[action] ?? action;
}
