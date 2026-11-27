package conda

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/joshyorko/rcc/common"
)

// NativeToolPin binds a Conda provider to one exact native executable.
type NativeToolPin struct {
	Version  string `yaml:"version" json:"version"`
	Target   string `yaml:"target" json:"target"`
	SHA256   string `yaml:"sha256" json:"sha256"`
	Protocol string `yaml:"protocol,omitempty" json:"protocol,omitempty"`
}

var nativeVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:[-+][A-Za-z0-9.-]+)?$`)

func (p NativeToolPin) Validate(tool string) error {
	if tool != "micromamba" && tool != "rattler" {
		return fmt.Errorf("unsupported native tool %q", tool)
	}
	b, err := hex.DecodeString(p.SHA256)
	if !nativeVersion.MatchString(p.Version) || err != nil || len(b) != sha256.Size || p.SHA256 != strings.ToLower(p.SHA256) {
		return fmt.Errorf("invalid %s native tool pin", tool)
	}
	if p.Target != common.Platform() {
		return fmt.Errorf("%s native tool target mismatch", tool)
	}
	if tool == "rattler" && p.Protocol != "1" {
		return fmt.Errorf("rattler native tool pin requires protocol")
	}
	if tool == "micromamba" && p.Protocol != "" {
		return fmt.Errorf("micromamba does not support a protocol pin")
	}
	return nil
}

func nativeToolName(tool string) string {
	name := tool
	if tool == "rattler" {
		name = "rcc-rattler-helper"
	}
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

// ResolveNativeToolPin resolves only the digest-addressed preseeded binary or
// the caller's explicit source file. A source file is never selected by PATH.
// Callers must independently authorize rattler digests before invoking this.
func ResolveNativeToolPin(home, tool string, pin NativeToolPin, source string) (string, error) {
	if err := pin.Validate(tool); err != nil {
		return "", err
	}
	name := nativeToolName(tool)
	path := filepath.Join(home, "provider-tools", tool, pin.Version, pin.Target, pin.SHA256, name)
	if source != "" {
		// An operator supplies this source outside the manifest.
		f, err := openNativeToolSource(source)
		if err != nil {
			return "", err
		}
		defer func() { _ = f.Close() }()
		info, err := f.Stat()
		if err != nil {
			return "", err
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("expected an executable file")
		}
		if err = publishExecutable(path, pin.SHA256, info.Size(), f); err != nil {
			return "", err
		}
	}
	info, err := os.Lstat(path)
	if err != nil || !verifiedExecutable(path, pin.SHA256, info.Size()) {
		return "", fmt.Errorf("no approved, verified executable for %s %s", tool, pin.Version)
	}
	// The operator’s preseeded digest path authorizes this executable.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	probe := func(arg, want string) bool {
		cmd := exec.CommandContext(ctx, path, arg)
		cmd.Env = []string{"SystemRoot=" + os.Getenv("SystemRoot")}
		out, err := cmd.Output()
		return err == nil && strings.TrimSpace(string(out)) == want
	}
	if !probe("--version", pin.Version) || (tool == "rattler" && !probe("--protocol-version", "1")) {
		return "", fmt.Errorf("incompatible %s executable or protocol", tool)
	}
	return path, nil
}
