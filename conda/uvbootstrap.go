package conda

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/joshyorko/rcc/cloud"
	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/fail"
	"github.com/joshyorko/rcc/pathlib"
	"github.com/joshyorko/rcc/pretty"
	"github.com/joshyorko/rcc/settings"
)

// MustUv ensures uv binary is available for the given version
func MustUv(version string) bool {
	uvPath := UvBinaryPath(version)
	if pathlib.IsFile(uvPath) {
		common.Trace("UV binary already exists at: %s", uvPath)
		return true
	}

	// Try downloading with retry pattern
	return downloadUv(version, 1*time.Millisecond) ||
		downloadUv(version, 1*time.Second) ||
		downloadUv(version, 3*time.Second) ||
		doFailUv(version)
}

// doFailUv handles final failure case
func doFailUv(version string) bool {
	pretty.Exit(113, "Could not download uv binary version %s, see above stream for more details.", version)
	return false
}

// downloadUv downloads and extracts uv binary
func downloadUv(version string, delay time.Duration) bool {
	time.Sleep(delay)

	pretty.Highlight("Downloading uv binary version %s...", version)

	target := uvPlatformTarget()
	isWindows := runtime.GOOS == "windows"

	// Windows uses .zip, others use .tar.gz
	ext := ".tar.gz"
	if isWindows {
		ext = ".zip"
	}
	url := fmt.Sprintf("%s/%s/uv-%s%s", settings.Global.UvReleasesURL(), version, target, ext)

	// Create temp file for download
	tempDir := pathlib.TempDir()
	tempFile := filepath.Join(tempDir, fmt.Sprintf("uv-%s%s", version, ext))
	defer os.Remove(tempFile)

	common.Debug("Downloading from: %s", url)
	err := cloud.Download(url, tempFile)
	if err != nil {
		common.Log("Failed to download uv: %v", err)
		return false
	}

	// Extract to temp directory first
	extractDir := filepath.Join(tempDir, fmt.Sprintf("uv-extract-%s", version))
	defer os.RemoveAll(extractDir)

	if isWindows {
		err = extractZip(tempFile, extractDir)
		if err != nil {
			common.Log("Failed to extract uv zip: %v", err)
			return false
		}
	} else {
		err = extractTarGz(tempFile, extractDir)
		if err != nil {
			common.Log("Failed to extract uv tarball: %v", err)
			return false
		}
	}

	// Find the uv binary in extracted contents
	// It's typically in a directory like uv-{target}/uv (or uv.exe on Windows)
	uvBinaryName := "uv"
	if isWindows {
		uvBinaryName = "uv.exe"
	}

	uvSource := ""
	entries, err := os.ReadDir(extractDir)
	if err != nil {
		common.Log("Failed to read extract directory: %v", err)
		return false
	}

	for _, entry := range entries {
		if entry.IsDir() {
			candidate := filepath.Join(extractDir, entry.Name(), uvBinaryName)
			if pathlib.IsFile(candidate) {
				uvSource = candidate
				break
			}
		}
		// Also check if uv is directly in extract dir
		candidate := filepath.Join(extractDir, uvBinaryName)
		if pathlib.IsFile(candidate) {
			uvSource = candidate
			break
		}
	}

	if uvSource == "" {
		common.Log("Could not find uv binary in extracted tarball")
		return false
	}

	// Ensure target directory exists
	targetDir := filepath.Dir(UvBinaryPath(version))
	err = pathlib.EnsureDirectoryExists(targetDir)
	if err != nil {
		common.Log("Failed to create uv cache directory: %v", err)
		return false
	}

	// Move uv binary to final location
	targetPath := UvBinaryPath(version)
	err = os.Rename(uvSource, targetPath)
	if err != nil {
		// If rename fails (cross-device), copy instead
		err = copyFile(uvSource, targetPath)
		if err != nil {
			common.Log("Failed to move uv binary to cache: %v", err)
			return false
		}
	}

	// Set executable permissions
	err = os.Chmod(targetPath, 0o755)
	if err != nil {
		common.Log("Failed to set uv binary permissions: %v", err)
		os.Remove(targetPath)
		return false
	}

	cloud.InternalBackgroundMetric(common.ControllerIdentity(), "rcc.uv.download", common.Version)
	common.PlatformSyncDelay()
	return true
}

