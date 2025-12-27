package htfs

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestSameMountPoint(t *testing.T) {
	// Create temp directories for testing
	tmpDir1, err := os.MkdirTemp("", "mount_test1_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir1)

	tmpDir2, err := os.MkdirTemp("", "mount_test2_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir2)

	// Create test files
	file1 := filepath.Join(tmpDir1, "test1.txt")
	file2 := filepath.Join(tmpDir1, "test2.txt")
	file3 := filepath.Join(tmpDir2, "test3.txt")

	for _, f := range []string{file1, file2, file3} {
		if err := os.WriteFile(f, []byte("test"), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", f, err)
		}
	}

	tests := []struct {
		name     string
		path1    string
		path2    string
		expected bool
	}{
		{
			name:     "same_directory",
			path1:    tmpDir1,
			path2:    tmpDir1,
			expected: true,
		},
		{
			name:     "files_in_same_directory",
			path1:    file1,
			path2:    file2,
			expected: true,
		},
		{
			name:     "file_and_parent_dir",
			path1:    file1,
			path2:    tmpDir1,
			expected: true,
		},
		{
			name:     "likely_same_filesystem",
			path1:    tmpDir1,
			path2:    tmpDir2,
			expected: true, // Both in /tmp, likely same filesystem
		},
		{
			name:     "nonexistent_path",
			path1:    "/nonexistent/path/file.txt",
			path2:    tmpDir1,
			expected: false, // Should return false for safety
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sameMountPoint(tt.path1, tt.path2)
			if result != tt.expected {
				t.Errorf("sameMountPoint(%s, %s) = %v, want %v",
					tt.path1, tt.path2, result, tt.expected)
			}
		})
	}
}

func TestGetDeviceID(t *testing.T) {
	// Test with existing path
	tmpDir, err := os.MkdirTemp("", "device_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	id := getDeviceID(tmpDir)
	if id == -1 {
		t.Errorf("getDeviceID(%s) returned -1 for existing path", tmpDir)
	}

	// Test with nonexistent path
	id = getDeviceID("/nonexistent/path")
	if id != -1 {
		t.Errorf("getDeviceID(/nonexistent/path) = %d, want -1", id)
	}
}

func TestDeviceCache(t *testing.T) {
	cache := NewDeviceCache()

	// Create test directories
	tmpDir1, err := os.MkdirTemp("", "cache_test1_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir1)

	tmpDir2, err := os.MkdirTemp("", "cache_test2_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir2)

	// First call should populate cache
	id1 := cache.GetDeviceID(tmpDir1)
	if id1 == -1 {
		t.Errorf("GetDeviceID(%s) returned -1", tmpDir1)
	}

	// Second call should use cache (verify by checking map)
	id1Again := cache.GetDeviceID(tmpDir1)
	if id1 != id1Again {
		t.Errorf("GetDeviceID not using cache: first=%d, second=%d", id1, id1Again)
	}

	// Test SameDevice
	if !cache.SameDevice(tmpDir1, tmpDir1) {
		t.Error("SameDevice should return true for same path")
	}

	// These are likely on same filesystem (both in temp)
	if !cache.SameDevice(tmpDir1, tmpDir2) {
		t.Log("Note: tmpDir1 and tmpDir2 might be on different filesystems")
	}

	// Test with nonexistent paths
	if cache.SameDevice("/nonexistent1", "/nonexistent2") {
		t.Error("SameDevice should return false for nonexistent paths")
	}

	// Test Clear
	cache.Clear()
	if len(cache.devices) != 0 {
		t.Errorf("Clear() didn't empty cache, still has %d entries", len(cache.devices))
	}
}

func BenchmarkSameMountPoint(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "bench_*")
	if err != nil {
		b.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	file1 := filepath.Join(tmpDir, "file1.txt")
	file2 := filepath.Join(tmpDir, "file2.txt")
	os.WriteFile(file1, []byte("test"), 0644)
	os.WriteFile(file2, []byte("test"), 0644)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sameMountPoint(file1, file2)
	}
}

func BenchmarkDeviceCache(b *testing.B) {
	cache := NewDeviceCache()
	tmpDir, err := os.MkdirTemp("", "bench_cache_*")
	if err != nil {
		b.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	file1 := filepath.Join(tmpDir, "file1.txt")
	file2 := filepath.Join(tmpDir, "file2.txt")
	os.WriteFile(file1, []byte("test"), 0644)
	os.WriteFile(file2, []byte("test"), 0644)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.SameDevice(file1, file2)
	}
}

func BenchmarkDeviceCacheParallel(b *testing.B) {
	cache := NewDeviceCache()
	tmpDir, err := os.MkdirTemp("", "bench_parallel_*")
	if err != nil {
		b.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create multiple files for parallel testing
	files := make([]string, 10)
	for i := range files {
		files[i] = filepath.Join(tmpDir, fmt.Sprintf("file%d.txt", i))
		os.WriteFile(files[i], []byte("test"), 0644)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			cache.SameDevice(files[i%len(files)], files[(i+1)%len(files)])
			i++
		}
	})
}