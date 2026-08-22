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
			if root == "" || dependencies.serve == nil {
				return fmt.Errorf("--root is required")
			}
			ctx, stop := signal.NotifyContext(command.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return dependencies.serve(ctx, root, listen, command.OutOrStdout())
		},
	}
	command.Flags().StringVar(&root, "root", "", "Filesystem provider root.")
	command.Flags().StringVar(&listen, "listen", "127.0.0.1:0", "Loopback listen address.")
	command.Flags().BoolVar(&jsonOutput, "json", false, "Write one JSON startup object to stdout.")
	return command
}

func serveArtifactCache(ctx context.Context, root, listen string, output io.Writer) error {
	if err := validateLoopbackListen(listen); err != nil {
		return err
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve cache root: %w", err)
	}
	provider, err := artifactprovider.NewFilesystem(absoluteRoot)
	if err != nil {
		return err
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
