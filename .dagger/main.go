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
	"fmt"
	"path"
	"strings"

	"dagger/rcc-ci/internal/dagger"
)

const defaultRccVersion = "v18.17.3"

type RccCi struct{}

// Returns a container that echoes whatever string argument is provided
func (m *RccCi) ContainerEcho(stringArg string) *dagger.Container {
	return dag.Container().From("alpine:latest").WithExec([]string{"echo", stringArg})
}

// Run tests using the Go container
func (m *RccCi) RunRobotTests(ctx context.Context, source *dagger.Directory) (string, error) {
	return dag.Container().
		From("golang:1.25.7").
		WithExec([]string{"apt-get", "update"}).
		WithExec([]string{"apt-get", "install", "-y", "curl", "git", "unzip", "ca-certificates"}).
		WithExec([]string{"curl", "-L", "-o", "/usr/local/bin/rcc", "https://github.com/joshyorko/rcc/releases/download/v18.13.1/rcc-linux64"}).
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

// Build a Linux container with RCC installed and the source mounted at /src.
func (m *RccCi) buildRccContainer(
	source *dagger.Directory,
	rccVersion string,
) *dagger.Container {
	if rccVersion == "" {
		rccVersion = defaultRccVersion
	}
	rccURL := fmt.Sprintf("https://github.com/joshyorko/rcc/releases/download/%s/rcc-linux64", rccVersion)

	return dag.
		Container(dagger.ContainerOpts{Platform: dagger.Platform("linux/amd64")}).
		From("python:3.11-slim").
		WithExec([]string{"apt-get", "update"}).
		WithExec([]string{"apt-get", "install", "-y", "curl", "ca-certificates"}).
		WithExec([]string{"curl", "-L", "-o", "/usr/local/bin/rcc", rccURL}).
		WithExec([]string{"chmod", "+x", "/usr/local/bin/rcc"}).
		WithMountedCache("/root/.robocorp", dag.CacheVolume("robocorp-home")).
		WithEnvVariable("ROBOCORP_HOME", "/root/.robocorp").
		WithMountedDirectory("/src", source).
		WithWorkdir("/src")
}

// Run any RCC command and return stdout.
func (m *RccCi) Rcc(
	ctx context.Context,
	// The RCC command, for example: "run -t ProcessLocal".
	c string,
	// +defaultPath="."
	source *dagger.Directory,
	// RCC release version to install in the container.
	// +default="v18.17.3"
	rccVersion string,
) (string, error) {
	args, err := splitCommand(c)
	if err != nil {
		return "", err
	}

	container := m.buildRccContainer(source, rccVersion)
	return container.WithExec(append([]string{"rcc"}, args...)).Stdout(ctx)
}

// Run any RCC command and return an output directory from the container.
func (m *RccCi) RccWithOutput(
	ctx context.Context,
	// The RCC command, for example: "run -t ProcessLocal".
	c string,
	// +defaultPath="."
	source *dagger.Directory,
	// Container path to return after the RCC command runs.
	// +default="./output"
	outputPath string,
	// RCC release version to install in the container.
	// +default="v18.17.3"
	rccVersion string,
) (*dagger.Directory, error) {
	args, err := splitCommand(c)
	if err != nil {
		return nil, err
	}

	container := m.buildRccContainer(source, rccVersion)
	result := container.WithExec(append([]string{"rcc"}, args...))
	return result.Directory(containerOutputPath(outputPath)), nil
}

func containerOutputPath(outputPath string) string {
	if outputPath == "" || outputPath == "." {
		return "/src/output"
	}
	if strings.HasPrefix(outputPath, "/") {
		return path.Clean(outputPath)
	}
	return path.Join("/src", outputPath)
}

func splitCommand(command string) ([]string, error) {
	var args []string
	var current strings.Builder
	var quote rune
	escaped := false

	for _, r := range command {
		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}

		if r == '\\' && quote != '\'' {
			escaped = true
			continue
		}

		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				current.WriteRune(r)
			}
			continue
		}

		switch r {
		case '\'', '"':
			quote = r
		case ' ', '\t', '\n', '\r':
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(r)
		}
	}

	if escaped {
		current.WriteRune('\\')
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated quote in RCC command")
	}
	if current.Len() > 0 {
		args = append(args, current.String())
	}

	return args, nil
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
