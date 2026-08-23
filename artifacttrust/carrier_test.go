package artifacttrust

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHTTPAttachmentVerificationBindsArtifact(t *testing.T) {
	artifact := "sha256:http"
	data := []byte(`{"mediaType":"application/vnd.rcc.environment.provenance.v1+json","artifactDigest":"sha256:http"}`)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || (r.URL.Path != "/sha256:http/provenance.json" && r.URL.Path != "/sha256:other/provenance.json") {
			t.Errorf("request=%s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write(data)
	}))
	defer s.Close()
	got, err := GetAttachment(&HTTPCarrier{BaseURL: s.URL}, artifact, "provenance")
	if err != nil || string(got) != string(data) {
		t.Fatalf("got=%s err=%v", got, err)
	}
	if _, err := GetAttachment(&HTTPCarrier{BaseURL: s.URL}, "sha256:other", "provenance"); err == nil {
		t.Fatal("mismatched HTTP attachment accepted")
	}
}

func TestHTTPCarrierRejectsCredentialBearingBaseURL(t *testing.T) {
	for _, baseURL := range []string{"https://user:secret@example.invalid", "https://example.invalid/?token=secret", "https://example.invalid/#secret"} {
		if _, err := (&HTTPCarrier{BaseURL: baseURL}).Read("sha256:a/provenance.json"); err == nil || bytes.Contains([]byte(err.Error()), []byte("secret")) {
			t.Fatalf("base URL %q err=%v", baseURL, err)
		}
	}
}

func TestFilesystemCarrierRejectsTraversalOnReadAndWrite(t *testing.T) {
	c := NewFilesystemCarrier(t.TempDir())
	for _, name := range []string{"../outside", "/absolute", "sha256:a/../outside"} {
		if _, err := c.Read(name); err == nil {
			t.Fatalf("read accepted unsafe carrier name %q", name)
		}
		if err := c.Write(name, []byte("secret")); err == nil {
			t.Fatalf("write accepted unsafe carrier name %q", name)
		}
	}
	outside := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	name := AttachmentName("sha256:a", "provenance")
	path := filepath.Join(c.Root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path); err != nil {
		t.Skipf("symlink test unavailable: %v", err)
	}
	if _, err := c.Read(name); err == nil {
		t.Fatal("read followed carrier symlink")
	}
	if err := c.Write(name, []byte("new")); err == nil {
		t.Fatal("write replaced carrier symlink")
	}
}

func TestArchiveCarrierRejectsUnsafeMembersAndRoundTripsAttachments(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "carrier.zip")
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	zipper := zip.NewWriter(file)
	entry, err := zipper.Create("../escape.json")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = entry.Write([]byte("bad"))
	if err := zipper.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenArchiveCarrier(archive); err == nil {
		t.Fatal("archive traversal member accepted")
	}

	root := t.TempDir()
	filesystem := NewFilesystemCarrier(root)
	data := []byte(`{"mediaType":"application/vnd.rcc.environment.provenance.v1+json","artifactDigest":"sha256:a"}`)
	if err := PutAttachment(filesystem, "sha256:a", "provenance", data); err != nil {
		t.Fatal(err)
	}
	archive = filepath.Join(root, "safe.zip")
	if err := ExportArchive(filesystem, archive, "sha256:a", []string{"provenance"}); err != nil {
		t.Fatal(err)
	}
	offline, err := OpenArchiveCarrier(archive)
	if err != nil {
		t.Fatal(err)
	}
	got, err := GetAttachment(offline, "sha256:a", "provenance")
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("got=%q err=%v", got, err)
	}
}

func TestLoadAttachmentsRejectsMalformedPresentData(t *testing.T) {
	c := NewFilesystemCarrier(t.TempDir())
	if err := c.Write(AttachmentName("sha256:a", "provenance"), []byte(`{"mediaType":"bad","artifactDigest":"sha256:a"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAttachments(c, "sha256:a"); err == nil {
		t.Fatal("malformed present attachment accepted")
	}
}

func TestPutAttachmentNormalizesLegacySignatureArrayToBoundBundle(t *testing.T) {
	public, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := Sign("sha256:a", "key", private)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := json.Marshal([]Signature{signature})
	if err != nil {
		t.Fatal(err)
	}
	carrier := NewFilesystemCarrier(t.TempDir())
	if err := PutAttachment(carrier, "sha256:a", "signature", legacy); err != nil {
		t.Fatal(err)
	}
	attachments, err := LoadAttachments(carrier, "sha256:a")
	if err != nil || len(attachments.Signatures) != 1 || VerifySignature(attachments.Signatures[0], public, "sha256:a") != nil {
		t.Fatalf("attachments=%+v err=%v", attachments, err)
	}
}

func TestHTTPAndOfflineArchiveCarriersLoadSameSignatureAndRevocationBundles(t *testing.T) {
	_, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := Sign("sha256:a", "key", private)
	if err != nil {
		t.Fatal(err)
	}
	_, signatureBytes, err := NewSignatureBundle("sha256:a", []Signature{signature})
	if err != nil {
		t.Fatal(err)
	}
	_, revocationBytes, err := NewRevocationBundleAt("sha256:a", nil, time.Unix(10, 0), "offline")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	filesystem := NewFilesystemCarrier(root)
	if err := PutAttachment(filesystem, "sha256:a", "signature", signatureBytes); err != nil {
		t.Fatal(err)
	}
	if err := PutAttachment(filesystem, "sha256:a", "revocations", revocationBytes); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(root, "trust.zip")
	if err := ExportArchive(filesystem, archivePath, "sha256:a", []string{"signature", "revocations"}); err != nil {
		t.Fatal(err)
	}
	offline, err := OpenArchiveCarrier(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	attachments, err := LoadAttachments(offline, "sha256:a")
	if err != nil || len(attachments.Signatures) != 1 || len(attachments.Revocations) != 0 || attachments.RevocationFetchedAt == "" {
		t.Fatalf("offline attachments=%+v err=%v", attachments, err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/" + AttachmentName("sha256:a", "signature"):
			_, _ = writer.Write(signatureBytes)
		case "/" + AttachmentName("sha256:a", "revocations"):
			_, _ = writer.Write(revocationBytes)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	remote, err := LoadAttachments(&HTTPCarrier{BaseURL: server.URL, Client: server.Client()}, "sha256:a")
	if err != nil || len(remote.Signatures) != 1 || remote.RevocationFetchedAt == "" {
		t.Fatalf("HTTP attachments=%+v err=%v", remote, err)
	}
}
