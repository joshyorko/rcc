package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/joshyorko/rcc/environmentartifact"
	"github.com/joshyorko/rcc/environmentlifecycle"
	"github.com/spf13/cobra"
)

func newEnvironmentLifecycleCommand() *cobra.Command {
	command:=&cobra.Command{Use:"lifecycle",Short:"Inspect and repair local environment lifecycle state.",Args:cobra.NoArgs}
	for _,name:=range []string{"inspect","verify","repair"}{n:=name;var artifact string;var jsonOutput bool;c:=&cobra.Command{Use:n,Args:cobra.NoArgs,SilenceUsage:true,RunE:func(cmd *cobra.Command,_ []string)error{if !jsonOutput{return fmt.Errorf("--json is required")};d,e:=environmentartifact.ParseDigest(artifact);if e!=nil{return e};var v any;switch n{case "inspect":v,e=environmentlifecycle.Inspect(cmd.Context(),d);case "verify":v,e=environmentlifecycle.Verify(cmd.Context(),d);default:v,e=environmentlifecycle.Repair(cmd.Context(),d)};if e!=nil{return e};return json.NewEncoder(cmd.OutOrStdout()).Encode(v)}};c.Flags().StringVar(&artifact,"artifact","","Canonical sha256 artifact digest.");c.Flags().BoolVar(&jsonOutput,"json",false,"Write one JSON result object.");command.AddCommand(c)}
	return command
}
