// Package artifacttrust provides offline verification for Environment Artifacts.
// Trust metadata is detached from the immutable artifact digest.
package artifacttrust

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	ProvenanceMediaType     = "application/vnd.rcc.environment.provenance.v1+json"
	SBOMMediaType           = "application/vnd.rcc.environment.sbom.spdx+json"
	SignatureMediaType      = "application/vnd.rcc.environment.signature.v1+json"
	RevocationMediaType     = "application/vnd.rcc.environment.revocations.v1+json"
	DefaultPolicyRevision   = "artifacttrust/v1"
	DefaultRevocationMaxAge = 24 * time.Hour
)

type PolicyMode string

const (
	PermissiveLocal PolicyMode = "permissive-local"
	StrictRemote    PolicyMode = "strict-remote"
)
const (
	CodeValid           = "valid"
	CodeUnsigned        = "unsigned"
	CodeInvalid         = "invalid"
	CodeUnknownSigner   = "unknown-signer"
	CodeExpired         = "expired"
	CodeRevoked         = "revoked"
	CodePolicy          = "policy"
	CodeBinding         = "binding"
	CodeRevocationStale = "revocation-stale"
)

type Provenance struct {
	MediaType                    string   `json:"mediaType"`
	ArtifactDigest               string   `json:"artifactDigest"`
	SpecificationDigest          string   `json:"specificationDigest"`
	SourceRepository             string   `json:"sourceRepository,omitempty"`
	SourceRevision               string   `json:"sourceRevision,omitempty"`
	Platform                     string   `json:"platform"`
	Builder                      string   `json:"builder"`
	RCCVersion                   string   `json:"rccVersion"`
	ResolutionMode               string   `json:"resolutionMode,omitempty"`
	CreatedAt                    string   `json:"createdAt"`
	LegacyBlueprintDigest        string   `json:"legacyBlueprintDigest,omitempty"`
	LegacyBlueprintKey           string   `json:"legacyBlueprintKey,omitempty"`
	DependencySources            []string `json:"dependencySources,omitempty"`
	OfflineResolution            bool     `json:"offlineResolution,omitempty"`
	SystemRequirementsOverridden bool     `json:"systemRequirementsOverridden,omitempty"`
	CatalogDigest                string   `json:"catalogDigest,omitempty"`
	ManifestDigest               string   `json:"manifestDigest,omitempty"`
	BuildIdentity                string   `json:"buildIdentity,omitempty"`
}

type Component struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	PackageType string `json:"packageType"`
	Hash        string `json:"hash,omitempty"`
	License     string `json:"license,omitempty"`
	Source      string `json:"source,omitempty"`
}

type SBOM struct {
	MediaType      string      `json:"mediaType"`
	ArtifactDigest string      `json:"artifactDigest"`
	Components     []Component `json:"components"`
}

type Signature struct {
	MediaType      string `json:"mediaType"`
	ArtifactDigest string `json:"artifactDigest"`
	KeyID          string `json:"keyID"`
	Algorithm      string `json:"algorithm"`
	Signature      string `json:"signature"`
	NotBefore      string `json:"notBefore,omitempty"`
	NotAfter       string `json:"notAfter,omitempty"`
}

type Policy struct {
	Mode                      PolicyMode    `json:"mode,omitempty"`
	RequireSignature          bool          `json:"requireSignature"`
	AllowUnsignedLocal        bool          `json:"allowUnsignedLocal"`
	AcceptedKeys              []string      `json:"acceptedKeys,omitempty"`
	AcceptedBuilders          []string      `json:"acceptedBuilders,omitempty"`
	AcceptedPlatforms         []string      `json:"acceptedPlatforms,omitempty"`
	AcceptedRCCVersions       []string      `json:"acceptedRCCVersions,omitempty"`
	AcceptedSources           []string      `json:"acceptedSources,omitempty"`
	AcceptedDependencySources []string      `json:"acceptedDependencySources,omitempty"`
	MaxArtifactAge            time.Duration `json:"maxArtifactAge,omitempty"`
	FailClosedRevocations     bool          `json:"failClosedRevocations,omitempty"`
	RevocationMaxAge          time.Duration `json:"revocationMaxAge,omitempty"`
	Revision                  string        `json:"revision,omitempty"`
}

