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
		wantError  string
	}{
		{name: "success", statusCode: http.StatusCreated},
		{name: "error status", statusCode: http.StatusForbidden, body: "upload denied", wantError: "upload denied"},
		{name: "body close error", statusCode: http.StatusCreated, closeErr: errors.New("response close failed"), wantError: "response close failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			responseBody := &assistantTrackingBody{Reader: strings.NewReader(test.body), closeErr: test.closeErr}
			client := &http.Client{Transport: assistantRoundTripFunc(func(request *http.Request) (*http.Response, error) {
				if request.Method != http.MethodPost {
					t.Errorf("request method = %q, want POST", request.Method)
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
			if test.wantError == "" {
				if err != nil {
					t.Fatalf("multipartUpload returned error: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("multipartUpload error = %v, want it to contain %q", err, test.wantError)
			}
			if !responseBody.closed {
				t.Fatal("multipartUpload left the response body open")
			}
		})
	}
}
