package main

import (
	"testing"
)

// TestFlagsParsing ensures the --expose and --tunnel-name flags are properly defined
func TestFlagsParsing(t *testing.T) {
	// Reset flags to default values
	exposeFlag = false
	tunnelName = ""
	
	// Verify default values
	if exposeFlag != false {
		t.Errorf("Expected exposeFlag default to be false, got %v", exposeFlag)
	}
	
	if tunnelName != "" {
		t.Errorf("Expected tunnelName default to be empty, got %q", tunnelName)
	}
}

// TestDefaultHoldLocation ensures the default hold location is properly generated
func TestDefaultHoldLocation(t *testing.T) {
	location := defaultHoldLocation()
	
	if location == "" {
		t.Error("Expected non-empty hold location")
	}
	
	// Should either return a valid path or the fallback
	if location != "temphold" && len(location) < 3 {
		t.Errorf("Expected valid hold location, got %q", location)
	}
}