type VerifyRequest struct {
	ArtifactDigest, Platform, Builder string
	Provenance                        *Provenance
	SBOM                              *SBOM
	Signatures                        []Signature
	Revocations                       []Revocation
	Keys                              map[string]ed25519.PublicKey
	At                                time.Time
	RevocationFetchedAt               time.Time
	RevocationSource                  string
}
type VerificationReceipt struct {
	Valid               bool       `json:"valid"`
	Code                string     `json:"code"`
	ArtifactDigest      string     `json:"artifactDigest"`
	KeyID               string     `json:"keyID,omitempty"`
	PolicyMode          PolicyMode `json:"policyMode"`
	VerifiedAt          string     `json:"verifiedAt"`
	Diagnostic          string     `json:"diagnostic,omitempty"`
	DecisionID          string     `json:"decisionID"`
	PolicyRevision      string     `json:"policyRevision"`
	PolicyDigest        string     `json:"policyDigest,omitempty"`
	Platform            string     `json:"platform,omitempty"`
	Builder             string     `json:"builder,omitempty"`
	ProvenanceDigest    string     `json:"provenanceDigest,omitempty"`
	SBOMDigest          string     `json:"sbomDigest,omitempty"`
	RevocationSnapshot  string     `json:"revocationSnapshot,omitempty"`
	RevocationCheckedAt string     `json:"revocationCheckedAt,omitempty"`
	RevocationSource    string     `json:"revocationSource,omitempty"`
	LeaseID             string     `json:"leaseID,omitempty"`
}

func (r VerificationReceipt) JSON() ([]byte, error) { return canonical(r) }

// Carrier is deliberately content-addressed and provider-neutral. HTTP,
// archives, and OCI adapters only need to implement these two operations.
type Carrier interface {
	Read(string) ([]byte, error)
	Write(string, []byte) error
}

func AttachmentName(artifact, kind string) string { return artifact + "/" + Normalize(kind) + ".json" }
func PutAttachment(c Carrier, artifact, kind string, data []byte) error {
	if c == nil {
		return fmt.Errorf("trust carrier is nil")
	}
	if len(data) > maxCarrierAttachmentBytes {
		return fmt.Errorf("carrier attachment exceeds %d bytes", maxCarrierAttachmentBytes)
	}
	if err := validateCarrierName(AttachmentName(artifact, kind)); err != nil {
		return err
	}
	var err error
	data, err = normalizeAttachmentData(artifact, kind, data)
	if err != nil {
		return err
	}
	if err := VerifyAttestationBinding(data, mediaTypeFor(kind), artifact); err != nil {
		return err
	}
	return c.Write(AttachmentName(artifact, kind), data)
}

func normalizeAttachmentData(artifact, kind string, data []byte) ([]byte, error) {
	if VerifyAttestationBinding(data, mediaTypeFor(kind), artifact) == nil {
		if err := validateAttachmentData(artifact, kind, data); err != nil {
			return nil, err
		}
		return data, nil
	}
	switch Normalize(kind) {
	case "signature", "signatures":
		var signatures []Signature
		if err := decodeStrict(data, &signatures); err != nil {
			return nil, fmt.Errorf("invalid signature attachment: %w", err)
		}
		_, canonicalData, err := NewSignatureBundle(artifact, signatures)
		return canonicalData, err
	case "revocation", "revocations":
		var revocations []Revocation
		if err := decodeStrict(data, &revocations); err != nil {
			return nil, fmt.Errorf("invalid revocation attachment: %w", err)
		}
		_, canonicalData, err := NewRevocationBundle(artifact, revocations)
		return canonicalData, err
	default:
		return nil, fmt.Errorf("invalid detached attachment")
	}
}

func validateAttachmentData(artifact, kind string, data []byte) error {
	switch Normalize(kind) {
	case "provenance":
		var provenance Provenance
		if err := decodeStrict(data, &provenance); err != nil {
			return fmt.Errorf("invalid provenance attachment: %w", err)
		}
		if provenance.MediaType != ProvenanceMediaType || provenance.ArtifactDigest != artifact {
			return fmt.Errorf("provenance attachment is not bound")
		}
	case "sbom":
		var sbom SBOM
		if err := decodeStrict(data, &sbom); err != nil {
			return fmt.Errorf("invalid SBOM attachment: %w", err)
		}
		if sbom.MediaType != SBOMMediaType || sbom.ArtifactDigest != artifact {
			return fmt.Errorf("SBOM attachment is not bound")
		}
	case "signature", "signatures":
		if _, err := DecodeSignatureBundle(data, artifact); err != nil {
			return err
		}
	case "revocation", "revocations":
		if _, err := DecodeRevocationBundle(data, artifact); err != nil {
			return err
		}
	default:
		return fmt.Errorf("invalid detached attachment")
	}
	return nil
}
func GetAttachment(c Carrier, artifact, kind string) ([]byte, error) {
	if c == nil {
		return nil, fmt.Errorf("trust carrier is nil")
	}
	if err := validateCarrierName(AttachmentName(artifact, kind)); err != nil {
		return nil, err
	}
	data, err := c.Read(AttachmentName(artifact, kind))
	if err != nil {
		return nil, err
	}
	if len(data) > maxCarrierAttachmentBytes {
		return nil, fmt.Errorf("carrier attachment exceeds %d bytes", maxCarrierAttachmentBytes)
	}
	if err := VerifyAttestationBinding(data, mediaTypeFor(kind), artifact); err != nil {
		return nil, err
	}
	return data, nil
}
func mediaTypeFor(kind string) string {
	switch Normalize(kind) {
	case "provenance":
		return ProvenanceMediaType
	case "sbom":
		return SBOMMediaType
	case "signature", "signatures":
		return SignatureMediaType
	case "revocation", "revocations":
		return RevocationMediaType
	}
	return ""
}

