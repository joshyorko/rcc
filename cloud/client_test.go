package cloud_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joshyorko/rcc/cloud"
	"github.com/joshyorko/rcc/hamlet"
)

func assertDownloadFailurePreservesDestination(t *testing.T, handler http.HandlerFunc) {
	t.Helper()

	server := httptest.NewServer(handler)
	defer server.Close()

	directory := t.TempDir()
	destination := filepath.Join(directory, "download.bin")
	original := []byte("original content")
	if err := os.WriteFile(destination, original, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := cloud.Download(server.URL, destination); err == nil {
		t.Fatal("download unexpectedly succeeded")
	}
	content, err := os.ReadFile(destination)
	if err != nil {
		t.Fatalf("existing destination was removed: %v", err)
	}
	if string(content) != string(original) {
		t.Fatalf("existing destination was replaced with %q", content)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(destination) {
		t.Fatalf("download left temporary files behind: %v", entries)
	}
}

func TestDownloadPreservesExistingDestinationOnHTTPError(t *testing.T) {
	assertDownloadFailurePreservesDestination(t, func(response http.ResponseWriter, request *http.Request) {
		http.Error(response, "failed", http.StatusInternalServerError)
	})
}

func TestDownloadPreservesExistingDestinationOnInterruptedBody(t *testing.T) {
	assertDownloadFailurePreservesDestination(t, func(response http.ResponseWriter, request *http.Request) {
		connection, buffer, _ := response.(http.Hijacker).Hijack()
		defer connection.Close()
		fmt.Fprint(buffer, "HTTP/1.1 200 OK\r\nContent-Length: 100\r\n\r\npartial")
		buffer.Flush()
	})
}

func TestCannotCreateClientForBadEndpoint(t *testing.T) {
	must_be, wont_be := hamlet.Specifications(t)

	sut, err := cloud.NewClient("http://some.server.com/endpoint")
	must_be.Nil(sut)
	wont_be.Nil(err)
	must_be.True(strings.HasPrefix(err.Error(), "Endpoint '"))

	sut, err = cloud.NewClient("some.server.com/endpoint")
	must_be.Nil(sut)
	wont_be.Nil(err)
	must_be.True(strings.HasPrefix(err.Error(), "Endpoint '"))
}

func TestCanCreateClient(t *testing.T) {
	must_be, wont_be := hamlet.Specifications(t)

	sut, err := cloud.NewClient("https://some.server.com/endpoint")
	wont_be.Nil(sut)
	must_be.Nil(err)
}

func TestCanEnsureHttps(t *testing.T) {
	must_be, wont_be := hamlet.Specifications(t)

	_, err := cloud.EnsureHttps("http://some.server.com/endpoint")
	wont_be.Nil(err)

	incoming := "https://some.server.com/endpoint"
	output, err := cloud.EnsureHttps(incoming)
	must_be.Nil(err)
	must_be.Equal(incoming, output)

	special := "http://127.0.0.1:8192/endpoint"
	output, err = cloud.EnsureHttps(special)
	must_be.Nil(err)
	must_be.Equal(special, output)
}
