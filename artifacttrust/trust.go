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
}

type Policy struct {
	RequireSignature   bool     `json:"requireSignature"`
	AllowUnsignedLocal bool     `json:"allowUnsignedLocal"`
	AcceptedKeys       []string `json:"acceptedKeys,omitempty"`
	AcceptedBuilders   []string `json:"acceptedBuilders,omitempty"`
	AcceptedPlatforms  []string `json:"acceptedPlatforms,omitempty"`
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

func Sign(artifact string, keyID string, privateKey ed25519.PrivateKey) (Signature, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return Signature{}, fmt.Errorf("invalid Ed25519 private key")
	}
	b := []byte(artifact)
	return Signature{MediaType: SignatureMediaType, ArtifactDigest: artifact, KeyID: keyID, Algorithm: "Ed25519", Signature: base64.RawStdEncoding.EncodeToString(ed25519.Sign(privateKey, b))}, nil
}

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
