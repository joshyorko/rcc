package cmd

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"github.com/robocorp/rcc/cloud"
	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/operations"
	"github.com/robocorp/rcc/pathlib"
	"github.com/robocorp/rcc/pretty"
	"github.com/spf13/cobra"
)

func isUrl(name string) bool {
	link, err := url.Parse(name)
	if err != nil {
		return false
	}
	return link.IsAbs() && (link.Scheme == "http" || link.Scheme == "https")
}

func temporaryDownload(at int, link string) (string, error) {
	common.Timeline("Download %v", link)
	zipfile := filepath.Join(pathlib.TempDir(), fmt.Sprintf("hololib%x%x.zip", common.When, at))
	err := cloud.Download(link, zipfile)
	if err != nil {
		return "", err
	}
	return zipfile, nil
}

func reportAllErrors(filename string, errors []error) error {
	if errors == nil || len(errors) == 0 {
		return nil
	}
	if len(errors) == 1 {
		return errors[0]
	}
	common.Log("Errors from zip %q:", filename)
	for at, err := range errors {
		common.Log("- %d: %v", at+1, err)
	}
	return errors[0]
}

var holotreeImportCmd = &cobra.Command{
	Use:   "import hololib.zip+",
	Short: "Import one or more hololib.zip files into local hololib.",
	Long:  "Import one or more hololib.zip files into local hololib.",
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		var err error

		if common.DebugFlag() {
			defer common.Stopwatch("Holotree import command lasted").Report()
		}
		for at, filename := range args {
			if isUrl(filename) {
				filename, err = temporaryDownload(at, filename)
				pretty.Guard(err == nil, 2, "Could not download %q, reason: %v", filename, err)
				defer os.Remove(filename)
			}
			if common.StrictFlag {
				errors := operations.VerifyZip(filename, operations.HololibZipShape)
				err = reportAllErrors(filename, errors)
				pretty.Guard(err == nil, 3, "Could not verify %q, first reason: %v", filename, err)
			}
			err = operations.ProtectedImport(filename)
			pretty.Guard(err == nil, 1, "Could not import %q, reason: %v", filename, err)
		}
		pretty.Ok()
	},
}

func init() {
	holotreeCmd.AddCommand(holotreeImportCmd)
}
