package operations

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joshyorko/rcc/xviper"
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

func TestSummonCacheTreatsOnlyMissingFilesAsEmpty(t *testing.T) {
	t.Run("missing cache", func(t *testing.T) {
		cacheFile := configureCacheTestLocation(t)

		cache, err := SummonCache()
		if err != nil {
			t.Fatalf("SummonCache returned error for a missing cache: %v", err)
		}
		if cache == nil || len(cache.Credentials) != 0 || len(cache.Stamps) != 0 {
			t.Fatalf("SummonCache returned unexpected empty cache: %#v", cache)
		}
		if _, err := os.Stat(cacheFile); !os.IsNotExist(err) {
			t.Fatalf("SummonCache created cache file on read: %v", err)
		}
	})

	t.Run("readable cache", func(t *testing.T) {
		cacheFile := configureCacheTestLocation(t)
		content := []byte("credentials:\n  retained:\n    account: robot\n    token: secret\n")
		if err := os.WriteFile(cacheFile, content, 0o600); err != nil {
			t.Fatal(err)
		}

		cache, err := SummonCache()
		if err != nil {
			t.Fatalf("SummonCache returned error for a readable cache: %v", err)
		}
		if credential := cache.Credentials["retained"]; credential == nil || credential.Token != "secret" {
			t.Fatalf("SummonCache did not retain the credential: %#v", cache.Credentials)
		}
	})
}

func TestSummonCachePreservesUnreadableWritableFile(t *testing.T) {
	cacheFile := configureCacheTestLocation(t)
	content := []byte("credentials:\n  retained:\n    account: robot\n    token: secret\n")
	if err := os.WriteFile(cacheFile, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(cacheFile, 0o200); err != nil {
		t.Skipf("cannot set unreadable cache mode: %v", err)
	}
	probe, err := os.Open(cacheFile)
	if err == nil {
		_ = probe.Close()
		t.Skip("test process can read a mode-0200 cache")
	}

	cache, err := SummonCache()
	if err == nil || cache != nil {
		t.Fatalf("SummonCache returned cache %#v, error %v for an unreadable file", cache, err)
	}

	if err := os.Chmod(cacheFile, 0o600); err != nil {
		t.Fatal(err)
	}
	persisted, err := os.ReadFile(cacheFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(persisted, content) {
		t.Fatalf("unreadable cache changed after SummonCache: got %q, want %q", persisted, content)
	}
}

func configureCacheTestLocation(t *testing.T) string {
	t.Helper()
	previous := xviper.ConfigFileUsed()
	_, lockFileExists := os.Stat(".lck")
	root := t.TempDir()
	configFile := filepath.Join(root, "rcc.yaml")
	if err := os.WriteFile(configFile, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	xviper.SetConfigFile(configFile)
	t.Cleanup(func() {
		xviper.SetConfigFile(previous)
		_ = xviper.ConfigFileUsed()
		if previous == "" && os.IsNotExist(lockFileExists) {
			_ = os.Remove(".lck")
		}
	})
	if got := xviper.ConfigFileUsed(); got != configFile {
		t.Fatalf("configuration file = %q, want %q", got, configFile)
	}
	return filepath.Join(root, "rcccache.yaml")
}
