package conda

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
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
		return dest, nil
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
	if err = cloud.Download(url, archivePath); err != nil {
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

	return dest, nil
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
			if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA {
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
	endpoint := "https://api.github.com/repos/astral-sh/uv/releases/"
	if version == "latest" {
		endpoint += "latest"
	} else {
		endpoint += "tags/" + version
	}
	if !settings.Global.VerifySsl() {
		return l, fmt.Errorf("verified release discovery requires TLS verification")
	}
	client := &http.Client{Transport: settings.Global.ConfiguredHttpTransport(), Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("release metadata redirect refused") }}
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return l, err
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
			l.ArchiveSHA256 = strings.TrimPrefix(a.Digest, "sha256:")
			if !strings.HasPrefix(a.Digest, "sha256:") || !uvDigest(l.ArchiveSHA256) {
				return UVToolLock{}, fmt.Errorf("missing official SHA-256 digest")
			}
		}
	}
	if matches != 1 {
		return UVToolLock{}, fmt.Errorf("expected exactly one uv release asset")
	}
	dir, err := os.MkdirTemp("", "rcc-uv-lock-*")
	if err != nil {
		return UVToolLock{}, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	archive := filepath.Join(dir, "archive")
	if err = cloud.Download(strings.TrimRight(settings.Global.UvReleasesURL(), "/")+"/"+l.Version+"/"+uvAsset(l.Target), archive); err != nil {
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
