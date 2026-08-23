package artifactprovider

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joshyorko/rcc/environmentartifact"
)

const (
	maxProviderJSONBytes   = 4 << 20
	maxProviderObjectBytes = int64(64 << 30)
	maxProviderErrorBytes  = 8 << 10
)

type HTTP struct {
	baseURL   string
	client    *http.Client
	userAgent string
}

type HTTPOptions struct {
	Client           *http.Client
	AuthorizationEnv string
	ProxyURL         string
	NoProxy          string
	CAFile           string
	CAPEM            []byte
	TLSMinVersion    uint16
	Timeout          time.Duration
	UserAgent        string
}

type authorizationTransport struct {
	base http.RoundTripper
	env  string
}

func (t authorizationTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	value, ok := os.LookupEnv(t.env)
	if !ok {
		return nil, fmt.Errorf("authorization environment variable %q is not set", t.env)
	}
	clone := request.Clone(request.Context())
	clone.Header.Set("Authorization", value)
	return t.base.RoundTrip(clone)
}

type missingRequest struct {
	Descriptors []environmentartifact.Descriptor `json:"descriptors"`
}

type missingResponse struct {
	Missing []environmentartifact.Digest `json:"missing"`
}

func NewHTTP(baseURL string, client *http.Client) (*HTTP, error) {
	return NewHTTPWithOptions(baseURL, HTTPOptions{Client: client})
}

func NewHTTPWithOptions(raw string, options HTTPOptions) (*HTTP, error) {
	baseURL, err := NormalizeHTTPURL(raw)
	if err != nil {
		return nil, err
	}
	base := options.Client
	if base == nil {
		base = &http.Client{}
	}
	clone := *base
	if options.Timeout > 0 {
		clone.Timeout = options.Timeout
	}
	clone.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	transport := clone.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	if options.ProxyURL != "" || options.NoProxy != "" || options.CAFile != "" || len(options.CAPEM) != 0 || options.TLSMinVersion != 0 {
		baseTransport, ok := transport.(*http.Transport)
		if !ok {
			return nil, fmt.Errorf("provider transport does not support TLS/proxy configuration")
		}
		configured := baseTransport.Clone()
		if options.ProxyURL != "" {
			proxy, err := url.Parse(options.ProxyURL)
			if err != nil || proxy.Scheme == "" || proxy.Host == "" {
				return nil, fmt.Errorf("invalid provider proxy URL")
			}
			configured.Proxy = http.ProxyURL(proxy)
		}
		if options.NoProxy != "" {
			configured.Proxy = func(request *http.Request) (*url.URL, error) {
				if providerNoProxyMatch(request.URL, options.NoProxy) {
					return nil, nil
				}
				if options.ProxyURL == "" {
					return http.ProxyFromEnvironment(request)
				}
				proxy, err := url.Parse(options.ProxyURL)
				return proxy, err
			}
		}
		if options.CAFile != "" {
			pem, err := os.ReadFile(filepath.Clean(options.CAFile))
			if err != nil {
				return nil, fmt.Errorf("read provider CA file: %w", err)
			}
			options.CAPEM = append(options.CAPEM, pem...)
		}
		if len(options.CAPEM) != 0 {
			pool, err := x509.SystemCertPool()
			if err != nil {
				pool = x509.NewCertPool()
			}
			if !pool.AppendCertsFromPEM(options.CAPEM) {
				return nil, fmt.Errorf("provider CA PEM could not be parsed")
			}
			configured.TLSClientConfig = cloneTLS(configured.TLSClientConfig)
			configured.TLSClientConfig.RootCAs = pool
		}
		if options.TLSMinVersion != 0 {
			configured.TLSClientConfig = cloneTLS(configured.TLSClientConfig)
			configured.TLSClientConfig.MinVersion = options.TLSMinVersion
		}
		transport = configured
	}
	if options.AuthorizationEnv != "" {
		clone.Transport = authorizationTransport{base: transport, env: options.AuthorizationEnv}
	} else {
		clone.Transport = transport
	}
	return &HTTP{baseURL: baseURL, client: &clone, userAgent: options.UserAgent}, nil
}

