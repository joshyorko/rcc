package cmd

import (
	"encoding/json"
	"fmt"
	"github.com/joshyorko/rcc/artifacttrust"
	"github.com/spf13/cobra"
)

// env trust verify is provider/carrier independent: it evaluates detached
// attestations supplied by the caller and emits a stable receipt.
func newEnvironmentTrustCommand() *cobra.Command {
	var artifact, platform, builder string
	var strict, local, jsonOutput bool
	verify := &cobra.Command{Use:"verify", Args:cobra.NoArgs, SilenceUsage:true, RunE:func(cmd *cobra.Command,_ []string) error {
		if !jsonOutput { return fmt.Errorf("--json is required") }
		p:=artifacttrust.Policy{Mode:artifacttrust.PermissiveLocal}; if strict { p.Mode=artifacttrust.StrictRemote }; if !local && !strict { p.Mode=artifacttrust.StrictRemote }
		r:=p.Verify(artifacttrust.VerifyRequest{ArtifactDigest:artifact, Platform:platform, Builder:builder})
		if err:=json.NewEncoder(cmd.OutOrStdout()).Encode(r); err!=nil{return err}; if !r.Valid{return fmt.Errorf("artifact trust verification failed: %s",r.Code)}; return nil
	}}
	verify.Flags().StringVar(&artifact,"artifact","","Canonical sha256 environment artifact digest")
	verify.Flags().StringVar(&platform,"platform","","RCC compatibility platform")
	verify.Flags().StringVar(&builder,"builder","","Builder identity")
	verify.Flags().BoolVar(&strict,"strict-remote",false,"Require detached signature")
	verify.Flags().BoolVar(&local,"permissive-local",false,"Explicitly allow unsigned local artifacts")
	verify.Flags().BoolVar(&jsonOutput,"json",false,"Write one JSON receipt")
	root:=&cobra.Command{Use:"trust",Short:"Verify detached artifact trust attestations.",Args:cobra.NoArgs}; root.AddCommand(verify); return root
}