// uvPlatformTarget returns the platform-specific target string for uv downloads
func uvPlatformTarget() string {
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	switch {
	case goos == "linux" && goarch == "amd64":
		return "x86_64-unknown-linux-gnu"
	case goos == "linux" && goarch == "arm64":
		return "aarch64-unknown-linux-gnu"
	case goos == "darwin" && goarch == "amd64":
		return "x86_64-apple-darwin"
	case goos == "darwin" && goarch == "arm64":
		return "aarch64-apple-darwin"
	case goos == "windows" && goarch == "amd64":
		return "x86_64-pc-windows-msvc"
	default:
		panic(fmt.Sprintf("Unsupported platform for uv: %s/%s", goos, goarch))
	}
}

// extractTarGz extracts a tar.gz file to the specified directory
func extractTarGz(tarGzPath, destDir string) (err error) {
	defer fail.Around(&err)

	// Open the tar.gz file
	file, err := os.Open(tarGzPath)
	fail.Fast(err)
	defer file.Close()

	// Create gzip reader
	gzReader, err := gzip.NewReader(file)
	fail.Fast(err)
	defer gzReader.Close()

	// Create tar reader
	tarReader := tar.NewReader(gzReader)

	// Ensure destination directory exists
	err = pathlib.EnsureDirectoryExists(destDir)
	fail.Fast(err)

	// Extract files
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		fail.Fast(err)

		// Construct the target path
		target := filepath.Join(destDir, header.Name)

		// Ensure the target path is within destDir (prevent path traversal)
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(destDir)) {
			fail.On(true, "tar entry outside of target directory: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			// Create directory
			err = pathlib.EnsureDirectoryExists(target)
			fail.Fast(err)

		case tar.TypeReg:
			// Create the directory for this file
			err = pathlib.EnsureDirectoryExists(filepath.Dir(target))
			fail.Fast(err)

			// Create the file
			outFile, err := os.Create(target)
			fail.Fast(err)

			// Copy file contents
			_, err = io.Copy(outFile, tarReader)
			outFile.Close()
			fail.Fast(err)

			// Set file permissions
			err = os.Chmod(target, os.FileMode(header.Mode))
			fail.Fast(err)

		case tar.TypeSymlink:
			// Create symlink
			err = os.Symlink(header.Linkname, target)
			fail.Fast(err)

		default:
			common.Trace("Skipping tar entry type %v for %s", header.Typeflag, header.Name)
		}
	}

	return nil
}

// UvBinaryPath returns the full path to the uv binary for a given version
func UvBinaryPath(version string) string {
	uvBinaryName := "uv"
	if runtime.GOOS == "windows" {
		uvBinaryName = "uv.exe"
	}
	return filepath.Join(common.UvBinaryCache(), version, uvBinaryName)
}

// extractZip extracts a zip file to the specified directory (Windows)
func extractZip(zipPath, destDir string) (err error) {
	defer fail.Around(&err)

	// Open the zip file
	reader, err := zip.OpenReader(zipPath)
	fail.Fast(err)
	defer reader.Close()

	// Ensure destination directory exists
	err = pathlib.EnsureDirectoryExists(destDir)
	fail.Fast(err)

	// Extract files
	for _, file := range reader.File {
		// Construct the target path
		target := filepath.Join(destDir, file.Name)

		// Ensure the target path is within destDir (prevent path traversal)
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(destDir)) {
			fail.On(true, "zip entry outside of target directory: %s", file.Name)
		}

		if file.FileInfo().IsDir() {
			// Create directory
			err = pathlib.EnsureDirectoryExists(target)
			fail.Fast(err)
			continue
		}

		// Create the directory for this file
		err = pathlib.EnsureDirectoryExists(filepath.Dir(target))
		fail.Fast(err)

		// Open file in zip
		srcFile, err := file.Open()
		fail.Fast(err)
		defer srcFile.Close()

		// Create the target file
		dstFile, err := os.Create(target)
		fail.Fast(err)

		// Copy file contents
		_, err = io.Copy(dstFile, srcFile)
		dstFile.Close()
		fail.Fast(err)

		// Set file permissions (on Windows, just make it executable)
		if strings.HasSuffix(target, ".exe") {
			err = os.Chmod(target, 0o755)
			fail.Fast(err)
		}
	}

	return nil
}

// copyFile is a helper to copy a file when os.Rename fails across devices
func copyFile(src, dst string) error {
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()

	destination, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destination.Close()

	_, err = io.Copy(destination, source)
	return err
}