func providerNoProxyMatch(target *url.URL, raw string) bool {
	host := strings.ToLower(target.Hostname())
	ip := net.ParseIP(host)
	for _, rawToken := range strings.Split(raw, ",") {
		token := strings.TrimSpace(strings.ToLower(rawToken))
		if token == "" {
			continue
		}
		if token == "*" {
			return true
		}
		port := ""
		if h, p, err := net.SplitHostPort(token); err == nil {
			token, port = h, p
		} else if strings.HasPrefix(token, "[") && strings.Contains(token, "]:") {
			end := strings.IndexByte(token, ']')
			token, port = token[1:end], token[end+2:]
		} else if strings.Count(token, ":") == 1 {
			parts := strings.SplitN(token, ":", 2)
			token, port = parts[0], parts[1]
		}
		if port != "" && port != target.Port() {
			continue
		}
		token = strings.Trim(token, "[]")
		if _, cidr, err := net.ParseCIDR(token); err == nil {
			if ip != nil && cidr.Contains(ip) {
				return true
			}
			continue
		}
		if strings.HasPrefix(token, ".") {
			domain := strings.TrimPrefix(token, ".")
			if host == domain || strings.HasSuffix(host, "."+domain) {
				return true
			}
		} else if token == host {
			return true
		}
	}
	return false
}

func cloneTLS(config *tls.Config) *tls.Config {
	if config == nil {
		return &tls.Config{}
	}
	return config.Clone()
}

type HandlerOptions struct{ RequestTimeout time.Duration }

func NewHandler(provider Provider) http.Handler {
	return NewHandlerWithOptions(provider, HandlerOptions{})
}

func NewHandlerWithOptions(provider Provider, options HandlerOptions) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if options.RequestTimeout > 0 {
			ctx, cancel := context.WithTimeout(request.Context(), options.RequestTimeout)
			defer cancel()
			request = request.Clone(ctx)
		}
		if provider == nil || request.URL.RawPath != "" || request.URL.RawQuery != "" {
			http.Error(writer, "invalid provider request", http.StatusBadRequest)
			return
		}
		if request.ContentLength >= 0 && len(request.Header.Values("Content-Length")) > 1 || (request.ContentLength >= 0 && len(request.TransferEncoding) > 0) {
			writeProviderFailure(writer, http.StatusBadRequest)
			return
		}
		if request.Method == http.MethodGet && request.ContentLength != 0 {
			http.Error(writer, "GET requests must not contain a body", http.StatusBadRequest)
			return
		}
		handleProviderRequest(provider, writer, request)
	})
}

