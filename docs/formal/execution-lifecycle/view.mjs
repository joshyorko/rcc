export const title = "RCC strict environment admission";
export const colors = { admission: "blue", execution: "teal", refused: "pink" };

export const state = (v) => {
  if (v.processPermitted) return "Process permitted under lease";
  if (v.leaseHeld) return "Verified materialization leased";
  if (v.materialized) return "Materialization admitted";
  if (v.stage === "refused") return "Untrusted artifact refused";
  if (v.stage === "verified") return "Signature verified";
  if (v.policy === "permissive") return v.explicitOptIn ? "Explicit unsigned-local opt-in" : "Strict policy silently downgraded";
  if (v.policy === "strict" && v.strictChosen) return "Strict policy selected";
  return "Artifact and carrier presented";
};

export const group = (v) => v.stage === "refused" ? "refused" : v.stage === "running" ? "execution" : "admission";

export const step = (action) => ({
  Done: null,
  SelectStrict: "operator selects strict verification",
  SelectPermissive: "operator explicitly opts into unsigned local use",
  VerifyStrict: "carrier and signature evidence are verified",
  MaterializeStrict: "validly signed artifact is materialized",
  MaterializePermissive: "explicitly allowed local artifact is materialized",
  DowngradeOnUnsignedMetadata: "unsigned metadata attempts to lower strict policy",
  CreateLease: "admitted materialization receives a lease",
  PermitProcess: "process becomes permitted",
  Refuse: "invalid evidence is refused",
})[action] ?? action;
