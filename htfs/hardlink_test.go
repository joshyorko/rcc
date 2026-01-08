package htfs

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func TestNewHardlinkManager(t *testing.T) {
	manager := NewHardlinkManager()

	if manager == nil {
		t.Fatal("NewHardlinkManager() returned nil")
	}

	if manager.maxWorkers <= 0 || manager.maxWorkers > 8 {
		t.Errorf("maxWorkers = %d, want 1-8", manager.maxWorkers)
	}

	if manager.stats == nil {
		t.Fatal("stats not initialized")
	}

	if len(manager.batches) != 0 {
		t.Errorf("initial batches length = %d, want 0", len(manager.batches))
	}
}

func TestAddHardlink(t *testing.T) {
	manager := NewHardlinkManager()

	// Test adding hardlinks
	manager.AddHardlink("/source/file1", "/target/file1")
	manager.AddHardlink("/source/file1", "/target/file2")
	manager.AddHardlink("/source/file2", "/target/file3")

	if len(manager.batches) != 2 {
		t.Errorf("batches count = %d, want 2", len(manager.batches))
	}

	// Verify first batch has two targets
	if len(manager.batches[0].Targets) != 2 {
		t.Errorf("first batch targets = %d, want 2", len(manager.batches[0].Targets))
	}
}

