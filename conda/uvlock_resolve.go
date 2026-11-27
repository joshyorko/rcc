package conda

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/joshyorko/rcc/cloud"
	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/settings"
)

// UVToolLock pins both the distribution archive and the executable it contains.
type UVToolLock struct {
	Version       string `json:"version" yaml:"version"`
	Target        string `json:"target" yaml:"target"`
	ArchiveSHA256 string `json:"archiveSHA256" yaml:"archiveSHA256"`
	BinarySHA256  string `json:"binarySHA256" yaml:"binarySHA256"`
}

func uvAsset(target string) string {
	if runtime.GOOS == "windows" {
		return "uv-" + target + ".zip"
	}
	return "uv-" + target + ".tar.gz"
}
func uvDigest(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == sha256.Size && s == strings.ToLower(s)
}

// Historical checksums are from Astral's per-asset .sha256 files at
// https://github.com/astral-sh/uv/releases/tag/0.6.1. They pin archive bytes,
// including when the archive is downloaded from the configured mirror.
func uvReleaseDigest(version, asset, digest string) (string, error) {
	if version == "0.6.1" {
		pinned := map[string]string{
			"uv-x86_64-unknown-linux-gnu.tar.gz":  "0dcad9831d3f10f3bc4dcd7678948dfc74c0b3ab3f07aa684eb9e5135b971a58",
			"uv-aarch64-unknown-linux-gnu.tar.gz": "f355989fb5ecf47c9f9087a0b21e2ee7d7c802bc3d0cf6edae07560d4297751f",
			"uv-x86_64-apple-darwin.tar.gz":       "d8609b53f280d5e784a7586bf7a3fd90c557656af109cee8572b24a0c1443191",
			"uv-aarch64-apple-darwin.tar.gz":      "90e10cc7f26cbaf3eaa867cf99344ffd550e942fd4b660e88f2f91c23022dc5a",
			"uv-x86_64-pc-windows-msvc.zip":       "32de1730597db0a7c5f34e2257ab491b660374b22c016c3d9a59ae279d837697",
		}[asset]
		if pinned != "" {
			if digest != "" && digest != "sha256:"+pinned {
				return "", fmt.Errorf("official uv release digest conflicts with pinned checksum")
			}
			return pinned, nil
		}
	}
	if !strings.HasPrefix(digest, "sha256:") || !uvDigest(strings.TrimPrefix(digest, "sha256:")) {
		return "", fmt.Errorf("missing official SHA-256 digest")
	}
	return strings.TrimPrefix(digest, "sha256:"), nil
}
func uvHash(path string) (string, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	_, e = io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), e
}
func uvLockValid(l UVToolLock) error {
	if !exactUvVersion.MatchString(l.Version) || !uvDigest(l.ArchiveSHA256) || !uvDigest(l.BinarySHA256) {
		return fmt.Errorf("invalid uv lock version or digest")
	}
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64", "windows/amd64":
	default:
		return fmt.Errorf("unsupported uv platform")
	}
	if l.Target != uvPlatformTarget() {
		return fmt.Errorf("uv lock target mismatch")
	}
	return nil
}

