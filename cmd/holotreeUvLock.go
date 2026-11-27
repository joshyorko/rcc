package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/joshyorko/rcc/conda"
	"github.com/spf13/cobra"
)

var uvLockVersion, uvLockOutput string
var holotreeUvLockCmd = &cobra.Command{
	Use: "uv-lock", Short: "Create a verified uv tool lock", Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if uvLockVersion == "" || uvLockOutput == "" {
			return fmt.Errorf("--version and --output are required")
		}
		lock, err := conda.CreateUVToolLock(uvLockVersion)
		if err != nil {
			return err
		}
		data, err := json.MarshalIndent(lock, "", "  ")
		if err != nil {
			return err
		}
		data = append(data, '\n')
		f, err := os.CreateTemp(filepath.Dir(uvLockOutput), ".uv-lock-*")
		if err != nil {
			return err
		}
		defer func() { _ = os.Remove(f.Name()) }()
		if _, err = f.Write(data); err != nil {
			_ = f.Close()
			return err
		}
		if err = f.Close(); err != nil {
			return err
		}
		return os.Rename(f.Name(), uvLockOutput)
	},
}

func init() {
	holotreeUvLockCmd.Flags().StringVar(&uvLockVersion, "version", "", "uv version: latest or X.Y.Z")
	holotreeUvLockCmd.Flags().StringVar(&uvLockOutput, "output", "", "lock output file")
	holotreeCmd.AddCommand(holotreeUvLockCmd)
}
