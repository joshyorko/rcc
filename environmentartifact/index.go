package environmentartifact

import (
	"fmt"
	"regexp"
	"sort"
)

var legacyObjectIDPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type ObjectEntry struct {
	LegacyObjectID               string `json:"legacyObjectId"`
	StoredDigest                 Digest `json:"storedDigest"`
	StoredSize                   int64  `json:"storedSize"`
	LogicalSize                  int64  `json:"logicalSize"`
	Encoding                     string `json:"encoding"`
	LegacyLogicalDigestAlgorithm string `json:"legacyLogicalDigestAlgorithm"`
}

type ObjectIndex struct {
	MediaType                    string        `json:"mediaType"`
	SchemaVersion                int           `json:"schemaVersion"`
	Count                        int           `json:"count"`
	TotalStoredBytes             int64         `json:"totalStoredBytes"`
	TotalLogicalBytes            int64         `json:"totalLogicalBytes"`
	Encoding                     string        `json:"encoding"`
	LegacyLogicalDigestAlgorithm string        `json:"legacyLogicalDigestAlgorithm"`
	Entries                      []ObjectEntry `json:"entries"`
}

func NewObjectIndex(entries []ObjectEntry) (ObjectIndex, []byte, error) {
	ordered := append([]ObjectEntry(nil), entries...)
	sort.Slice(ordered, func(left, right int) bool {
		return ordered[left].LegacyObjectID < ordered[right].LegacyObjectID
	})
	index := ObjectIndex{
		MediaType: ObjectIndexMediaType, SchemaVersion: SchemaVersionV1,
		Count: len(ordered), Encoding: "gzip", LegacyLogicalDigestAlgorithm: "sha256",
		Entries: ordered,
	}
	for _, entry := range ordered {
		index.TotalStoredBytes += entry.StoredSize
		index.TotalLogicalBytes += entry.LogicalSize
	}
	if err := index.Validate(); err != nil {
		return ObjectIndex{}, nil, err
	}
	content, err := canonicalEncode(index)
	return index, content, err
}

func DecodeObjectIndex(content []byte) (ObjectIndex, error) {
	var index ObjectIndex
	if err := strictDecodeCanonical(content, &index); err != nil {
		return ObjectIndex{}, err
	}
	if err := index.Validate(); err != nil {
		return ObjectIndex{}, err
	}
	return index, nil
}

func (it ObjectIndex) Validate() error {
	if it.MediaType != ObjectIndexMediaType || it.SchemaVersion != SchemaVersionV1 {
		return fmt.Errorf("unsupported object index media type or schema version")
	}
	if it.Encoding != "gzip" || it.LegacyLogicalDigestAlgorithm != "sha256" {
		return fmt.Errorf("unsupported object index encoding or logical digest")
	}
	if it.Count != len(it.Entries) {
		return fmt.Errorf("object index count mismatch")
	}
	var storedTotal, logicalTotal int64
	previous := ""
	for _, entry := range it.Entries {
		if !legacyObjectIDPattern.MatchString(entry.LegacyObjectID) {
			return fmt.Errorf("invalid legacy object ID %q", entry.LegacyObjectID)
		}
		if previous != "" && entry.LegacyObjectID <= previous {
			return fmt.Errorf("object entries are not strictly sorted and unique")
		}
		if entry.StoredDigest.hex == "" || entry.StoredSize < 0 || entry.LogicalSize < 0 {
			return fmt.Errorf("invalid object descriptor for %s", entry.LegacyObjectID)
		}
		if entry.Encoding != it.Encoding || entry.LegacyLogicalDigestAlgorithm != it.LegacyLogicalDigestAlgorithm {
			return fmt.Errorf("mixed object storage mode")
		}
		previous = entry.LegacyObjectID
		storedTotal += entry.StoredSize
		logicalTotal += entry.LogicalSize
	}
	if storedTotal != it.TotalStoredBytes || logicalTotal != it.TotalLogicalBytes {
		return fmt.Errorf("object index totals mismatch")
	}
	return nil
}
