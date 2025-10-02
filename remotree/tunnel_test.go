package remotree

import (
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
