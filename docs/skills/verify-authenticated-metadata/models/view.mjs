export const title = "Publish authenticated metadata without losing retained state";
export function state(v) {
  if (v.phase === "distributed" && !v.retained) return "Published without retained checkpoint";
  return {
    received: "Snapshot supplied",
    authenticated: "Authority checked",
    retained: v.retained ? "Checkpoint retained" : "Checkpoint skipped",
    written: "Carrier written, awaiting readback",
    distributed: "Publication confirmed",
    failed: v.retained ? "Failed, checkpoint retained" : "Rejected before retention",
  }[v.phase];
}
export function step(action, from, to) {
  if (action === "Retain" && !to.retained) return "skip checkpoint retention [injected model fault]";
  return {
    Authenticate: to.phase === "failed" ? "reject unauthenticated snapshot" : "verify independent authority",
    Retain: "retain snapshot under operator lock",
    RejectRollback: "reject stale snapshot",
    StorageFailure: "storage fails closed",
    Write: "write detached carrier",
    TransportFailure: from.phase === "written" ? "readback fails or differs" : "carrier write fails",
    ReadBack: "confirm matching authenticated envelope",
    Done: "finish",
  }[action] ?? action;
}
