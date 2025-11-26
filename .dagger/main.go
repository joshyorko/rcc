// Dagger module for running RCC robot tests in CI.
// Provides a reproducible container that installs RCC and executes the robot suite.

package main

import (
	"context"
	"dagger/rcc-ci/internal/dagger"
)

type RccCi struct{}

// RunRobotTests executes the robot suite inside a Go-based container.
func (m *RccCi) RunRobotTests(ctx context.Context, source *dagger.Directory) (string, error) {
	return dag.Container().
		From("golang:1.22").
		WithExec([]string{"apt-get", "update"}).
		WithExec([]string{"apt-get", "install", "-y", "curl", "git", "unzip", "ca-certificates"}).
		WithExec([]string{"curl", "-L", "-o", "/usr/local/bin/rcc", "https://github.com/joshyorko/rcc/releases/latest/download/rcc-linux64"}).
		WithExec([]string{"chmod", "+x", "/usr/local/bin/rcc"}).
		WithMountedDirectory("/src", source).
		WithWorkdir("/src").
		WithMountedCache("/go/pkg/mod", dag.CacheVolume("go-mod-cache")).
		WithMountedCache("/root/.robocorp", dag.CacheVolume("robocorp-home")).
		WithEnvVariable("PIP_ROOT_USER_ACTION", "ignore").
		WithExec([]string{"rcc", "holotree", "variables", "-r", "developer/toolkit.yaml"}).
		WithExec([]string{"rcc", "run", "-r", "developer/toolkit.yaml", "-t", "robot"}).
		Stdout(ctx)
}
