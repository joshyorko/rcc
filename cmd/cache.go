package cmd

import (
	"context"
	"io"

	"github.com/joshyorko/rcc/artifactprovider"
	"github.com/spf13/cobra"
)

type cacheCommandDependencies struct {
	serve          func(context.Context, string, string, io.Writer) error
	serveWithLimit func(context.Context, string, string, io.Writer, artifactprovider.Limits) error
}

func newCacheCommand(dependencies cacheCommandDependencies) *cobra.Command {
	command := &cobra.Command{
		Use:   "cache",
		Short: "Serve immutable RCC cache content.",
		Args:  cobra.NoArgs,
	}
	command.AddCommand(newCacheServeCommand(dependencies))
	return command
}

func init() {
	rootCmd.AddCommand(newCacheCommand(cacheCommandDependencies{serve: serveArtifactCache, serveWithLimit: serveArtifactCacheWithOptions}))
}
