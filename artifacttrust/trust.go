// Package artifacttrust provides offline verification for Environment Artifacts.
// Trust metadata is detached from the immutable artifact digest.
package artifacttrust

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	ProvenanceMediaType = "application/vnd.rcc.environment.provenance.v1+json"
	SBOMMediaType       = "application/vnd.rcc.environment.sbom.spdx+json"
	SignatureMediaType  = "application/vnd.rcc.environment.signature.v1+json"
)

type PolicyMode string
const ( PermissiveLocal PolicyMode = "permissive-local"; StrictRemote PolicyMode = "strict-remote" )
const (
	CodeValid = "valid"; CodeUnsigned = "unsigned"; CodeInvalid = "invalid"; CodeUnknownSigner = "unknown-signer"; CodeExpired = "expired"; CodeRevoked = "revoked"; CodePolicy = "policy"; CodeBinding = "binding"
)

type Provenance struct {
	MediaType           string `json:"mediaType"`
	ArtifactDigest      string `json:"artifactDigest"`
	SpecificationDigest string `json:"specificationDigest"`
	SourceRepository    string `json:"sourceRepository,omitempty"`
	SourceRevision      string `json:"sourceRevision,omitempty"`
	Platform            string `json:"platform"`
	Builder             string `json:"builder"`
	RCCVersion          string `json:"rccVersion"`
	ResolutionMode      string `json:"resolutionMode,omitempty"`
	CreatedAt           string `json:"createdAt"`
	LegacyBlueprintDigest string `json:"legacyBlueprintDigest,omitempty"`
	LegacyBlueprintKey string `json:"legacyBlueprintKey,omitempty"`
	DependencySources []string `json:"dependencySources,omitempty"`
	OfflineResolution bool `json:"offlineResolution,omitempty"`
	SystemRequirementsOverridden bool `json:"systemRequirementsOverridden,omitempty"`
	CatalogDigest string `json:"catalogDigest,omitempty"`
	ManifestDigest string `json:"manifestDigest,omitempty"`
	BuildIdentity string `json:"buildIdentity,omitempty"`
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
	NotBefore string `json:"notBefore,omitempty"`
	NotAfter string `json:"notAfter,omitempty"`
}

type Policy struct {
	Mode PolicyMode `json:"mode,omitempty"`
	RequireSignature   bool     `json:"requireSignature"`
	AllowUnsignedLocal bool     `json:"allowUnsignedLocal"`
	AcceptedKeys       []string `json:"acceptedKeys,omitempty"`
	AcceptedBuilders   []string `json:"acceptedBuilders,omitempty"`
	AcceptedPlatforms  []string `json:"acceptedPlatforms,omitempty"`
	AcceptedRCCVersions []string `json:"acceptedRCCVersions,omitempty"`
	AcceptedSources []string `json:"acceptedSources,omitempty"`
	AcceptedDependencySources []string `json:"acceptedDependencySources,omitempty"`
	MaxArtifactAge time.Duration `json:"maxArtifactAge,omitempty"`
	FailClosedRevocations bool `json:"failClosedRevocations,omitempty"`
}

type VerifyRequest struct { ArtifactDigest, Platform, Builder string; Provenance *Provenance; SBOM *SBOM; Signatures []Signature; Revocations []Revocation; Keys map[string]ed25519.PublicKey; At time.Time }
type VerificationReceipt struct { Valid bool `json:"valid"`; Code string `json:"code"`; ArtifactDigest string `json:"artifactDigest"`; KeyID string `json:"keyID,omitempty"`; PolicyMode PolicyMode `json:"policyMode"`; VerifiedAt string `json:"verifiedAt"`; Diagnostic string `json:"diagnostic,omitempty"` }
func (r VerificationReceipt) JSON() ([]byte,error) { return canonical(r) }

// Carrier is deliberately content-addressed and provider-neutral. HTTP,
// archives, and OCI adapters only need to implement these two operations.
type Carrier interface { Read(string) ([]byte,error); Write(string,[]byte) error }
func AttachmentName(artifact, kind string) string { return artifact+"/"+Normalize(kind)+".json" }
func PutAttachment(c Carrier, artifact, kind string, data []byte) error { if err:=VerifyAttestationBinding(data, mediaTypeFor(kind), artifact); err!=nil{return err}; return c.Write(AttachmentName(artifact,kind),data) }
func GetAttachment(c Carrier, artifact, kind string) ([]byte,error) { data,err:=c.Read(AttachmentName(artifact,kind)); if err!=nil{return nil,err}; if err:=VerifyAttestationBinding(data,mediaTypeFor(kind),artifact); err!=nil{return nil,err}; return data,nil }
func mediaTypeFor(kind string) string { switch Normalize(kind) { case "provenance": return ProvenanceMediaType; case "sbom": return SBOMMediaType; case "signature": return SignatureMediaType }; return "" }

type Revocation struct {
	ArtifactDigests []string `json:"artifactDigests,omitempty"`
	KeyIDs          []string `json:"keyIDs,omitempty"`
	Reason          string   `json:"reason,omitempty"`
	UpdatedAt       string   `json:"updatedAt"`
}

func canonical(v any) ([]byte, error) { return json.Marshal(v) }

func NewSBOM(artifact string, components []Component) (SBOM, []byte, error) {
	copyComponents := append([]Component(nil), components...)
	sort.Slice(copyComponents, func(i, j int) bool {
		if copyComponents[i].Name != copyComponents[j].Name {
			return copyComponents[i].Name < copyComponents[j].Name
		}
		return copyComponents[i].Version < copyComponents[j].Version
	})
	s := SBOM{MediaType: SBOMMediaType, ArtifactDigest: artifact, Components: copyComponents}
	b, err := canonical(s)
	return s, b, err
}

