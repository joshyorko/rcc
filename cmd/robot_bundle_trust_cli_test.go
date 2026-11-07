package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestBundleTrustFlagsExposeStrictDefaultAndNamedLocalFallback(t *testing.T) {
	var options bundleTrustOptions
	command := &cobra.Command{Use: "run-from-bundle"}
	options.addFlags(command)
	for _, name := range []string{"trust-roots", "trust-carrier", "trust-carrier-type", "permissive-local"} {
		if command.Flags().Lookup(name) == nil {
			t.Fatalf("missing bundle trust flag --%s", name)
		}
	}
	if options.permissiveLocal {
		t.Fatal("unsigned local fallback must be disabled by default")
	}
	if err := command.Flags().Set("permissive-local", "true"); err != nil {
		t.Fatal(err)
	}
	if !options.permissiveLocal {
		t.Fatal("--permissive-local did not select the explicit migration fallback")
	}
}