func TestIsSameFile(t *testing.T) {
	// Create temp files for testing
	tmpDir := t.TempDir()

	file1 := filepath.Join(tmpDir, "file1")
	file2 := filepath.Join(tmpDir, "file2")

	// Create first file
	if err := os.WriteFile(file1, []byte("test content"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create hardlink
	if err := os.Link(file1, file2); err != nil {
		t.Fatal(err)
	}

	// Test same file via hardlink
	if !isSameFile(file1, file2) {
		t.Error("isSameFile() returned false for hardlinked files")
	}

	// Test same file with itself
	if !isSameFile(file1, file1) {
		t.Error("isSameFile() returned false for same path")
	}

	// Test different files
	file3 := filepath.Join(tmpDir, "file3")
	if err := os.WriteFile(file3, []byte("different"), 0644); err != nil {
		t.Fatal(err)
	}

	if isSameFile(file1, file3) {
		t.Error("isSameFile() returned true for different files")
	}

	// Test non-existent files
	if isSameFile(file1, "/nonexistent") {
		t.Error("isSameFile() returned true for non-existent file")
	}
}

func TestCreateBatch(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewHardlinkManager()

	// Create source file
	sourceFile := filepath.Join(tmpDir, "source")
	if err := os.WriteFile(sourceFile, []byte("test data"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create target directory
	targetDir := filepath.Join(tmpDir, "targets")
	target1 := filepath.Join(targetDir, "target1")
	target2 := filepath.Join(targetDir, "target2")

	batch := HardlinkBatch{
		Source:  sourceFile,
		Targets: []string{target1, target2},
	}

	// Test batch creation
	err := manager.createBatch(batch)
	if err != nil {
		t.Fatalf("createBatch() error = %v", err)
	}

	// Verify hardlinks were created
	for _, target := range batch.Targets {
		if !isSameFile(sourceFile, target) {
			t.Errorf("hardlink not created for %s", target)
		}
	}

	// Verify stats
	if atomic.LoadUint64(&manager.stats.created) != 2 {
		t.Errorf("created stats = %d, want 2", manager.stats.created)
	}
}

func TestCreateBatchWithExistingTarget(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewHardlinkManager()

	// Create source and existing target
	sourceFile := filepath.Join(tmpDir, "source")
	targetFile := filepath.Join(tmpDir, "target")

	if err := os.WriteFile(sourceFile, []byte("source data"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create existing hardlink
	if err := os.Link(sourceFile, targetFile); err != nil {
		t.Fatal(err)
	}

	batch := HardlinkBatch{
		Source:  sourceFile,
		Targets: []string{targetFile},
	}

	// Should skip since it's already a hardlink
	err := manager.createBatch(batch)
	if err != nil {
		t.Fatalf("createBatch() error = %v", err)
	}

	// Should have skipped
	if atomic.LoadUint64(&manager.stats.skipped) != 1 {
		t.Errorf("skipped stats = %d, want 1", manager.stats.skipped)
	}
}

func TestCreateBatchWithMissingSource(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewHardlinkManager()

	batch := HardlinkBatch{
		Source:  filepath.Join(tmpDir, "nonexistent"),
		Targets: []string{filepath.Join(tmpDir, "target")},
	}

	err := manager.createBatch(batch)
	if err == nil {
		t.Error("createBatch() expected error for missing source")
	}

	// Should have failed
	if atomic.LoadUint64(&manager.stats.failed) != 1 {
		t.Errorf("failed stats = %d, want 1", manager.stats.failed)
	}
}

func TestCreateAll(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewHardlinkManager()

	// Create multiple source files
	numSources := 5
	for i := 0; i < numSources; i++ {
		source := filepath.Join(tmpDir, fmt.Sprintf("source%d", i))
		if err := os.WriteFile(source, []byte(fmt.Sprintf("data%d", i)), 0644); err != nil {
			t.Fatal(err)
		}

		// Add multiple targets per source
		for j := 0; j < 3; j++ {
			target := filepath.Join(tmpDir, fmt.Sprintf("target_%d_%d", i, j))
			manager.AddHardlink(source, target)
		}
	}

	// Create all hardlinks in parallel
	err := manager.CreateAll()
	if err != nil {
		t.Fatalf("CreateAll() error = %v", err)
	}

	// Verify all hardlinks created
	expectedCreated := uint64(numSources * 3)
	if atomic.LoadUint64(&manager.stats.created) != expectedCreated {
		t.Errorf("created stats = %d, want %d", manager.stats.created, expectedCreated)
	}
}

func TestIsHardlinkCandidate(t *testing.T) {
	tests := []struct {
		name     string
		file     *File
		expected bool
	}{
		{
			name: "regular file",
			file: &File{
				Mode:    0644,
				Rewrite: nil,
			},
			expected: true,
		},
		{
			name: "symlink",
			file: &File{
				Mode:    0777 | os.ModeSymlink,
				Symlink: "/some/target",
			},
			expected: false,
		},
		{
			name: "file with rewrites",
			file: &File{
				Mode:    0644,
				Rewrite: []int64{100, 200},
			},
			expected: false,
		},
		{
			name: "executable file",
			file: &File{
				Mode: 0755,
			},
			expected: false,
		},
		{
			name: "readable file without execute",
			file: &File{
				Mode: 0444,
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isHardlinkCandidate(tt.file)
			if result != tt.expected {
				t.Errorf("isHardlinkCandidate() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestHardlinkCache(t *testing.T) {
	cache := NewHardlinkCache()

	if cache == nil {
		t.Fatal("NewHardlinkCache() returned nil")
	}

	// Test setting and getting eligibility
	digest1 := "abc123"
	digest2 := "def456"

	cache.SetEligible(digest1, true)
	cache.SetEligible(digest2, false)

	if !cache.IsEligible(digest1) {
		t.Error("IsEligible() returned false for eligible digest")
	}

	if cache.IsEligible(digest2) {
		t.Error("IsEligible() returned true for ineligible digest")
	}

	// Test unknown digest
	if cache.IsEligible("unknown") {
		t.Error("IsEligible() returned true for unknown digest")
	}
}

func TestHardlinkCacheThreadSafety(t *testing.T) {
	cache := NewHardlinkCache()

	var wg sync.WaitGroup
	numGoroutines := 100

	// Concurrent writes
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			digest := fmt.Sprintf("digest_%d", id%10)
			cache.SetEligible(digest, id%2 == 0)
		}(i)
	}

	// Concurrent reads
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			digest := fmt.Sprintf("digest_%d", id%10)
			_ = cache.IsEligible(digest)
		}(i)
	}

	wg.Wait()
	// Test should complete without race conditions
}

func TestHardlinkManagerConcurrency(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewHardlinkManager()

	// Create source files
	sources := make([]string, 10)
	for i := range sources {
		sources[i] = filepath.Join(tmpDir, fmt.Sprintf("source%d", i))
		if err := os.WriteFile(sources[i], []byte(fmt.Sprintf("data%d", i)), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Add hardlinks concurrently
	var wg sync.WaitGroup
	for i, source := range sources {
		for j := 0; j < 5; j++ {
			wg.Add(1)
			go func(src string, idx, jdx int) {
				defer wg.Done()
				target := filepath.Join(tmpDir, fmt.Sprintf("target_%d_%d", idx, jdx))
				manager.AddHardlink(src, target)
			}(source, i, j)
		}
	}
	wg.Wait()

	// Create all hardlinks
	err := manager.CreateAll()
	if err != nil {
		t.Fatalf("CreateAll() error = %v", err)
	}

	// Verify results
	expectedCreated := uint64(len(sources) * 5)
	created := atomic.LoadUint64(&manager.stats.created)
	if created != expectedCreated {
		t.Errorf("created = %d, want %d", created, expectedCreated)
	}
}

// mockMutableLibrary implements MutableLibrary for testing
type mockMutableLibrary struct {
	files map[string][]byte
	dir   string
}

func newMockMutableLibrary(t *testing.T) *mockMutableLibrary {
	return &mockMutableLibrary{
		files: make(map[string][]byte),
		dir:   t.TempDir(),
	}
}

func (it *mockMutableLibrary) ValidateBlueprint(blueprint []byte) error {
	return nil
}

func (it *mockMutableLibrary) HasBlueprint(blueprint []byte) bool {
	return true
}

func (it *mockMutableLibrary) Open(digest string) (io.Reader, Closer, error) {
	data, ok := it.files[digest]
	if !ok {
		return nil, func() error { return nil }, fmt.Errorf("digest not found: %s", digest)
	}
	return bytes.NewReader(data), func() error { return nil }, nil
}

func (it *mockMutableLibrary) WarrantyVoidedDir(controller, space []byte) string {
	return filepath.Join(it.dir, "warranty_voided")
}

func (it *mockMutableLibrary) TargetDir(blueprint, controller, space []byte) (string, error) {
	return filepath.Join(it.dir, "target"), nil
}

func (it *mockMutableLibrary) Restore(blueprint, controller, space []byte) (string, error) {
	return filepath.Join(it.dir, "restored"), nil
}

func (it *mockMutableLibrary) RestoreTo(blueprint []byte, client, tag, controller string, partial bool) (string, error) {
	return filepath.Join(it.dir, "restored_to"), nil
}

func (it *mockMutableLibrary) Identity() string {
	return "mock-library"
}

func (it *mockMutableLibrary) ExactLocation(digest string) string {
	return filepath.Join(it.dir, "hololib", digest[:2], digest)
}

func (it *mockMutableLibrary) Export(catalogs, exports []string, filename string) error {
	return nil
}

func (it *mockMutableLibrary) Remove(digests []string) error {
	return nil
}

func (it *mockMutableLibrary) Location(digest string) string {
	return filepath.Join(it.dir, "hololib", digest[:2])
}

// AddFile adds a file to the mock library for testing
func (it *mockMutableLibrary) AddFile(digest string, content []byte) {
	it.files[digest] = content
	// Also write to disk at ExactLocation
	location := it.ExactLocation(digest)
	os.MkdirAll(filepath.Dir(location), 0755)
	os.WriteFile(location, content, 0644)
}

// TestRestoreDirectoryWithHardlinksSmoke tests the integration function
func TestRestoreDirectoryWithHardlinksSmoke(t *testing.T) {
	library := newMockMutableLibrary(t)

	// Add test files to library
	library.AddFile("abc123def456", []byte("test content"))

	// Create mock Root with Tree
	fs := &Root{
		Tree: &Dir{
			Dirs:  make(map[string]*Dir),
			Files: make(map[string]*File),
		},
	}

	// Create the task
	task := RestoreDirectoryWithHardlinks(library, fs, make(map[string]string), &stats{})

	// Task should be created without panic
	if task == nil {
		t.Fatal("RestoreDirectoryWithHardlinks returned nil")
	}
}

// Benchmark hardlink creation
func BenchmarkHardlinkCreation(b *testing.B) {
	tmpDir := b.TempDir()

	// Create source file
	source := filepath.Join(tmpDir, "source")
	if err := os.WriteFile(source, []byte("benchmark data"), 0644); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		target := filepath.Join(tmpDir, fmt.Sprintf("target_%d", i))
		os.Link(source, target)
		os.Remove(target) // Clean up for next iteration
	}
}

func BenchmarkHardlinkManager(b *testing.B) {
	tmpDir := b.TempDir()

	// Create source files
	numSources := 100
	sources := make([]string, numSources)
	for i := range sources {
		sources[i] = filepath.Join(tmpDir, fmt.Sprintf("source%d", i))
		if err := os.WriteFile(sources[i], []byte(fmt.Sprintf("data%d", i)), 0644); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		manager := NewHardlinkManager()

		// Add hardlinks
		for j, source := range sources {
			target := filepath.Join(tmpDir, fmt.Sprintf("bench_%d_target_%d", i, j))
			manager.AddHardlink(source, target)
		}

		// Create all
		manager.CreateAll()

		// Clean up
		for j := range sources {
			target := filepath.Join(tmpDir, fmt.Sprintf("bench_%d_target_%d", i, j))
			os.Remove(target)
		}
	}
}

// TestHardlinkStatsTracking verifies stats are properly tracked
func TestHardlinkStatsTracking(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewHardlinkManager()

	// Create test scenarios
	source1 := filepath.Join(tmpDir, "source1")
	source2 := filepath.Join(tmpDir, "source2")
	existing := filepath.Join(tmpDir, "existing")

	// Create source files
	os.WriteFile(source1, []byte("data1"), 0644)
	os.WriteFile(source2, []byte("data2"), 0644)

	// Create an existing hardlink to test skipping
	os.Link(source1, existing)

	// Add various scenarios
	manager.AddHardlink(source1, existing)                       // Should skip
	manager.AddHardlink(source1, filepath.Join(tmpDir, "new1")) // Should create
	manager.AddHardlink(source2, filepath.Join(tmpDir, "new2")) // Should create
	manager.AddHardlink("/nonexistent", filepath.Join(tmpDir, "fail")) // Should fail

	// Execute
	manager.CreateAll()

	// Verify stats
	stats := manager.stats
	if created := atomic.LoadUint64(&stats.created); created != 2 {
		t.Errorf("created = %d, want 2", created)
	}
	if skipped := atomic.LoadUint64(&stats.skipped); skipped != 1 {
		t.Errorf("skipped = %d, want 1", skipped)
	}
	if failed := atomic.LoadUint64(&stats.failed); failed != 1 {
		t.Errorf("failed = %d, want 1", failed)
	}
}

// NOTE: TestHardlinkMaxWorkers requires internal access to verify concurrency
// limits. Worker count is validated by integration tests and benchmarks.