package conda

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/settings"
)

func fileIdentity(path string) (string, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// Store only the digest in blueprints. Profile contents may contain credentials.
func providerResolutionIdentity(layer ProviderLayer) (string, error) {
	config, err := settings.SummonSettings()
	if err != nil {
		return "", err
	}
	fields := map[string]interface{}{"platform": common.Platform(), "certificates": config.Certificates, "network": config.Network}
	files := map[string]string{"ca": common.CaBundleFile()}
	if (layer.Provider == "rattler" || layer.Provider == "micromamba") && slices.Contains(layer.Channels, "local") {
		if channel, ok := LocalChannel(); ok {
			for _, subdir := range []string{rattlerSubdir(), "noarch"} {
				files["channel-"+subdir] = filepath.Join(channel, subdir, "repodata.json")
			}
		}
	}
	switch layer.Provider {
	case "rattler":
		fields["helper"] = rattlerHelperSHA256

	case "micromamba":
		fields["conda"] = config.Endpoints["conda"]
		files["micromambarc"] = common.MicroMambaRcFile()
	case "pip", "uv":
		fields["pypi"] = config.Endpoints["pypi"]
		fields["trusted"] = config.Endpoints["pypi-trusted"]
		files["piprc"] = common.PipRcFile()
		entries, err := os.ReadDir(common.WheelCache())
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".whl") {
				files["wheel-"+entry.Name()] = filepath.Join(common.WheelCache(), entry.Name())
			}
		}
	}
	for key, path := range files {
		value, err := fileIdentity(path)
		if err != nil {
			return "", err
		}
		fields[key] = value
	}
	body, err := json.Marshal(fields)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(body)), nil
}
