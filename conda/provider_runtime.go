package conda

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/joshyorko/rcc/blobs"
	"github.com/joshyorko/rcc/common"
	"gopkg.in/yaml.v2"
)

func providerInput(content []byte) (string, error) {
	return providerInputNamed("rcc-provider-*", content)
}

func providerInputNamed(pattern string, content []byte) (string, error) {
	file, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", err
	}
	_, err = file.Write(content)
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(file.Name())
		return "", err
	}
	return file.Name(), nil
}

func runProviderLayer(layer ProviderLayer, key, prefix string, output io.Writer, force bool) (bool, string, error) {
	watch := common.Stopwatch("package provider")
	switch layer.Provider {
	case "uv-runtime":
		bin, err := providerExecutable(layer)
		if err != nil {
			return false, "", err
		}
		// The verified UV release supplies the managed Python download catalog.
		// Start with private empty directories so an existing Python cache cannot select the artifact.
		root, err := os.MkdirTemp("", "rcc-uv-runtime-*")
		if err != nil {
			return false, "", err
		}
		defer func() { _ = os.RemoveAll(root) }()
		overrides := []string{"UV_CACHE_DIR=" + filepath.Join(root, "cache"), "UV_PYTHON_BIN_DIR=" + filepath.Join(root, "bin")}
		if ok, _ := uvNativeLayerFrom(key, prefix, strings.TrimPrefix(layer.Packages[0], "python="), bin, filepath.Join(root, "python"), overrides, watch, output); !ok {
			return false, "", fmt.Errorf("uv managed Python installation failed")
		}
	case "rattler":
		return false, "", installRattlerLayer(prefix, layer)
	case "micromamba":
		mamba, err := providerExecutable(layer)
		if err != nil {
			return false, "", err
		}
		channels := append([]string(nil), layer.Channels...)
		for i, name := range channels {
			if name == "local" {
				path, ok := LocalChannel()
				if !ok {
					return false, "", fmt.Errorf("RCC local channel is unavailable")
				}
				channels[i] = path
			}
		}
		content, err := yaml.Marshal(map[string][]string{"channels": channels, "dependencies": layer.Packages})
		if err != nil {
			return false, "", err
		}
		file, err := providerInputNamed("rcc-conda-*.yaml", content)
		if err != nil {
			return false, "", err
		}
		defer func() { _ = os.Remove(file) }()
		if ok, _ := micromambaLayerWithBinary(key, file, prefix, watch, output, force, mamba, layer.Version); !ok {
			return false, "", fmt.Errorf("micromamba install failed")
		}
	case "pip", "uv":
		python, err := stagedTargetPython(prefix)
		if err != nil {
			return false, "", err
		}
		installer := pipLayer
		var version string
		uvVersion, _, _, hasDefault := blobs.ProviderDefaultInfo("uv")
		if layer.Provider == "uv" && (layer.ToolLock != nil || (hasDefault && uvVersion == layer.Version)) {
			bin, err := providerExecutable(layer)
			if err != nil {
				return false, "", err
			}
			version = UvVersion(bin)
			installer = func(key, file, prefix string, watch fmt.Stringer, output io.Writer) (bool, bool, bool, string) {
				return uvNativePipLayer(key, file, prefix, bin, watch, output)
			}
		} else if layer.Provider == "uv" {
			uv, ok := FindUv(prefix)
			if !ok {
				return false, "", fmt.Errorf("staged uv is missing")
			}
			version = UvVersion(uv)
			installer = uvLayer
		} else {
			version = PipVersion(python)
		}
		if version != layer.Version {
			return false, "", fmt.Errorf("requires staged %s %s", layer.Provider, layer.Version)
		}
		if err := checkProviderCollisions(prefix, layer.Packages); err != nil {
			return false, "", err
		}
		before, err := snapshotProviderFiles(prefix)
		if err != nil {
			return false, "", err
		}
		file, err := providerInput([]byte(strings.Join(layer.Packages, "\n") + "\n"))
		if err != nil {
			return false, "", err
		}
		defer func() { _ = os.Remove(file) }()
		ok, _, used, python := installer(key, file, prefix, watch, output)
		if !ok {
			return false, "", fmt.Errorf("%s install failed", layer.Provider)
		}
		if err := verifyProviderOwnership(prefix, before); err != nil {
			return false, "", err
		}
		return used, python, nil
	default:
		return false, "", fmt.Errorf("unknown provider %q", layer.Provider)
	}
	return false, "", nil
}

func providerPlanLayers(env *Environment, prefix string, output io.Writer, skip SkipLayer, recorder Recorder, force bool) (bool, bool, bool, string) {
	keys, err := env.ProviderLayerKeys()
	if err != nil {
		_, _ = fmt.Fprintln(output, err)
		return false, true, false, ""
	}
	start := 0
	if skip != SkipNoLayers {
		if skip <= SkipError || int(skip-SkipError) > len(keys) {
			_, _ = fmt.Fprintln(output, "invalid provider restore point")
			return false, true, false, ""
		}
		start = int(skip - SkipError)
	}
	var used bool
	var python string
	for i, layer := range env.ProviderPlan.Layers {
		if i >= start {
			status := "release default"
			if layer.ToolLock != nil || layer.NativeToolPin != nil {
				status = "operator pin; outside RCC qualification unless independently matched"
			}
			_, _ = fmt.Fprintf(output, "Installing layer %d with %s %s (%s)\n", i+1, layer.Provider, layer.Version, status)
			pypi, interpreter, err := runProviderLayer(layer, keys[i], prefix, output, force)
			if err != nil {
				_, _ = fmt.Fprintln(output, err)
				return false, true, false, ""
			}
			if pypi {
				used, python = true, interpreter
			}
			if recorder != nil && (i+1 < len(keys) || len(env.PostInstall) > 0) {
				if err := recorder.Record([]byte("rcc-provider-layer-v1:" + keys[i])); err != nil {
					_, _ = fmt.Fprintln(output, err)
					return false, true, false, ""
				}
			}
		} else if layer.Provider == "pip" || layer.Provider == "uv" {
			python, err = stagedTargetPython(prefix)
			if err != nil {
				_, _ = fmt.Fprintln(output, err)
				return false, true, false, ""
			}
			used = true
		}
	}
	ok, fatal := postInstallLayer("provider-plan", env.PostInstall, prefix, common.Stopwatch("post-install"), output)
	return ok, fatal, used, python
}
