package remotree

import (
	"archive/zip"
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/fail"
	"github.com/robocorp/rcc/htfs"
	"github.com/robocorp/rcc/operations"
	"github.com/robocorp/rcc/pathlib"
	"github.com/robocorp/rcc/set"
)

func isSelfRequest(request *http.Request) bool {
	identity, ok := request.Header[operations.X_RCC_RANDOM_IDENTITY]
	return ok && len(identity) > 0 && identity[0] == common.RandomIdentifier()
}

func makeDeltaHandler(queries Partqueries) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		catalog := filepath.Base(request.URL.Path)
		defer common.Stopwatch("Delta of catalog %q took", catalog).Debug()
		if request.Method != http.MethodPost {
			response.WriteHeader(http.StatusMethodNotAllowed)
			common.Trace("Delta: rejecting request %q for catalog %q.", request.Method, catalog)
			return
		}
		if isSelfRequest(request) {
			response.WriteHeader(http.StatusConflict)
			common.Trace("Delta: rejecting /SELF/ request for catalog %q.", catalog)
			return
		}
		reply := make(chan string)
		queries <- &Partquery{
			Catalog: catalog,
			Reply:   reply,
		}
		known, ok := <-reply
		common.Debug("query handler: %q -> %v", catalog, ok)
		if !ok {
			response.WriteHeader(http.StatusNotFound)
			response.Write([]byte("404 not found, sorry"))
			return
		}

		membership := set.Membership(strings.Split(known, "\n"))

		approved := make([]string, 0, 1000)
		todo := bufio.NewReader(request.Body)
	todoloop:
		for {
			line, err := todo.ReadString('\n')
			stopping := err == io.EOF
			candidate := filepath.Base(strings.TrimSpace(line))
			if len(candidate) > 10 {
				if membership[candidate] {
					approved = append(approved, candidate)
				} else {
					common.Trace("DELTA: ignoring extra %q entry, not part of set!", candidate)
					if !stopping {
						continue todoloop
					}
				}
			}
			if stopping {
				break todoloop
			}
			if err != nil {
				common.Trace("DELTA: error %v with line %q", err, line)
				break todoloop
			}
		}

		partfile, err := exportMissing(catalog, approved)
		if err != nil {
			common.Debug("DELTA: error %v", err)
			response.WriteHeader(http.StatusInternalServerError)
			return
		}

		http.ServeFile(response, request, partfile)
	}
}

func tempDir() (string, bool) {
	root := pathlib.TempDir()
	directory := filepath.Join(root, "rccremote")
	fullpath, err := pathlib.EnsureDirectory(directory)
	if err != nil {
		return root, false
	}
	return fullpath, true
}

func exportMissing(catalog string, missing []string) (result string, err error) {
	defer fail.Around(&err)

	tempdir, _ := tempDir()
	identity := common.Digest(strings.Join(missing, "\n"))
	filename := filepath.Join(tempdir, fmt.Sprintf("%s_parts.zip", identity))
	if pathlib.IsFile(filename) {
		common.Debug("Using existing cache file %q [size: %s]", filename, pathlib.HumaneSize(filename))
		return filename, nil
	}

	tempfile := filepath.Join(tempdir, fmt.Sprintf("%s_%x_build.zip", identity, os.Getppid()))
	err = exportMissingToFile(catalog, missing, tempfile)
	fail.On(err != nil, "%v", err)

	err = os.Rename(tempfile, filename)
	fail.On(err != nil, "%v", err)

	common.Debug("Created cache file %q [size: %s]", filename, pathlib.HumaneSize(filename))
	return filename, nil
}

func exportMissingToFile(catalog string, missing []string, filename string) (err error) {
	defer fail.Around(&err)

	handle, err := pathlib.Create(filename)
	fail.On(err != nil, "Could not create export file %q, reason: %v", filename, err)
	defer handle.Close()

	sink := zip.NewWriter(handle)
	defer sink.Close()

	for _, member := range missing {
		relative := htfs.RelativeDefaultLocation(member)
		fullpath := htfs.ExactDefaultLocation(member)
		err = operations.ZipAppend(sink, fullpath, relative)
		fail.On(err != nil, "Could not zip file %q, reason: %v", fullpath, err)
	}

	fullpath := filepath.Join(common.HololibCatalogLocation(), catalog)
	relative, err := filepath.Rel(common.HololibLocation(), fullpath)
	fail.On(err != nil, "Could not get relative path for catalog %q, reason: %v", fullpath, err)
	err = operations.ZipAppend(sink, fullpath, relative)
	fail.On(err != nil, "Could not zip catalog %q, reason: %v", fullpath, err)
	return nil
}