type SignatureBundle struct {
	MediaType      string      `json:"mediaType"`
	ArtifactDigest string      `json:"artifactDigest"`
	Signatures     []Signature `json:"signatures"`
}

type RevocationBundle struct {
	MediaType      string       `json:"mediaType"`
	ArtifactDigest string       `json:"artifactDigest"`
	Revocations    []Revocation `json:"revocations"`
	FetchedAt      string       `json:"fetchedAt,omitempty"`
	Source         string       `json:"source,omitempty"`
}

func NewSignatureBundle(artifact string, signatures []Signature) (SignatureBundle, []byte, error) {
	ordered := append([]Signature(nil), signatures...)
	if ordered == nil {
		ordered = []Signature{}
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		return signatureSortKey(ordered[i]) < signatureSortKey(ordered[j])
	})
	for _, signature := range ordered {
		if err := VerifyAttestationBinding(mustCanonical(signature), SignatureMediaType, artifact); err != nil {
			return SignatureBundle{}, nil, err
		}
	}
	bundle := SignatureBundle{MediaType: SignatureMediaType, ArtifactDigest: artifact, Signatures: ordered}
	data, err := canonical(bundle)
	return bundle, data, err
}

func signatureSortKey(value Signature) string {
	return strings.Join([]string{value.KeyID, value.Signature, value.NotBefore, value.NotAfter}, "|")
}

func DecodeSignatureBundle(data []byte, artifact string) (SignatureBundle, error) {
	var bundle SignatureBundle
	if err := decodeStrict(data, &bundle); err != nil {
		return SignatureBundle{}, fmt.Errorf("decode signature bundle: %w", err)
	}
	if bundle.MediaType != SignatureMediaType || bundle.ArtifactDigest != artifact {
		return SignatureBundle{}, fmt.Errorf("signature bundle artifact binding mismatch")
	}
	canonicalData, err := canonical(bundle)
	if err != nil || !bytes.Equal(canonicalData, data) {
		return SignatureBundle{}, fmt.Errorf("signature bundle is not canonical")
	}
	for _, signature := range bundle.Signatures {
		if signature.MediaType != SignatureMediaType || signature.ArtifactDigest != artifact {
			return SignatureBundle{}, fmt.Errorf("signature bundle contains an unbound signature")
		}
	}
	for i := 1; i < len(bundle.Signatures); i++ {
		if signatureSortKey(bundle.Signatures[i-1]) > signatureSortKey(bundle.Signatures[i]) {
			return SignatureBundle{}, fmt.Errorf("signature bundle is not deterministically ordered")
		}
	}
	return bundle, nil
}

func NewRevocationBundle(artifact string, revocations []Revocation) (RevocationBundle, []byte, error) {
	return NewRevocationBundleAt(artifact, revocations, time.Time{}, "")
}

func NewRevocationBundleAt(artifact string, revocations []Revocation, fetchedAt time.Time, source string) (RevocationBundle, []byte, error) {
	if containsCredential(source) {
		return RevocationBundle{}, nil, fmt.Errorf("revocation source contains credentials")
	}
	ordered := normalizeRevocations(revocations)
	sort.SliceStable(ordered, func(i, j int) bool {
		return revocationSortKey(ordered[i]) < revocationSortKey(ordered[j])
	})
	bundle := RevocationBundle{MediaType: RevocationMediaType, ArtifactDigest: artifact, Revocations: ordered, Source: source}
	if !fetchedAt.IsZero() {
		bundle.FetchedAt = FreshTimestamp(fetchedAt)
	}
	data, err := canonical(bundle)
	return bundle, data, err
}

