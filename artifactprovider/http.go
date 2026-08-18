package artifactprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/joshyorko/rcc/environmentartifact"
)

const (
	maxProviderJSONBytes   = 4 << 20
	maxProviderObjectBytes = int64(64 << 30)
	maxProviderErrorBytes  = 8 << 10
)

type HTTP struct {
	baseURL string
	client  *http.Client
}

type missingRequest struct {
	Descriptors []environmentartifact.Descriptor `json:"descriptors"`
}

type missingResponse struct {
	Missing []environmentartifact.Digest `json:"missing"`
}

func NewHTTP(baseURL string, client *http.Client) (*HTTP, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("invalid artifact provider URL %q", baseURL)
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return nil, fmt.Errorf("artifact provider URL must not contain a path")
	}
	if client == nil {
		client = http.DefaultClient
	}
	return &HTTP{baseURL: strings.TrimRight(baseURL, "/"), client: client}, nil
}

func NewHandler(provider *Filesystem) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if provider == nil || request.URL.RawPath != "" || request.URL.RawQuery != "" {
			http.Error(writer, "invalid provider request", http.StatusBadRequest)
			return
		}
		if request.Method == http.MethodGet && request.ContentLength != 0 {
			http.Error(writer, "GET requests must not contain a body", http.StatusBadRequest)
			return
		}
		handleProviderRequest(provider, writer, request)
	})
}

