package remotree

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"time"

	"github.com/robocorp/rcc/common"
)

// TunnelManager manages a Cloudflare tunnel connection
type TunnelManager struct {
	tunnelName string // Empty for Quick Tunnel, set for Named Tunnel
	publicURL  string
	cmd        *exec.Cmd
	ctx        context.Context
	cancel     context.CancelFunc
}

// NewTunnelManager creates a new tunnel manager
// tunnelName should be empty for Quick Tunnels (no auth required)
// or set to a tunnel name for Named Tunnels (requires CF_TUNNEL_TOKEN)
func NewTunnelManager(tunnelName string) *TunnelManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &TunnelManager{
		tunnelName: tunnelName,
		ctx:        ctx,
		cancel:     cancel,
	}
}

// Start initiates the Cloudflare tunnel
func (tm *TunnelManager) Start(localPort int) error {
	// Check if cloudflared exists
	if _, err := exec.LookPath("cloudflared"); err != nil {
		return fmt.Errorf("cloudflared not found: install from https://github.com/cloudflare/cloudflared/releases")
	}

	var args []string

	if tm.tunnelName == "" {
		// Phase 1: Quick Tunnel (no token needed!)
		args = []string{"tunnel", "--url", fmt.Sprintf("http://localhost:%d", localPort)}
		common.Log("Starting Cloudflare Quick Tunnel...")
	} else {
		// Phase 2: Named Tunnel (requires CF_TUNNEL_TOKEN)
		token := os.Getenv("CF_TUNNEL_TOKEN")
		if token == "" {
			return fmt.Errorf("CF_TUNNEL_TOKEN environment variable required for named tunnels")
		}
		args = []string{"tunnel", "run", "--token", token, tm.tunnelName}
		common.Log("Starting Cloudflare Named Tunnel: %s", tm.tunnelName)
	}

	tm.cmd = exec.CommandContext(tm.ctx, "cloudflared", args...)

	// Capture stderr to extract public URL
	stderr, err := tm.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to get stderr pipe: %w", err)
	}

	// Start tunnel
	if err := tm.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start cloudflared: %w", err)
	}

	// Parse stderr for public URL
	go tm.parseOutput(stderr)

	return nil
}

// parseOutput reads cloudflared stderr to extract the public URL
func (tm *TunnelManager) parseOutput(stderr io.ReadCloser) {
	scanner := bufio.NewScanner(stderr)
	// Match any https:// URL (for both Quick and Named Tunnels)
	urlRegex := regexp.MustCompile(`https://[^\s"]+`)

	for scanner.Scan() {
		line := scanner.Text()
		if common.DebugFlag() {
			common.Debug("cloudflared: %s", line)
		}
		if match := urlRegex.FindString(line); match != "" {
			tm.publicURL = match
			common.Log("Tunnel URL found: %s", match)
			break
		}
	}
}

// GetPublicURL waits for the public URL to be available, up to the timeout
func (tm *TunnelManager) GetPublicURL(timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if tm.publicURL != "" {
			return tm.publicURL, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return "", fmt.Errorf("timeout waiting for public URL")
}

// Stop gracefully shuts down the tunnel
func (tm *TunnelManager) Stop() error {
	common.Log("Stopping tunnel...")
	tm.cancel()
	if tm.cmd != nil && tm.cmd.Process != nil {
		// Try graceful shutdown first
		if err := tm.cmd.Process.Signal(os.Interrupt); err != nil {
			// If interrupt fails, kill it
			return tm.cmd.Process.Kill()
		}
	}
	return nil
}