// ResolveLockedUv verifies a warm cache entry or recovers it from the configured mirror.
func ResolveLockedUv(l UVToolLock) (string, error) {
	if err := uvLockValid(l); err != nil {
		return "", err
	}
	name := "uv"
	if runtime.GOOS == "windows" {
		name = "uv.exe"
	}
	dir := filepath.Join(common.UvBinaryCache(), "locked", l.Version, l.Target, l.ArchiveSHA256, l.BinarySHA256)
	dest := filepath.Join(dir, name)
	if info, err := os.Lstat(dest); err == nil && verifiedExecutable(dest, l.BinarySHA256, info.Size()) {
		return checkedLockedUV(dest, l.Version)
	}
	if err := os.MkdirAll(dir, 0750); err != nil {
		return "", err
	}
	archive, err := os.CreateTemp(dir, ".archive-*")
	if err != nil {
		return "", err
	}
	archivePath := archive.Name()
	if err = archive.Close(); err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(archivePath) }()
	url := strings.TrimRight(settings.Global.UvReleasesURL(), "/") + "/" + l.Version + "/" + uvAsset(l.Target)
	if err = downloadUvArchive(url, archivePath); err != nil {
		return "", err
	}
	if err = VerifyUvArchive(UvArchivePin{Version: l.Version, Target: l.Target, SHA256: l.ArchiveSHA256}, archivePath); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, ".uv-*")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	err = extractLockedUv(archivePath, l.Target, name, tmp)
	if err == nil {
		err = tmp.Chmod(0755)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", err
	}
	if err := publishUvFile(dest, l.BinarySHA256, tmpPath); err != nil {
		return "", err
	}

	return checkedLockedUV(dest, l.Version)
}

func checkedLockedUV(path, version string) (string, error) {
	if UvVersion(path) != version {
		return "", fmt.Errorf("verified uv executable does not report requested version %s", version)
	}
	return path, nil
}

func extractLockedUv(archive, target, name string, out io.Writer) error {
	wanted := "uv-" + target + "/" + name
	count := 0
	copyEntry := func(r io.Reader, size int64) error {
		count++
		if count != 1 {
			return fmt.Errorf("duplicate uv executable")
		}
		n, e := io.Copy(out, io.LimitReader(r, size))
		if e != nil {
			return e
		}
		if n != size {
			return fmt.Errorf("incomplete uv executable")
		}
		return nil
	}
	if runtime.GOOS == "windows" {
		z, e := zip.OpenReader(archive)
		if e != nil {
			return e
		}
		defer func() { _ = z.Close() }()
		for _, f := range z.File {
			if f.Name != wanted && f.Name != name {
				continue
			}
			if !f.Mode().IsRegular() {
				return fmt.Errorf("uv executable is not regular")
			}
			r, e := f.Open()
			if e != nil {
				return e
			}
			e = copyEntry(r, int64(f.UncompressedSize64))
			c := r.Close()
			if e != nil {
				return e
			}
			if c != nil {
				return c
			}
		}
	} else {
		f, e := os.Open(archive)
		if e != nil {
			return e
		}
		defer func() { _ = f.Close() }()
		gz, e := gzip.NewReader(f)
		if e != nil {
			return e
		}
		defer func() { _ = gz.Close() }()
		tr := tar.NewReader(gz)
		for {
			h, e := tr.Next()
			if e == io.EOF {
				break
			}
			if e != nil {
				return e
			}
			if h.Name != wanted {
				continue
			}
			if h.Typeflag != tar.TypeReg {
				return fmt.Errorf("uv executable is not regular")
			}
			if e = copyEntry(tr, h.Size); e != nil {
				return e
			}
		}
	}
	if count != 1 {
		return fmt.Errorf("uv executable missing from archive")
	}
	return nil
}