func handleProviderRequest(provider Provider, writer http.ResponseWriter, request *http.Request) {
	ctx := request.Context()
	switch {
	case request.URL.Path == "/v1/health":
		if request.Method != http.MethodGet {
			methodNotAllowed(writer)
			return
		}
		hp, ok := provider.(HealthProvider)
		if !ok {
			http.Error(writer, "health unavailable", http.StatusNotImplemented)
			return
		}
		health, err := hp.Health(ctx)
		writeProviderJSON(writer, health, err)
	case request.URL.Path == "/v1/capabilities":
		if request.Method != http.MethodGet {
			methodNotAllowed(writer)
			return
		}
		capabilities, err := provider.Capabilities(ctx)
		writeProviderJSON(writer, capabilities, err)
	case request.URL.Path == "/v1/protocol":
		if request.Method != http.MethodGet {
			methodNotAllowed(writer)
			return
		}
		caps, err := provider.Capabilities(ctx)
		selected := 0
		restart := "unsafe"
		if err == nil {
			selected = 1
			if caps.SafeRestart {
				restart = "safe"
			}
		}
		transfer := "full-restart-only"
		if err == nil && (caps.RangeSupport || caps.ResumeSupport) {
			transfer = "resumable"
		}
		writeProviderJSON(writer, ProtocolCapabilities{Protocol: "rcc.artifact.v1", Versions: []int{1}, SelectedVersion: selected, Extensions: []string{"rcc.artifact.v1/admin", "rcc.artifact.v1/backup", "rcc.artifact.v1/restore"}, AuthRequired: false, RestartOutcome: restart, TransferOutcome: transfer, RetentionPolicy: "caller-selected", Immutability: "content-addressed", Capabilities: caps}, err)
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
			writeProviderFailure(writer, http.StatusBadRequest)
			return
		}
		if len(input.Descriptors) > 4096 {
			http.Error(writer, "descriptor fanout exceeds limit", http.StatusRequestEntityTooLarge)
			return
		}
		missing, err := provider.MissingObjects(ctx, input.Descriptors)
		writeProviderJSON(writer, missingResponse{Missing: missing}, err)
	case request.URL.Path == "/v1/admin/cleanup":
		admin, ok := provider.(ProviderV1Admin)
		if !ok {
			http.Error(writer, "admin unavailable", http.StatusNotImplemented)
			return
		}
		if request.Method != http.MethodPost {
			methodNotAllowed(writer)
			return
		}
		n, err := admin.Cleanup(ctx)
		writeProviderJSON(writer, map[string]int{"removed": n}, err)
	case request.URL.Path == "/v1/admin/objects" || request.URL.Path == "/v1/admin/manifests":
		admin, ok := provider.(ProviderV1Enumerable)
		if !ok {
			http.Error(writer, "enumeration unavailable", http.StatusNotImplemented)
			return
		}
		if request.Method != http.MethodGet {
			methodNotAllowed(writer)
			return
		}
		if request.URL.Path == "/v1/admin/objects" {
			objects, err := admin.ListObjects(ctx)
			writeProviderJSON(writer, objects, err)
		} else {
			manifests, err := admin.ListManifests(ctx)
			writeProviderJSON(writer, manifests, err)
		}
	case request.URL.Path == "/v1/admin/audit":
		audit, ok := provider.(ProviderV1Audit)
		if !ok {
			http.Error(writer, "audit unavailable", http.StatusNotImplemented)
			return
		}
		if request.Method != http.MethodGet {
			methodNotAllowed(writer)
			return
		}
		records, err := audit.Audit(ctx)
		writeProviderJSON(writer, records, err)
	case request.URL.Path == "/v1/admin/gc":
		admin, ok := provider.(ProviderV1Admin)
		if !ok {
			http.Error(writer, "admin unavailable", http.StatusNotImplemented)
			return
		}
		if request.Method != http.MethodPost {
			methodNotAllowed(writer)
			return
		}
		var input struct {
			MaxAgeSeconds int64 `json:"maxAgeSeconds"`
			KeepManifests int   `json:"keepManifests"`
		}
		if err := decodeBoundedJSON(request.Body, request.ContentLength, maxProviderJSONBytes, &input); err != nil {
			writeProviderFailure(writer, http.StatusBadRequest)
			return
		}
		report, err := admin.GarbageCollect(ctx, Retention{MaxAge: time.Duration(input.MaxAgeSeconds) * time.Second, KeepManifests: input.KeepManifests})
		writeProviderJSON(writer, report, err)
	case request.URL.Path == "/v1/admin/repair":
		admin, ok := provider.(ProviderV1Admin)
		if !ok {
			http.Error(writer, "admin unavailable", http.StatusNotImplemented)
			return
		}
		if request.Method != http.MethodPost {
			methodNotAllowed(writer)
			return
		}
		health, err := admin.Repair(ctx)
		writeProviderJSON(writer, health, err)
	case request.URL.Path == "/v1/admin/backup":
		backup, ok := provider.(ProviderV1Backup)
		if !ok {
			http.Error(writer, "backup unavailable", http.StatusNotImplemented)
			return
		}
		if request.Method != http.MethodGet {
			methodNotAllowed(writer)
			return
		}
		writer.Header().Set("Content-Type", "application/x-tar")
		if err := backup.Backup(ctx, writer); err != nil {
			return
		}
	case request.URL.Path == "/v1/admin/restore":
		backup, ok := provider.(ProviderV1Backup)
		if !ok {
			http.Error(writer, "restore unavailable", http.StatusNotImplemented)
			return
		}
		if request.Method != http.MethodPost {
			methodNotAllowed(writer)
			return
		}
		if request.ContentLength < 0 || request.ContentLength > maxProviderArchiveBytes {
			http.Error(writer, "invalid restore size", http.StatusBadRequest)
			return
		}
		if err := backup.Restore(ctx, io.LimitReader(request.Body, request.ContentLength+1)); err != nil {
			writeProviderFailure(writer, http.StatusUnprocessableEntity)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	case strings.HasPrefix(request.URL.Path, "/v1/objects/sha256/"):
		digest, ok := digestFromExactPath(request.URL.Path, "/v1/objects/sha256/")
		if !ok {
			http.NotFound(writer, request)
			return
		}
		switch request.Method {
		case http.MethodPut:
			mediaType := request.Header.Get("Content-Type")
			if !validProviderMediaType(mediaType) || request.ContentLength < 0 || request.ContentLength > maxProviderObjectBytes {
				http.Error(writer, "invalid object content metadata", http.StatusBadRequest)
				return
			}
			descriptor := environmentartifact.Descriptor{MediaType: mediaType, Digest: digest, Size: request.ContentLength}
			err := provider.PutObject(ctx, Blob{Descriptor: descriptor, Reader: io.LimitReader(request.Body, request.ContentLength+1)})
			if err != nil {
				writeProviderFailure(writer, http.StatusUnprocessableEntity)
				return
			}
			writer.WriteHeader(http.StatusCreated)
		case http.MethodGet:
			if request.Header.Get("Range") != "" {
				writer.Header().Set("Accept-Ranges", "none")
				http.Error(writer, "range requests unsupported; restart the full object", http.StatusRequestedRangeNotSatisfiable)
				return
			}
			readerProvider, ok := provider.(ObjectReaderProvider)
			if !ok {
				http.Error(writer, "object reads unavailable", http.StatusNotImplemented)
				return
			}
			reader, size, err := readerProvider.GetObjectByDigest(ctx, digest)
			if err != nil {
				writeProviderReadError(writer, request, err)
				return
			}
			defer reader.Close()
			writer.Header().Set("Content-Type", "application/octet-stream")
			writer.Header().Set("Content-Length", fmt.Sprintf("%d", size))
			_, _ = io.CopyN(writer, reader, size)
		default:
			methodNotAllowed(writer)
		}
	case strings.HasPrefix(request.URL.Path, "/v1/manifests/sha256/"):
		handleManifestRequest(provider, writer, request)
	default:
		http.NotFound(writer, request)
	}
}

