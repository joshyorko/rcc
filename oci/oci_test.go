package oci

import (
	"os"
	"testing"
)

func TestParseRegistryURL(t *testing.T) {
	tests := []struct {
		url      string
		wantBase string
		wantRepo string
		wantErr  bool
	}{
		{"ghcr.io/org/sboms", "https://ghcr.io", "org/sboms", false},
		{"https://ghcr.io/org/sboms", "https://ghcr.io", "org/sboms", false},
		{"http://localhost:5000/test/repo", "http://localhost:5000", "test/repo", false},
		{"docker.io/library/nginx", "https://docker.io", "library/nginx", false},
		{"ghcr.io", "", "", true},
		{"", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			gotBase, gotRepo, err := parseRegistryURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseRegistryURL(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
				return
			}
			if gotBase != tt.wantBase {
				t.Errorf("parseRegistryURL(%q) base = %v, want %v", tt.url, gotBase, tt.wantBase)
			}
			if gotRepo != tt.wantRepo {
				t.Errorf("parseRegistryURL(%q) repo = %v, want %v", tt.url, gotRepo, tt.wantRepo)
			}
		})
	}
}

func TestCalculateDigest(t *testing.T) {
	content := []byte("hello world")
	digest := calculateDigest(content)

	// SHA256 of "hello world"
	expected := "sha256:b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
	if digest != expected {
		t.Errorf("calculateDigest() = %v, want %v", digest, expected)
	}
}

func TestNewClientFromEnv(t *testing.T) {
	// Save original env vars
	origUsername := os.Getenv("OCI_USERNAME")
	origPassword := os.Getenv("OCI_PASSWORD")

	// Clean up after test
	defer func() {
		os.Setenv("OCI_USERNAME", origUsername)
		os.Setenv("OCI_PASSWORD", origPassword)
	}()

	// Set test values
	os.Setenv("OCI_USERNAME", "testuser")
	os.Setenv("OCI_PASSWORD", "testpass")

	client := NewClientFromEnv("ghcr.io/test/repo", "v1.0.0")

	if client.config.Registry != "ghcr.io/test/repo" {
		t.Errorf("Registry = %q, want %q", client.config.Registry, "ghcr.io/test/repo")
	}
	if client.config.Tag != "v1.0.0" {
		t.Errorf("Tag = %q, want %q", client.config.Tag, "v1.0.0")
	}
	if client.config.Username != "testuser" {
		t.Errorf("Username = %q, want %q", client.config.Username, "testuser")
	}
	if client.config.Password != "testpass" {
		t.Errorf("Password = %q, want %q", client.config.Password, "testpass")
	}
}

func TestNewClient(t *testing.T) {
	config := Config{
		Registry: "ghcr.io/test/repo",
		Tag:      "v1.0.0",
		Username: "user",
		Password: "pass",
	}

	client := NewClient(config)

	if client.config.Registry != config.Registry {
		t.Errorf("Registry = %q, want %q", client.config.Registry, config.Registry)
	}
	if client.httpClient == nil {
		t.Error("httpClient should not be nil")
	}
}
