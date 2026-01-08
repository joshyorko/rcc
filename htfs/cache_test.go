package htfs

import (
	"path/filepath"
	"testing"
	"time"
)

func TestNewMetadataCache(t *testing.T) {
	cache := NewMetadataCache()
	if cache == nil {
		t.Fatal("NewMetadataCache returned nil")
	}
	if cache.Size() != 0 {
		t.Errorf("Expected empty cache, got size %d", cache.Size())
	}
}

func TestMetadataCacheClear(t *testing.T) {
	cache := NewMetadataCache()

	// Manually add some entries to test clearing
	cache.mu.Lock()
	cache.roots["test1"] = &Root{}
	cache.roots["test2"] = &Root{}
	cache.timestamps["test1"] = time.Now()
	cache.timestamps["test2"] = time.Now()
	cache.mu.Unlock()

	if cache.Size() != 2 {
		t.Errorf("Expected size 2, got %d", cache.Size())
	}

	cache.Clear()

	if cache.Size() != 0 {
		t.Errorf("Expected empty cache after Clear, got size %d", cache.Size())
	}
}

func TestMetadataCacheInvalidate(t *testing.T) {
	cache := NewMetadataCache()

	// Manually add entries
	cache.mu.Lock()
	cache.roots["test1"] = &Root{}
	cache.roots["test2"] = &Root{}
	cache.timestamps["test1"] = time.Now()
	cache.timestamps["test2"] = time.Now()
	cache.mu.Unlock()

	cache.Invalidate("test1")

	if cache.Size() != 1 {
		t.Errorf("Expected size 1 after invalidating one entry, got %d", cache.Size())
	}

	cache.mu.RLock()
	_, exists := cache.roots["test1"]
	cache.mu.RUnlock()

	if exists {
		t.Error("Expected test1 to be invalidated")
	}
}

func TestMetadataCacheGetOrLoadNonexistent(t *testing.T) {
	cache := NewMetadataCache()

	// Try to load a file that doesn't exist
	_, err := cache.GetOrLoad("/nonexistent/path.meta")
	if err == nil {
		t.Error("Expected error when loading nonexistent file")
	}
}

func TestMetadataCacheThreadSafety(t *testing.T) {
	cache := NewMetadataCache()
	done := make(chan bool)

	// Simulate concurrent access
	for i := 0; i < 10; i++ {
		go func() {
			cache.Size()
			cache.Clear()
			cache.Invalidate("test")
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestMetadataCacheGetOrLoadWithTempFile(t *testing.T) {
	cache := NewMetadataCache()

	// Create a temporary directory and file
	tmpDir := t.TempDir()
	testPath := filepath.Join(tmpDir, "test")
	metaPath := testPath + ".meta"

	// Create a minimal root structure and save it
	root, err := NewRoot(testPath)
	if err != nil {
		t.Fatalf("Failed to create root: %v", err)
	}

	err = root.SaveAs(metaPath)
	if err != nil {
		t.Fatalf("Failed to save root: %v", err)
	}

	// First load - should be a cache miss
	loaded1, err := cache.GetOrLoad(metaPath)
	if err != nil {
		t.Fatalf("Failed to load from cache: %v", err)
	}
	if loaded1 == nil {
		t.Fatal("GetOrLoad returned nil root")
	}

	if cache.Size() != 1 {
		t.Errorf("Expected cache size 1 after first load, got %d", cache.Size())
	}

	// Second load - should be a cache hit
	loaded2, err := cache.GetOrLoad(metaPath)
	if err != nil {
		t.Fatalf("Failed to load from cache on second attempt: %v", err)
	}
	if loaded2 != loaded1 {
		t.Error("Expected same root instance on cache hit")
	}

	// Modify the file
	time.Sleep(10 * time.Millisecond) // Ensure mtime changes
	root.Identity = "modified"
	err = root.SaveAs(metaPath)
	if err != nil {
		t.Fatalf("Failed to save modified root: %v", err)
	}

	// Third load - should detect modification and reload
	loaded3, err := cache.GetOrLoad(metaPath)
	if err != nil {
		t.Fatalf("Failed to load after modification: %v", err)
	}
	if loaded3 == loaded1 {
		t.Error("Expected different root instance after file modification")
	}
}

func TestMetadataCacheSizeThreadSafe(t *testing.T) {
	cache := NewMetadataCache()
	done := make(chan bool)

	// Add entries concurrently
	for i := 0; i < 5; i++ {
		go func(id int) {
			cache.mu.Lock()
			cache.roots[string(rune(id))] = &Root{}
			cache.mu.Unlock()
			done <- true
		}(i)
	}

	// Wait for all additions
	for i := 0; i < 5; i++ {
		<-done
	}

	// Read size concurrently
	for i := 0; i < 10; i++ {
		go func() {
			_ = cache.Size()
			done <- true
		}()
	}

	// Wait for all reads
	for i := 0; i < 10; i++ {
		<-done
	}
}
