package operations

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

type cacheTestWriter struct {
	bytes.Buffer
	writeErr error
	closeErr error
	closed   bool
}

func (it *cacheTestWriter) Write(value []byte) (int, error) {
	if it.writeErr != nil {
		return 0, it.writeErr
	}
	return it.Buffer.Write(value)
}

func (it *cacheTestWriter) Close() error {
	it.closed = true
	return it.closeErr
}

func TestEncodeCacheFlushesAndClosesOutput(t *testing.T) {
	writer := &cacheTestWriter{}
	value := &Cache{Credentials: CredentialMap{"robot": {Account: "robot"}}}

	if err := encodeCache(writer, value); err != nil {
		t.Fatalf("encodeCache returned error: %v", err)
	}
	if !writer.closed {
		t.Fatal("encodeCache did not close its writer")
	}
	if output := writer.String(); !strings.Contains(output, "credentials:") || !strings.Contains(output, "account: robot") {
		t.Fatalf("cache output was not flushed: %q", output)
	}
}

func TestEncodeCacheReturnsWriteAndCloseErrors(t *testing.T) {
	writeErr := errors.New("cache output failed")
	closeErr := errors.New("cache close failed")
	writer := &cacheTestWriter{writeErr: writeErr, closeErr: closeErr}

	err := encodeCache(writer, &Cache{})

	if err == nil || !strings.Contains(err.Error(), writeErr.Error()) || !strings.Contains(err.Error(), closeErr.Error()) {
		t.Fatalf("encodeCache error = %v, want write and close errors", err)
	}
	if !writer.closed {
		t.Fatal("encodeCache did not close output after a write failure")
	}
}
