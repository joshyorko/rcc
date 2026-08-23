package artifacttrust

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const maxCarrierAttachmentBytes = 16 << 20

// FilesystemCarrier stores detached attestations below one owner-controlled
// directory. Names are validated on both reads and writes so a read cannot be
// redirected outside the carrier root by untrusted metadata.
type FilesystemCarrier struct{ Root string }

func NewFilesystemCarrier(root string) *FilesystemCarrier { return &FilesystemCarrier{Root: root} }

func (c *FilesystemCarrier) path(name string) (string, error) {
	if err := validateCarrierName(name); err != nil {
		return "", err
	}
	root, err := filepath.Abs(c.Root)
	if err != nil {
		return "", fmt.Errorf("resolve carrier root: %w", err)
	}
	target := filepath.Join(root, filepath.FromSlash(name))
	if err := rejectSymlinkComponents(root, filepath.Dir(target)); err != nil {
		return "", err
	}
	return target, nil
}

func (c *FilesystemCarrier) Read(name string) ([]byte, error) {
	p, err := c.path(name)
	if err != nil {
		return nil, err
	}
	if info, err := os.Lstat(p); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("carrier target is not a regular file")
		}
	}
	return readBoundedFile(p)
}

func (c *FilesystemCarrier) Write(name string, data []byte) error {
	p, err := c.path(name)
	if err != nil {
		return err
	}
	if len(data) > maxCarrierAttachmentBytes {
		return fmt.Errorf("carrier attachment exceeds %d bytes", maxCarrierAttachmentBytes)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	root, err := filepath.Abs(c.Root)
	if err != nil {
		return err
	}
	if err := rejectSymlinkComponents(root, filepath.Dir(p)); err != nil {
		return err
	}
	if info, err := os.Lstat(p); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("carrier target is a symlink")
	}
	return os.WriteFile(p, data, 0600)
}

// HTTPCarrier maps the provider-neutral attachment name to a URL below BaseURL.
// It deliberately has no credential-bearing URL or implicit authorization
// behavior; authentication belongs to the provider/client supplied by callers.
type HTTPCarrier struct {
	BaseURL string
	Client  *http.Client
}

func (c *HTTPCarrier) attachmentURL(name string) (*url.URL, error) {
	if err := validateCarrierName(name); err != nil {
		return nil, err
	}
	base, err := url.Parse(c.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse carrier URL: %w", err)
	}
	if base.Scheme != "http" && base.Scheme != "https" {
		return nil, fmt.Errorf("carrier URL must use HTTP or HTTPS")
	}
	if base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return nil, fmt.Errorf("carrier URL contains unsupported credentials or query data")
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/" + name
	base.RawPath = ""
	return base, nil
}

func (c *HTTPCarrier) Read(name string) ([]byte, error) {
	target, err := c.attachmentURL(name)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%w: carrier attachment", os.ErrNotExist)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("carrier HTTP status %s", resp.Status)
	}
	return readBounded(resp.Body)
}

func (c *HTTPCarrier) Write(name string, data []byte) error {
	if len(data) > maxCarrierAttachmentBytes {
		return fmt.Errorf("carrier attachment exceeds %d bytes", maxCarrierAttachmentBytes)
	}
	target, err := c.attachmentURL(name)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPut, target.String(), bytes.NewReader(data))
	if err != nil {
		return err
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("%w: carrier attachment", os.ErrNotExist)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("carrier HTTP status %s", resp.Status)
	}
	return nil
}

func (c *HTTPCarrier) client() *http.Client {
	if c.Client != nil {
		return c.Client
	}
	return http.DefaultClient
}

// ArchiveCarrier is an in-memory, read/write carrier loaded from a validated
// ZIP archive. It is useful for air-gapped transport and testing.
type ArchiveCarrier struct{ Files map[string][]byte }

func OpenArchiveCarrier(filePath string) (*ArchiveCarrier, error) {
	archive, err := zip.OpenReader(filePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = archive.Close() }()
	out := &ArchiveCarrier{Files: map[string][]byte{}}
	for _, entry := range archive.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		if entry.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("archive carrier contains symlink")
		}
		if err := validateCarrierName(entry.Name); err != nil {
			return nil, fmt.Errorf("unsafe archive carrier member: %w", err)
		}
		if _, found := out.Files[entry.Name]; found {
			return nil, fmt.Errorf("duplicate archive carrier member")
		}
		reader, err := entry.Open()
		if err != nil {
			return nil, err
		}
		data, readErr := readBounded(reader)
		closeErr := reader.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		out.Files[entry.Name] = data
	}
	return out, nil
}

