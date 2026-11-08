package artifacttrust

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"strings"
	"time"
)

// MetadataOperator uses deployment-owned roots and retained state, never keys or
// sequence floors supplied by the carrier. Give issuers and distributors their
// own retained journals; workers keep their existing admission journals.
type MetadataOperator struct {
	Roots MetadataRoots
	State MetadataState
}

type MetadataChange struct {
	Operation      string
	ArtifactDigest string
	Sequence       uint64
	KeyID          string
	IssuedAt       string
	ExpiresAt      string
	Provenance     *Provenance
	SBOM           *SBOM
	Revocations    []Revocation
	RevokeArtifact bool
	RevokeKeyID    string
	Reason         string
}

// MetadataOperatorReceipt describes completed side effects even when a later
// output/transport step fails. MetadataDigest identifies the exact signed bytes.
type MetadataOperatorReceipt struct {
	Operation      string `json:"operation"`
	Valid          bool   `json:"valid"`
	Code           string `json:"code"`
	ArtifactDigest string `json:"artifactDigest"`
	Scope          string `json:"scope"`
	Sequence       uint64 `json:"sequence"`
	KeyID          string `json:"keyID"`
	IssuedAt       string `json:"issuedAt,omitempty"`
	ExpiresAt      string `json:"expiresAt,omitempty"`
	MetadataDigest string `json:"metadataDigest,omitempty"`
	PreviousDigest string `json:"previousDigest,omitempty"`
	Retained       bool   `json:"retained"`
	Distributed    bool   `json:"distributed"`
}

// Change signs a complete snapshot. Renew/revoke authenticate the previous
// snapshot at its issuance time, permitting renewal after expiry. The old key
// must still be in the independently provisioned roots; the new envelope must
// also be authorized and fresh now. Recovery from a retired key uses issue with
// independently reviewed complete inputs and a higher sequence, retaining state.
func (o MetadataOperator) Change(c MetadataChange, previous *MetadataEnvelope, key ed25519.PrivateKey, at time.Time) (MetadataEnvelope, MetadataOperatorReceipt, error) {
	r := MetadataOperatorReceipt{Operation: c.Operation, ArtifactDigest: c.ArtifactDigest, Scope: o.Roots.Scope, Sequence: c.Sequence, KeyID: c.KeyID, IssuedAt: c.IssuedAt, ExpiresAt: c.ExpiresAt}
	fail := func(err error) (MetadataEnvelope, MetadataOperatorReceipt, error) {
		r.Code = metadataFailureCode(err)
		return MetadataEnvelope{}, r, err
	}
	if c.Operation != "issue" && c.Operation != "renew" && c.Operation != "revoke" {
		return fail(metadataError(CodeMetadataInvalid))
	}
	p := MetadataPayload{SchemaVersion: 2, Scope: o.Roots.Scope, ArtifactDigest: c.ArtifactDigest, Sequence: c.Sequence, KeyID: c.KeyID, IssuedAt: c.IssuedAt, ExpiresAt: c.ExpiresAt, Provenance: c.Provenance, SBOM: c.SBOM, Revocations: c.Revocations}
	var old MetadataPayload
	if c.Operation == "issue" {
		if previous != nil || c.Revocations == nil || c.RevokeArtifact || c.RevokeKeyID != "" {
			return fail(metadataError(CodeMetadataInvalid))
		}
	} else {
		if previous == nil || c.Provenance != nil || c.SBOM != nil || c.Revocations != nil {
			return fail(metadataError(CodeMetadataInvalid))
		}
		// Untrusted decoding selects only a historical verification time. All
		// fields are authenticated by VerifyMetadata before use or persistence.
		if len(previous.Payload) > 2<<20 {
			return fail(metadataError(CodeMetadataInvalid))
		}
		data, err := base64.RawStdEncoding.DecodeString(previous.Payload)
		if err != nil || len(data) > 2<<20 || decodeCanonical(data, &old) != nil {
			return fail(metadataError(CodeMetadataInvalid))
		}
		issued, err := time.Parse(time.RFC3339, old.IssuedAt)
		if err != nil {
			return fail(metadataError(CodeMetadataInvalid))
		}
		old, err = VerifyMetadata(*previous, o.Roots, c.ArtifactDigest, issued)
		if err != nil {
			return fail(err)
		}
		if c.Sequence <= old.Sequence {
			return fail(metadataError(CodeMetadataRollback))
		}
		nextTime, err := time.Parse(time.RFC3339, c.IssuedAt)
		if err != nil || nextTime.Before(issued) || issued.After(at) {
			return fail(metadataError(CodeMetadataExpired))
		}
		p.Provenance, p.SBOM, p.Revocations = old.Provenance, old.SBOM, old.Revocations
		r.PreviousDigest, _ = MetadataDigest(*previous)
	}
	if c.Operation == "revoke" {
		if (!c.RevokeArtifact && c.RevokeKeyID == "") || strings.TrimSpace(c.Reason) == "" || len(c.Reason) > 1024 || containsCredential(c.Reason) || len(c.RevokeKeyID) > 256 || containsCredential(c.RevokeKeyID) {
			return fail(metadataError(CodeMetadataInvalid))
		}
		entry := Revocation{Reason: c.Reason}
		if c.RevokeArtifact {
			entry.ArtifactDigests = []string{c.ArtifactDigest}
		}
		if c.RevokeKeyID != "" {
			if strings.TrimSpace(c.RevokeKeyID) == "" {
				return fail(metadataError(CodeMetadataInvalid))
			}
			entry.KeyIDs = []string{c.RevokeKeyID}
		}
		p.Revocations = append(p.Revocations, entry)
	} else if c.RevokeArtifact || c.RevokeKeyID != "" || c.Reason != "" {
		return fail(metadataError(CodeMetadataInvalid))
	}
	// Renewal reaffirms the complete deny set, including old revocations. A
	// stale entry must never turn an authenticated revoke into revocation-stale.
	p.Revocations = append([]Revocation{}, p.Revocations...)
	for i := range p.Revocations {
		p.Revocations[i].UpdatedAt = c.IssuedAt
	}
	envelope, err := SignMetadata(p, key)
	if err != nil {
		return fail(err)
	}
	if _, err := VerifyMetadata(envelope, o.Roots, c.ArtifactDigest, at); err != nil {
		return fail(err)
	}
	r.MetadataDigest, _ = MetadataDigest(envelope)
	unlock, err := o.lock(c.ArtifactDigest)
	if err != nil {
		return fail(err)
	}
	defer unlock()
	if previous != nil {
		if err := o.State.Admit(old, *previous); err != nil {
			return fail(err)
		}
	}
	if err := o.State.Admit(p, envelope); err != nil {
		return fail(err)
	}
	r.Valid, r.Code, r.Retained = true, CodeValid, true
	return envelope, r, nil
}

