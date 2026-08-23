package cmd

import (
	"bytes"
	"context"
	"testing"

	"github.com/joshyorko/rcc/artifactprovider"
	"github.com/joshyorko/rcc/environmentartifact"
	"github.com/joshyorko/rcc/environmentlifecycle"
)

func TestEnvironmentLifecycleRepairRoutesProviderAndWritesJSON(t *testing.T) {
	digest := environmentartifact.DigestBytes([]byte("provider-repair"))
	provider, err := artifactprovider.NewFilesystem(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var got artifactprovider.Provider
	command := newEnvironmentLifecycleCommandWithDependencies(environmentLifecycleCommandDependencies{
		newProvider: func(string) (artifactprovider.Provider, error) { return provider, nil },
		repair: func(_ context.Context, gotDigest environmentartifact.Digest, p artifactprovider.Provider) (environmentlifecycle.RepairReport, error) {
			got = p
			reconcile := environmentlifecycle.ReconcileReport{ArtifactDigest: gotDigest}
			return environmentlifecycle.RepairReport{Inspection: environmentlifecycle.Inspection{Digest: gotDigest, Lease: reconcile}, Reconciled: reconcile, Verification: environmentlifecycle.Verification{Digest: gotDigest, Verified: true}}, nil
		},
	})
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"repair", "--artifact", digest.String(), "--provider", "remote", "--json"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("provider repair was not invoked")
	}
	if !bytes.Contains(output.Bytes(), []byte(`"verified":true`)) {
		t.Fatalf("JSON output = %s", output.Bytes())
	}
}
