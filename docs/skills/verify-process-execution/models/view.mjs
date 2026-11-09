export const title = "RCC child execution and lease lifetime";
export const state = (v) => {
  if (v.started && !v.reaped && !v.lease) return "Child alive without lease";
  return ({ request: "Execution requested", leased: "Lease held", prepared: "Command and supervisor prepared", running: "Child running under lease", terminating: "Child termination requested", cleanup: v.started ? "Child reaped; cleanup pending" : "Child not started; cleanup pending", returned: v.reported === "success" ? "Execution succeeded" : "Failure returned" })[v.phase];
};
export const step = (action) => ({ AcquireLease: "acquire execution lease", ResolveAndPrepare: "resolve command and prepare supervisor", SetupFailure: "command or setup rejected", StartChild: "start child", SpawnFailure: "child cannot start", TerminateChild: "cancel or fail supervisor attachment", ReapChild: "wait for child", ReleaseLease: "release lease and return result", ReleaseFailure: "lease release fails; return error", PrematureRelease: "fault: release lease before child reap", Done: null })[action];
