export const title = "RCC build ownership and stale publication";
export const state = v => {
  if (v.phase === "committed") return v.published === v.owner && v.live ? "Artifact committed by live owner" : "Stale owner committed an artifact";
  return ({unclaimed:"Build requested", building:"First builder owns claim", expired:"First claim expired", successor:"Successor owns claim", rejected:v.live ? "Stale publication rejected" : "Expired publication rejected"})[v.phase];
};
export const step = a => ({Claim:"claim build", Expire:"claim expires", Takeover:"another builder takes over", PublishCurrent:"owner publishes verified result", PublishOld:"first builder tries to publish", Done:"retain result"})[a] ?? a;
export const group = v => v.phase === "committed" ? "result" : "ownership";
export const colors = {result:"teal", ownership:"blue"};
