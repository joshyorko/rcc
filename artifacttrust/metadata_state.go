package artifacttrust

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
)

// MetadataState retains the authority's high-water mark outside the carrier.
// The root and its ancestors must be deployment-owned, not carrier-writable.
// Journal corruption/partial writes fail closed; no automatic reset is allowed.
type MetadataState struct{ Root string }
type metadataCheckpoint struct {
	Sequence uint64 `json:"sequence"`
	Digest   string `json:"digest"`
}

func (s MetadataState) Admit(payload MetadataPayload, envelope MetadataEnvelope) error {
	if s.Root == "" {
		return metadataError(CodeMetadataState)
	}
	if err := os.MkdirAll(s.Root, 0700); err != nil {
		return metadataError(CodeMetadataState)
	}
	if err := rejectSymlinkComponents(s.Root, s.Root); err != nil {
		return metadataError(CodeMetadataState)
	}
	name := Digest([]byte(payload.Scope + "\x00" + payload.ArtifactDigest))[7:]
	path := filepath.Join(s.Root, name+".jsonl")
	unlock, err := metadataStateLock(filepath.Join(s.Root, name+".lock"))
	if err != nil {
		return metadataError(CodeMetadataState)
	}
	defer unlock()
	if info, err := os.Lstat(path); err == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		return metadataError(CodeMetadataState)
	} else if err != nil && !os.IsNotExist(err) {
		return metadataError(CodeMetadataState)
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		return metadataError(CodeMetadataState)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (8<<20)+1))
	if err != nil || len(data) > 8<<20 {
		return metadataError(CodeMetadataState)
	}
	var last metadataCheckpoint
	if len(data) > 0 {
		if data[len(data)-1] != '\n' {
			return metadataError(CodeMetadataState)
		}
		for _, line := range bytes.Split(data[:len(data)-1], []byte{'\n'}) {
			var entry metadataCheckpoint
			if err := decodeCanonical(line, &entry); err != nil || entry.Sequence <= last.Sequence || !validArtifactDigest(entry.Digest) {
				return metadataError(CodeMetadataState)
			}
			last = entry
		}
	}
	digest, err := MetadataDigest(envelope)
	if err != nil {
		return metadataError(CodeMetadataState)
	}
	if payload.Sequence < last.Sequence || payload.Sequence == last.Sequence && digest != last.Digest {
		return metadataError(CodeMetadataRollback)
	}
	if payload.Sequence == last.Sequence {
		return nil
	}
	entry, err := json.Marshal(metadataCheckpoint{Sequence: payload.Sequence, Digest: digest})
	if err != nil {
		return metadataError(CodeMetadataState)
	}
	entry = append(entry, '\n')
	if n, err := f.Write(entry); err != nil || n != len(entry) {
		return metadataError(CodeMetadataState)
	}
	if err := f.Sync(); err != nil {
		return metadataError(CodeMetadataState)
	}
	if err := syncMetadataStateDirectory(s.Root); err != nil {
		return metadataError(CodeMetadataState)
	}
	return nil
}
