package environmentlifecycle

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/joshyorko/rcc/artifactprovider"
	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/environmentartifact"
	"github.com/joshyorko/rcc/htfs"
)

type failOnTouchProvider struct{ t *testing.T }

func (it failOnTouchProvider) fail() {
	it.t.Helper()
	it.t.Fatal("warm acquisition touched the provider")
}

func (it failOnTouchProvider) Capabilities(context.Context) (artifactprovider.Capabilities, error) {
	it.fail()
	return artifactprovider.Capabilities{}, nil
}
func (it failOnTouchProvider) ResolveManifest(context.Context, environmentartifact.Digest) ([]byte, error) {
	it.fail()
	return nil, nil
}
func (it failOnTouchProvider) MissingObjects(context.Context, []environmentartifact.Descriptor) ([]environmentartifact.Digest, error) {
	it.fail()
	return nil, nil
}
func (it failOnTouchProvider) PutObject(context.Context, artifactprovider.Blob) error {
	it.fail()
	return nil
}
func (it failOnTouchProvider) GetObject(context.Context, environmentartifact.Descriptor) (io.ReadCloser, error) {
	it.fail()
	return nil, nil
}
func (it failOnTouchProvider) CommitManifest(context.Context, []byte) error {
	it.fail()
	return nil
}