func (c *ArchiveCarrier) Read(name string) ([]byte, error) {
	if err := validateCarrierName(name); err != nil {
		return nil, err
	}
	data, ok := c.Files[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	return append([]byte(nil), data...), nil
}

func (c *ArchiveCarrier) Write(name string, data []byte) error {
	if err := validateCarrierName(name); err != nil {
		return err
	}
	if len(data) > maxCarrierAttachmentBytes {
		return fmt.Errorf("carrier attachment exceeds %d bytes", maxCarrierAttachmentBytes)
	}
	if c.Files == nil {
		c.Files = map[string][]byte{}
	}
	c.Files[name] = append([]byte(nil), data...)
	return nil
}

func ExportArchive(carrier Carrier, filePath, artifact string, kinds []string) error {
	file, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	completed := false
	defer func() {
		_ = file.Close()
		if !completed {
			_ = os.Remove(filePath)
		}
	}()
	archive := zip.NewWriter(file)
	for _, kind := range kinds {
		data, err := GetAttachment(carrier, artifact, kind)
		if err != nil {
			_ = archive.Close()
			return err
		}
		entry, err := archive.Create(AttachmentName(artifact, kind))
		if err != nil {
			_ = archive.Close()
			return err
		}
		if _, err := entry.Write(data); err != nil {
			_ = archive.Close()
			return err
		}
	}
	if err := archive.Close(); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	completed = true
	return nil
}

// Attachments is the decoded, carrier-independent trust input for one artifact.
type Attachments struct {
	Provenance          *Provenance
	SBOM                *SBOM
	Signatures          []Signature
	Revocations         []Revocation
	RevocationFetchedAt string
	RevocationSource    string
}

// LoadAttachments traverses all supported detached attachment kinds. Missing
// optional files are ignored, while malformed present files fail closed.
func LoadAttachments(carrier Carrier, artifact string) (Attachments, error) {
	var result Attachments
	if data, err := GetAttachment(carrier, artifact, "provenance"); err == nil {
		var value Provenance
		if err := decodeCanonical(data, &value); err != nil {
			return Attachments{}, fmt.Errorf("decode provenance attachment: %w", err)
		}
		result.Provenance = &value
	} else if !errors.Is(err, os.ErrNotExist) {
		return Attachments{}, err
	}
	if data, err := GetAttachment(carrier, artifact, "sbom"); err == nil {
		var value SBOM
		if err := decodeCanonical(data, &value); err != nil {
			return Attachments{}, fmt.Errorf("decode SBOM attachment: %w", err)
		}
		result.SBOM = &value
	} else if !errors.Is(err, os.ErrNotExist) {
		return Attachments{}, err
	}
	data, err := GetAttachment(carrier, artifact, "signature")
	if errors.Is(err, os.ErrNotExist) {
		data, err = GetAttachment(carrier, artifact, "signatures")
	}
	if err == nil {
		bundle, err := DecodeSignatureBundle(data, artifact)
		if err != nil {
			return Attachments{}, err
		}
		result.Signatures = bundle.Signatures
	} else if !errors.Is(err, os.ErrNotExist) {
		return Attachments{}, err
	}
	data, err = GetAttachment(carrier, artifact, "revocations")
	if errors.Is(err, os.ErrNotExist) {
		data, err = GetAttachment(carrier, artifact, "revocation")
	}
	if err == nil {
		bundle, err := DecodeRevocationBundle(data, artifact)
		if err != nil {
			return Attachments{}, err
		}
		result.Revocations = bundle.Revocations
		result.RevocationFetchedAt = bundle.FetchedAt
		result.RevocationSource = bundle.Source
	} else if !errors.Is(err, os.ErrNotExist) {
		return Attachments{}, err
	}
	return result, nil
}

func decodeCanonical(data []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("attachment contains trailing data")
	}
	return nil
}

func readBoundedFile(filePath string) ([]byte, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	return readBounded(file)
}

func readBounded(reader io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maxCarrierAttachmentBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxCarrierAttachmentBytes {
		return nil, fmt.Errorf("carrier attachment exceeds %d bytes", maxCarrierAttachmentBytes)
	}
	return data, nil
}

func validateCarrierName(name string) error {
	if name == "" || strings.ContainsRune(name, '\x00') || strings.ContainsRune(name, '\\') {
		return fmt.Errorf("invalid carrier attachment name")
	}
	if filepath.IsAbs(filepath.FromSlash(name)) {
		return fmt.Errorf("carrier path is absolute")
	}
	clean := path.Clean(name)
	if clean == "." || clean != name || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("carrier path escapes root")
	}
	return nil
}

func rejectSymlinkComponents(root, target string) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("carrier path escapes root")
	}
	current := root
	if info, err := os.Lstat(current); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("carrier root is a symlink")
	}
	for _, component := range strings.Split(relative, string(os.PathSeparator)) {
		if component == "." || component == "" {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			break
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("carrier path contains symlink")
		}
	}
	return nil
}
