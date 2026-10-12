package operations

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type assistantRoundTripFunc func(*http.Request) (*http.Response, error)

func (it assistantRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return it(request)
}

type assistantTrackingBody struct {
	io.Reader
	closed   bool
	closeErr error
}

func (it *assistantTrackingBody) Close() error {
	it.closed = true
	return it.closeErr
}

func TestMultipartUploadClosesResponseBody(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "artifact.txt")
	if err := os.WriteFile(filename, []byte("artifact contents"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name       string
		statusCode int
		body       string
		closeErr   error
		wantErrors []string
	}{
		{name: "success", statusCode: http.StatusCreated},
		{name: "error status", statusCode: http.StatusForbidden, body: "upload denied", wantErrors: []string{"403", "upload denied"}},
		{name: "body close error", statusCode: http.StatusCreated, closeErr: errors.New("response close failed"), wantErrors: []string{"response close failed"}},
		{
			name:       "status and body close errors",
			statusCode: http.StatusForbidden,
			body:       "upload denied",
			closeErr:   errors.New("response close failed"),
			wantErrors: []string{"403", "upload denied", "response close failed"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			responseBody := &assistantTrackingBody{Reader: strings.NewReader(test.body), closeErr: test.closeErr}
			client := &http.Client{Transport: assistantRoundTripFunc(func(request *http.Request) (*http.Response, error) {
				if request.Method != http.MethodPost {
					t.Errorf("request method = %q, want POST", request.Method)
				}
				reader, err := request.MultipartReader()
				if err != nil {
					t.Fatalf("request multipart reader failed: %v", err)
				}
				fields := make(map[string]string)
				var uploadedName, uploadedContents string
				for {
					part, err := reader.NextPart()
					if err == io.EOF {
						break
					}
					if err != nil {
						t.Fatalf("read multipart part: %v", err)
					}
					contents, err := io.ReadAll(part)
					if err != nil {
						t.Fatalf("read multipart contents: %v", err)
					}
					if part.FileName() != "" {
						uploadedName = part.FileName()
						uploadedContents = string(contents)
					} else {
						fields[part.FormName()] = string(contents)
					}
				}
				if fields["key"] != "value" {
					t.Errorf("multipart field key = %q, want %q", fields["key"], "value")
				}
				if uploadedName != filepath.Base(filename) || uploadedContents != "artifact contents" {
					t.Errorf("multipart file = (%q, %q), want (%q, %q)", uploadedName, uploadedContents, filepath.Base(filename), "artifact contents")
				}
				return &http.Response{
					StatusCode: test.statusCode,
					Status:     http.StatusText(test.statusCode),
					Body:       responseBody,
					Header:     make(http.Header),
					Request:    request,
				}, nil
			})}

			err := multipartUpload(client, "https://upload.invalid/artifacts", map[string]string{"key": "value"}, filepath.Base(filename), filename)
			if len(test.wantErrors) == 0 && err != nil {
				t.Fatalf("multipartUpload returned error: %v", err)
			}
			for _, want := range test.wantErrors {
				if err == nil || !strings.Contains(err.Error(), want) {
					t.Fatalf("multipartUpload error = %v, want it to contain %q", err, want)
				}
			}
			if !responseBody.closed {
				t.Fatal("multipartUpload left the response body open")
			}
		})
	}
}
