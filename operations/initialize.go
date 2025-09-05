package operations

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/robocorp/rcc/blobs"
	"github.com/robocorp/rcc/cloud"
	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/fail"
	"github.com/robocorp/rcc/pathlib"
	"github.com/robocorp/rcc/pretty"
	"github.com/robocorp/rcc/settings"
	"gopkg.in/yaml.v2"
)

type StringMap map[string]string
type StringPair [2]string
type StringPairList []StringPair

type MetaTemplates struct {
	Date      string    `yaml:"date"`
	Hash      string    `yaml:"hash"`
	Templates StringMap `yaml:"templates"`
	Url       string    `yaml:"url"`
}

func (it *MetaTemplates) MatchingHash(hash string) bool {
	return strings.EqualFold(strings.TrimSpace(hash), strings.TrimSpace(it.Hash))
}

func (it StringPairList) Len() int {
	return len(it)
}

func (it StringPairList) Less(left, right int) bool {
	return it[left][0] < it[right][0]
}

func (it StringPairList) Swap(left, right int) {
	it[left], it[right] = it[right], it[left]
}

func parseTemplateInfo(raw []byte) (ingore *MetaTemplates, err error) {
	var metadata MetaTemplates
	err = yaml.Unmarshal(raw, &metadata)
	if err != nil {
		return nil, err
	}
	return &metadata, nil
}

func TemplateInfo(filename string) (ingore *MetaTemplates, err error) {
	defer fail.Around(&err)

	raw, err := os.ReadFile(filename)
	fail.On(err != nil, "Failure reading %q, reason: %v", filename, err)
	metadata, err := parseTemplateInfo(raw)
	fail.On(err != nil, "Failure parsing %q, reason: %v", filename, err)
	return metadata, nil
}

func templatesYamlPart() string {
	return filepath.Join(common.TemplateLocation(), "templates.yaml.part")
}

func templatesYamlFinal() string {
	return filepath.Join(common.TemplateLocation(), "templates.yaml")
}

func templatesZipPart() string {
	return filepath.Join(common.TemplateLocation(), "templates.zip.part")
}

func TemplatesZip() string {
	return filepath.Join(common.TemplateLocation(), "templates.zip")
}

func needNewTemplates() (ignore *MetaTemplates, err error) {
	defer fail.Around(&err)

	metadata := settings.Global.TemplatesYamlURL()
	if len(metadata) == 0 {
		common.Debug("No URL for templates.yaml available.")
		return nil, nil
	}
	partfile := templatesYamlPart()
	err = cloud.Download(metadata, partfile)
	fail.On(err != nil, "Failure loading %q, reason: %s", metadata, err)
	meta, err := TemplateInfo(partfile)
	fail.On(err != nil, "%s", err)
	fail.On(!strings.HasPrefix(meta.Url, "https:"), "Location for templates.zip is not https: %q", meta.Url)
	hash, err := pathlib.Sha256(TemplatesZip())
	if err != nil || !meta.MatchingHash(hash) {
		return meta, nil
	}
	return nil, nil
}

func activeTemplateInfo(internal bool) (*MetaTemplates, error) {
	if !internal {
		meta, err := TemplateInfo(templatesYamlFinal())
		if err == nil {
			return meta, nil
		}
	}
	raw, err := blobs.Asset("assets/templates.yaml")
	if err != nil {
		return nil, err
	}
	return parseTemplateInfo(raw)
}

func downloadTemplatesZip(meta *MetaTemplates) (err error) {
	defer fail.Around(&err)

	partfile := templatesZipPart()
	err = cloud.Download(meta.Url, partfile)
	fail.On(err != nil, "Failure loading %q, reason: %s", meta.Url, err)
	hash, err := pathlib.Sha256(partfile)
	fail.On(err != nil, "Failure hashing %q, reason: %s", partfile, err)
	fail.On(!meta.MatchingHash(hash), "Received broken templates.zip, hash mismatch from expected %q vs. actual %q", meta.Hash, hash)
	return nil
}

func ensureUpdatedTemplates() {
	err := updateTemplates()
	if err != nil {
		pretty.Warning("Problem updating templates.zip, reason: %v", err)
	}
}

func updateTemplates() (err error) {
	defer fail.Around(&err)

	defer os.Remove(templatesZipPart())
	defer os.Remove(templatesYamlPart())

	meta, err := needNewTemplates()
	fail.On(err != nil, "%s", err)
	if meta == nil {
		return nil
	}
	err = downloadTemplatesZip(meta)
	fail.On(err != nil, "%s", err)
	err = os.Rename(templatesYamlPart(), templatesYamlFinal())
	alt := os.Rename(templatesZipPart(), TemplatesZip())
	fail.On(alt != nil, "%s", alt)
	fail.On(err != nil, "%s", err)
	return nil
}

func unpack(content []byte, directory string) error {
	common.Debug("Initializing:")
	size := int64(len(content))
	byter := bytes.NewReader(content)
	reader, err := zip.NewReader(byter, size)
	if err != nil {
		return err
	}
	success := true
	for _, entry := range reader.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		target := filepath.Join(directory, entry.Name)
		todo := WriteTarget{
			Source: entry,
			Target: target,
		}
		success = todo.Execute() && success
	}
	common.Debug("Done.")
	if !success {
		return fmt.Errorf("Problems while initializing robot. Use --debug to see details.")
	}
	return nil
}

func ListTemplatesWithDescription(internal bool) StringPairList {
	ensureUpdatedTemplates()
	result := make(StringPairList, 0, 10)
	meta, err := activeTemplateInfo(internal)
	if err != nil {
		pretty.Warning("Problem getting template list, reason: %v", err)
		return result
	}
	for name, description := range meta.Templates {
		result = append(result, StringPair{name, description})
	}
	sort.Sort(result)
	return result
}

func ListTemplates(internal bool) []string {
	ensureUpdatedTemplates()
	pairs := ListTemplatesWithDescription(internal)
	result := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		result = append(result, pair[0])
	}
	return result
}

func templateByName(name string, internal bool) ([]byte, error) {
	zipfile := TemplatesZip()
	blobname := fmt.Sprintf("assets/%s.zip", name)
	if internal || !pathlib.IsFile(zipfile) {
		return blobs.Asset(blobname)
	}
	unzipper, err := newUnzipper(zipfile, false)
	if err != nil {
		return nil, err
	}
	defer unzipper.Close()
	zipname := fmt.Sprintf("%s.zip", name)
	blob, err := unzipper.Asset(zipname)
	if err != nil {
		return nil, err
	}
	if blob != nil {
		return blob, nil
	}
	return blobs.Asset(blobname)
}

func InitializeWorkarea(directory, name string, internal, force bool) error {
	ensureUpdatedTemplates()
	content, err := templateByName(name, internal)
	if err != nil {
		return err
	}
	fullpath, err := filepath.Abs(directory)
	if err != nil {
		return err
	}
	if force {
		err = pathlib.EnsureDirectoryExists(fullpath)
	} else {
		err = pathlib.EnsureEmptyDirectory(fullpath)
	}
	if err != nil {
		return err
	}
	return unpack(content, fullpath)
}