func handleProviderRequest(provider *Filesystem, writer http.ResponseWriter, request *http.Request) {
	ctx := request.Context()
	switch {
	case request.URL.Path == "/v1/capabilities":
		if request.Method != http.MethodGet {
			methodNotAllowed(writer)
			return
		}
		capabilities, err := provider.Capabilities(ctx)
		writeProviderJSON(writer, capabilities, err)
	case request.URL.Path == "/v1/objects/missing":
		if request.Method != http.MethodPost {
			methodNotAllowed(writer)
			return
		}
		if request.Header.Get("Content-Type") != "application/json" {
			http.Error(writer, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
			return
		}
		var input missingRequest
		if err := decodeBoundedJSON(request.Body, request.ContentLength, maxProviderJSONBytes, &input); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		missing, err := provider.MissingObjects(ctx, input.Descriptors)
		writeProviderJSON(writer, missingResponse{Missing: missing}, err)
	case strings.HasPrefix(request.URL.Path, "/v1/objects/sha256/"):
		digest, ok := digestFromExactPath(request.URL.Path, "/v1/objects/sha256/")
		if !ok {
			http.NotFound(writer, request)
			return
		}
		switch request.Method {
		case http.MethodPut:
			mediaType := request.Header.Get("Content-Type")
			if mediaType == "" || strings.Contains(mediaType, ";") || request.ContentLength < 0 || request.ContentLength > maxProviderObjectBytes {
				http.Error(writer, "invalid object content metadata", http.StatusBadRequest)
				return
			}
			descriptor := environmentartifact.Descriptor{MediaType: mediaType, Digest: digest, Size: request.ContentLength}
			err := provider.PutObject(ctx, Blob{Descriptor: descriptor, Reader: io.LimitReader(request.Body, request.ContentLength+1)})
			if err != nil {
				http.Error(writer, err.Error(), http.StatusUnprocessableEntity)
				return
			}
			writer.WriteHeader(http.StatusCreated)
		case http.MethodGet:
			content, err := provider.getObjectByDigest(ctx, digest)
			if err != nil {
				writeProviderReadError(writer, request, err)
				return
			}
			writer.Header().Set("Content-Type", "application/octet-stream")
			writer.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
			_, _ = writer.Write(content)
		default:
			methodNotAllowed(writer)
		}
	case strings.HasPrefix(request.URL.Path, "/v1/manifests/sha256/"):
		handleManifestRequest(provider, writer, request)
	default:
		http.NotFound(writer, request)
	}
}

func handleManifestRequest(provider *Filesystem, writer http.ResponseWriter, request *http.Request) {
	path := strings.TrimPrefix(request.URL.Path, "/v1/manifests/sha256/")
	commit := strings.HasSuffix(path, "/commit")
	if commit {
		path = strings.TrimSuffix(path, "/commit")
	}
	digest, err := environmentartifact.ParseDigest("sha256:" + path)
	if err != nil || len(path) != 64 {
		http.NotFound(writer, request)
		return
	}
	if commit {
		if request.Method != http.MethodPost {
			methodNotAllowed(writer)
			return
		}
		if request.Header.Get("Content-Type") != environmentartifact.ManifestMediaType || request.ContentLength < 0 || request.ContentLength > maxManifestBytes {
			http.Error(writer, "invalid manifest content metadata", http.StatusBadRequest)
			return
		}
		content, err := readExactBody(request.Body, request.ContentLength, maxManifestBytes)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		manifest, err := environmentartifact.DecodeManifest(content)
		if err != nil || manifest.ArtifactDigest != digest {
			http.Error(writer, "manifest URL identity mismatch", http.StatusUnprocessableEntity)
			return
		}
		if err := provider.CommitManifest(request.Context(), content); err != nil {
			http.Error(writer, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		writer.WriteHeader(http.StatusCreated)
		return
	}
	if request.Method != http.MethodGet {
		methodNotAllowed(writer)
		return
	}
	content, err := provider.ResolveManifest(request.Context(), digest)
	if err != nil {
		writeProviderReadError(writer, request, err)
		return
	}
	writer.Header().Set("Content-Type", environmentartifact.ManifestMediaType)
	writer.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
	_, _ = writer.Write(content)
}

func digestFromExactPath(path, prefix string) (environmentartifact.Digest, bool) {
	hex := strings.TrimPrefix(path, prefix)
	if len(hex) != 64 || strings.Contains(hex, "/") {
		return environmentartifact.Digest{}, false
	}
	digest, err := environmentartifact.ParseDigest("sha256:" + hex)
	return digest, err == nil
}

func methodNotAllowed(writer http.ResponseWriter) {
	http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
}

func writeProviderReadError(writer http.ResponseWriter, request *http.Request, err error) {
	if os.IsNotExist(err) {
		http.NotFound(writer, request)
		return
	}
	http.Error(writer, "provider content failed verification", http.StatusInternalServerError)
}

func writeProviderJSON(writer http.ResponseWriter, value any, err error) {
	if err != nil {
		http.Error(writer, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(value)
}

func decodeBoundedJSON(body io.Reader, declared, maximum int64, target any) error {
	if declared < 0 || declared > maximum {
		return fmt.Errorf("invalid JSON body size")
	}
	content, err := readExactBody(body, declared, maximum)
	if err != nil {
		return err
	}
	if err := rejectDuplicateHTTPJSON(content); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := requireHTTPJSONEOF(decoder); err != nil {
		return err
	}
	return nil
}

func rejectDuplicateHTTPJSON(content []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(content))
	if err := scanHTTPJSONValue(decoder); err != nil {
		return err
	}
	return requireHTTPJSONEOF(decoder)
}

func scanHTTPJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("JSON object key is not a string")
			}
			if _, found := seen[key]; found {
				return fmt.Errorf("duplicate JSON key %q", key)
			}
			seen[key] = struct{}{}
			if err := scanHTTPJSONValue(decoder); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := scanHTTPJSONValue(decoder); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delimiter)
	}
	_, err = decoder.Token()
	return err
}

func requireHTTPJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("trailing JSON value")
		}
		return err
	}
	return nil
}

