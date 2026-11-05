package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joshyorko/rcc/artifactprovider"
	"github.com/joshyorko/rcc/environmentartifact"
	"github.com/joshyorko/rcc/treeartifact"
	"github.com/spf13/cobra"
)

func treeArtifactEngine(store, workspace string) (*treeartifact.Engine, error) {
	if store == "" || workspace == "" {
		return nil, fmt.Errorf("store and workspace paths are required")
	}
	storePath, err := canonicalTreeStorePath(store)
	if err != nil {
		return nil, err
	}
	workspacePath, err := filepath.Abs(workspace)
	if err != nil {
		return nil, err
	}
	workspacePath, err = filepath.EvalSymlinks(workspacePath)
	if err != nil {
		return nil, err
	}
	for _, pair := range [][2]string{{storePath, workspacePath}, {workspacePath, storePath}} {
		rel, err := filepath.Rel(pair[0], pair[1])
		if err != nil {
			return nil, err
		}
		if rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
			return nil, fmt.Errorf("experimental store and workspace must not overlap")
		}
	}
	provider, err := artifactprovider.NewObjectFilesystem(storePath)
	if err != nil {
		return nil, err
	}
	return &treeartifact.Engine{Store: provider}, nil
}

// Resolve aliases before checking overlap, including a store not created yet.
func canonicalTreeStorePath(store string) (string, error) {
	current, err := filepath.Abs(store)
	if err != nil {
		return "", err
	}
	var suffix []string
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return resolved, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		suffix = append(suffix, filepath.Base(current))
		current = parent
	}
}

func optionalTreeParent(value string) (*treeartifact.Digest, error) {
	if value == "" {
		return nil, nil
	}
	d, err := environmentartifact.ParseDigest(value)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func newTreeArtifactCommand() *cobra.Command {
	root := &cobra.Command{Use: "tree-artifact", Short: "EXPERIMENTAL generic workspace snapshots (unencrypted; private CAS only).", Args: cobra.NoArgs}
	var captureStore, source, captureParent string
	var dirty []string
	capture := &cobra.Command{Use: "capture", Short: "Capture a quiescent tree or trusted explicit dirty set.", Args: cobra.NoArgs, SilenceUsage: true,
		RunE: func(command *cobra.Command, _ []string) error {
			parent, err := optionalTreeParent(captureParent)
			if err != nil {
				return err
			}
			if len(dirty) != 0 && parent == nil {
				return fmt.Errorf("dirty paths require a parent snapshot")
			}
			e, err := treeArtifactEngine(captureStore, source)
			if err != nil {
				return err
			}
			result, err := e.Capture(command.Context(), source, parent, dirty)
			if err != nil {
				return err
			}
			return json.NewEncoder(command.OutOrStdout()).Encode(result)
		}}
	capture.Flags().StringVar(&captureStore, "store", "", "Dedicated filesystem CAS; never share with Environment Artifact v1 GC.")
	capture.Flags().StringVar(&source, "source", "", "Quiescent workspace directory.")
	capture.Flags().StringVar(&captureParent, "parent", "", "Previous immutable snapshot digest.")
	capture.Flags().StringArrayVar(&dirty, "dirty", nil, "Trusted dirty relative path; repeat for deletions and both sides of a rename. A directory scans its subtree.")
	var materializeStore, destination, snapshot, materializeParent string
	materialize := &cobra.Command{Use: "materialize", Short: "Diff immutable trees over a trusted existing parent or empty directory.", Args: cobra.NoArgs, SilenceUsage: true,
		RunE: func(command *cobra.Command, _ []string) error {
			parent, err := optionalTreeParent(materializeParent)
			if err != nil {
				return err
			}
			child, err := environmentartifact.ParseDigest(snapshot)
			if err != nil {
				return err
			}
			e, err := treeArtifactEngine(materializeStore, destination)
			if err != nil {
				return err
			}
			stats, err := e.Materialize(command.Context(), destination, parent, child)
			if err != nil {
				return err
			}
			return json.NewEncoder(command.OutOrStdout()).Encode(stats)
		}}
	materialize.Flags().StringVar(&materializeStore, "store", "", "Dedicated filesystem CAS.")
	materialize.Flags().StringVar(&destination, "destination", "", "Quiescent exact parent materialization, or existing empty directory.")
	materialize.Flags().StringVar(&snapshot, "snapshot", "", "Child immutable snapshot digest.")
	materialize.Flags().StringVar(&materializeParent, "parent", "", "Snapshot currently materialized at destination.")
	root.AddCommand(capture, materialize)
	return root
}

func init() { rootCmd.AddCommand(newTreeArtifactCommand()) }
