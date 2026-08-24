package artifactprovider

import (
	"bytes"
	"context"
	"io"
	"net/http/httptest"
	"testing"
)

// TestHTTPProviderConformance runs the same contract against both maintained
// storage implementations. The HTTP transport is deliberately part of the
// harness so filesystem and journal providers cannot drift at the protocol
// boundary.
func TestHTTPProviderConformance(t *testing.T) {
	for _, tc := range []struct {
		name string
		new  func(string) (Provider, error)
	}{
		{name: "filesystem", new: func(root string) (Provider, error) { return NewFilesystem(root) }},
		{name: "journal", new: func(root string) (Provider, error) { return NewJournal(root + ".log") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider, err := tc.new(t.TempDir() + "/provider")
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(NewHandler(provider))
			defer server.Close()
			client, err := NewHTTP(server.URL, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.Capabilities(context.Background()); err != nil {
				t.Fatal(err)
			}
			protocol, err := client.Protocol(context.Background())
			if err != nil || protocol.TransferOutcome != "full-restart-only" {
				t.Fatalf("protocol=%+v err=%v", protocol, err)
			}
			fixture := newProviderFixture(t)
			for _, blob := range fixture.blobs {
				content, err := io.ReadAll(blob.Reader)
				if err != nil {
					t.Fatal(err)
				}
				if err := client.PutObject(context.Background(), Blob{Descriptor: blob.Descriptor, Reader: bytes.NewReader(content)}); err != nil {
					t.Fatal(err)
				}
			}
			if err := client.CommitManifest(context.Background(), fixture.manifestBytes); err != nil {
				t.Fatal(err)
			}
			resolved, err := client.ResolveManifest(context.Background(), fixture.manifest.ArtifactDigest)
			if err != nil || !bytes.Equal(resolved, fixture.manifestBytes) {
				t.Fatalf("resolved manifest err=%v equal=%v", err, bytes.Equal(resolved, fixture.manifestBytes))
			}
		})
	}
}