func DecodeRevocationBundle(data []byte, artifact string) (RevocationBundle, error) {
	var bundle RevocationBundle
	if err := decodeStrict(data, &bundle); err != nil {
		return RevocationBundle{}, fmt.Errorf("decode revocation bundle: %w", err)
	}
	if bundle.MediaType != RevocationMediaType || bundle.ArtifactDigest != artifact {
		return RevocationBundle{}, fmt.Errorf("revocation bundle artifact binding mismatch")
	}
	if containsCredential(bundle.Source) {
		return RevocationBundle{}, fmt.Errorf("revocation source contains credentials")
	}
	for i := 1; i < len(bundle.Revocations); i++ {
		if revocationSortKey(bundle.Revocations[i-1]) > revocationSortKey(bundle.Revocations[i]) {
			return RevocationBundle{}, fmt.Errorf("revocation bundle is not deterministically ordered")
		}
	}
	normalized := bundle
	normalized.Revocations = normalizeRevocations(bundle.Revocations)
	sort.SliceStable(normalized.Revocations, func(i, j int) bool {
		return revocationSortKey(normalized.Revocations[i]) < revocationSortKey(normalized.Revocations[j])
	})
	canonicalData, err := canonical(normalized)
	if err != nil || !bytes.Equal(canonicalData, data) {
		return RevocationBundle{}, fmt.Errorf("revocation bundle is not canonical")
	}
	return bundle, nil
}

func mustCanonical(value any) []byte {
	data, _ := canonical(value)
	return data
}

func decodeStrict(data []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("trailing attachment data")
	}
	return nil
}

type Revocation struct {
	ArtifactDigests []string `json:"artifactDigests,omitempty"`
	KeyIDs          []string `json:"keyIDs,omitempty"`
	Reason          string   `json:"reason,omitempty"`
	UpdatedAt       string   `json:"updatedAt"`
}

func canonical(v any) ([]byte, error) { return json.Marshal(v) }

func NewSBOM(artifact string, components []Component) (SBOM, []byte, error) {
	copyComponents := append([]Component(nil), components...)
	s := SBOM{MediaType: SBOMMediaType, ArtifactDigest: artifact, Components: copyComponents}
	b, err := CanonicalSBOM(s)
	if err != nil {
		return SBOM{}, nil, err
	}
	var canonicalSBOM SBOM
	if err := decodeStrict(b, &canonicalSBOM); err != nil {
		return SBOM{}, nil, err
	}
	return canonicalSBOM, b, nil
}

func CanonicalSBOM(s SBOM) ([]byte, error) {
	if s.MediaType == "" {
		s.MediaType = SBOMMediaType
	}
	if s.MediaType != SBOMMediaType {
		return nil, fmt.Errorf("unsupported SBOM media type")
	}
	for _, component := range s.Components {
		if containsCredential(component.Name) || containsCredential(component.Version) || containsCredential(component.PackageType) || containsCredential(component.Hash) || containsCredential(component.License) || containsCredential(component.Source) {
			return nil, fmt.Errorf("SBOM component contains credentials")
		}
	}
	s.Components = append([]Component(nil), s.Components...)
	if s.Components == nil {
		s.Components = []Component{}
	}
	sort.SliceStable(s.Components, func(i, j int) bool {
		left, right := s.Components[i], s.Components[j]
		if left.Name != right.Name {
			return left.Name < right.Name
		}
		if left.Version != right.Version {
			return left.Version < right.Version
		}
		if left.PackageType != right.PackageType {
			return left.PackageType < right.PackageType
		}
		if left.Hash != right.Hash {
			return left.Hash < right.Hash
		}
		if left.License != right.License {
			return left.License < right.License
		}
		return left.Source < right.Source
	})
	return canonical(s)
}

func CanonicalProvenance(p Provenance) ([]byte, error) {
	if p.MediaType == "" {
		p.MediaType = ProvenanceMediaType
	}
	if p.MediaType != ProvenanceMediaType {
		return nil, fmt.Errorf("unsupported provenance media type")
	}
	p.DependencySources = append([]string(nil), p.DependencySources...)
	sort.Strings(p.DependencySources)
	for _, s := range append([]string{
		p.ArtifactDigest, p.SpecificationDigest, p.SourceRepository, p.SourceRevision,
		p.Platform, p.Builder, p.RCCVersion, p.ResolutionMode, p.CreatedAt,
		p.LegacyBlueprintDigest, p.LegacyBlueprintKey, p.CatalogDigest, p.ManifestDigest,
		p.BuildIdentity,
	}, p.DependencySources...) {
		if containsCredential(s) {
			return nil, fmt.Errorf("provenance source contains credentials")
		}
	}
	return canonical(p)
}

