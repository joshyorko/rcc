package environmentartifact

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	digestAlgorithm = "sha256"
	digestHexLength = sha256.Size * 2
)

type Digest struct {
	hex string
}

func ParseDigest(value string) (Digest, error) {
	prefix := digestAlgorithm + ":"
	if !strings.HasPrefix(value, prefix) {
		return Digest{}, fmt.Errorf("unsupported or malformed digest %q", value)
	}

	hexdigest := strings.TrimPrefix(value, prefix)
	if len(hexdigest) != digestHexLength {
		return Digest{}, fmt.Errorf("sha256 digest has %d hex characters, expected %d", len(hexdigest), digestHexLength)
	}
	if hexdigest != strings.ToLower(hexdigest) {
		return Digest{}, fmt.Errorf("sha256 digest is not lowercase canonical hex")
	}
	decoded, err := hex.DecodeString(hexdigest)
	if err != nil || len(decoded) != sha256.Size {
		return Digest{}, fmt.Errorf("invalid sha256 digest %q", value)
	}
	return Digest{hex: hexdigest}, nil
}

func DigestBytes(content []byte) Digest {
	sum := sha256.Sum256(content)
	return Digest{hex: hex.EncodeToString(sum[:])}
}

func (it Digest) String() string {
	if it.hex == "" {
		return ""
	}
	return digestAlgorithm + ":" + it.hex
}

func (it Digest) Hex() string {
	return it.hex
}

func (it Digest) MarshalJSON() ([]byte, error) {
	if it.hex == "" {
		return nil, fmt.Errorf("cannot encode an empty digest")
	}
	return json.Marshal(it.String())
}

func (it *Digest) UnmarshalJSON(content []byte) error {
	var value string
	if err := json.Unmarshal(content, &value); err != nil {
		return fmt.Errorf("digest must be a canonical string: %w", err)
	}
	digest, err := ParseDigest(value)
	if err != nil {
		return err
	}
	*it = digest
	return nil
}

type Descriptor struct {
	MediaType string `json:"mediaType"`
	Digest    Digest `json:"digest"`
	Size      int64  `json:"size"`
}

func VerifyDescriptor(descriptor Descriptor, content []byte) error {
	if descriptor.Size < 0 || descriptor.Size != int64(len(content)) {
		return fmt.Errorf("descriptor size mismatch: declared %d, actual %d", descriptor.Size, len(content))
	}
	actual := DigestBytes(content)
	if descriptor.Digest != actual {
		return fmt.Errorf("descriptor digest mismatch: declared %s, actual %s", descriptor.Digest, actual)
	}
	return nil
}
