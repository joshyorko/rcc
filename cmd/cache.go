package cmd

import (
	"context"
	"io"
	"path/filepath"

	"github.com/joshyorko/rcc/artifactprovider"
	"github.com/joshyorko/rcc/common"
	"github.com/spf13/cobra"
)

type cacheCommandDependencies struct {
	serve                     func(context.Context, string, string, io.Writer) error
	serveWithLimit            func(context.Context, string, string, io.Writer, artifactprovider.Limits) error
	serveConfigured           func(context.Context, string, string, io.Writer, string, artifactprovider.Limits) error
	serveConfiguredWithOutput func(context.Context, string, string, io.Writer, string, artifactprovider.Limits, bool) error
}

func defaultCacheProviderRoot() string {
	return filepath.Join(common.Product.Home(), "artifacts", "v1", "provider")
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
	rootCmd.AddCommand(newCacheCommand(cacheCommandDependencies{serve: serveArtifactCache, serveWithLimit: serveArtifactCacheWithOptions, serveConfigured: serveArtifactCacheConfigured, serveConfiguredWithOutput: serveArtifactCacheConfiguredOutput}))
}
