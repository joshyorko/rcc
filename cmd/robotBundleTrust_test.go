package cmd

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/joshyorko/rcc/artifacttrust"
	"github.com/joshyorko/rcc/environmentartifact"
	"github.com/joshyorko/rcc/environmentlifecycle"
)

type recordingBundleAcquirer struct {
	request environmentlifecycle.AcquireRequest
	result  environmentlifecycle.AcquireResult
}

func (acquirer *recordingBundleAcquirer) Acquire(_ context.Context, request environmentlifecycle.AcquireRequest) (environmentlifecycle.AcquireResult, error) {
	acquirer.request = request
	acquirer.result.TrustPolicy = request.TrustPolicy
	acquirer.result.TrustRequest = request.TrustRequest
	acquirer.result.TrustCarrier = request.TrustCarrier
	if acquirer.result.Verification.Code == "" {
		acquirer.result.Verification = artifacttrust.VerificationReceipt{
			Valid: true, Code: artifacttrust.CodeValid, ArtifactDigest: request.ArtifactDigest.String(),
		}
	}
	return acquirer.result, nil
}

func TestBundleTrustDefaultsToStrictAndCarriesLeaseRefreshContext(t *testing.T) {
	options := bundleTrustOptions{}
	digest := environmentartifact.DigestBytes([]byte("embedded artifact"))
	acquirer := &recordingBundleAcquirer{result: environmentlifecycle.AcquireResult{
		ArtifactDigest: digest, MaterializationID: "materialization", Path: "/private/home/holotree/materialization",
		CacheHit:     environmentlifecycle.CacheLocalMaterialization,
		Verification: artifacttrust.VerificationReceipt{Valid: true, Code: artifacttrust.CodeValid, ArtifactDigest: digest.String()},
	}}
	acquired, err := options.acquireWith(context.Background(), digest, acquirer)
	if err != nil {
		t.Fatal(err)
	}
	if acquirer.request.TrustPolicy == nil || acquirer.request.TrustPolicy.Mode != artifacttrust.StrictRemote || !acquirer.request.TrustPolicy.FailClosedRevocations {
		t.Fatalf("default policy is not fail-closed strict: %#v", acquirer.request.TrustPolicy)
	}
	if acquirer.request.Provider != nil || acquirer.request.TrustRequest != nil || acquirer.request.TrustCarrier != nil {
		t.Fatalf("bundle trust inputs unexpectedly inferred from embedded artifact: %#v", acquirer.request)
	}
	materialization := materializationForBundledArtifact(acquired)
	if materialization.Verification.Code != artifacttrust.CodeValid || materialization.TrustPolicy != acquirer.request.TrustPolicy || materialization.TrustRequest != acquirer.request.TrustRequest || materialization.TrustCarrier != acquirer.request.TrustCarrier {
		t.Fatalf("materialization lost trust decision or lease refresh inputs: %#v", materialization)
	}
}

func TestBundleTrustAllowsOnlyExplicitPermissiveLocalFallback(t *testing.T) {
	digest := environmentartifact.DigestBytes([]byte("legacy unsigned artifact"))
	acquirer := &recordingBundleAcquirer{}
	_, err := (bundleTrustOptions{permissiveLocal: true}).acquireWith(context.Background(), digest, acquirer)
	if err != nil {
		t.Fatal(err)
	}
	if acquirer.request.TrustPolicy == nil || acquirer.request.TrustPolicy.Mode != artifacttrust.PermissiveLocal || acquirer.request.TrustPolicy.FailClosedRevocations {
		t.Fatalf("explicit fallback did not select permissive local policy: %#v", acquirer.request.TrustPolicy)
	}
}

func TestBundleTrustUsesExplicitDeploymentRootsAndCarrier(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rootPath := filepath.Join(t.TempDir(), "trust-roots.json")
	encoded, err := json.Marshal(map[string]string{"deployment-key": base64.RawStdEncoding.EncodeToString(publicKey)})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rootPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	carrierPath := t.TempDir()
	acquirer := &recordingBundleAcquirer{}
	digest := environmentartifact.DigestBytes([]byte("signed artifact"))
	options := bundleTrustOptions{trustRootsPath: rootPath, trustCarrierPath: carrierPath, trustCarrierType: "filesystem"}
	if _, err := options.acquireWith(context.Background(), digest, acquirer); err != nil {
		t.Fatal(err)
	}
	if acquirer.request.TrustRequest == nil || len(acquirer.request.TrustRequest.Keys) != 1 || len(acquirer.request.TrustPolicy.AcceptedKeys) != 1 || acquirer.request.TrustPolicy.AcceptedKeys[0] != "deployment-key" {
		t.Fatalf("deployment trust root was not passed to verification: %#v", acquirer.request)
	}
	if _, ok := acquirer.request.TrustCarrier.(*artifacttrust.FilesystemCarrier); !ok {
		t.Fatalf("explicit carrier type = %T, want filesystem carrier", acquirer.request.TrustCarrier)
	}
}

func TestBundleTrustRejectsInvalidRootsBeforeAcquisition(t *testing.T) {
	rootPath := filepath.Join(t.TempDir(), "trust-roots.json")
	if err := os.WriteFile(rootPath, []byte(`{"deployment-key":"not-base64"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	acquirer := &recordingBundleAcquirer{}
	_, err := (bundleTrustOptions{trustRootsPath: rootPath}).acquireWith(context.Background(), environmentartifact.DigestBytes([]byte("artifact")), acquirer)
	if err == nil {
		t.Fatal("expected malformed deployment roots to be rejected")
	}
	if acquirer.request.ArtifactDigest.Hex() != "" {
		t.Fatal("acquisition began before deployment roots were validated")
	}
}

func TestBundleTrustRejectsAcquisitionWithoutMatchingValidReceipt(t *testing.T) {
	digest := environmentartifact.DigestBytes([]byte("artifact"))
	acquirer := &recordingBundleAcquirer{result: environmentlifecycle.AcquireResult{
		ArtifactDigest: digest,
		Verification:   artifacttrust.VerificationReceipt{Valid: false, Code: artifacttrust.CodeUnknownSigner, ArtifactDigest: digest.String()},
	}}
	if _, err := (bundleTrustOptions{}).acquireWith(context.Background(), digest, acquirer); err == nil {
		t.Fatal("expected acquisition without valid trust receipt to fail closed")
	}
}

func TestBundleTrustRejectsConflictingPolicySelection(t *testing.T) {
	acquirer := &recordingBundleAcquirer{}
	_, err := (bundleTrustOptions{strictRemote: true, permissiveLocal: true}).acquireWith(context.Background(), environmentartifact.DigestBytes([]byte("artifact")), acquirer)
	if err == nil || acquirer.request.ArtifactDigest.Hex() != "" {
		t.Fatal("conflicting policy flags must fail before acquisition")
	}
}
