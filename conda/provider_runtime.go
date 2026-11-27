package conda

import (
	"fmt"
	"io"
	"os"
	"strings"

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
		os.Remove(file.Name())
		return "", err
	}
	return file.Name(), nil
}

func runProviderLayer(layer ProviderLayer, key, prefix string, output io.Writer, force bool) (bool, string, error) {
	watch := common.Stopwatch("package provider")
	switch layer.Provider {
	case "rattler":
		return false, "", installRattlerLayer(prefix, layer)
	case "micromamba":
		if !MustMicromamba() || MicromambaVersion() != layer.Version {
			return false, "", fmt.Errorf("requires micromamba %s", layer.Version)
		}
		content, err := yaml.Marshal(map[string][]string{"channels": layer.Channels, "dependencies": layer.Packages})
		if err != nil {
			return false, "", err
		}
		file, err := providerInput(content)
		if err != nil {
			return false, "", err
		}
		defer os.Remove(file)
		if ok, _ := micromambaLayer(key, file, prefix, watch, output, force); !ok {
			return false, "", fmt.Errorf("micromamba install failed")
		}
	case "pip", "uv":
		python, err := stagedTargetPython(prefix)
		if err != nil {
			return false, "", err
		}
		installer := pipLayer
		var version string
		if layer.Provider == "uv" {
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
		file, err := providerInput([]byte(strings.Join(layer.Packages, "\n") + "\n"))
		if err != nil {
			return false, "", err
		}
		defer os.Remove(file)
		ok, _, used, python := installer(key, file, prefix, watch, output)
		if !ok {
			return false, "", fmt.Errorf("%s install failed", layer.Provider)
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
		fmt.Fprintln(output, err)
		return false, true, false, ""
	}
	start := 0
	if skip != SkipNoLayers {
		if skip <= SkipError || int(skip-SkipError) > len(keys) {
			fmt.Fprintln(output, "invalid provider restore point")
			return false, true, false, ""
		}
		start = int(skip - SkipError)
	}
	var used bool
	var python string
	for i, layer := range env.ProviderPlan.Layers {
		if i >= start {
			fmt.Fprintf(output, "Installing layer %d with %s %s\n", i+1, layer.Provider, layer.Version)
			pypi, interpreter, err := runProviderLayer(layer, keys[i], prefix, output, force)
			if err != nil {
				fmt.Fprintln(output, err)
				return false, true, false, ""
			}
			if pypi {
				used, python = true, interpreter
			}
			if recorder != nil && (i+1 < len(keys) || len(env.PostInstall) > 0) {
				if err := recorder.Record([]byte("rcc-provider-layer-v1:" + keys[i])); err != nil {
					fmt.Fprintln(output, err)
					return false, true, false, ""
				}
			}
		} else if layer.Provider == "pip" || layer.Provider == "uv" {
			python, err = stagedTargetPython(prefix)
			if err != nil {
				fmt.Fprintln(output, err)
				return false, true, false, ""
			}
			used = true
		}
	}
	ok, fatal := postInstallLayer("provider-plan", env.PostInstall, prefix, common.Stopwatch("post-install"), output)
	return ok, fatal, used, python
}
