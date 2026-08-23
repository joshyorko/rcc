package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/joshyorko/rcc/artifactprovider"
	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/environmentartifact"
	"github.com/joshyorko/rcc/environmentlifecycle"
	"github.com/spf13/cobra"
)

var cliTestDigest = "sha256:" + strings.Repeat("a", 64)

type inertProvider struct{}

func (inertProvider) Capabilities(context.Context) (artifactprovider.Capabilities, error) {
	return artifactprovider.Capabilities{}, nil
}
func (inertProvider) ResolveManifest(context.Context, environmentartifact.Digest) ([]byte, error) {
	return nil, os.ErrNotExist
}
func (inertProvider) MissingObjects(context.Context, []environmentartifact.Descriptor) ([]environmentartifact.Digest, error) {
	return nil, nil
}
func (inertProvider) PutObject(context.Context, artifactprovider.Blob) error { return nil }
func (inertProvider) GetObject(context.Context, environmentartifact.Descriptor) (io.ReadCloser, error) {
	return nil, os.ErrNotExist
}
func (inertProvider) CommitManifest(context.Context, []byte) error { return nil }

func TestEnvironmentCommandContracts(t *testing.T) {
	command := newEnvironmentCommand(environmentCommandDependencies{})
	if command.Use != "env" {
		t.Fatalf("environment command use = %q", command.Use)
	}
	for _, name := range []string{"publish", "acquire", "exec"} {
		child, _, err := command.Find([]string{name})
		if err != nil || child == command || child.Name() != name {
			t.Fatalf("missing env %s command: %v", name, err)
		}
		if child.Flags().Lookup("artifact") == nil && name != "publish" {
			t.Fatalf("env %s lacks --artifact", name)
		}
		if child.Flags().Lookup("provider") == nil || child.Flags().Lookup("json") == nil {
			t.Fatalf("env %s lacks provider/json flags", name)
		}
	}
}

func TestEnvironmentRejectsNoncanonicalDigestBeforeLifecycleOrProvider(t *testing.T) {
	called := false
	dependencies := environmentCommandDependencies{
		newProvider: func(string) (artifactprovider.Provider, error) { called = true; return inertProvider{}, nil },
		acquire: func(context.Context, environmentlifecycle.AcquireRequest) (environmentlifecycle.AcquireResult, error) {
			called = true
			return environmentlifecycle.AcquireResult{}, nil
		},
	}
	command := newEnvironmentCommand(dependencies)
	arguments := []string{"acquire", "--artifact", "sha256:" + strings.Repeat("A", 64), "--provider", "http://127.0.0.1:1", "--json"}
	var stdout, stderr bytes.Buffer
	command.SetOut(&stdout)
	command.SetErr(&stderr)

	if err := runCobraCommand(command, arguments); err == nil {
		t.Fatal("noncanonical digest was accepted")
	}
	if called {
		t.Fatal("provider or lifecycle was called before digest validation")
	}
	if stdout.Len() != 0 {
		t.Fatalf("invalid request polluted JSON stdout: %q", stdout.String())
	}
	_ = stderr
}

func TestEnvironmentPublishEmitsStableJSON(t *testing.T) {
	digest, _ := environmentartifact.ParseDigest(cliTestDigest)
	specification := environmentartifact.DigestBytes([]byte("specification"))
	var providerURL, robotFile string
	dependencies := environmentCommandDependencies{
		newProvider: func(value string) (artifactprovider.Provider, error) {
			providerURL = value
			return inertProvider{}, nil
		},
		publish: func(_ context.Context, request environmentlifecycle.PublishRequest) (environmentlifecycle.PublishResult, error) {
			robotFile = request.RobotFile
			if request.Provider == nil {
				t.Fatal("publish received no provider")
			}
			return environmentlifecycle.PublishResult{
				ArtifactDigest: digest, SpecificationDigest: specification, LegacyBlueprintKey: "legacy-key",
				ObjectCount: 7, UploadedBytes: 101, ReusedBytes: 202,
			}, nil
		},
	}
	command := newEnvironmentCommand(dependencies)
	var stdout bytes.Buffer
	command.SetOut(&stdout)
	arguments := []string{"publish", "--robot", "project/robot.yaml", "--provider", "http://127.0.0.1:8080", "--json"}
	if err := runCobraCommand(command, arguments); err != nil {
		t.Fatal(err)
	}
	want := `{"artifactDigest":"` + cliTestDigest + `","specificationDigest":"` + specification.String() + `","legacyBlueprintKey":"legacy-key","objectCount":7,"uploadedBytes":101,"reusedBytes":202}` + "\n"
	if stdout.String() != want || providerURL != "http://127.0.0.1:8080" || robotFile != "project/robot.yaml" {
		t.Fatalf("publish output=%q provider=%q robot=%q", stdout.String(), providerURL, robotFile)
	}
}

func TestEnvironmentAcquireWithoutProviderExposesMaterializationNotLease(t *testing.T) {
	digest, _ := environmentartifact.ParseDigest(cliTestDigest)
	dependencies := environmentCommandDependencies{
		acquire: func(_ context.Context, request environmentlifecycle.AcquireRequest) (environmentlifecycle.AcquireResult, error) {
			if request.Provider != nil {
				t.Fatal("provider was constructed for providerless warm acquire")
			}
			return environmentlifecycle.AcquireResult{
				ArtifactDigest: digest, MaterializationID: "materialized", Path: "/worker/holotree/materialized",
				CacheHit: environmentlifecycle.CacheLocalMaterialization,
			}, nil
		},
	}
	command := newEnvironmentCommand(dependencies)
	var stdout bytes.Buffer
	command.SetOut(&stdout)
	arguments := []string{"acquire", "--artifact", cliTestDigest, "--json"}
	if err := runCobraCommand(command, arguments); err != nil {
		t.Fatal(err)
	}
	want := `{"artifactDigest":"` + cliTestDigest + `","materializationId":"materialized","path":"/worker/holotree/materialized","cacheHit":"local-materialization"}` + "\n"
	if stdout.String() != want || strings.Contains(strings.ToLower(stdout.String()), "lease") {
		t.Fatalf("acquire output = %q", stdout.String())
	}
}

