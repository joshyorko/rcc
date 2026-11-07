package artifacttrust

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const MetadataMediaType = "application/vnd.rcc.environment.metadata.v2+json"
const metadataSignatureDomain = "rcc.environment.metadata/v2\x00"

const (
	CodeMetadataUnsigned  = "metadata-unsigned"
	CodeMetadataInvalid   = "metadata-invalid"
	CodeMetadataAuthority = "metadata-authority"
	CodeMetadataExpired   = "metadata-expired"
	CodeMetadataRollback  = "metadata-rollback"
	CodeMetadataState     = "metadata-state"
)

// MetadataPayload is one complete authority statement. Nothing from an unsigned
// parallel attachment may be merged into this snapshot after verification.
type MetadataPayload struct {
	SchemaVersion  int          `json:"schemaVersion"`
	Scope          string       `json:"scope"`
	ArtifactDigest string       `json:"artifactDigest"`
	Sequence       uint64       `json:"sequence"`
	KeyID          string       `json:"keyID"`
	IssuedAt       string       `json:"issuedAt"`
	ExpiresAt      string       `json:"expiresAt"`
	Provenance     *Provenance  `json:"provenance"`
	SBOM           *SBOM        `json:"sbom"`
	Revocations    []Revocation `json:"revocations"`
}

// MetadataEnvelope signs the exact decoded payload bytes with a v2-specific
// domain separator. Keys are supplied independently, never inside the envelope.
type MetadataEnvelope struct {
	MediaType string `json:"mediaType"`
	Payload   string `json:"payload"`
	Signature string `json:"signature"`
}

type MetadataKey struct {
	PublicKey string `json:"publicKey"`
	NotBefore string `json:"notBefore"`
	NotAfter  string `json:"notAfter"`
}

// MetadataRoots is trusted deployment configuration. Rotation changes its key
// authorization windows, not the scope/Artifact sequence namespace. MinimumSequence
// supplies the independently provisioned bootstrap floor for a fresh consumer.
type MetadataRoots struct {
	SchemaVersion   int                    `json:"schemaVersion"`
	Scope           string                 `json:"scope"`
	MinimumSequence uint64                 `json:"minimumSequence"`
	Keys            map[string]MetadataKey `json:"keys"`
}

type MetadataError struct{ Code string }

func (e *MetadataError) Error() string { return e.Code }
func metadataError(code string) error  { return &MetadataError{Code: code} }

func metadataFailureCode(err error) string {
	var failure *MetadataError
	if errors.As(err, &failure) {
		return failure.Code
	}
	return CodeMetadataInvalid
}

func ReadMetadata(carrier Carrier, artifact string) (*MetadataEnvelope, error) {
	data, err := carrier.Read(AttachmentName(artifact, "metadata"))
	if err != nil {
		return nil, err
	}
	if len(data) > 3<<20 {
		return nil, metadataError(CodeMetadataInvalid)
	}
	var envelope MetadataEnvelope
	if err := decodeCanonical(data, &envelope); err != nil {
		return nil, metadataError(CodeMetadataInvalid)
	}
	return &envelope, nil
}

func SignMetadata(payload MetadataPayload, key ed25519.PrivateKey) (MetadataEnvelope, error) {
	if len(key) != ed25519.PrivateKeySize {
		return MetadataEnvelope{}, metadataError(CodeMetadataInvalid)
	}
	if err := validateMetadataPayload(payload); err != nil {
		return MetadataEnvelope{}, err
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return MetadataEnvelope{}, err
	}
	message := append([]byte(metadataSignatureDomain), data...)
	return MetadataEnvelope{MediaType: MetadataMediaType, Payload: base64.RawStdEncoding.EncodeToString(data), Signature: base64.RawStdEncoding.EncodeToString(ed25519.Sign(key, message))}, nil
}