func CanonicalProvenance(p Provenance) ([]byte,error) { if p.MediaType == "" { p.MediaType = ProvenanceMediaType }; return canonical(p) }
func VerifyAttestationBinding(data []byte, mediaType, artifact string) error {
	var v struct { MediaType string `json:"mediaType"`; ArtifactDigest string `json:"artifactDigest"` }
	if err:=json.Unmarshal(data,&v); err!=nil { return fmt.Errorf("invalid attestation: %w",err) }
	if v.MediaType != mediaType || v.ArtifactDigest != artifact { return fmt.Errorf("attestation artifact digest mismatch") }; return nil
}

func Sign(artifact string, keyID string, privateKey ed25519.PrivateKey) (Signature, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return Signature{}, fmt.Errorf("invalid Ed25519 private key")
	}
	b := []byte(artifact)
	return Signature{MediaType: SignatureMediaType, ArtifactDigest: artifact, KeyID: keyID, Algorithm: "Ed25519", Signature: base64.RawStdEncoding.EncodeToString(ed25519.Sign(privateKey, b))}, nil
}
func SignAt(artifact, keyID string, privateKey ed25519.PrivateKey, notBefore, notAfter time.Time) (Signature,error) { s,err:=Sign(artifact,keyID,privateKey); if err==nil { s.NotBefore=FreshTimestamp(notBefore); s.NotAfter=FreshTimestamp(notAfter) }; return s,err }

func VerifySignature(s Signature, publicKey ed25519.PublicKey, artifact string) error {
	if s.MediaType != SignatureMediaType || s.Algorithm != "Ed25519" {
		return fmt.Errorf("unsupported signature media type or algorithm")
	}
	if s.ArtifactDigest != artifact {
		return fmt.Errorf("signature artifact digest mismatch")
	}
	raw, err := base64.RawStdEncoding.DecodeString(s.Signature)
	if err != nil || !ed25519.Verify(publicKey, []byte(artifact), raw) {
		return fmt.Errorf("invalid artifact signature")
	}
	return nil
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
	if p.Mode == StrictRemote { local = false; p.RequireSignature = true }
	if !containsOrEmpty(p.AcceptedPlatforms, platform) {
		return fmt.Errorf("trust policy disallows platform %q", platform)
	}
	if !containsOrEmpty(p.AcceptedBuilders, builder) {
		return fmt.Errorf("trust policy disallows builder %q", builder)
	}
	if local && p.AllowUnsignedLocal && !p.RequireSignature {
		return nil
	}
	for _, s := range signatures {
		if !containsOrEmpty(p.AcceptedKeys, s.KeyID) {
			continue
		}
		if r := findRevocation(revocations, artifact, s.KeyID); r != nil {
			return fmt.Errorf("artifact trust rejected: revoked (%s)", r.Reason)
		}
		if key := keys[s.KeyID]; key != nil {
			if err := VerifySignature(s, key, artifact); err == nil {
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
	if q.At.IsZero() { q.At=time.Now().UTC() }
	r := VerificationReceipt{ArtifactDigest:q.ArtifactDigest, PolicyMode:p.Mode, VerifiedAt:FreshTimestamp(time.Now().UTC())}
	if p.Mode == StrictRemote { p.RequireSignature=true }
	if q.Provenance != nil && q.Provenance.ArtifactDigest != q.ArtifactDigest { r.Code=CodeBinding; r.Diagnostic="provenance artifact digest mismatch"; return r }
	if q.SBOM != nil && (q.SBOM.MediaType != SBOMMediaType || q.SBOM.ArtifactDigest != q.ArtifactDigest) { r.Code=CodeBinding; r.Diagnostic="SBOM artifact digest mismatch"; return r }
	if q.Provenance != nil {
		if !containsOrEmpty(p.AcceptedRCCVersions,q.Provenance.RCCVersion) || !containsOrEmpty(p.AcceptedSources,q.Provenance.SourceRepository) { r.Code=CodePolicy; r.Diagnostic="provenance disallowed by policy"; return r }
		if p.MaxArtifactAge > 0 { if t,e:=time.Parse(time.RFC3339,q.Provenance.CreatedAt); e==nil && q.At.Sub(t)>p.MaxArtifactAge { r.Code=CodePolicy; r.Diagnostic="artifact exceeds maximum age"; return r } }
	}
	if err:=p.Evaluate(p.Mode==PermissiveLocal,q.ArtifactDigest,q.Platform,q.Builder,q.Signatures,q.Revocations,q.Keys); err==nil { r.Valid=true; r.Code=CodeValid; return r } else { r.Diagnostic=err.Error() }
	if len(q.Signatures)==0 { r.Code=CodeUnsigned } else { r.Code=CodeInvalid; for _,s:=range q.Signatures { if q.Keys[s.KeyID]==nil { r.Code=CodeUnknownSigner }; if rev:=findRevocation(q.Revocations,q.ArtifactDigest,s.KeyID); rev!=nil { r.Code=CodeRevoked }; if s.NotAfter!="" { if t,e:=time.Parse(time.RFC3339,s.NotAfter); e==nil && !q.At.Before(t) { r.Code=CodeExpired } } } }; return r
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
func Normalize(value string) string     { return strings.TrimSpace(value) }
