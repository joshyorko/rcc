package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/conda"
	"github.com/robocorp/rcc/pretty"

	"github.com/spf13/cobra"
)

var (
	showDiff                  bool
	showIntermediateDirhashes bool
)

var dirhashCmd = &cobra.Command{
	Use:   "dirhash",
	Short: "Calculate hash for directory content.",
	Long:  `Calculate SHA256 of directory tree structure.`,
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		defer common.Stopwatch("rcc dirhash lasted").Report()
		diffMaps := make([]map[string]string, 0, len(args))
		for _, directory := range args {
			stat, err := os.Stat(directory)
			if err != nil {
				common.Error("dirhash", err)
				continue
			}
			if !stat.IsDir() {
				continue
			}
			fullpath, err := filepath.Abs(directory)
			if err != nil {
				continue
			}
			collector := make(map[string]string)
			digest, err := conda.DigestFor(fullpath, collector)
			if err != nil {
				common.Error("dirhash", err)
				continue
			}
			collector = conda.MakeRelativeMap(fullpath, collector)
			diffMaps = append(diffMaps, collector)
			result := common.Hexdigest(digest)
			common.Log("+ %v %v", result, directory)
			if showIntermediateDirhashes {
				relative := make(map[string]string)
				keyset := make([]string, 0, len(collector))
				for key, value := range collector {
					keyset = append(keyset, key)
					relative[key] = value
				}
				sort.Strings(keyset)
				for _, key := range keyset {
					fmt.Printf("%s  %s\n", relative[key], key)
				}
				fmt.Println()
			}
		}
		if showDiff && len(diffMaps) != 2 {
			pretty.Exit(1, "Diff expects exactly 2 environments, now got %d!", len(diffMaps))
		}
		if showDiff {
			conda.DirhashDiff(diffMaps[0], diffMaps[1], false)
		}
	},
}

func init() {
	internalCmd.AddCommand(dirhashCmd)
	dirhashCmd.Flags().BoolVarP(&showIntermediateDirhashes, "print", "", false, "Print all intermediate folder hashes also.")
	dirhashCmd.Flags().BoolVarP(&showDiff, "diff", "", false, "Diff two environments with differences.")
}
