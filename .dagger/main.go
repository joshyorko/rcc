// A generated module for RccCi functions
//
// This module has been generated via dagger init and serves as a reference to
// basic module structure as you get started with Dagger.
//
// Two functions have been pre-created. You can modify, delete, or add to them,
// as needed. They demonstrate usage of arguments and return types using simple
// echo and grep commands. The functions can be called from the dagger CLI or
// from one of the SDKs.
//
// The first line in this comment block is a short description line and the
// rest is a long description with more detail on the module's purpose or usage,
// if appropriate. All modules should have a short description.

package main

import (
	"context"
	"dagger/rcc-ci/internal/dagger"
)

type RccCi struct{}

// Returns a container that echoes whatever string argument is provided
func (m *RccCi) ContainerEcho(stringArg string) *dagger.Container {
	return dag.Container().From("alpine:latest").WithExec([]string{"echo", stringArg})
}

// Run tests using the Go container
func (m *RccCi) RunRobotTests(ctx context.Context, source *dagger.Directory) (string, error) {
	return dag.Container().
		From("golang:1.22").
		WithExec([]string{"apt-get", "update"}).
		WithExec([]string{"apt-get", "install", "-y", "curl", "git", "unzip", "ca-certificates"}).
		WithExec([]string{"curl", "-L", "-o", "/usr/local/bin/rcc", "https://github.com/joshyorko/rcc/releases/download/v18.10.0/rcc-linux64"}).
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

// Returns lines that match a pattern in the files of the provided Directory
func (m *RccCi) GrepDir(ctx context.Context, directoryArg *dagger.Directory, pattern string) (string, error) {
	return dag.Container().
		From("alpine:latest").
		WithMountedDirectory("/mnt", directoryArg).
		WithWorkdir("/mnt").
		WithExec([]string{"grep", "-R", pattern, "."}).
		Stdout(ctx)
}
