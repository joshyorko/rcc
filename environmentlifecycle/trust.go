package environmentlifecycle

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/joshyorko/rcc/artifacttrust"
	"github.com/joshyorko/rcc/common"
)

func trustRequestFor(artifact, platform, builder string, supplied *artifacttrust.VerifyRequest, carrier artifacttrust.Carrier, at time.Time) (artifacttrust.VerifyRequest, error) {
	request := artifacttrust.VerifyRequest{ArtifactDigest: artifact, Platform: platform, Builder: builder, At: at}
	if supplied != nil {
		request = *supplied
		request.ArtifactDigest = artifact
		if request.Platform == "" {
			request.Platform = platform
		}
		if request.Builder == "" {
			request.Builder = builder
		}
		if request.At.IsZero() {
			request.At = at
		}
	}
	if carrier == nil {
		return request, nil
	}
	attachments, err := artifacttrust.LoadAttachments(carrier, artifact)
	if err != nil {
		return artifacttrust.VerifyRequest{}, err
	}
	if attachments.Provenance != nil {
		request.Provenance = attachments.Provenance
	}
	if attachments.SBOM != nil {
		request.SBOM = attachments.SBOM
	}
	if attachments.SignaturesPresent {
		request.Signatures = attachments.Signatures
	}
	if attachments.RevocationsPresent {
		request.Revocations = attachments.Revocations
	}
	if attachments.RevocationFetchedAt != "" {
		fetchedAt, err := time.Parse(time.RFC3339, attachments.RevocationFetchedAt)
		if err != nil {
			return artifacttrust.VerifyRequest{}, fmt.Errorf("invalid revocation fetch timestamp")
		}
		request.RevocationFetchedAt = fetchedAt
	}
	request.RevocationSource = attachments.RevocationSource
	return request, nil
}

func persistVerificationReceipt(receipt artifacttrust.VerificationReceipt) error {
	store := artifacttrust.NewReceiptStore(filepath.Join(common.Product.Home(), "artifacts", "v1", "verification"))
	return store.Put(receipt)
}

func persistTrustFailure(policy artifacttrust.Policy, artifact, platform, builder string, _ error, at time.Time) (artifacttrust.VerificationReceipt, error) {
	receipt := policy.FailureReceipt(artifact, platform, builder, artifacttrust.CodeInvalid, "trust attachment could not be decoded", at)
	if persistErr := persistVerificationReceipt(receipt); persistErr != nil {
		return receipt, fmt.Errorf("persist artifact trust failure receipt: %w", persistErr)
	}
	return receipt, fmt.Errorf("artifact trust attachment verification failed")
}

func refreshMaterializationTrust(ctx context.Context, materialization Materialization) (artifacttrust.VerificationReceipt, error) {
	if err := ctx.Err(); err != nil {
		return materialization.Verification, err
	}
	if materialization.TrustPolicy == nil {
		return materialization.Verification, nil
	}
	at := time.Now().UTC()
	var supplied *artifacttrust.VerifyRequest
	if materialization.TrustRequest != nil {
		copyRequest := *materialization.TrustRequest
		copyRequest.At = time.Time{}
		supplied = &copyRequest
	}
	request, err := trustRequestFor(materialization.ArtifactDigest.String(), materialization.Verification.Platform, materialization.Verification.Builder, supplied, materialization.TrustCarrier, at)
	if err != nil {
		return persistTrustFailure(*materialization.TrustPolicy, materialization.ArtifactDigest.String(), materialization.Verification.Platform, materialization.Verification.Builder, err, at)
	}
	receipt := materialization.TrustPolicy.Verify(request)
	if err := persistVerificationReceipt(receipt); err != nil {
		return receipt, fmt.Errorf("persist artifact trust receipt: %w", err)
	}
	if !receipt.Valid {
		return receipt, fmt.Errorf("artifact trust verification failed: %s", receipt.Code)
	}
	return receipt, nil
}
