package operations

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/pathlib"
	"github.com/robocorp/rcc/set"
	"github.com/robocorp/rcc/xviper"

	"gopkg.in/yaml.v2"
)

type Credential struct {
	Account  string `yaml:"account"`
	Context  string `yaml:"context"`
	Token    string `yaml:"token"`
	Deadline int64  `yaml:"deadline"`
}

type CredentialMap map[string]*Credential
type StampMap map[string]int64

type Cache struct {
	Credentials CredentialMap `yaml:"credentials"`
	Stamps      StampMap      `yaml:"stamps"`
	Users       []string      `yaml:"users"`
}

func (it Cache) Ready() *Cache {
	if it.Credentials == nil {
		it.Credentials = make(CredentialMap)
	}
	if it.Stamps == nil {
		it.Stamps = make(StampMap)
	}
	if it.Users == nil {
		it.Users = []string{}
	} else {
		it.Users = it.Userset()
	}
	return &it
}

func (it Cache) Userset() []string {
	result := make([]string, 0, len(it.Users))
	for _, username := range it.Users {
		result, _ = set.Update(result, strings.ToLower(username))
	}
	return result
}

func cacheLockFile() string {
	return fmt.Sprintf("%s.lck", cacheLocation())
}

func cacheLocation() string {
	reference := xviper.ConfigFileUsed()
	if len(reference) > 0 {
		return filepath.Join(filepath.Dir(reference), "rcccache.yaml")
	} else {
		return filepath.Join(common.Product.Home(), "rcccache.yaml")
	}
}

func SummonCache() (*Cache, error) {
	var result Cache
	lockfile := cacheLockFile()
	completed := pathlib.LockWaitMessage(lockfile, "Serialized cache access [cache lock]")
	locker, err := pathlib.Locker(lockfile, 125, false)
	completed()
	if err != nil {
		return nil, err
	}
	defer locker.Release()

	cacheFile := cacheLocation()
	source, err := os.Open(cacheFile)
	if err != nil {
		return result.Ready(), nil
	}
	defer source.Close()
	defer pathlib.RestrictOwnerOnly(cacheFile)
	decoder := yaml.NewDecoder(source)
	err = decoder.Decode(&result)
	if err != nil {
		return nil, err
	}
	return result.Ready(), nil
}

func (it *Cache) Save() error {
	if common.WarrantyVoided() {
		return nil
	}
	lockfile := cacheLockFile()
	completed := pathlib.LockWaitMessage(lockfile, "Serialized cache access [cache lock]")
	locker, err := pathlib.Locker(lockfile, 125, false)
	completed()
	if err != nil {
		return err
	}
	defer locker.Release()

	cacheFile := cacheLocation()
	sink, err := pathlib.Create(cacheFile)
	if err != nil {
		return err
	}
	defer sink.Close()
	defer pathlib.RestrictOwnerOnly(cacheFile)
	encoder := yaml.NewEncoder(sink)
	err = encoder.Encode(it)
	if err != nil {
		return err
	}
	return nil
}
