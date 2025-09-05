package conda

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/robocorp/rcc/blobs"
	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/pathlib"
	"github.com/robocorp/rcc/pretty"
	"github.com/robocorp/rcc/settings"
	"github.com/robocorp/rcc/shell"
	"github.com/robocorp/rcc/xviper"
)

var (
	ignoredPaths = []string{
		"python",
		"conda",
		"pyenv",
		"venv",
		"pypoetry",
		".poetry",
		"virtualenv",
	}
	hashPattern    = regexp.MustCompile("^[0-9a-f]{16}(?:\\.meta)?$")
	versionPattern = regexp.MustCompile("^[^0-9]*([0-9.]+).*$")
)

func micromambaLink(platform, filename string) string {
	return fmt.Sprintf("micromamba/%s/%s/%s", blobs.MicromambaVersion(), platform, filename)
}

func sorted(files []os.FileInfo) {
	sort.SliceStable(files, func(left, right int) bool {
		return files[left].Name() < files[right].Name()
	})
}

func ignoreDynamicDirectories(folder, entryName string) bool {
	base := strings.ToLower(filepath.Base(folder))
	name := strings.ToLower(entryName)
	return name == "__pycache__" || (name == "gen" && base == "comtypes")
}

func DigestFor(folder string, collect map[string]string) ([]byte, error) {
	handle, err := os.Open(folder)
	if err != nil {
		return nil, err
	}
	defer handle.Close()
	entries, err := handle.Readdir(-1)
	if err != nil {
		return nil, err
	}
	digester := sha256.New()
	sorted(entries)
	for _, entry := range entries {
		if entry.IsDir() {
			if ignoreDynamicDirectories(folder, entry.Name()) {
				continue
			}
			digest, err := DigestFor(filepath.Join(folder, entry.Name()), collect)
			if err != nil {
				return nil, err
			}
			digester.Write(digest)
			continue
		}
		repr := fmt.Sprintf("%s -- %x", entry.Name(), entry.Size())
		digester.Write([]byte(repr))
	}
	result := digester.Sum([]byte{})
	if collect != nil {
		key := fmt.Sprintf("%02x", result)
		collect[folder] = key
	}
	return result, nil
}

func HolotreePath(environment string) pathlib.PathParts {
	return pathlib.PathFrom(CondaPaths(environment)...)
}

func FindPath(environment string) pathlib.PathParts {
	target := pathlib.TargetPath()
	target = target.Remove(ignoredPaths)
	target = target.Prepend(CondaPaths(environment)...)
	return target
}

func FindPython(location string) (string, bool) {
	holotreePath := HolotreePath(location)
	python, ok := holotreePath.Which("python3", FileExtensions)
	if ok {
		return python, ok
	}
	return holotreePath.Which("python", FileExtensions)
}

func FindUv(location string) (string, bool) {
	holotreePath := HolotreePath(location)
	uv, ok := holotreePath.Which("uv", FileExtensions)
	if ok {
		return uv, ok
	}
	return holotreePath.Which("uv", FileExtensions)
}

func injectNetworkEnvironment(environment []string) []string {
	if settings.Global.NoRevocation() {
		environment = append(environment, "MAMBA_SSL_NO_REVOKE=true")
	}
	if !settings.Global.VerifySsl() {
		environment = append(environment, "MAMBA_SSL_VERIFY=false")
		environment = append(environment, "RC_DISABLE_SSL=true")
		environment = append(environment, "WDM_SSL_VERIFY=0")
		environment = append(environment, "NODE_TLS_REJECT_UNAUTHORIZED=0")
	}
	if settings.Global.LegacyRenegotiation() {
		environment = append(environment, "RC_TLS_LEGACY_RENEGOTIATION_ALLOWED=true")
	}
	environment = appendIfValue(environment, "https_proxy", settings.Global.HttpsProxy())
	environment = appendIfValue(environment, "HTTPS_PROXY", settings.Global.HttpsProxy())
	environment = appendIfValue(environment, "http_proxy", settings.Global.HttpProxy())
	environment = appendIfValue(environment, "HTTP_PROXY", settings.Global.HttpProxy())
	environment = appendIfValue(environment, "no_proxy", settings.Global.NoProxy())
	environment = appendIfValue(environment, "NO_PROXY", settings.Global.NoProxy())
	if common.WarrantyVoided() {
		environment = append(environment, "RCC_WARRANTY_VOIDED=true")
	}
	return environment
}

func removeIncompatibleEnvironmentVariables(environment []string, unwanted ...string) []string {
	result := make([]string, 0, len(environment))
search:
	for _, here := range environment {
		parts := strings.Split(strings.TrimSpace(here), "=")
		for _, name := range unwanted {
			if strings.EqualFold(name, parts[0]) {
				pretty.Warning("Removing incompatible variable %q from environment.", here)
				continue search
			}
		}
		result = append(result, here)
	}
	return result
}