func readExactBody(body io.Reader, declared, maximum int64) ([]byte, error) {
	if declared < 0 || declared > maximum {
		return nil, fmt.Errorf("invalid request body size")
	}
	content, err := io.ReadAll(io.LimitReader(body, declared+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) != declared {
		return nil, fmt.Errorf("request body does not match declared size")
	}
	return content, nil
}

func (it *HTTP) Capabilities(ctx context.Context) (Capabilities, error) {
	var result Capabilities
	err := it.doJSON(ctx, http.MethodGet, "/v1/capabilities", nil, &result)
	return result, err
}

func (it *HTTP) MissingObjects(ctx context.Context, descriptors []environmentartifact.Descriptor) ([]environmentartifact.Digest, error) {
	var result missingResponse
	err := it.doJSON(ctx, http.MethodPost, "/v1/objects/missing", missingRequest{Descriptors: descriptors}, &result)
	return result.Missing, err
}

func (it *HTTP) PutObject(ctx context.Context, blob Blob) error {
	if blob.Reader == nil || blob.Descriptor.Size < 0 || blob.Descriptor.Size > maxProviderObjectBytes || blob.Descriptor.MediaType == "" {
		return fmt.Errorf("invalid object upload")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, it.baseURL+"/v1/objects/sha256/"+blob.Descriptor.Digest.Hex(), io.LimitReader(blob.Reader, blob.Descriptor.Size+1))
	if err != nil {
		return err
	}
	request.ContentLength = blob.Descriptor.Size
	request.Header.Set("Content-Type", blob.Descriptor.MediaType)
	response, err := it.client.Do(request)
	return closeProviderResponse(response, err)
}

func (it *HTTP) GetObject(ctx context.Context, descriptor environmentartifact.Descriptor) (io.ReadCloser, error) {
	if descriptor.Size < 0 || descriptor.Size > maxProviderObjectBytes || len(descriptor.Digest.Hex()) != 64 {
		return nil, fmt.Errorf("invalid object descriptor")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, it.baseURL+"/v1/objects/sha256/"+descriptor.Digest.Hex(), nil)
	if err != nil {
		return nil, err
	}
	response, err := it.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if err := providerResponseError(response); err != nil {
		return nil, err
	}
	if response.Header.Get("Content-Type") != "application/octet-stream" {
		return nil, fmt.Errorf("unexpected object response Content-Type %q", response.Header.Get("Content-Type"))
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, descriptor.Size+1))
	if err != nil {
		return nil, err
	}
	if err := environmentartifact.VerifyDescriptor(descriptor, content); err != nil {
		return nil, fmt.Errorf("verify HTTP object response: %w", err)
	}
	return io.NopCloser(bytes.NewReader(content)), nil
}

func (it *HTTP) CommitManifest(ctx context.Context, content []byte) error {
	manifest, err := environmentartifact.DecodeManifest(content)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, it.baseURL+"/v1/manifests/sha256/"+manifest.ArtifactDigest.Hex()+"/commit", bytes.NewReader(content))
	if err != nil {
		return err
	}
	request.ContentLength = int64(len(content))
	request.Header.Set("Content-Type", environmentartifact.ManifestMediaType)
	response, err := it.client.Do(request)
	return closeProviderResponse(response, err)
}

func (it *HTTP) ResolveManifest(ctx context.Context, digest environmentartifact.Digest) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, it.baseURL+"/v1/manifests/sha256/"+digest.Hex(), nil)
	if err != nil {
		return nil, err
	}
	response, err := it.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if err := providerResponseError(response); err != nil {
		return nil, err
	}
	if response.Header.Get("Content-Type") != environmentartifact.ManifestMediaType {
		return nil, fmt.Errorf("unexpected manifest response Content-Type %q", response.Header.Get("Content-Type"))
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, maxManifestBytes+1))
	if err != nil {
		return nil, err
	}
	manifest, err := environmentartifact.DecodeManifest(content)
	if err != nil || manifest.ArtifactDigest != digest {
		return nil, fmt.Errorf("verify HTTP manifest response: %w", err)
	}
	return content, nil
}

func (it *HTTP) doJSON(ctx context.Context, method, path string, input, output any) error {
	var body io.Reader
	if input != nil {
		content, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(content)
	}
	request, err := http.NewRequestWithContext(ctx, method, it.baseURL+path, body)
	if err != nil {
		return err
	}
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := it.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if err := providerResponseError(response); err != nil {
		return err
	}
	if response.Header.Get("Content-Type") != "application/json" {
		return fmt.Errorf("unexpected JSON response Content-Type %q", response.Header.Get("Content-Type"))
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, maxProviderJSONBytes+1))
	if err != nil {
		return err
	}
	if len(content) > maxProviderJSONBytes {
		return fmt.Errorf("JSON response exceeds maximum size")
	}
	if err := rejectDuplicateHTTPJSON(content); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return err
	}
	return requireHTTPJSONEOF(decoder)
}

func closeProviderResponse(response *http.Response, err error) error {
	if err != nil {
		return err
	}
	defer response.Body.Close()
	return providerResponseError(response)
}

func providerResponseError(response *http.Response) error {
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return nil
	}
	content, _ := io.ReadAll(io.LimitReader(response.Body, maxProviderErrorBytes))
	return fmt.Errorf("artifact provider HTTP %s: %s", response.Status, strings.TrimSpace(string(content)))
}

var _ Provider = (*HTTP)(nil)
