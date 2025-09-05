package remotree

import (
	"bufio"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/fail"
	"github.com/robocorp/rcc/htfs"
	"github.com/robocorp/rcc/set"
)

const (
	partCacheSize = 20
)

func makeQueryHandler(queries Partqueries, triggers chan string) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		catalog := filepath.Base(request.URL.Path)
		defer common.Stopwatch("Query of catalog %q took", catalog).Debug()
		if request.Method != http.MethodGet {
			response.WriteHeader(http.StatusMethodNotAllowed)
			common.Trace("Query: rejecting request %q for catalog %q.", request.Method, catalog)
			return
		}
		if isSelfRequest(request) {
			response.WriteHeader(http.StatusConflict)
			common.Trace("Query: rejecting /SELF/ request for catalog %q.", catalog)
			return
		}
		reply := make(chan string)
		queries <- &Partquery{
			Catalog: catalog,
			Reply:   reply,
		}
		content, ok := <-reply
		common.Debug("query handler: %q -> %v", catalog, ok)
		if !ok {
			triggers <- catalog
			response.WriteHeader(http.StatusNotFound)
			response.Write([]byte("404 not found, sorry"))
			return
		}
		headers := response.Header()
		headers.Add("Content-Type", "text/plain")
		response.WriteHeader(http.StatusOK)
		writer := bufio.NewWriter(response)
		defer writer.Flush()
		writer.WriteString(content)
	}
}

func loadSingleCatalog(catalog string) (root *htfs.Root, err error) {
	defer fail.Around(&err)
	tempdir := filepath.Join(common.ProductTemp(), "rccremote")
	shadow, err := htfs.NewRoot(tempdir)
	fail.On(err != nil, "Could not create root, reason: %v", err)
	filename := filepath.Join(common.HololibCatalogLocation(), catalog)
	err = shadow.LoadFrom(filename)
	fail.On(err != nil, "Could not load root, reason: %v", err)
	common.Trace("Catalog %q loaded.", catalog)
	return shadow, nil
}

func loadCatalogParts(catalog string) (string, bool) {
	catalogs := htfs.CatalogNames()
	if !set.Member(catalogs, catalog) {
		return "", false
	}
	root, err := loadSingleCatalog(catalog)
	if err != nil {
		return "", false
	}
	collector := make(map[string]string)
	task := htfs.DigestMapper(collector)
	err = task(root.Path, root.Tree)
	if err != nil {
		return "", false
	}
	keys := set.Keys(collector)
	return strings.Join(keys, "\n"), true
}

func listProvider(queries Partqueries) {
	cache := make(map[string]string)
	keys := make([]string, partCacheSize)
	cursor := uint64(0)
loop:
	for {
		query, ok := <-queries
		if !ok {
			break loop
		}
		known, ok := cache[query.Catalog]
		if ok {
			query.Reply <- known
			close(query.Reply)
			continue
		}
		created, ok := loadCatalogParts(query.Catalog)
		if !ok {
			close(query.Reply)
			continue
		}
		delete(cache, keys[cursor%partCacheSize])
		cache[query.Catalog] = created
		keys[cursor] = query.Catalog
		cursor += 1
		query.Reply <- created
		close(query.Reply)
	}
}
