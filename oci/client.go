package oci

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/robocorp/rcc/common"
)

// Config holds OCI registry configuration.
type Config struct {
	Registry string // Registry URL (e.g., "ghcr.io/org/sboms")
	Tag      string // Tag for the artifact
	Username string // Username for authentication
	Password string // Password/token for authentication
}

// Client provides OCI registry operations.
type Client struct {
	config     Config
	httpClient *http.Client
}

// NewClient creates a new OCI client.
func NewClient(config Config) *Client {
	return &Client{
		config: config,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// NewClientFromEnv creates a new OCI client with authentication from environment variables.
// It looks for OCI_USERNAME and OCI_PASSWORD, or DOCKER_USERNAME and DOCKER_PASSWORD.
func NewClientFromEnv(registry, tag string) *Client {
	username := os.Getenv("OCI_USERNAME")
	if username == "" {
		username = os.Getenv("DOCKER_USERNAME")
	}
	password := os.Getenv("OCI_PASSWORD")
	if password == "" {
		password = os.Getenv("DOCKER_PASSWORD")
	}

	return NewClient(Config{
		Registry: registry,
		Tag:      tag,
		Username: username,
		Password: password,
	})
}

// PushResult contains the result of a push operation.
type PushResult struct {
	Digest    string `json:"digest"`
	Tag       string `json:"tag"`
	Registry  string `json:"registry"`
	MediaType string `json:"mediaType"`
}

// OCI manifest structure
type ociManifest struct {
	SchemaVersion int              `json:"schemaVersion"`
	MediaType     string           `json:"mediaType"`
	Config        ociDescriptor    `json:"config"`
	Layers        []ociDescriptor  `json:"layers"`
	Annotations   map[string]string `json:"annotations,omitempty"`
}

type ociDescriptor struct {
	MediaType   string            `json:"mediaType"`
	Digest      string            `json:"digest"`
	Size        int64             `json:"size"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

// Push pushes an SBOM artifact to the OCI registry.
func (c *Client) Push(ctx context.Context, content []byte, mediaType string) (*PushResult, error) {
	// Parse registry URL
	registryBase, repository, err := parseRegistryURL(c.config.Registry)
	if err != nil {
		return nil, fmt.Errorf("invalid registry URL: %w", err)
	}

	common.Debug("Pushing SBOM to %s/%s:%s", registryBase, repository, c.config.Tag)

	// Calculate digest of the content
	contentDigest := calculateDigest(content)
	
	// Create empty config blob (required for OCI artifacts)
	emptyConfig := []byte("{}")
	configDigest := calculateDigest(emptyConfig)

	// Step 1: Check if we need to authenticate and get a token
	token, err := c.authenticate(ctx, registryBase, repository)
	if err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	// Step 2: Upload the config blob
	err = c.uploadBlob(ctx, registryBase, repository, emptyConfig, configDigest, token)
	if err != nil {
		return nil, fmt.Errorf("failed to upload config blob: %w", err)
	}

	// Step 3: Upload the content blob
	err = c.uploadBlob(ctx, registryBase, repository, content, contentDigest, token)
	if err != nil {
		return nil, fmt.Errorf("failed to upload content blob: %w", err)
	}

	// Step 4: Create and push manifest
	manifest := ociManifest{
		SchemaVersion: 2,
		MediaType:     "application/vnd.oci.image.manifest.v1+json",
		Config: ociDescriptor{
			MediaType: "application/vnd.oci.empty.v1+json",
			Digest:    configDigest,
			Size:      int64(len(emptyConfig)),
		},
		Layers: []ociDescriptor{
			{
				MediaType: mediaType,
				Digest:    contentDigest,
				Size:      int64(len(content)),
				Annotations: map[string]string{
					"org.opencontainers.image.title": "sbom.json",
				},
			},
		},
		Annotations: map[string]string{
			"org.opencontainers.image.created": time.Now().UTC().Format(time.RFC3339),
		},
	}

	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal manifest: %w", err)
	}

	manifestDigest, err := c.pushManifest(ctx, registryBase, repository, manifestBytes, c.config.Tag, token)
	if err != nil {
		return nil, fmt.Errorf("failed to push manifest: %w", err)
	}

	return &PushResult{
		Digest:    manifestDigest,
		Tag:       c.config.Tag,
		Registry:  c.config.Registry,
		MediaType: mediaType,
	}, nil
}

// parseRegistryURL parses a registry URL into base URL and repository.
func parseRegistryURL(registryURL string) (string, string, error) {
	// Remove protocol if present
	url := registryURL
	protocol := "https://"
	if strings.HasPrefix(url, "https://") {
		url = strings.TrimPrefix(url, "https://")
	} else if strings.HasPrefix(url, "http://") {
		protocol = "http://"
		url = strings.TrimPrefix(url, "http://")
		// Warning: HTTP connections are insecure and should only be used for local development
		common.Debug("WARNING: Using insecure HTTP connection to registry")
	}

	// Split into registry and repository
	parts := strings.SplitN(url, "/", 2)
	if len(parts) < 2 {
		return "", "", fmt.Errorf("invalid registry URL format: expected 'registry/repository'")
	}

	registryBase := protocol + parts[0]
	repository := parts[1]

	return registryBase, repository, nil
}

// calculateDigest calculates the SHA256 digest of content.
func calculateDigest(content []byte) string {
	hash := sha256.Sum256(content)
	return fmt.Sprintf("sha256:%x", hash)
}

// authenticate attempts to authenticate with the registry.
func (c *Client) authenticate(ctx context.Context, registryBase, repository string) (string, error) {
	// If no credentials, try without authentication
	if c.config.Username == "" || c.config.Password == "" {
		return "", nil
	}

	// Try to get a token from the registry's token service
	// First, check the /v2/ endpoint to get the www-authenticate header
	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/v2/", registryBase), nil)
	if err != nil {
		return "", err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		// No authentication required
		return "", nil
	}

	if resp.StatusCode != http.StatusUnauthorized {
		return "", fmt.Errorf("unexpected response from registry: %d", resp.StatusCode)
	}

	// Parse WWW-Authenticate header
	// Note: This implementation uses Basic auth for simplicity. Registries that require
	// OAuth2/Bearer token authentication (like GitHub Container Registry in some configurations)
	// may need additional token exchange logic. For most use cases with personal access tokens
	// or docker credentials, basic auth works well.
	authHeader := resp.Header.Get("WWW-Authenticate")
	_ = authHeader // Currently unused, but available for future Bearer token implementation

	// Use basic auth - works with most registries when using personal access tokens
	return base64.StdEncoding.EncodeToString([]byte(c.config.Username + ":" + c.config.Password)), nil
}

// uploadBlob uploads a blob to the registry.
func (c *Client) uploadBlob(ctx context.Context, registryBase, repository string, content []byte, digest, token string) error {
	// Check if blob already exists
	exists, err := c.blobExists(ctx, registryBase, repository, digest, token)
	if err != nil {
		return err
	}
	if exists {
		common.Debug("Blob %s already exists, skipping upload", digest)
		return nil
	}

	// Start upload session
	initURL := fmt.Sprintf("%s/v2/%s/blobs/uploads/", registryBase, repository)
	req, err := http.NewRequestWithContext(ctx, "POST", initURL, nil)
	if err != nil {
		return err
	}
	c.addAuth(req, token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to initiate upload: status %d, body: %s", resp.StatusCode, string(body))
	}

	// Get the upload URL from the Location header
	uploadURL := resp.Header.Get("Location")
	if uploadURL == "" {
		return fmt.Errorf("no upload location returned")
	}

	// Make URL absolute if needed
	if !strings.HasPrefix(uploadURL, "http") {
		uploadURL = registryBase + uploadURL
	}

	// Complete the upload with the blob content
	if strings.Contains(uploadURL, "?") {
		uploadURL = uploadURL + "&digest=" + digest
	} else {
		uploadURL = uploadURL + "?digest=" + digest
	}

	req, err = http.NewRequestWithContext(ctx, "PUT", uploadURL, bytes.NewReader(content))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Content-Length", fmt.Sprintf("%d", len(content)))
	c.addAuth(req, token)

	resp, err = c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to upload blob: status %d, body: %s", resp.StatusCode, string(body))
	}

	return nil
}

// blobExists checks if a blob exists in the registry.
func (c *Client) blobExists(ctx context.Context, registryBase, repository, digest, token string) (bool, error) {
	url := fmt.Sprintf("%s/v2/%s/blobs/%s", registryBase, repository, digest)
	req, err := http.NewRequestWithContext(ctx, "HEAD", url, nil)
	if err != nil {
		return false, err
	}
	c.addAuth(req, token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK, nil
}

// pushManifest pushes the manifest to the registry.
func (c *Client) pushManifest(ctx context.Context, registryBase, repository string, manifest []byte, tag, token string) (string, error) {
	url := fmt.Sprintf("%s/v2/%s/manifests/%s", registryBase, repository, tag)
	req, err := http.NewRequestWithContext(ctx, "PUT", url, bytes.NewReader(manifest))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
	c.addAuth(req, token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("failed to push manifest: status %d, body: %s", resp.StatusCode, string(body))
	}

	// Get the manifest digest from the response
	digest := resp.Header.Get("Docker-Content-Digest")
	if digest == "" {
		digest = calculateDigest(manifest)
	}

	return digest, nil
}

// addAuth adds authentication to the request.
func (c *Client) addAuth(req *http.Request, token string) {
	if token != "" {
		req.Header.Set("Authorization", "Basic "+token)
	}
}
