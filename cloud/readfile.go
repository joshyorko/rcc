package cloud

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/pathlib"
)

func ReadFile(resource string) ([]byte, error) {
	if pathlib.IsFile(resource) {
		return os.ReadFile(resource)
	}
	link, err := url.ParseRequestURI(resource)
	if err != nil {
		return os.ReadFile(resource)
	}
	if link.Scheme == "file" || link.Scheme == "" || pathlib.IsFile(link.Path) {
		return os.ReadFile(link.Path)
	}
	tempfile := filepath.Join(pathlib.TempDir(), fmt.Sprintf("temp%x.part", common.When))
	defer os.Remove(tempfile)
	err = Download(resource, tempfile)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(tempfile)
}
