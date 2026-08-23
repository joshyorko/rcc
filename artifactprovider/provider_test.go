package artifactprovider

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/joshyorko/rcc/environmentartifact"
)

type boundedMissingProvider struct {
	batchSizes []int
}

func (p *boundedMissingProvider) Capabilities(context.Context) (Capabilities, error) {
	return Capabilities{}, nil
}

func (p *boundedMissingProvider) ResolveManifest(context.Context, environmentartifact.Digest) ([]byte, error) {
	return nil, nil
}

func (p *boundedMissingProvider) MissingObjects(_ context.Context, descriptors []environmentartifact.Descriptor) ([]environmentartifact.Digest, error) {
	if len(descriptors) > MaxDescriptorFanout {
		return nil, fmt.Errorf("descriptor fanout exceeds limit")
	}
	p.batchSizes = append(p.batchSizes, len(descriptors))
	missing := make([]environmentartifact.Digest, len(descriptors))
	for index, descriptor := range descriptors {
		missing[index] = descriptor.Digest
	}
	return missing, nil
}

func (p *boundedMissingProvider) PutObject(context.Context, Blob) error { return nil }

func (p *boundedMissingProvider) GetObject(context.Context, environmentartifact.Descriptor) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(nil)), nil
}

func (p *boundedMissingProvider) CommitManifest(context.Context, []byte) error { return nil }

func TestMissingObjectsBatchedPreservesLargeClosure(t *testing.T) {
	descriptors := make([]environmentartifact.Descriptor, MaxDescriptorFanout+3)
	for index := range descriptors {
		descriptors[index] = environmentartifact.Descriptor{
			Digest: environmentartifact.DigestBytes([]byte(fmt.Sprintf("object-%d", index))),
			Size:   1,
		}
	}
	provider := &boundedMissingProvider{}
	missing, err := MissingObjectsBatched(context.Background(), provider, descriptors)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != len(descriptors) {
		t.Fatalf("missing descriptors = %d, want %d", len(missing), len(descriptors))
	}
	if len(provider.batchSizes) != 2 || provider.batchSizes[0] != MaxDescriptorFanout || provider.batchSizes[1] != 3 {
		t.Fatalf("missing-object batch sizes = %v", provider.batchSizes)
	}
	for index := range descriptors {
		if missing[index] != descriptors[index].Digest {
			t.Fatalf("missing descriptor %d changed order", index)
		}
	}
}
