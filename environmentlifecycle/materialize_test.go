package environmentlifecycle

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/joshyorko/rcc/artifactprovider"
	"github.com/joshyorko/rcc/artifacttrust"
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

func TestAcquireLoadsSignatureFromFilesystemCarrierWithRequestTrustRoots(t *testing.T) {
	_, remote, artifactDigest := publishedFixture(t)
	common.Product.ForceHome(t.TempDir())
	common.SharedHolotree = false
	public, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := artifacttrust.Sign(artifactDigest.String(), "build-key", private)
	if err != nil {
		t.Fatal(err)
	}
	_, bundleBytes, err := artifacttrust.NewSignatureBundle(artifactDigest.String(), []artifacttrust.Signature{signature})
	if err != nil {
		t.Fatal(err)
	}
	carrier := artifacttrust.NewFilesystemCarrier(t.TempDir())
	if err := artifacttrust.PutAttachment(carrier, artifactDigest.String(), "signature", bundleBytes); err != nil {
		t.Fatal(err)
	}
	result, err := NewAcquirer().Acquire(context.Background(), AcquireRequest{
		ArtifactDigest: artifactDigest, Provider: remote,
		TrustPolicy:  &artifacttrust.Policy{Mode: artifacttrust.StrictRemote, AcceptedKeys: []string{"build-key"}},
		TrustRequest: &artifacttrust.VerifyRequest{Keys: map[string]ed25519.PublicKey{"build-key": public}, At: time.Unix(10, 0)},
		TrustCarrier: carrier,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Verification.Valid || result.Verification.KeyID != "build-key" {
		t.Fatalf("verification=%+v", result.Verification)
	}
}

func TestAcquirePersistsFailureReceiptForMalformedCarrierAttachment(t *testing.T) {
	_, remote, artifactDigest := publishedFixture(t)
	home := t.TempDir()
	previousHome := common.Product.Home()
	previousShared := common.SharedHolotree
	common.Product.ForceHome(home)
	common.SharedHolotree = false
	t.Cleanup(func() {
		common.Product.ForceHome(previousHome)
		common.SharedHolotree = previousShared
	})
	carrier := artifacttrust.NewFilesystemCarrier(t.TempDir())
	malformed := []byte(`{"mediaType":"application/vnd.rcc.environment.provenance.v1+json","artifactDigest":"` + artifactDigest.String() + `","unexpected":"credential=carrier-secret"}`)
	if err := carrier.Write(artifacttrust.AttachmentName(artifactDigest.String(), "provenance"), malformed); err != nil {
		t.Fatal(err)
	}
	_, err := NewAcquirer().Acquire(context.Background(), AcquireRequest{
		ArtifactDigest: artifactDigest, Provider: remote,
		TrustPolicy: &artifacttrust.Policy{Mode: artifacttrust.StrictRemote, FailClosedRevocations: true}, TrustCarrier: carrier,
	})
	if err == nil {
		t.Fatal("malformed carrier attachment was accepted")
	}
	history, err := artifacttrust.NewReceiptStore(filepath.Join(home, "artifacts", "v1", "verification")).History(artifactDigest.String())
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].Valid || history[0].Code != artifacttrust.CodeInvalid {
		t.Fatalf("failure history=%+v", history)
	}
	data, err := history[0].JSON()
	if err != nil || bytes.Contains(data, []byte("carrier-secret")) {
		t.Fatalf("failure receipt leaked carrier data: %q err=%v", data, err)
	}
}

func TestAcquireProviderAuthorizationDoesNotLeakToTrustCarrierClient(t *testing.T) {
	_, filesystem, artifactDigest := publishedFixture(t)
	home := t.TempDir()
	previousHome := common.Product.Home()
	previousShared := common.SharedHolotree
	common.Product.ForceHome(home)
	common.SharedHolotree = false
	t.Cleanup(func() {
		common.Product.ForceHome(previousHome)
		common.SharedHolotree = previousShared
	})
	public, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := artifacttrust.Sign(artifactDigest.String(), "build-key", private)
	if err != nil {
		t.Fatal(err)
	}
	_, signatureBytes, err := artifacttrust.NewSignatureBundle(artifactDigest.String(), []artifacttrust.Signature{signature})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	_, revocationBytes, err := artifacttrust.NewRevocationBundleAt(artifactDigest.String(), nil, now, "auth-isolated")
	if err != nil {
		t.Fatal(err)
	}
	const authorizationEnv = "RCC_TRUST_PROVIDER_AUTH_TEST"
	const authorizationValue = "Bearer provider-secret-sentinel"
	t.Setenv(authorizationEnv, authorizationValue)
	var leaked string
	providerHandler := artifactprovider.NewHandler(filesystem)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.HasPrefix(request.URL.Path, "/v1/") {
			if request.Header.Get("Authorization") != authorizationValue {
				http.Error(writer, "provider authorization missing", http.StatusUnauthorized)
				return
			}
			providerHandler.ServeHTTP(writer, request)
			return
		}
		if request.Header.Get("Authorization") != "" {
			leaked = request.Header.Get("Authorization")
		}
		switch request.URL.Path {
		case "/" + artifacttrust.AttachmentName(artifactDigest.String(), "signature"):
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write(signatureBytes)
		case "/" + artifacttrust.AttachmentName(artifactDigest.String(), "revocations"):
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write(revocationBytes)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	provider, err := artifactprovider.NewHTTPWithOptions(server.URL, artifactprovider.HTTPOptions{Client: server.Client(), AuthorizationEnv: authorizationEnv})
	if err != nil {
		t.Fatal(err)
	}
	result, err := NewAcquirer().Acquire(context.Background(), AcquireRequest{
		ArtifactDigest: artifactDigest, Provider: provider,
		TrustPolicy:  &artifacttrust.Policy{Mode: artifacttrust.StrictRemote, FailClosedRevocations: true, AcceptedKeys: []string{"build-key"}},
		TrustRequest: &artifacttrust.VerifyRequest{Keys: map[string]ed25519.PublicKey{"build-key": public}},
		TrustCarrier: &artifacttrust.HTTPCarrier{BaseURL: server.URL, Client: server.Client()},
	})
	if err != nil || !result.Verification.Valid {
		t.Fatalf("acquire result=%+v err=%v", result, err)
	}
	if leaked != "" || strings.Contains(leaked, authorizationValue) {
		t.Fatalf("provider authorization leaked to trust carrier: %q", leaked)
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
	if runtime.GOOS == "windows" {
		t.Skip("Windows executable eligibility is not represented by POSIX mode bits")
	}
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

func TestWarmAcquireRejectsMaterializedPythonABIMismatch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a POSIX Python wrapper")
	}
	hostPython, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("host Python is unavailable")
	}
	_, remote, artifactDigest := publishedFixture(t)
	common.Product.ForceHome(t.TempDir())
	common.SharedHolotree = false
	acquirer := NewAcquirer()
	first, err := acquirer.Acquire(context.Background(), AcquireRequest{ArtifactDigest: artifactDigest, Provider: remote})
	if err != nil {
		t.Fatal(err)
	}
	python := filepath.Join(first.Path, "python")
	if err := os.WriteFile(python, []byte("#!/bin/sh\nexec \""+hostPython+"\" \"$@\"\n"), 0o750); err != nil {
		t.Fatal(err)
	}
	verifyMaterializedCompatibility = validateMaterializedCompatibility

	_, err = acquirer.Acquire(context.Background(), AcquireRequest{
		ArtifactDigest: artifactDigest, Provider: failOnTouchProvider{t: t},
	})
	var mismatch *environmentartifact.CompatibilityError
	if !errors.As(err, &mismatch) || mismatch.Code != "materialized-python-abi" {
		t.Fatalf("warm ABI mismatch = %T %v", err, err)
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