// Distribute changes only the detached metadata attachment. The per-Artifact
// operator lock covers admission AND transport so cooperating distributors
// sharing this state cannot overwrite a newer snapshot with an older one.
func (o MetadataOperator) Distribute(envelope MetadataEnvelope, artifact string, carrier Carrier, at time.Time) (MetadataOperatorReceipt, error) {
	r := MetadataOperatorReceipt{Operation: "distribute", ArtifactDigest: artifact, Scope: o.Roots.Scope}
	fail := func(err error) (MetadataOperatorReceipt, error) {
		r.Valid, r.Code = false, metadataFailureCode(err)
		return r, err
	}
	p, err := VerifyMetadata(envelope, o.Roots, artifact, at)
	if err != nil {
		return fail(err)
	}
	r.Sequence, r.KeyID, r.IssuedAt, r.ExpiresAt = p.Sequence, p.KeyID, p.IssuedAt, p.ExpiresAt
	r.MetadataDigest, _ = MetadataDigest(envelope)
	if carrier == nil {
		return fail(metadataError(CodeMetadataInvalid))
	}
	unlock, err := o.lock(artifact)
	if err != nil {
		return fail(err)
	}
	defer unlock()
	if err := o.State.Admit(p, envelope); err != nil {
		return fail(err)
	}
	r.Retained = true
	data, err := json.Marshal(envelope)
	if err == nil {
		err = PutAttachment(carrier, artifact, "metadata", data)
	}
	if err != nil {
		r.Code = "metadata-distribution"
		return r, err
	}
	readback, err := ReadMetadata(carrier, artifact)
	if err != nil || *readback != envelope {
		r.Code = "metadata-distribution"
		if err == nil {
			err = metadataError(r.Code)
		}
		return r, err
	}
	r.Valid, r.Code, r.Distributed = true, CodeValid, true
	return r, nil
}

func (o MetadataOperator) lock(artifact string) (func(), error) {
	if !validArtifactDigest(artifact) || o.Roots.Scope == "" || o.State.Root == "" {
		return nil, metadataError(CodeMetadataState)
	}
	if err := ensureMetadataStateDirectory(o.State.Root); err != nil {
		return nil, metadataError(CodeMetadataState)
	}
	if err := rejectSymlinkComponents(o.State.Root, o.State.Root); err != nil {
		return nil, metadataError(CodeMetadataState)
	}
	name := Digest([]byte(o.Roots.Scope + "\x00" + artifact))[7:]
	unlock, err := metadataStateLock(filepath.Join(o.State.Root, name+".operator.lock"))
	if err != nil {
		return nil, metadataError(CodeMetadataState)
	}
	return unlock, nil
}
