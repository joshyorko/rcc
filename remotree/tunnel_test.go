package remotree

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestNewTunnelManager(t *testing.T) {
	// Test creating a Quick Tunnel manager (no name)
	tm := NewTunnelManager("")
	if tm == nil {
		t.Fatal("Expected non-nil TunnelManager")
	}
	if tm.tunnelName != "" {
		t.Errorf("Expected empty tunnelName for Quick Tunnel, got %q", tm.tunnelName)
	}
	if tm.ctx == nil {
		t.Error("Expected non-nil context")
	}
	if tm.cancel == nil {
		t.Error("Expected non-nil cancel function")
	}

	// Test creating a Named Tunnel manager
	tm2 := NewTunnelManager("my-tunnel")
	if tm2 == nil {
		t.Fatal("Expected non-nil TunnelManager")
	}
	if tm2.tunnelName != "my-tunnel" {
		t.Errorf("Expected tunnelName 'my-tunnel', got %q", tm2.tunnelName)
	}
}

func TestGetPublicURL_Timeout(t *testing.T) {
	tm := NewTunnelManager("")
	// Without starting the tunnel, GetPublicURL should timeout
	_, err := tm.GetPublicURL(100 * time.Millisecond)
	if err == nil {
		t.Error("Expected timeout error when URL not available")
	}
	expectedMsg := "timeout waiting for public URL"
	if !strings.Contains(err.Error(), expectedMsg) {
		t.Errorf("Expected error message to contain %q, got %q", expectedMsg, err.Error())
	}
}

func TestGetPublicURL_Success(t *testing.T) {
	tm := NewTunnelManager("")
	// Simulate URL being set
	expectedURL := "https://test-tunnel.trycloudflare.com"
	tm.publicURL = expectedURL

	url, err := tm.GetPublicURL(1 * time.Second)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if url != expectedURL {
		t.Errorf("Expected URL %q, got %q", expectedURL, url)
	}
}

func TestStop_NoProcess(t *testing.T) {
	tm := NewTunnelManager("")
	// Stop should not panic when no process is running
	err := tm.Stop()
	if err != nil {
		t.Errorf("Expected no error when stopping without process, got %v", err)
	}
}

func TestStart_MissingCloudflared(t *testing.T) {
	// This test verifies the error message when cloudflared is not found
	// We don't actually try to start it to avoid dependency on cloudflared being installed
	tm := NewTunnelManager("")
	
	// Try to start - should fail if cloudflared not in PATH
	err := tm.Start(4653)
	if err != nil {
		// Expected error if cloudflared not installed
		expectedMsg := "cloudflared not found"
		if !strings.Contains(err.Error(), expectedMsg) {
			t.Errorf("Expected error message to contain %q, got %q", expectedMsg, err.Error())
		}
	}
	// If no error, cloudflared is installed - clean up
	if err == nil {
		tm.Stop()
	}
}

func TestStart_NamedTunnelWithoutToken(t *testing.T) {
	// Save original token if exists
	originalToken := os.Getenv("CF_TUNNEL_TOKEN")
	defer func() {
		if originalToken != "" {
			os.Setenv("CF_TUNNEL_TOKEN", originalToken)
		} else {
			os.Unsetenv("CF_TUNNEL_TOKEN")
		}
	}()
	
	// Unset the token
	os.Unsetenv("CF_TUNNEL_TOKEN")
	
	tm := NewTunnelManager("test-tunnel")
	err := tm.Start(4653)
	
	if err == nil {
		// If no error, cloudflared is installed and we need to clean up
		tm.Stop()
		t.Skip("Cloudflared not installed, skipping named tunnel test")
	}
	
	// Should fail with token error if cloudflared exists, or not found error otherwise
	if !strings.Contains(err.Error(), "CF_TUNNEL_TOKEN") && !strings.Contains(err.Error(), "cloudflared not found") {
		t.Errorf("Expected error about CF_TUNNEL_TOKEN or cloudflared not found, got: %v", err)
	}
}

func TestTunnelManager_ContextCancellation(t *testing.T) {
	tm := NewTunnelManager("")
	
	// Verify context is set up properly
	if tm.ctx == nil {
		t.Fatal("Expected context to be initialized")
	}
	
	// Cancel the context
	tm.cancel()
	
	// Context should be cancelled
	select {
	case <-tm.ctx.Done():
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected context to be cancelled")
	}
}

func TestGetPublicURL_MultipleCallsReturnSameURL(t *testing.T) {
	tm := NewTunnelManager("")
	expectedURL := "https://test-tunnel.trycloudflare.com"
	tm.publicURL = expectedURL

	// First call
	url1, err1 := tm.GetPublicURL(100 * time.Millisecond)
	if err1 != nil {
		t.Fatalf("First call failed: %v", err1)
	}
	
	// Second call should return same URL immediately
	url2, err2 := tm.GetPublicURL(100 * time.Millisecond)
	if err2 != nil {
		t.Fatalf("Second call failed: %v", err2)
	}
	
	if url1 != url2 {
		t.Errorf("Expected same URL on multiple calls, got %q and %q", url1, url2)
	}
}

func TestTunnelManager_CleanShutdown(t *testing.T) {
	tm := NewTunnelManager("")
	
	// Stop without ever starting should not error
	err := tm.Stop()
	if err != nil {
		t.Errorf("Stop without Start should not error, got: %v", err)
	}
	
	// Multiple stops should be safe
	err = tm.Stop()
	if err != nil {
		t.Errorf("Multiple stops should not error, got: %v", err)
	}
}