func TestMaterializationUsesPortableV12CatalogWithoutChangingArtifactBytes(t *testing.T) {
	fixture, remote, artifactDigest := publishedFixture(t)
	producer, err := htfs.LoadPortableCatalog(fixture.build.CatalogPath)
	if err != nil {
		t.Fatal(err)
	}
	producerPath := producer.Root().Path
	consumerHome := t.TempDir()
	common.Product.ForceHome(consumerHome)
	common.SharedHolotree = false
	provider := &recordingProvider{delegate: remote}

	result, err := NewAcquirer().Acquire(context.Background(), AcquireRequest{
		ArtifactDigest: artifactDigest, Provider: provider,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ArtifactDigest != artifactDigest || result.CacheHit != CacheProvider {
		t.Fatalf("acquire result = %+v", result)
	}
	if result.Path == producerPath || filepath.Dir(result.Path) != common.HolotreeLocation() {
		t.Fatalf("materialization was not rebased from %q to consumer home: %q", producerPath, result.Path)
	}
	assertFileBytes(t, filepath.Join(result.Path, "python"), []byte("immutable python bytes"))
	if info, err := os.Lstat(filepath.Join(result.Path, "python")); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("materialized Python is not a regular file: %v, %v", info, err)
	}
	assertFileBytes(t, filepath.Join(common.HololibCatalogLocation(), htfs.CatalogName(common.BlueprintHash(fixture.build.LegacyBlueprint))), fixture.catalogBytes)
	if len(provider.events) == 0 {
		t.Fatal("cold acquisition made no provider requests")
	}
}

func TestWarmAcquireDoesNotTouchProviderOrBuilder(t *testing.T) {
	_, remote, artifactDigest := publishedFixture(t)
	common.Product.ForceHome(t.TempDir())
	common.SharedHolotree = false
	acquirer := NewAcquirer()
	first, err := acquirer.Acquire(context.Background(), AcquireRequest{ArtifactDigest: artifactDigest, Provider: remote})
	if err != nil {
		t.Fatal(err)
	}

	second, err := acquirer.Acquire(context.Background(), AcquireRequest{
		ArtifactDigest: artifactDigest, Provider: failOnTouchProvider{t: t},
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.ArtifactDigest != first.ArtifactDigest || second.MaterializationID != first.MaterializationID || second.Path != first.Path {
		t.Fatalf("warm result changed identity: first %+v, second %+v", first, second)
	}
	if second.CacheHit != CacheLocalMaterialization {
		t.Fatalf("warm cache provenance = %q", second.CacheHit)
	}
	if got, err := os.ReadFile(filepath.Join(second.Path, "python")); err != nil || !bytes.Equal(got, []byte("immutable python bytes")) {
		t.Fatalf("warm materialization is invalid: %q, %v", got, err)
	}
}

func TestWarmAcquireDoesNotResolveDeferredProvider(t *testing.T) {
	_, remote, artifactDigest := publishedFixture(t)
	common.Product.ForceHome(t.TempDir())
	common.SharedHolotree = false
	acquirer := NewAcquirer()
	if _, err := acquirer.Acquire(context.Background(), AcquireRequest{ArtifactDigest: artifactDigest, Provider: remote}); err != nil {
		t.Fatal(err)
	}
	resolved := false
	deferred := artifactprovider.NewDeferred(func() (artifactprovider.Provider, error) {
		resolved = true
		return nil, errors.New("provider profile missing")
	})
	result, err := acquirer.Acquire(context.Background(), AcquireRequest{ArtifactDigest: artifactDigest, Provider: deferred})
	if err != nil {
		t.Fatal(err)
	}
	if resolved {
		t.Fatal("warm acquisition resolved deferred provider")
	}
	if result.CacheHit != CacheLocalMaterialization {
		t.Fatalf("warm cache provenance = %q", result.CacheHit)
	}
}

func TestWarmAcquireIgnoresProviderResolutionAndNetworkBoundaries(t *testing.T) {
	cases := []struct {
		name     string
		provider func(*testing.T, *int) artifactprovider.Provider
	}{
		{
			name: "missing profile resolver error",
			provider: func(t *testing.T, calls *int) artifactprovider.Provider {
				return artifactprovider.NewDeferred(func() (artifactprovider.Provider, error) {
					(*calls)++
					return nil, errors.New("provider profile missing")
				})
			},
		},
		{
			name: "absent authorization environment",
			provider: func(t *testing.T, calls *int) artifactprovider.Provider {
				const env = "RCC_TASK4_DEFINITELY_MISSING_AUTH"
				if err := os.Unsetenv(env); err != nil {
					t.Fatal(err)
				}
				return artifactprovider.NewDeferred(func() (artifactprovider.Provider, error) {
					(*calls)++
					return artifactprovider.NewHTTPWithOptions("http://127.0.0.1:1", artifactprovider.HTTPOptions{
						Client: http.DefaultClient, AuthorizationEnv: env,
					})
				})
			},
		},
		{
			name: "unreachable provider endpoint",
			provider: func(t *testing.T, calls *int) artifactprovider.Provider {
				return artifactprovider.NewDeferred(func() (artifactprovider.Provider, error) {
					(*calls)++
					return artifactprovider.NewHTTPWithOptions("http://127.0.0.1:1", artifactprovider.HTTPOptions{Client: http.DefaultClient})
				})
			},
		},
		{
			name: "resolver panic",
			provider: func(t *testing.T, calls *int) artifactprovider.Provider {
				return artifactprovider.NewDeferred(func() (artifactprovider.Provider, error) {
					panic("warm acquisition resolved provider")
				})
			},
		},
		{
			name:     "provider panic",
			provider: func(t *testing.T, calls *int) artifactprovider.Provider { return failOnTouchProvider{t: t} },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, remote, artifactDigest := publishedFixture(t)
			common.Product.ForceHome(t.TempDir())
			common.SharedHolotree = false
			acquirer := NewAcquirer()
			first, err := acquirer.Acquire(context.Background(), AcquireRequest{ArtifactDigest: artifactDigest, Provider: remote})
			if err != nil {
				t.Fatal(err)
			}
			resolverCalls := 0
			second, err := acquirer.Acquire(context.Background(), AcquireRequest{
				ArtifactDigest: artifactDigest, Provider: tc.provider(t, &resolverCalls),
			})
			if err != nil {
				t.Fatal(err)
			}
			if resolverCalls != 0 {
				t.Fatalf("resolver calls = %d", resolverCalls)
			}
			if second.CacheHit != CacheLocalMaterialization || second.ArtifactDigest != first.ArtifactDigest {
				t.Fatalf("warm result = %+v, first = %+v", second, first)
			}
		})
	}
}

func TestMaterializationFailureNeverPublishesReadyRecord(t *testing.T) {
	_, remote, artifactDigest := publishedFixture(t)
	common.Product.ForceHome(t.TempDir())
	common.SharedHolotree = false
	content, err := acquireVerifiedContent(context.Background(), artifactDigest, remote)
	if err != nil {
		t.Fatal(err)
	}
	legacyID := content.index.Entries[0].LegacyObjectID
	if err := os.WriteFile(htfs.ExactDefaultLocation(legacyID), []byte("corrupt after verification"), 0o640); err != nil {
		t.Fatal(err)
	}

	if _, err := NewLocalMaterializer().Materialize(context.Background(), content.manifest); err == nil {
		t.Fatal("materialization succeeded with corrupt local legacy content")
	}
	if _, err := readReadyRecord(artifactDigest); err == nil {
		t.Fatal("failed materialization published a ready record")
	}
}

func TestWarmAcquireRepairsNonExecutablePythonWithoutProvider(t *testing.T) {
	_, remote, artifactDigest := publishedFixture(t)
	common.Product.ForceHome(t.TempDir())
	common.SharedHolotree = false
	acquirer := NewAcquirer()
	first, err := acquirer.Acquire(context.Background(), AcquireRequest{ArtifactDigest: artifactDigest, Provider: remote})
	if err != nil {
		t.Fatal(err)
	}
	python := filepath.Join(first.Path, "python")
	if err := os.Chmod(python, 0o640); err != nil {
		t.Fatal(err)
	}

	second, err := acquirer.Acquire(context.Background(), AcquireRequest{
		ArtifactDigest: artifactDigest, Provider: failOnTouchProvider{t: t},
	})
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(filepath.Join(second.Path, "python"))
	if err != nil || info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("warm acquire retained non-executable Python: %v, %v", info, err)
	}
}

func TestWarmAcquireAndExecutionRejectSymlinkedPythonParent(t *testing.T) {
	_, remote, artifactDigest := publishedFixture(t)
	common.Product.ForceHome(t.TempDir())
	common.SharedHolotree = false
	acquirer := NewAcquirer()
	first, err := acquirer.Acquire(context.Background(), AcquireRequest{ArtifactDigest: artifactDigest, Provider: remote})
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "python"), []byte("#!/bin/sh\nexit 0\n"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(first.Path, "bin")); err != nil {
		t.Fatal(err)
	}
	if candidate, err := materializedPython(first.Path); err == nil {
		t.Fatalf("component-wise executable validation trusted %q", candidate)
	}
	if _, err := acquirer.Acquire(context.Background(), AcquireRequest{
		ArtifactDigest: artifactDigest, Provider: failOnTouchProvider{t: t},
	}); err == nil {
		t.Fatal("warm acquire trusted Python through a symlinked parent")
	}
	materializer := NewLocalMaterializer()
	materialization := Materialization{
		ArtifactDigest: first.ArtifactDigest, ID: first.MaterializationID, Path: first.Path, CacheHit: first.CacheHit,
	}
	lease, err := materializer.Lease(context.Background(), materialization)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := materializer.Release(context.Background(), lease); err != nil {
			t.Errorf("release materialization: %v", err)
		}
	}()
	if _, err := materializer.ExecutionHandle(context.Background(), lease, []string{"python", "-V"}); err == nil {
		t.Fatal("execution handle trusted Python through a symlinked parent")
	}
}