// VerifyMetadata authenticates before policy evaluation or any retained-state
// update. Persistence/anti-rollback admission is a separate mandatory boundary.
func VerifyMetadata(envelope MetadataEnvelope, roots MetadataRoots, artifact string, at time.Time) (MetadataPayload, error) {
	var payload MetadataPayload
	if envelope.MediaType != MetadataMediaType || len(envelope.Payload) > 2<<20 {
		return payload, metadataError(CodeMetadataInvalid)
	}
	data, err := base64.RawStdEncoding.DecodeString(envelope.Payload)
	if err != nil {
		return payload, metadataError(CodeMetadataInvalid)
	}
	if err := decodeCanonical(data, &payload); err != nil {
		return MetadataPayload{}, metadataError(CodeMetadataInvalid)
	}
	canonical, err := json.Marshal(payload)
	if err != nil || !bytes.Equal(data, canonical) {
		return MetadataPayload{}, metadataError(CodeMetadataInvalid)
	}
	if err := validateMetadataPayload(payload); err != nil {
		return MetadataPayload{}, err
	}
	if roots.SchemaVersion != 2 || roots.Scope == "" || roots.MinimumSequence == 0 || roots.Scope != payload.Scope || artifact != payload.ArtifactDigest {
		return MetadataPayload{}, metadataError(CodeMetadataAuthority)
	}
	authority, ok := roots.Keys[payload.KeyID]
	if !ok {
		return MetadataPayload{}, metadataError(CodeMetadataAuthority)
	}
	public, err := base64.RawStdEncoding.DecodeString(authority.PublicKey)
	if err != nil || len(public) != ed25519.PublicKeySize {
		return MetadataPayload{}, metadataError(CodeMetadataAuthority)
	}
	signature, err := base64.RawStdEncoding.DecodeString(envelope.Signature)
	if err != nil || !ed25519.Verify(ed25519.PublicKey(public), append([]byte(metadataSignatureDomain), data...), signature) {
		return MetadataPayload{}, metadataError(CodeMetadataInvalid)
	}
	issued, _ := time.Parse(time.RFC3339, payload.IssuedAt)
	expiry, _ := time.Parse(time.RFC3339, payload.ExpiresAt)
	start, startErr := time.Parse(time.RFC3339, authority.NotBefore)
	end, endErr := time.Parse(time.RFC3339, authority.NotAfter)
	if startErr != nil || endErr != nil || !end.After(start) || at.Before(start) || !at.Before(end) || issued.Before(start) || expiry.After(end) {
		return MetadataPayload{}, metadataError(CodeMetadataAuthority)
	}
	if issued.After(at) || !at.Before(expiry) || at.Sub(issued) > DefaultRevocationMaxAge {
		return MetadataPayload{}, metadataError(CodeMetadataExpired)
	}
	if payload.Sequence < roots.MinimumSequence {
		return MetadataPayload{}, metadataError(CodeMetadataRollback)
	}
	return payload, nil
}

func validateMetadataPayload(p MetadataPayload) error {
	if p.SchemaVersion != 2 || p.Scope == "" || len(p.Scope) > 256 || p.KeyID == "" || len(p.KeyID) > 256 || p.Sequence == 0 || !validArtifactDigest(p.ArtifactDigest) {
		return metadataError(CodeMetadataInvalid)
	}
	if containsCredential(p.Scope) || containsCredential(p.KeyID) {
		return metadataError(CodeMetadataInvalid)
	}
	issued, err := time.Parse(time.RFC3339, p.IssuedAt)
	if err != nil {
		return metadataError(CodeMetadataInvalid)
	}
	expires, err := time.Parse(time.RFC3339, p.ExpiresAt)
	if err != nil || !expires.After(issued) || expires.Sub(issued) > DefaultRevocationMaxAge {
		return metadataError(CodeMetadataInvalid)
	}
	if p.Provenance != nil && p.Provenance.ArtifactDigest != p.ArtifactDigest {
		return metadataError(CodeMetadataInvalid)
	}
	if p.SBOM != nil && p.SBOM.ArtifactDigest != p.ArtifactDigest {
		return metadataError(CodeMetadataInvalid)
	}
	if _, err := revocationSnapshot(p.Revocations); err != nil {
		return metadataError(CodeMetadataInvalid)
	}
	return nil
}

func validArtifactDigest(value string) bool {
	if len(value) != len("sha256:")+64 || value[:7] != "sha256:" {
		return false
	}
	for _, c := range value[7:] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func MetadataDigest(envelope MetadataEnvelope) (string, error) {
	data, err := json.Marshal(envelope)
	if err != nil {
		return "", fmt.Errorf("encode metadata envelope: %w", err)
	}
	return Digest(data), nil
}