// CreateUVToolLock authenticates the official release metadata and pins the mirror bytes.
func CreateUVToolLock(version string) (UVToolLock, error) {
	var l UVToolLock
	if version != "latest" && !exactUvVersion.MatchString(version) {
		return l, fmt.Errorf("uv version must be latest or X.Y.Z")
	}
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64", "windows/amd64":
	default:
		return l, fmt.Errorf("unsupported uv platform")
	}
	l, err := discoverUVArchive(version)
	if err != nil {
		return UVToolLock{}, err
	}
	dir, err := os.MkdirTemp("", "rcc-uv-lock-*")
	if err != nil {
		return UVToolLock{}, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	archive := filepath.Join(dir, "archive")
	if err = downloadUvArchive(strings.TrimRight(settings.Global.UvReleasesURL(), "/")+"/"+l.Version+"/"+uvAsset(l.Target), archive); err != nil {
		return UVToolLock{}, err
	}
	if err = VerifyUvArchive(UvArchivePin{l.Version, l.Target, l.ArchiveSHA256}, archive); err != nil {
		return UVToolLock{}, err
	}
	binary, err := os.CreateTemp(dir, "binary-*")
	if err != nil {
		return UVToolLock{}, err
	}
	name := "uv"
	if runtime.GOOS == "windows" {
		name = "uv.exe"
	}
	err = extractLockedUv(archive, l.Target, name, binary)
	if e := binary.Close(); err == nil {
		err = e
	}
	if err != nil {
		return UVToolLock{}, err
	}
	l.BinarySHA256, err = uvHash(binary.Name())
	if err != nil {
		return UVToolLock{}, err
	}
	cache := filepath.Join(common.UvBinaryCache(), "locked", l.Version, l.Target, l.ArchiveSHA256, l.BinarySHA256, name)
	if err = publishUvFile(cache, l.BinarySHA256, binary.Name()); err != nil {
		return UVToolLock{}, err
	}
	if UvVersion(cache) != l.Version {
		return UVToolLock{}, fmt.Errorf("verified uv executable does not report requested version %s", l.Version)
	}
	return l, nil
}

func downloadUvArchive(url, path string) error {
	const timeout = 30 * time.Second
	err := cloud.DownloadWithTimeout(url, path, timeout)
	var status *cloud.DownloadStatusError
	var network net.Error
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) ||
		(errors.As(err, &network) && network.Timeout()) ||
		(errors.As(err, &status) && (status.StatusCode == 408 || status.StatusCode == 429 || status.StatusCode >= 500)) {
		return cloud.DownloadWithTimeout(url, path, timeout)
	}
	return err
}

func discoverUVArchive(version string) (UVToolLock, error) {
	var l UVToolLock
	if !settings.Global.VerifySsl() {
		return l, fmt.Errorf("verified release discovery requires TLS verification")
	}
	target := uvPlatformTarget()
	if digest, err := uvReleaseDigest(version, uvAsset(target), ""); err == nil {
		return UVToolLock{Version: version, Target: target, ArchiveSHA256: digest}, nil
	}
	endpoint := strings.TrimRight(settings.Global.UvMetadataURL(), "/") + "/"
	if version == "latest" {
		endpoint += "latest"
	} else {
		endpoint += "tags/" + version
	}
	client := &http.Client{Transport: settings.Global.ConfiguredHttpTransport(), Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("release metadata redirect refused") }}
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return l, err
	}
	if req.URL.Scheme != "https" || req.URL.Hostname() == "" || req.URL.User != nil || req.URL.RawQuery != "" || req.URL.Fragment != "" {
		return l, fmt.Errorf("uv release metadata endpoint must be an HTTPS base URL")
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "rcc-uv-lock")
	resp, err := client.Do(req)
	if err != nil {
		return l, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return l, fmt.Errorf("uv release API: %s", resp.Status)
	}
	var release struct {
		Tag        string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
		Assets     []struct {
			Name   string `json:"name"`
			Digest string `json:"digest"`
		} `json:"assets"`
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&release); err != nil {
		return l, err
	}
	if release.Draft || release.Prerelease || !exactUvVersion.MatchString(release.Tag) || (version != "latest" && release.Tag != version) {
		return l, fmt.Errorf("release tag is not the requested stable version")
	}
	l.Version = release.Tag
	l.Target = uvPlatformTarget()
	matches := 0
	for _, a := range release.Assets {
		if a.Name == uvAsset(l.Target) {
			matches++
			l.ArchiveSHA256, err = uvReleaseDigest(l.Version, a.Name, a.Digest)
			if err != nil {
				return UVToolLock{}, err
			}
		}
	}
	if matches != 1 {
		return UVToolLock{}, fmt.Errorf("expected exactly one uv release asset")
	}
	return l, nil
}

func publishUvFile(dest, digest, source string) error {
	f, err := os.Open(source)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if err = publishExecutable(dest, digest, info.Size(), f); err != nil {
		return err
	}
	common.PlatformSyncDelay()
	return nil
}
