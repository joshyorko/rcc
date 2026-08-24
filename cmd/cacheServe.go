package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/joshyorko/rcc/artifactprovider"
	"github.com/spf13/cobra"
)

type cacheServeResult struct {
	URL    string `json:"url"`
	Root   string `json:"root"`
	Listen string `json:"listen"`
}

func newCacheServeCommand(dependencies cacheCommandDependencies) *cobra.Command {
	var root, listen string
	var maxBytes, maxObjects, maxManifests, maxUploads, requestsPerSecond int64
	var jsonOutput bool
	command := &cobra.Command{
		Use:          "serve",
		Short:        "Serve a filesystem-backed environment artifact provider.",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(command *cobra.Command, _ []string) error {
			if !jsonOutput {
				return fmt.Errorf("--json is required")
			}
			if root == "" || (dependencies.serve == nil && dependencies.serveWithLimit == nil) {
				return fmt.Errorf("--root is required")
			}
			ctx, stop := signal.NotifyContext(command.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			limits := artifactprovider.Limits{MaxBytes: maxBytes, MaxObjects: maxObjects, MaxManifests: maxManifests, MaxUploads: maxUploads, RequestsPerSecond: requestsPerSecond}
			if limits.MaxBytes < 0 || limits.MaxObjects < 0 || limits.MaxManifests < 0 || limits.MaxUploads < 0 || limits.RequestsPerSecond < 0 {
				return fmt.Errorf("cache provider limits must be non-negative")
			}
			if dependencies.serveWithLimit != nil {
				return dependencies.serveWithLimit(ctx, root, listen, command.OutOrStdout(), limits)
			}
			return dependencies.serve(ctx, root, listen, command.OutOrStdout())
		},
	}
	command.Flags().StringVar(&root, "root", "", "Filesystem provider root.")
	command.Flags().StringVar(&listen, "listen", "127.0.0.1:0", "Loopback listen address.")
	command.Flags().Int64Var(&maxBytes, "max-bytes", 0, "Maximum committed object bytes (0 means unlimited).")
	command.Flags().Int64Var(&maxObjects, "max-objects", 0, "Maximum committed objects (0 means unlimited).")
	command.Flags().Int64Var(&maxManifests, "max-manifests", 0, "Maximum committed manifests (0 means unlimited).")
	command.Flags().Int64Var(&maxUploads, "max-uploads", 0, "Maximum object uploads (0 means unlimited).")
	command.Flags().Int64Var(&requestsPerSecond, "requests-per-second", 0, "Maximum provider requests per second (0 means unlimited).")
	command.Flags().BoolVar(&jsonOutput, "json", false, "Write one JSON startup object to stdout.")
	return command
}

func serveArtifactCache(ctx context.Context, root, listen string, output io.Writer) error {
	return serveArtifactCacheWithOptions(ctx, root, listen, output, artifactprovider.Limits{})
}

func serveArtifactCacheWithOptions(ctx context.Context, root, listen string, output io.Writer, limits artifactprovider.Limits) error {
	if err := validateLoopbackListen(listen); err != nil {
		return err
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve cache root: %w", err)
	}
	filesystem, err := artifactprovider.NewFilesystem(absoluteRoot)
	if err != nil {
		return err
	}
	provider := artifactprovider.Provider(filesystem)
	if limits != (artifactprovider.Limits{}) {
		provider = artifactprovider.NewPolicy(filesystem, limits)
	}
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		return fmt.Errorf("listen for cache provider: %w", err)
	}
	defer func() { _ = listener.Close() }()
	server := &http.Server{
		Handler:           artifactprovider.NewHandler(provider),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	serveErrors := make(chan error, 1)
	go func() { serveErrors <- server.Serve(listener) }()
	started := cacheServeResult{
		URL: "http://" + listener.Addr().String(), Root: absoluteRoot, Listen: listener.Addr().String(),
	}
	if err := json.NewEncoder(output).Encode(started); err != nil {
		_ = server.Close()
		<-serveErrors
		return fmt.Errorf("write cache provider startup result: %w", err)
	}
	select {
	case err := <-serveErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return fmt.Errorf("shut down cache provider: %w", err)
		}
		if err := <-serveErrors; !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

func validateLoopbackListen(value string) error {
	host, _, err := net.SplitHostPort(value)
	if err != nil || host == "" {
		return fmt.Errorf("cache provider listen address must be explicit loopback host:port")
	}
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("cache provider listen address must be loopback")
	}
	return nil
}