func CondaExecutionEnvironment(location string, inject []string, full bool) []string {
	environment := make([]string, 0, 100)
	if full {
		environment = append(environment, os.Environ()...)
		environment = removeIncompatibleEnvironmentVariables(environment, "VIRTUAL_ENV")
	}
	if inject != nil && len(inject) > 0 {
		environment = append(environment, inject...)
	}
	holotreePath := HolotreePath(location)
	python, ok := holotreePath.Which("python3", FileExtensions)
	if !ok {
		python, ok = holotreePath.Which("python", FileExtensions)
	}
	if ok {
		environment = append(environment, "PYTHON_EXE="+python)
	}
	if !common.DisablePycManagement() {
		environment = append(environment,
			"PYTHONDONTWRITEBYTECODE=x",
			"PYTHONPYCACHEPREFIX="+common.ProductTemp(),
		)
	} else {
		common.Timeline(".pyc file management was disabled.")
	}
	if !common.DisableTempManagement() {
		environment = append(environment,
			"TEMP="+common.ProductTemp(),
			"TMP="+common.ProductTemp(),
		)
	} else {
		common.Timeline("temp directory management was disabled.")
	}
	environment = append(environment,
		"CONDA_DEFAULT_ENV=rcc",
		"CONDA_PREFIX="+location,
		"CONDA_PROMPT_MODIFIER=(rcc) ",
		"CONDA_SHLVL=1",
		"PYTHONHOME=",
		"PYTHONSTARTUP=",
		"PYTHONEXECUTABLE=",
		"PYTHONNOUSERSITE=1",
		fmt.Sprintf("%s=%s", common.Product.HomeVariable(), common.Product.Home()),
		"RCC_ENVIRONMENT_HASH="+common.EnvironmentHash,
		"RCC_INSTALLATION_ID="+xviper.TrackingIdentity(),
		"RCC_HOLOTREE_SPACE_ROOT="+location,
		"RCC_TRACKING_ALLOWED="+fmt.Sprintf("%v", xviper.CanTrack()),
		"RCC_EXE="+common.BinRcc(),
		"RCC_VERSION="+common.Version,
		FindPath(location).AsEnvironmental("PATH"),
	)
	environment = append(environment, LoadActivationEnvironment(location)...)
	environment = injectNetworkEnvironment(environment)
	if settings.Global.HasPipRc() {
		environment = appendIfValue(environment, "PIP_CONFIG_FILE", common.PipRcFile())
	}
	if settings.Global.HasCaBundle() {
		environment = appendIfValue(environment, "REQUESTS_CA_BUNDLE", common.CaBundleFile())
		environment = appendIfValue(environment, "CURL_CA_BUNDLE", common.CaBundleFile())
		environment = appendIfValue(environment, "SSL_CERT_FILE", common.CaBundleFile())
		environment = appendIfValue(environment, "NODE_EXTRA_CA_CERTS", common.CaBundleFile())
	}
	return environment
}

func appendIfValue(environment []string, key, value string) []string {
	if len(value) > 0 {
		return append(environment, key+"="+value)
	}
	return environment
}

func AsVersion(incoming string) (uint64, string) {
	incoming = strings.TrimSpace(incoming)
	versionText := "0"
search:
	for _, line := range strings.SplitN(incoming, "\n", -1) {
		found := versionPattern.FindStringSubmatch(line)
		if found != nil {
			versionText = found[1]
			break search
		}
	}
	parts := strings.SplitN(versionText, ".", 4)
	steps := len(parts)
	multipliers := []uint64{1000000, 1000, 1}
	version := uint64(0)
	for at, multiplier := range multipliers {
		if steps <= at {
			break
		}
		value, err := strconv.ParseUint(parts[at], 10, 64)
		if err != nil {
			break
		}
		version += multiplier * value
	}
	return version, versionText
}

func UvVersion(uv string) string {
	environment := CondaExecutionEnvironment(".", nil, true)
	versionText, _, err := shell.New(environment, ".", uv, "--version").CaptureOutput()
	if err != nil {
		return err.Error()
	}
	_, versionText = AsVersion(versionText)
	return versionText
}

func PipVersion(python string) string {
	environment := CondaExecutionEnvironment(".", nil, true)
	versionText, _, err := shell.New(environment, ".", python, "-m", "pip", "--version").CaptureOutput()
	if err != nil {
		return err.Error()
	}
	_, versionText = AsVersion(versionText)
	return versionText
}

func MicromambaVersion() string {
	versionText, _, err := shell.New(CondaEnvironment(), ".", BinMicromamba(), "--repodata-ttl", "90000", "--version").CaptureOutput()
	if err != nil {
		return err.Error()
	}
	_, versionText = AsVersion(versionText)
	return versionText
}

func HasMicroMamba() bool {
	if !pathlib.IsFile(BinMicromamba()) {
		return false
	}
	version, versionText := AsVersion(MicromambaVersion())
	goodEnough := version >= blobs.MicromambaVersionLimit
	common.Debug("%q version is %q -> %v (good enough: %v)", BinMicromamba(), versionText, version, goodEnough)
	common.Timeline("µmamba version is %q (at %q).", versionText, BinMicromamba())
	return goodEnough
}

func LocalChannel() (string, bool) {
	basefolder := filepath.Join(common.Product.Home(), "channel")
	fullpath := filepath.Join(basefolder, "channeldata.json")
	stats, err := os.Stat(fullpath)
	if err != nil {
		return "", false
	}
	if !stats.IsDir() {
		return basefolder, true
	}
	return "", false
}