func TestEnvironmentAcquirePassesDeferredProviderToLifecycle(t *testing.T) {
	digest, _ := environmentartifact.ParseDigest(cliTestDigest)
	resolved := false
	dependencies := environmentCommandDependencies{
		newProvider: func(reference string) (artifactprovider.Provider, error) {
			if reference != "malformed-or-missing" {
				t.Fatalf("provider reference = %q", reference)
			}
			return artifactprovider.NewDeferred(func() (artifactprovider.Provider, error) {
				resolved = true
				return nil, os.ErrNotExist
			}), nil
		},
		acquire: func(_ context.Context, request environmentlifecycle.AcquireRequest) (environmentlifecycle.AcquireResult, error) {
			if request.Provider == nil {
				t.Fatal("lifecycle received no provider")
			}
			return environmentlifecycle.AcquireResult{ArtifactDigest: digest, CacheHit: environmentlifecycle.CacheLocalMaterialization}, nil
		},
	}
	command := newEnvironmentCommand(dependencies)
	var stdout bytes.Buffer
	command.SetOut(&stdout)
	if err := runCobraCommand(command, []string{"acquire", "--artifact", cliTestDigest, "--provider", "malformed-or-missing", "--json"}); err != nil {
		t.Fatal(err)
	}
	if resolved {
		t.Fatal("command resolved provider before lifecycle")
	}
}

func TestEnvironmentExecPreservesArgumentsAndPropagatesExitAfterRelease(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test child uses the Linux shell; Windows remains compile-only for artifact v1")
	}
	digest, _ := environmentartifact.ParseDigest(cliTestDigest)
	materializer := &exitMaterializer{cwd: t.TempDir()}
	dependencies := environmentCommandDependencies{
		acquire: func(context.Context, environmentlifecycle.AcquireRequest) (environmentlifecycle.AcquireResult, error) {
			return environmentlifecycle.AcquireResult{
				ArtifactDigest: digest, MaterializationID: "materialized", Path: materializer.cwd,
				CacheHit: environmentlifecycle.CacheLocalMaterialization,
			}, nil
		},
		execute:      environmentlifecycle.Execute,
		materializer: func() environmentlifecycle.Materializer { return materializer },
	}
	command := newEnvironmentCommand(dependencies)
	var stdout bytes.Buffer
	command.SetOut(&stdout)
	arguments := []string{"exec", "--artifact", cliTestDigest, "--json", "--", "sh", "-c", "test \"$1\" = 'two words'; exit 7", "proof", "two words"}

	var exit common.ExitCode
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				var ok bool
				exit, ok = recovered.(common.ExitCode)
				if !ok {
					panic(recovered)
				}
			}
		}()
		if err := runCobraCommand(command, arguments); err != nil {
			t.Fatal(err)
		}
	}()
	if exit.Code != 7 || materializer.releases != 1 {
		t.Fatalf("child exit=%d releases=%d", exit.Code, materializer.releases)
	}
	var result struct {
		ArtifactDigest    string `json:"artifactDigest"`
		MaterializationID string `json:"materializationId"`
		Path              string `json:"path"`
		CacheHit          string `json:"cacheHit"`
		ExitCode          int    `json:"exitCode"`
		LeaseID           string `json:"leaseId"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.ArtifactDigest != cliTestDigest || result.MaterializationID != "materialized" || filepath.ToSlash(result.Path) != filepath.ToSlash(materializer.cwd) || result.CacheHit != "local-materialization" || result.ExitCode != 7 || result.LeaseID == "" {
		t.Fatalf("exec output = %q", stdout.String())
	}
}

func runCobraCommand(command *cobra.Command, arguments []string) error {
	target, remaining, err := command.Find(arguments)
	if err != nil {
		return err
	}
	if err := target.ParseFlags(remaining); err != nil {
		return err
	}
	target.SetContext(context.Background())
	parsed := target.Flags().Args()
	if target.Args != nil {
		if err := target.Args(target, parsed); err != nil {
			return err
		}
	}
	if target.RunE != nil {
		return target.RunE(target, parsed)
	}
	if target.Run != nil {
		target.Run(target, parsed)
	}
	return nil
}

type exitMaterializer struct {
	cwd      string
	releases int
}

func (*exitMaterializer) Materialize(context.Context, environmentartifact.Manifest) (environmentlifecycle.Materialization, error) {
	return environmentlifecycle.Materialization{}, nil
}
func (*exitMaterializer) Lease(context.Context, environmentlifecycle.Materialization) (environmentlifecycle.Lease, error) {
	return environmentlifecycle.Lease{ID: "lease"}, nil
}
func (it *exitMaterializer) ExecutionHandle(context.Context, environmentlifecycle.Lease, []string) (environmentlifecycle.ExecutionHandle, error) {
	return environmentlifecycle.ExecutionHandle{Executable: "/bin/sh", CWD: it.cwd, Environment: os.Environ(), LeaseID: "lease"}, nil
}
func (it *exitMaterializer) Release(context.Context, environmentlifecycle.Lease) error {
	it.releases++
	return nil
}