func validProviderMediaType(value string) bool {
	if value == "" || len(value) > 256 || strings.ContainsAny(value, "\r\n") {
		return false
	}
	parsed, _, err := mime.ParseMediaType(value)
	return err == nil && parsed == value
}

func handleManifestRequest(provider Provider, writer http.ResponseWriter, request *http.Request) {
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
			writeProviderFailure(writer, http.StatusBadRequest)
			return
		}
		manifest, err := environmentartifact.DecodeManifest(content)
		if err != nil || manifest.ArtifactDigest != digest {
			http.Error(writer, "manifest URL identity mismatch", http.StatusUnprocessableEntity)
			return
		}
		if err := provider.CommitManifest(request.Context(), content); err != nil {
			writeProviderFailure(writer, http.StatusUnprocessableEntity)
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
		writeProviderFailure(writer, http.StatusUnprocessableEntity)
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(value)
}

func writeProviderFailure(writer http.ResponseWriter, status int) {
	http.Error(writer, "artifact provider request failed", status)
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
	if err == nil {
		err = ValidateCapabilities(result)
	}
	return result, err
}

func (it *HTTP) Protocol(ctx context.Context) (ProtocolCapabilities, error) {
	var result ProtocolCapabilities
	err := it.doJSON(ctx, http.MethodGet, "/v1/protocol", nil, &result)
	if err == nil && (result.Protocol != "rcc.artifact.v1" || len(result.Versions) == 0 || len(result.Versions) > 8 || !contains(result.Versions, 1) || result.SelectedVersion != 1 || (result.AuthRequired && result.AuthChallenge == "") || result.RestartOutcome != "safe" || result.Immutability != "content-addressed" || result.TransferOutcome != "full-restart-only") {
		err = fmt.Errorf("unsupported artifact provider protocol")
	}
	if err == nil {
		err = ValidateCapabilities(result.Capabilities)
	}
	return result, err
}

func (it *HTTP) NegotiateCapabilities(ctx context.Context, required Capabilities) (Capabilities, error) {
	protocol, err := it.Protocol(ctx)
	if err != nil {
		return Capabilities{}, err
	}
	if err := ValidateCapabilityIntersection(protocol.Capabilities, required); err != nil {
		return Capabilities{}, err
	}
	return protocol.Capabilities, nil
}

func (it *HTTP) Health(ctx context.Context) (Health, error) {
	var result Health
	err := it.doJSON(ctx, http.MethodGet, "/v1/health", nil, &result)
	return result, err
}

