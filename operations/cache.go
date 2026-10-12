package operations

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/pathlib"
	"github.com/joshyorko/rcc/set"
	"github.com/joshyorko/rcc/xviper"

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
		if os.IsNotExist(err) {
			return result.Ready(), nil
		}
		return nil, fmt.Errorf("failed to open cache %q: %w", cacheFile, err)
	}
	decoder := yaml.NewDecoder(source)
	err = decoder.Decode(&result)
	closeErr := source.Close()
	restrictErr := pathlib.RestrictOwnerOnly(cacheFile)
	if err != nil {
		return nil, errors.Join(err, closeErr, restrictErr)
	}
	if err := errors.Join(closeErr, restrictErr); err != nil {
		return nil, err
	}
	return result.Ready(), nil
}

func encodeCache(sink io.WriteCloser, value *Cache) error {
	encoder := yaml.NewEncoder(sink)
	encodeErr := encoder.Encode(value)
	encoderCloseErr := encoder.Close()
	sinkCloseErr := sink.Close()
	return errors.Join(encodeErr, encoderCloseErr, sinkCloseErr)
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
	writeErr := encodeCache(sink, it)
	permissionErr := pathlib.RestrictOwnerOnly(cacheFile)
	return errors.Join(writeErr, permissionErr)
}
