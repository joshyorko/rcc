package conda

import (
	"bytes"
	"context"

	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/joshyorko/rcc/common"
)

// Set by the native candidate build after building the locked helper.
var rattlerHelperSHA256 string

func installRattlerLayer(prefix string, layer ProviderLayer) error {
	_, decodeErr := hex.DecodeString(rattlerHelperSHA256)
	if len(rattlerHelperSHA256) != 64 || decodeErr != nil {
		return errors.New("this RCC build has no pinned native Rattler helper")
	}
	if len(layer.Channels) != 1 || layer.Channels[0] != "local" {
		return errors.New("this helper supports only the RCC local channel")
	}
	channel, ok := LocalChannel()
	if !ok {
		return errors.New("RCC local channel is unavailable")
	}
	platform := rattlerSubdir()
	if platform == "" {
		return errors.New("unsupported Rattler platform")
	}
	rcc, err := os.Executable()
	if err != nil {
		return err
	}
	name := "rcc-rattler-helper"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	source, err := os.Open(filepath.Join(filepath.Dir(rcc), name))
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()
	private, err := os.MkdirTemp("", "rcc-rattler-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(private) }()
	info, err := source.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("invalid Rattler sidecar")
	}
	verified := filepath.Join(common.Product.Home(), "rattler", layer.Version, rattlerHelperSHA256, name)
	if err := publishExecutable(verified, rattlerHelperSHA256, info.Size(), source); err != nil {
		return err
	}
	request, err := json.Marshal(map[string]interface{}{"protocol": 1, "prefix": prefix, "cache": filepath.Join(common.Product.Home(), "rattler-cache"), "platform": platform, "channels": []string{channel}, "packages": layer.Packages})
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
		return fmt.Errorf("local Rattler install: %w", err)
	}
	return nil
}

func rattlerSubdir() string {
	return map[string]string{"linux_amd64": "linux-64", "linux_arm64": "linux-aarch64", "darwin_amd64": "osx-64", "darwin_arm64": "osx-arm64", "windows_amd64": "win-64"}[common.Platform()]
}