func (it *HTTP) MissingObjects(ctx context.Context, descriptors []environmentartifact.Descriptor) ([]environmentartifact.Digest, error) {
	var result missingResponse
	err := it.doJSON(ctx, http.MethodPost, "/v1/objects/missing", missingRequest{Descriptors: descriptors}, &result)
	return result.Missing, err
}

func (it *HTTP) PutObject(ctx context.Context, blob Blob) error {
	if blob.Reader == nil || blob.Descriptor.Size < 0 || blob.Descriptor.Size > maxProviderObjectBytes || !validProviderMediaType(blob.Descriptor.MediaType) {
		return fmt.Errorf("invalid object upload")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, it.baseURL+"/v1/objects/sha256/"+blob.Descriptor.Digest.Hex(), io.LimitReader(blob.Reader, blob.Descriptor.Size+1))
	if err != nil {
		return err
	}
	request.ContentLength = blob.Descriptor.Size
	request.Header.Set("Content-Type", blob.Descriptor.MediaType)
	it.setHeaders(request)
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
	it.setHeaders(request)
	response, err := it.client.Do(request)
	if err != nil {
		return nil, err
	}
	if err := providerResponseError(response); err != nil {
		_ = response.Body.Close()
		return nil, err
	}
	if response.Header.Get("Content-Type") != "application/octet-stream" {
		_ = response.Body.Close()
		return nil, fmt.Errorf("unexpected object response Content-Type %q", response.Header.Get("Content-Type"))
	}
	if response.ContentLength >= 0 && response.ContentLength != descriptor.Size {
		_ = response.Body.Close()
		return nil, fmt.Errorf("verify HTTP object response: content length %d does not match descriptor size %d", response.ContentLength, descriptor.Size)
	}
	return &verifiedObjectReader{
		body:   response.Body,
		hash:   sha256.New(),
		digest: descriptor.Digest.Hex(),
		size:   descriptor.Size,
	}, nil
}

type verifiedObjectReader struct {
	body     io.ReadCloser
	hash     hash.Hash
	digest   string
	size     int64
	read     int64
	verified bool
}

func (it *verifiedObjectReader) Read(target []byte) (int, error) {
	if it.verified {
		return 0, io.EOF
	}
	if it.read == it.size {
		return it.verify()
	}
	limit := it.size - it.read
	if int64(len(target)) > limit {
		target = target[:limit]
	}
	count, err := it.body.Read(target)
	if count > 0 {
		it.read += int64(count)
		_, _ = it.hash.Write(target[:count])
	}
	if it.read > it.size {
		return count, fmt.Errorf("verify HTTP object response: content exceeds descriptor size")
	}
	if err == io.EOF && it.read != it.size {
		return count, fmt.Errorf("verify HTTP object response: content ended at %d bytes, expected %d", it.read, it.size)
	}
	if err == io.EOF {
		_, verifyErr := it.verify()
		return count, verifyErr
	}
	return count, err
}

func (it *verifiedObjectReader) verify() (int, error) {
	if it.verified {
		return 0, io.EOF
	}
	it.verified = true
	if hex.EncodeToString(it.hash.Sum(nil)) != it.digest {
		return 0, fmt.Errorf("verify HTTP object response: digest mismatch")
	}
	return 0, io.EOF
}

func (it *verifiedObjectReader) Close() error { return it.body.Close() }

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
	it.setHeaders(request)
	response, err := it.client.Do(request)
	return closeProviderResponse(response, err)
}

func (it *HTTP) ResolveManifest(ctx context.Context, digest environmentartifact.Digest) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, it.baseURL+"/v1/manifests/sha256/"+digest.Hex(), nil)
	if err != nil {
		return nil, err
	}
	it.setHeaders(request)
	response, err := it.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
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
	it.setHeaders(request)
	response, err := it.client.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
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

func (it *HTTP) setHeaders(request *http.Request) {
	if it.userAgent != "" {
		request.Header.Set("User-Agent", it.userAgent)
	}
}

func closeProviderResponse(response *http.Response, err error) error {
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	return providerResponseError(response)
}

func providerResponseError(response *http.Response) error {
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return nil
	}
	return fmt.Errorf("artifact provider HTTP %s", response.Status)
}

var _ Provider = (*HTTP)(nil)