func containsCredential(value string) bool {
	lower := strings.ToLower(value)
	for _, marker := range []string{
		"password=", "passwd=", "pass=", "secret=", "token=", "apikey=", "api_key=", "access_key=", "private_key=", "authorization:", "bearer ",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	if strings.Contains(lower, "-----begin ") {
		return true
	}
	u, err := url.Parse(value)
	if err != nil {
		return false
	}
	if u.User != nil && (u.User.Username() != "" || func() bool { _, ok := u.User.Password(); return ok }()) {
		return true
	}
	for key := range u.Query() {
		switch strings.ToLower(key) {
		case "password", "passwd", "pass", "secret", "token", "apikey", "api_key", "access_token", "authorization", "signature", "sig", "key":
			return true
		}
	}
	return false
}
func VerifyAttestationBinding(data []byte, mediaType, artifact string) error {
	var v struct {
		MediaType      string `json:"mediaType"`
		ArtifactDigest string `json:"artifactDigest"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return fmt.Errorf("invalid attestation: %w", err)
	}
	if mediaType == "" || v.MediaType != mediaType || v.ArtifactDigest != artifact {
		return fmt.Errorf("attestation artifact digest mismatch")
	}
	return nil
}

func Sign(artifact string, keyID string, privateKey ed25519.PrivateKey) (Signature, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return Signature{}, fmt.Errorf("invalid Ed25519 private key")
	}
	signature := Signature{MediaType: SignatureMediaType, ArtifactDigest: artifact, KeyID: keyID, Algorithm: "Ed25519"}
	signature.Signature = base64.RawStdEncoding.EncodeToString(ed25519.Sign(privateKey, signatureMessage(signature)))
	return signature, nil
}
func SignAt(artifact, keyID string, privateKey ed25519.PrivateKey, notBefore, notAfter time.Time) (Signature, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return Signature{}, fmt.Errorf("invalid Ed25519 private key")
	}
	s := Signature{MediaType: SignatureMediaType, ArtifactDigest: artifact, KeyID: keyID, Algorithm: "Ed25519", NotBefore: FreshTimestamp(notBefore), NotAfter: FreshTimestamp(notAfter)}
	s.Signature = base64.RawStdEncoding.EncodeToString(ed25519.Sign(privateKey, signatureMessage(s)))
	return s, nil
}

func VerifySignature(s Signature, publicKey ed25519.PublicKey, artifact string) error {
	if s.MediaType != SignatureMediaType || s.Algorithm != "Ed25519" {
		return fmt.Errorf("unsupported signature media type or algorithm")
	}
	if s.ArtifactDigest != artifact {
		return fmt.Errorf("signature artifact digest mismatch")
	}
	raw, err := base64.RawStdEncoding.DecodeString(s.Signature)
	if err != nil || !ed25519.Verify(publicKey, signatureMessage(s), raw) {
		return fmt.Errorf("invalid artifact signature")
	}
	return nil
}

func signatureMessage(signature Signature) []byte {
	return []byte(signature.ArtifactDigest + "\x00" + signature.KeyID + "\x00" + signature.Algorithm + "\x00" + signature.NotBefore + "\x00" + signature.NotAfter)
}

func (r Revocation) Revoked(artifact, keyID string) bool {
	for _, v := range r.ArtifactDigests {
		if v == artifact {
			return true
		}
	}
	for _, v := range r.KeyIDs {
		if v == keyID {
			return true
		}
	}
	return false
}

func (p Policy) Evaluate(local bool, artifact, platform, builder string, signatures []Signature, revocations []Revocation, keys map[string]ed25519.PublicKey) error {
	return p.evaluateAt(local, artifact, platform, builder, signatures, revocations, keys, time.Now().UTC())
}

func (p Policy) evaluateAt(local bool, artifact, platform, builder string, signatures []Signature, revocations []Revocation, keys map[string]ed25519.PublicKey, at time.Time) error {
	if p.Mode == StrictRemote {
		local = false
		p.RequireSignature = true
	}
	if !containsOrEmpty(p.AcceptedPlatforms, platform) {
		return fmt.Errorf("trust policy disallows platform")
	}
	if !containsOrEmpty(p.AcceptedBuilders, builder) {
		return fmt.Errorf("trust policy disallows builder")
	}
	if findRevocation(revocations, artifact, "") != nil {
		return fmt.Errorf("artifact trust rejected: revoked")
	}
	if local && p.AllowUnsignedLocal && !p.RequireSignature {
		return nil
	}
	for _, s := range signatures {
		if !containsOrEmpty(p.AcceptedKeys, s.KeyID) {
			continue
		}
		if r := findRevocation(revocations, artifact, s.KeyID); r != nil {
			return fmt.Errorf("artifact trust rejected: revoked")
		}
		if key := keys[s.KeyID]; key != nil {
			if err := VerifySignature(s, key, artifact); err == nil && signatureActive(s, at) {
				return nil
			}
		}
	}
	if p.RequireSignature {
		return fmt.Errorf("artifact trust rejected: no valid signature")
	}
	return nil
}

func (p Policy) Verify(q VerifyRequest) VerificationReceipt {
	if q.At.IsZero() {
		q.At = time.Now().UTC()
	}
	p.Mode = p.effectiveMode()
	if p.Mode == StrictRemote {
		p.RequireSignature = true
	}
	if containsCredential(q.Platform) || containsCredential(q.Builder) || containsCredential(q.ArtifactDigest) || containsCredential(q.RevocationSource) || containsCredential(p.Revision) {
		return VerificationReceipt{
			ArtifactDigest: q.ArtifactDigest, PolicyMode: p.Mode, VerifiedAt: FreshTimestamp(q.At),
			PolicyRevision: p.policyRevision(), PolicyDigest: p.digest(), Code: CodePolicy, Diagnostic: "trust input contains disallowed data",
		}.withDecisionID()
	}
	r := VerificationReceipt{
		ArtifactDigest: q.ArtifactDigest, PolicyMode: p.Mode, VerifiedAt: FreshTimestamp(q.At),
		PolicyRevision: p.policyRevision(), PolicyDigest: p.digest(), Platform: q.Platform, Builder: q.Builder,
		RevocationCheckedAt: FreshTimestamp(q.At), RevocationSource: q.RevocationSource,
	}
	if containsCredential(q.RevocationSource) {
		r.Code, r.Diagnostic, r.RevocationSource = CodePolicy, "revocation source contains disallowed data", ""
		return r.withDecisionID()
	}
	if q.Provenance != nil {
		if data, err := CanonicalProvenance(*q.Provenance); err != nil {
			r.Code, r.Diagnostic = CodePolicy, "provenance contains disallowed data"
			return r.withDecisionID()
		} else {
			r.ProvenanceDigest = Digest(data)
		}
		if q.Provenance.MediaType != ProvenanceMediaType || q.Provenance.ArtifactDigest != q.ArtifactDigest {
			r.Code, r.Diagnostic = CodeBinding, "provenance artifact digest mismatch"
			return r.withDecisionID()
		}
		if q.Platform == "" || q.Builder == "" || q.Provenance.Platform == "" || q.Provenance.Builder == "" || q.Provenance.Platform != q.Platform || q.Provenance.Builder != q.Builder {
			r.Code, r.Diagnostic = CodeBinding, "provenance platform or builder does not match request"
			return r.withDecisionID()
		}
		if !containsOrEmpty(p.AcceptedRCCVersions, q.Provenance.RCCVersion) || !containsOrEmpty(p.AcceptedSources, q.Provenance.SourceRepository) {
			r.Code, r.Diagnostic = CodePolicy, "provenance disallowed by policy"
			return r.withDecisionID()
		}
		for _, source := range q.Provenance.DependencySources {
			if !containsOrEmpty(p.AcceptedDependencySources, source) {
				r.Code, r.Diagnostic = CodePolicy, "dependency source disallowed by policy"
				return r.withDecisionID()
			}
		}
		if p.MaxArtifactAge > 0 {
			created, err := time.Parse(time.RFC3339, q.Provenance.CreatedAt)
			if err != nil || created.After(q.At) || q.At.Sub(created) > p.MaxArtifactAge {
				r.Code, r.Diagnostic = CodePolicy, "artifact exceeds maximum age policy"
				return r.withDecisionID()
			}
		}
	}
	if q.SBOM != nil {
		if data, err := CanonicalSBOM(*q.SBOM); err != nil {
			r.Code, r.Diagnostic = CodePolicy, "SBOM contains disallowed data"
			return r.withDecisionID()
		} else {
			r.SBOMDigest = Digest(data)
		}
		if q.SBOM.MediaType != SBOMMediaType || q.SBOM.ArtifactDigest != q.ArtifactDigest {
			r.Code, r.Diagnostic = CodeBinding, "SBOM artifact digest mismatch"
			return r.withDecisionID()
		}
	}
	if snapshot, err := revocationSnapshot(q.Revocations); err != nil {
		r.Code, r.Diagnostic = CodeRevocationStale, "revocation snapshot is invalid"
		return r.withDecisionID()
	} else {
		r.RevocationSnapshot = snapshot
	}
	if p.FailClosedRevocations {
		if err := validateRevocationFreshness(q, p); err != nil {
			r.Code, r.Diagnostic = CodeRevocationStale, "revocation snapshot is stale or unavailable"
			return r.withDecisionID()
		}
	}
	if err := p.evaluateAt(p.Mode == PermissiveLocal, q.ArtifactDigest, q.Platform, q.Builder, q.Signatures, q.Revocations, q.Keys, q.At); err == nil {
		r.Valid, r.Code = true, CodeValid
		for _, signature := range q.Signatures {
			if key := q.Keys[signature.KeyID]; key != nil && VerifySignature(signature, key, q.ArtifactDigest) == nil && signatureActive(signature, q.At) {
				r.KeyID = signature.KeyID
				break
			}
		}
		return r.withDecisionID()
	} else {
		r.Diagnostic = "artifact trust verification failed"
	}
	if findRevocation(q.Revocations, q.ArtifactDigest, "") != nil {
		r.Code = CodeRevoked
	} else if len(q.Signatures) == 0 {
		r.Code = CodeUnsigned
	} else {
		r.Code = CodeInvalid
		for _, signature := range q.Signatures {
			if q.Keys[signature.KeyID] == nil {
				r.Code = CodeUnknownSigner
			}
			if findRevocation(q.Revocations, q.ArtifactDigest, signature.KeyID) != nil {
				r.Code = CodeRevoked
			}
			if signature.NotAfter != "" {
				if expiry, err := time.Parse(time.RFC3339, signature.NotAfter); err == nil && !q.At.Before(expiry) {
					r.Code = CodeExpired
				}
			}
			if signature.NotBefore != "" {
				if start, err := time.Parse(time.RFC3339, signature.NotBefore); err == nil && q.At.Before(start) {
					r.Code = CodeInvalid
				}
			}
		}
	}
	return r.withDecisionID()
}

// FailureReceipt records a trust-input failure that occurred before the
// attestation payload could be passed to Verify. The diagnostic is deliberately
// bounded to avoid persisting carrier/provider secrets.
func (p Policy) FailureReceipt(artifact, platform, builder, code, diagnostic string, at time.Time) VerificationReceipt {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	p.Mode = p.effectiveMode()
	if p.Mode == StrictRemote {
		p.RequireSignature = true
	}
	if code == "" {
		code = CodeInvalid
	}
	if diagnostic == "" || containsCredential(diagnostic) {
		diagnostic = "trust attachment could not be decoded"
	}
	return VerificationReceipt{
		ArtifactDigest: artifact, PolicyMode: p.Mode, VerifiedAt: FreshTimestamp(at),
		PolicyRevision: p.policyRevision(), PolicyDigest: p.digest(), Platform: platform,
		Builder: builder, Code: code, Diagnostic: diagnostic,
	}.withDecisionID()
}

func (p Policy) effectiveMode() PolicyMode {
	if p.Mode == PermissiveLocal {
		return PermissiveLocal
	}
	return StrictRemote
}

func (p Policy) policyRevision() string {
	if p.Revision != "" {
		return p.Revision
	}
	return DefaultPolicyRevision
}

func (p Policy) digest() string {
	copyPolicy := p
	copyPolicy.AcceptedKeys = append([]string(nil), p.AcceptedKeys...)
	copyPolicy.AcceptedBuilders = append([]string(nil), p.AcceptedBuilders...)
	copyPolicy.AcceptedPlatforms = append([]string(nil), p.AcceptedPlatforms...)
	copyPolicy.AcceptedRCCVersions = append([]string(nil), p.AcceptedRCCVersions...)
	copyPolicy.AcceptedSources = append([]string(nil), p.AcceptedSources...)
	copyPolicy.AcceptedDependencySources = append([]string(nil), p.AcceptedDependencySources...)
	for _, values := range [][]string{
		copyPolicy.AcceptedKeys, copyPolicy.AcceptedBuilders, copyPolicy.AcceptedPlatforms,
		copyPolicy.AcceptedRCCVersions, copyPolicy.AcceptedSources, copyPolicy.AcceptedDependencySources,
	} {
		sort.Strings(values)
	}
	data, _ := canonical(copyPolicy)
	return Digest(data)
}

func (r VerificationReceipt) withDecisionID() VerificationReceipt {
	if r.DecisionID != "" {
		return r
	}
	data, _ := canonical(struct {
		ArtifactDigest   string     `json:"artifactDigest"`
		Code             string     `json:"code"`
		PolicyMode       PolicyMode `json:"policyMode"`
		PolicyRevision   string     `json:"policyRevision"`
		PolicyDigest     string     `json:"policyDigest,omitempty"`
		VerifiedAt       string     `json:"verifiedAt"`
		KeyID            string     `json:"keyID,omitempty"`
		Platform         string     `json:"platform,omitempty"`
		Builder          string     `json:"builder,omitempty"`
		Provenance       string     `json:"provenance,omitempty"`
		SBOM             string     `json:"sbom,omitempty"`
		Revocations      string     `json:"revocations,omitempty"`
		RevocationSource string     `json:"revocationSource,omitempty"`
	}{r.ArtifactDigest, r.Code, r.PolicyMode, r.PolicyRevision, r.PolicyDigest, r.VerifiedAt, r.KeyID, r.Platform, r.Builder, r.ProvenanceDigest, r.SBOMDigest, r.RevocationSnapshot, r.RevocationSource})
	r.DecisionID = Digest(data)
	return r
}

func revocationSortKey(value Revocation) string {
	keys := append([]string(nil), value.KeyIDs...)
	artifacts := append([]string(nil), value.ArtifactDigests...)
	sort.Strings(keys)
	sort.Strings(artifacts)
	return strings.Join(artifacts, ",") + "|" + strings.Join(keys, ",") + "|" + value.UpdatedAt + "|" + value.Reason
}

func revocationSnapshot(values []Revocation) (string, error) {
	ordered := normalizeRevocations(values)
	sort.SliceStable(ordered, func(i, j int) bool { return revocationSortKey(ordered[i]) < revocationSortKey(ordered[j]) })
	for _, value := range ordered {
		if value.UpdatedAt == "" {
			continue
		}
		if _, err := time.Parse(time.RFC3339, value.UpdatedAt); err != nil {
			return "", err
		}
	}
	data, err := canonical(ordered)
	if err != nil {
		return "", err
	}
	return Digest(data), nil
}

func normalizeRevocations(values []Revocation) []Revocation {
	ordered := append([]Revocation(nil), values...)
	if ordered == nil {
		ordered = []Revocation{}
	}
	for index := range ordered {
		ordered[index].ArtifactDigests = append([]string(nil), ordered[index].ArtifactDigests...)
		ordered[index].KeyIDs = append([]string(nil), ordered[index].KeyIDs...)
		sort.Strings(ordered[index].ArtifactDigests)
		sort.Strings(ordered[index].KeyIDs)
	}
	return ordered
}

func validateRevocationFreshness(q VerifyRequest, p Policy) error {
	maxAge := p.RevocationMaxAge
	if maxAge <= 0 {
		maxAge = DefaultRevocationMaxAge
	}
	if len(q.Revocations) == 0 {
		if q.RevocationFetchedAt.IsZero() || q.RevocationFetchedAt.After(q.At) || q.At.Sub(q.RevocationFetchedAt) > maxAge {
			return fmt.Errorf("missing revocation list")
		}
		return nil
	}
	latest := time.Time{}
	for _, revocation := range q.Revocations {
		updated, err := time.Parse(time.RFC3339, revocation.UpdatedAt)
		if err != nil || updated.After(q.At) || q.At.Sub(updated) > maxAge {
			return fmt.Errorf("stale revocation list")
		}
		if updated.After(latest) {
			latest = updated
		}
	}
	if !q.RevocationFetchedAt.IsZero() && (q.RevocationFetchedAt.After(q.At) || q.At.Sub(q.RevocationFetchedAt) > maxAge) {
		return fmt.Errorf("stale revocation fetch")
	}
	if latest.IsZero() {
		return fmt.Errorf("missing revocation timestamp")
	}
	return nil
}

func signatureActive(s Signature, at time.Time) bool {
	if s.NotBefore != "" {
		t, err := time.Parse(time.RFC3339, s.NotBefore)
		if err != nil || at.Before(t) {
			return false
		}
	}
	if s.NotAfter != "" {
		t, err := time.Parse(time.RFC3339, s.NotAfter)
		if err != nil || !at.Before(t) {
			return false
		}
	}
	return true
}

func findRevocation(rs []Revocation, artifact, key string) *Revocation {
	for i := range rs {
		if rs[i].Revoked(artifact, key) {
			return &rs[i]
		}
	}
	return nil
}
func containsOrEmpty(values []string, value string) bool {
	if len(values) == 0 {
		return true
	}
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

func Digest(content []byte) string {
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:])
}
func FreshTimestamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }
func Normalize(value string) string     { return strings.ToLower(strings.TrimSpace(value)) }
