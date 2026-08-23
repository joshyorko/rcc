package cmd

import (
	"context"

	"github.com/joshyorko/rcc/artifactprovider"
	"github.com/joshyorko/rcc/environmentlifecycle"
	"github.com/joshyorko/rcc/buildcoord"
	"github.com/spf13/cobra"
)

type environmentCommandDependencies struct {
	newProvider  func(string) (artifactprovider.Provider, error)
	publish      func(context.Context, environmentlifecycle.PublishRequest) (environmentlifecycle.PublishResult, error)
	acquire      func(context.Context, environmentlifecycle.AcquireRequest) (environmentlifecycle.AcquireResult, error)
	execute      func(context.Context, environmentlifecycle.Materializer, environmentlifecycle.Materialization, []string) (environmentlifecycle.ExecutionHandle, environmentlifecycle.ChildResult, error)
	materializer func() environmentlifecycle.Materializer
}

func defaultEnvironmentCommandDependencies() environmentCommandDependencies {
	return environmentCommandDependencies{
		newProvider: newProviderReference,
		publish:     environmentlifecycle.Publish,
		acquire: func(ctx context.Context, request environmentlifecycle.AcquireRequest) (environmentlifecycle.AcquireResult, error) {
			return environmentlifecycle.NewAcquirer().Acquire(ctx, request)
		},
		execute:      environmentlifecycle.Execute,
		materializer: func() environmentlifecycle.Materializer { return environmentlifecycle.NewLocalMaterializer() },
	}
}

func newEnvironmentCommand(dependencies environmentCommandDependencies) *cobra.Command {
	command := &cobra.Command{
		Use:   "env",
		Short: "Publish, acquire, and execute portable environments.",
		Args:  cobra.NoArgs,
	}
	command.AddCommand(
		newEnvironmentPublishCommand(dependencies),
		newEnvironmentAcquireCommand(dependencies),
		newEnvironmentExecCommand(dependencies),
		newEnvironmentCoordinateCommand(),
	)
	return command
}

func init() {
	rootCmd.AddCommand(newEnvironmentCommand(defaultEnvironmentCommandDependencies()))
	rootCmd.AddCommand(newProviderCommand(defaultProviderCommandDependencies()))
}
