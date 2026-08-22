package remotree

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joshyorko/rcc/common"
)

func handlerFixture(t *testing.T) string {
	t.Helper()
	previous := common.Product.Home()
	home := t.TempDir()
	common.Product.ForceHome(home)
	t.Cleanup(func() { common.Product.ForceHome(previous) })
	if err := os.MkdirAll(common.HololibCatalogLocation(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(common.HololibLibraryLocation(), "ab", "cd", "ef"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(common.HololibCatalogLocation(), "catalog"), []byte("catalog"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(common.HololibLibraryLocation(), "ab", "cd", "ef", "abcdef0123456789"), []byte("member"), 0o644); err != nil {
		t.Fatal(err)
	}
	return home
}

func TestQueryHandlerLegacyProtocol(t *testing.T) {
	handlerFixture(t)
	queries := make(Partqueries, 1)
	triggers := make(chan string, 1)
	go func() { q := <-queries; q.Reply <- "abcdef0123456789\n"; close(q.Reply) }()
	h := makeQueryHandler(queries, triggers)
	request := httptest.NewRequest(http.MethodGet, "/parts/catalog", nil)
	response := httptest.NewRecorder()
	h(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "abcdef0123456789\n" {
		t.Fatalf("GET /parts: %d %q", response.Code, response.Body.String())
	}
	response = httptest.NewRecorder()
	h(response, httptest.NewRequest(http.MethodPost, "/parts/catalog", nil))
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("wrong method: %d", response.Code)
	}
	request = httptest.NewRequest(http.MethodGet, "/parts/catalog", nil)
	request.Header.Set("X-RCC-Random-Identity", common.RandomIdentifier())
	response = httptest.NewRecorder()
	h(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("self request: %d", response.Code)
	}
	missingQueries := make(Partqueries, 1)
	missingTriggers := make(chan string, 1)
	go func() { q := <-missingQueries; close(q.Reply) }()
	response = httptest.NewRecorder()
	makeQueryHandler(missingQueries, missingTriggers)(response, httptest.NewRequest(http.MethodGet, "/parts/missing", nil))
	trigger := <-missingTriggers
	if response.Code != http.StatusNotFound || trigger != "missing" {
		t.Fatalf("missing catalog: %d trigger=%q", response.Code, trigger)
	}
}

func TestDeltaHandlerLegacyProtocolAndZip(t *testing.T) {
	handlerFixture(t)
	queries := make(Partqueries, 1)
	go func() { q := <-queries; q.Reply <- "abcdef0123456789\n"; close(q.Reply) }()
	h := makeDeltaHandler(queries)
	response := httptest.NewRecorder()
	h(response, httptest.NewRequest(http.MethodGet, "/delta/catalog", nil))
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("wrong method: %d", response.Code)
	}
	request := httptest.NewRequest(http.MethodPost, "/delta/catalog", nil)
	request.Header.Set("X-RCC-Random-Identity", common.RandomIdentifier())
	response = httptest.NewRecorder()
	h(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("self request: %d", response.Code)
	}
	go func() { q := <-queries; q.Reply <- "abcdef0123456789\n"; close(q.Reply) }()
	request = httptest.NewRequest(http.MethodPost, "/delta/catalog", strings.NewReader("abcdef0123456789\n"))
	response = httptest.NewRecorder()
	h(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("delta: %d", response.Code)
	}
	archive, err := zip.NewReader(bytes.NewReader(response.Body.Bytes()), int64(response.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, file := range archive.File {
		names[file.Name] = true
		_, _ = io.ReadAll(func() io.Reader { r, _ := file.Open(); return r }())
	}
	if !names["library/ab/cd/ef/abcdef0123456789"] || !names["catalog/catalog"] {
		t.Fatalf("zip members: %v", names)
	}
}
