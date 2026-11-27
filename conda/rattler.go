package conda

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/joshyorko/rcc/blobs"
	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/settings"
)

// Set by the native candidate build after building the locked helper.
var rattlerHelperSHA256 string

func installRattlerLayer(prefix string, layer ProviderLayer) error {
	verified, err := providerExecutable(layer)
	if err != nil {
		return err
	}
	channels, network, err := rattlerChannels(layer.Channels)
	if err != nil {
		return err
	}
	platform := rattlerSubdir()
	if platform == "" {
		return errors.New("unsupported Rattler platform")
	}
	private, err := os.MkdirTemp("", "rcc-rattler-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(private) }()
	request, err := json.Marshal(map[string]interface{}{"protocol": 1, "prefix": prefix, "cache": filepath.Join(common.Product.Home(), "rattler-cache"), "platform": platform, "network": network, "channels": channels, "packages": layer.Packages})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, verified)
	cmd.Dir = private
	cmd.Env = []string{"HOME=" + private, "TMPDIR=" + private, "TEMP=" + private, "TMP=" + private}
	if runtime.GOOS == "windows" {
		cmd.Env = append(cmd.Env, "SystemRoot="+os.Getenv("SystemRoot"))
	}
	cmd.Stdin = bytes.NewReader(request)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("rattler install: %w", err)
	}
	return nil
}

func embeddedTool(tool string) (string, error) {
	version, digest, size, ok := blobs.ProviderDefaultInfo(tool)
	if !ok {
		return "", fmt.Errorf("embedded %s default is unavailable in this build", tool)
	}
	path := filepath.Join(common.Product.Home(), "provider-tools", tool, version, common.Platform(), digest, nativeToolName(tool))
	if verifiedExecutable(path, digest, size) {
		return path, nil
	}
	stream, _, _, err := blobs.OpenProviderDefault(tool)
	if err != nil {
		return "", err
	}
	defer func() { _ = stream.Close() }()
	if err = publishExecutable(path, digest, size, stream); err != nil {
		return "", err
	}
	common.PlatformSyncDelay()
	return path, nil
}

func providerExecutable(layer ProviderLayer) (string, error) {
	if layer.NativeToolPin != nil {
		return ResolveNativeToolPin(common.Product.Home(), layer.Provider, *layer.NativeToolPin, os.Getenv("RCC_"+strings.ToUpper(layer.Provider)+"_SOURCE"))
	}
	if layer.Provider == "micromamba" {
		if MustMicromamba() {
			return BinMicromamba(), nil
		}
		return "", errors.New("micromamba unavailable")
	}
	if layer.Provider == "uv" || layer.Provider == "uv-runtime" {
		if layer.ToolLock != nil {
			return ResolveLockedUv(*layer.ToolLock)
		}
		return embeddedTool("uv")
	}
	if _, _, _, ok := blobs.ProviderDefaultInfo("rattler"); ok {
		return embeddedTool("rattler")
	}
	// Development-only sidecar, authenticated with the hash set by the native build.
	digest, err := hex.DecodeString(rattlerHelperSHA256)
	if err != nil || len(digest) != 32 {
		return "", errors.New("this RCC build has no pinned native Rattler helper")
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	pin := NativeToolPin{Version: layer.Version, Target: common.Platform(), SHA256: rattlerHelperSHA256, Protocol: "1"}
	return ResolveNativeToolPin(common.Product.Home(), "rattler", pin, filepath.Join(filepath.Dir(exe), nativeToolName("rattler")))
}

type rattlerNetwork struct {
	HTTPS         bool   `json:"allow_https"`
	CABundle      string `json:"ca_bundle_pem,omitempty"`
	ProxyURL      string `json:"proxy_url,omitempty"`
	Authorization string `json:"authorization,omitempty"`
}

func rattlerChannels(names []string) ([]string, *rattlerNetwork, error) {
	if len(names) == 1 && names[0] == "local" {
		path, ok := LocalChannel()
		if !ok {
			return nil, nil, errors.New("RCC local channel is unavailable")
		}
		return []string{path}, nil, nil
	}
	if len(names) == 0 || !settings.Global.VerifySsl() || settings.Global.NoRevocation() || settings.Global.LegacyRenegotiation() || settings.Global.HasMicroMambaRc() {
		return nil, nil, errors.New("requested Rattler transport policy is unsupported")
	}
	policy := &rattlerNetwork{HTTPS: true}
	if settings.Global.HasCaBundle() {
		data, err := os.ReadFile(common.CaBundleFile())
		if err != nil {
			return nil, nil, err
		}
		policy.CABundle = string(data)
	}
	alias := settings.Global.CondaURL()
	if alias == "" {
		alias = settings.Global.CondaLink(".")
	}
	endpoint, err := url.Parse(alias)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return nil, nil, errors.New("rattler requires an HTTPS channel alias")
	}
	if user := endpoint.User; user != nil {
		password, hasPassword := user.Password()
		if !hasPassword || user.Username() == "" || strings.Contains(user.Username(), ":") {
			return nil, nil, errors.New("rattler channel authentication requires an explicit HTTP Basic username and password")
		}
		policy.Authorization = "Basic " + base64.StdEncoding.EncodeToString([]byte(user.Username()+":"+password))
		endpoint.User = nil
	}
	if proxy := settings.ProfileProxyFor(endpoint); proxy != "" {
		u, err := url.Parse(proxy)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
			return nil, nil, errors.New("unsupported Rattler proxy URL")
		}
		policy.ProxyURL = proxy
	}
	channels := make([]string, 0, len(names))
	for _, name := range names {
		if name == "local" || !providerChannel.MatchString(name) {
			return nil, nil, errors.New("mixing local and remote Rattler channels is unsupported")
		}
		channels = append(channels, strings.TrimSuffix(endpoint.String(), "/")+"/"+name+"/")
	}
	return channels, policy, nil
}

func rattlerSubdir() string {
	return map[string]string{"linux_amd64": "linux-64", "linux_arm64": "linux-aarch64", "darwin_amd64": "osx-64", "darwin_arm64": "osx-arm64", "windows_amd64": "win-64"}[common.Platform()]
}
