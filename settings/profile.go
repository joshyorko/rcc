package settings

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/fail"
	"github.com/robocorp/rcc/pathlib"
	"gopkg.in/yaml.v2"
)

type Profile struct {
	Name         string    `yaml:"name" json:"name"`
	Description  string    `yaml:"description" json:"description"`
	Settings     *Settings `yaml:"settings,omitempty" json:"settings,omitempty"`
	PipRc        string    `yaml:"piprc,omitempty" json:"piprc,omitempty"`
	MicroMambaRc string    `yaml:"micromambarc,omitempty" json:"micromambarc,omitempty"`
	CaBundle     string    `yaml:"ca-bundle,omitempty" json:"ca-bundle,omitempty"`
}

func (it *Profile) AsYaml() ([]byte, error) {
	content, err := yaml.Marshal(it)
	if err != nil {
		return nil, err
	}
	return content, nil
}

func (it *Profile) SaveAs(filename string) error {
	body, err := it.AsYaml()
	if err != nil {
		return err
	}
	return pathlib.WriteFile(filename, body, 0o666)
}

func (it *Profile) LoadFrom(filename string) error {
	raw, err := os.ReadFile(filename)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(raw, it)
}

func (it *Profile) Import() (err error) {
	basename := fmt.Sprintf("profile_%s.yaml", strings.ToLower(it.Name))
	filename := common.ExpandPath(filepath.Join(common.Product.Home(), basename))
	return it.SaveAs(filename)
}

func (it *Profile) Activate() (err error) {
	defer fail.Around(&err)

	err = it.Remove()
	fail.On(err != nil, "%s", err)
	if it.Settings != nil {
		body, err := it.Settings.AsYaml()
		fail.On(err != nil, "Failed to parse settings.yaml, reason: %v", err)
		err = saveIfBody(common.SettingsFile(), body)
		fail.On(err != nil, "Failed to save settings.yaml, reason: %v", err)
	}
	err = saveIfBody(common.PipRcFile(), []byte(it.PipRc))
	fail.On(err != nil, "Failed to save piprc, reason: %v", err)
	err = saveIfBody(common.MicroMambaRcFile(), []byte(it.MicroMambaRc))
	fail.On(err != nil, "Failed to save micromambarc, reason: %v", err)
	err = saveIfBody(common.CaBundleFile(), []byte(it.CaBundle))
	fail.On(err != nil, "Failed to save ca-bundle.pem, reason: %v", err)
	return nil
}

func (it *Profile) Remove() (err error) {
	defer fail.Around(&err)

	err = removeIfExists(common.PipRcFile())
	fail.On(err != nil, "Failed to remove piprc, reason: %v", err)
	err = removeIfExists(common.MicroMambaRcFile())
	fail.On(err != nil, "Failed to remove micromambarc, reason: %v", err)
	err = removeIfExists(common.SettingsFile())
	fail.On(err != nil, "Failed to remove settings.yaml, reason: %v", err)
	err = removeIfExists(common.CaBundleFile())
	fail.On(err != nil, "Failed to remove ca-bundle.pem, reason: %v", err)
	return nil
}

func removeIfExists(filename string) error {
	if !pathlib.Exists(filename) {
		return nil
	}
	return os.Remove(filename)
}

func saveIfBody(filename string, body []byte) error {
	if body != nil && len(body) > 0 {
		return pathlib.WriteFile(filename, body, 0o666)
	}
	return nil
}
