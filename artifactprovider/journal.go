package artifactprovider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/joshyorko/rcc/environmentartifact"
)

// Journal is a durable append-only provider. It deliberately uses a journal
// rather than the CAS directory layout, making it useful for contract tests
// and deployments where a single durable file is preferable.
type Journal struct { path string; mu sync.RWMutex; objects map[string][]byte; manifests map[string][]byte }
type journalRecord struct { Kind, Digest, MediaType string; Content string }

func NewJournal(path string) (*Journal, error) {
	j := &Journal{path: path, objects: map[string][]byte{}, manifests: map[string][]byte{}}
	file, err := os.Open(path)
	if os.IsNotExist(err) { return j, nil }; if err != nil { return nil, err }; defer file.Close()
	scanner := bufio.NewScanner(file); scanner.Buffer(make([]byte, 4096), int(maxProviderObjectBytes+maxManifestBytes))
	for scanner.Scan() { var record journalRecord; if err := json.Unmarshal(scanner.Bytes(), &record); err != nil { return nil, fmt.Errorf("decode provider journal: %w", err) }; content, err := base64.StdEncoding.DecodeString(record.Content); if err != nil { return nil, err }; if record.Kind == "object" { j.objects[record.Digest] = content } else if record.Kind == "manifest" { j.manifests[record.Digest] = content } }
	if err := scanner.Err(); err != nil { return nil, err }; return j, nil
}

func (j *Journal) Capabilities(context.Context) (Capabilities, error) { return Capabilities{SchemaVersions: []int{1}, DigestAlgorithms: []string{"sha256"}, Encodings: []string{"gzip"}}, nil }
func (j *Journal) Health(ctx context.Context) (Health, error) { if err := ctx.Err(); err != nil { return Health{}, err }; j.mu.RLock(); defer j.mu.RUnlock(); var bytes int64; for _, b := range j.objects { bytes += int64(len(b)) }; return Health{Ready: true, Storage: "ok", Capability: "ok", Auth: "not-applicable", Quota: "ok", GC: "idle", Objects: int64(len(j.objects)), Manifests: int64(len(j.manifests)), Bytes: bytes}, nil }
func (j *Journal) MissingObjects(ctx context.Context, ds []environmentartifact.Descriptor) ([]environmentartifact.Digest, error) { j.mu.RLock(); defer j.mu.RUnlock(); missing := []environmentartifact.Digest{}; for _, d := range ds { if err := ctx.Err(); err != nil { return nil, err }; b, ok := j.objects[d.Digest.Hex()]; if !ok || int64(len(b)) != d.Size { missing = append(missing, d.Digest) } }; return missing, nil }
func (j *Journal) PutObject(ctx context.Context, blob Blob) error { if blob.Reader == nil || blob.Descriptor.Size < 0 || blob.Descriptor.Size > maxProviderObjectBytes { return fmt.Errorf("invalid object upload") }; b, err := io.ReadAll(io.LimitReader(&contextReader{ctx: ctx, reader: blob.Reader}, blob.Descriptor.Size+1)); if err != nil { return err }; if int64(len(b)) != blob.Descriptor.Size || environmentartifact.DigestBytes(b) != blob.Descriptor.Digest { return fmt.Errorf("object size or digest mismatch") }; j.mu.Lock(); defer j.mu.Unlock(); if prior, ok := j.objects[blob.Descriptor.Digest.Hex()]; ok { if !bytes.Equal(prior, b) { return fmt.Errorf("conflicting immutable object") }; return nil }; if err := j.append(journalRecord{Kind: "object", Digest: blob.Descriptor.Digest.Hex(), MediaType: blob.Descriptor.MediaType, Content: base64.StdEncoding.EncodeToString(b)}); err != nil { return err }; j.objects[blob.Descriptor.Digest.Hex()] = append([]byte(nil), b...); return nil }
func (j *Journal) GetObject(ctx context.Context, d environmentartifact.Descriptor) (io.ReadCloser, error) { if err := ctx.Err(); err != nil { return nil, err }; j.mu.RLock(); b, ok := j.objects[d.Digest.Hex()]; j.mu.RUnlock(); if !ok || int64(len(b)) != d.Size || environmentartifact.DigestBytes(b) != d.Digest { return nil, os.ErrNotExist }; return io.NopCloser(bytes.NewReader(append([]byte(nil), b...))), nil }
func (j *Journal) GetObjectByDigest(ctx context.Context, d environmentartifact.Digest) (io.ReadCloser, int64, error) { j.mu.RLock(); b,ok:=j.objects[d.Hex()];j.mu.RUnlock();if !ok{return nil,0,os.ErrNotExist};if err:=ctx.Err();err!=nil{return nil,0,err};return io.NopCloser(bytes.NewReader(append([]byte(nil),b...))),int64(len(b)),nil }
func (j *Journal) CommitManifest(ctx context.Context, content []byte) error { if err := ctx.Err(); err != nil { return err }; manifest, err := environmentartifact.DecodeManifest(content); if err != nil { return err }; j.mu.Lock(); defer j.mu.Unlock(); key := manifest.ArtifactDigest.Hex(); if prior, ok := j.manifests[key]; ok { if !bytes.Equal(prior, content) { return fmt.Errorf("conflicting immutable manifest") }; return nil }; if err := j.append(journalRecord{Kind: "manifest", Digest: key, Content: base64.StdEncoding.EncodeToString(content)}); err != nil { return err }; j.manifests[key] = append([]byte(nil), content...); return nil }
func (j *Journal) ResolveManifest(ctx context.Context, d environmentartifact.Digest) ([]byte, error) { if err := ctx.Err(); err != nil { return nil, err }; j.mu.RLock(); b, ok := j.manifests[d.Hex()]; j.mu.RUnlock(); if !ok { return nil, os.ErrNotExist }; return append([]byte(nil), b...), nil }
func (j *Journal) append(record journalRecord) error { file, err := os.OpenFile(j.path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600); if err != nil { return err }; defer file.Close(); content, err := json.Marshal(record); if err != nil { return err }; if _, err = file.Write(append(content, '\n')); err != nil { return err }; return file.Sync() }
var _ Provider = (*Journal)(nil)
