package conda

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v2"
)

const providerPlanMarker = "rcc-provider-plan-v1"

type providerManifest struct {
	Dependencies string            `yaml:"dependencies"`
	RCC          *providerPlanYaml `yaml:"rcc"`
	PostInstall  []string          `yaml:"rccPostInstall,omitempty"`
}

// Both legacy decoders require a container for dependencies. The scalar is
// deliberately part of every canonical serialization, including identity.yaml.
func explicitProviderEnvironment(content []byte, actions, dev bool) (*Environment, bool, error) {
	var root map[string]interface{}
	if err := yaml.Unmarshal(content, &root); err != nil {
		return nil, false, nil
	}
	section, declared := root["rcc"]
	marker, _ := root["dependencies"].(string)
	if !declared && marker != providerPlanMarker {
		return nil, false, nil
	}
	if !declared || marker != providerPlanMarker {
		return nil, true, fmt.Errorf("explicit providers require rcc and dependencies: %s", providerPlanMarker)
	}
	root = nil
	if err := yaml.UnmarshalStrict(content, &root); err != nil {
		return nil, true, err
	}
	// Validate provider-owned keys strictly while allowing Actions metadata.
	for _, key := range []string{"prefix", "channels", "dev-dependencies", "name", "post-install", "rccPostInstall"} {
		if _, ok := root[key]; !ok {
			continue
		}
		if actions && (key == "name" || key == "post-install") || !actions && key == "rccPostInstall" {
			continue
		}
		return nil, true, fmt.Errorf("unsupported %s alongside ordered provider layers", key)
	}
	data, err := yaml.Marshal(section)
	if err != nil {
		return nil, true, err
	}
	var spec providerPlanYaml
	var keys map[string]interface{}
	if err = yaml.Unmarshal(data, &keys); err != nil {
		return nil, true, err
	}
	if _, ok := keys["package-managers"]; ok {
		spec, err = normalizeShorthand(data, actions, dev)
	} else {
		err = yaml.UnmarshalStrict(data, &spec)
	}
	if err != nil {
		return nil, true, err
	}
	if err = spec.validate(); err != nil {
		return nil, true, err
	}
	var post struct {
		Robot   []string `yaml:"rccPostInstall"`
		Actions []string `yaml:"post-install"`
	}
	if err = yaml.Unmarshal(content, &post); err != nil {
		return nil, true, err
	}
	scripts := post.Robot
	if actions {
		scripts = post.Actions
	}
	return &Environment{ProviderPlan: spec.plan(), PostInstall: scripts}, true, nil
}

// Keep the legacy dependency shape inside the guarded section.
func normalizeShorthand(data []byte, actions, dev bool) (providerPlanYaml, error) {
	type managers struct {
		Conda string `yaml:"conda"`
		Pypi  string `yaml:"pypi"`
	}
	var input struct {
		Version      int                  `yaml:"schema-version"`
		Managers     managers             `yaml:"package-managers"`
		Channels     []string             `yaml:"channels"`
		Dependencies interface{}          `yaml:"dependencies"`
		Dev          *packageDependencies `yaml:"dev-dependencies"`
	}
	var out providerPlanYaml
	if err := yaml.UnmarshalStrict(data, &input); err != nil {
		return out, err
	}
	out.SchemaVersion = input.Version
	raw, err := yaml.Marshal(input.Dependencies)
	if err != nil {
		return out, err
	}
	var conda, pypi []string
	if actions {
		var deps packageDependencies
		if err = yaml.UnmarshalStrict(raw, &deps); err != nil {
			return out, err
		}
		conda, pypi = deps.CondaForge, deps.Pypi
		if dev && input.Dev != nil {
			conda = append(conda, input.Dev.CondaForge...)
			pypi = append(pypi, input.Dev.Pypi...)
		}
		if len(input.Channels) == 0 {
			input.Channels = []string{"conda-forge"}
		}
		for i, item := range pypi {
			if d := AsDependency(item); d != nil {
				pypi[i] = fixPipDependency(d).Original
			}
		}
	} else {
		if input.Dev != nil {
			return out, fmt.Errorf("dev-dependencies require package.yaml")
		}
		var deps []interface{}
		if err = yaml.UnmarshalStrict(raw, &deps); err != nil {
			return out, err
		}
		for _, d := range deps {
			if pkg, ok := d.(string); ok {
				conda = append(conda, pkg)
				continue
			}
			var group struct {
				Pip []string `yaml:"pip"`
			}
			raw, err := yaml.Marshal(d)
			if err != nil {
				return out, err
			}
			if err = yaml.UnmarshalStrict(raw, &group); err != nil || len(group.Pip) == 0 {
				return out, fmt.Errorf("unsupported PyPI group")
			}
			pypi = append(pypi, group.Pip...)
		}
	}
	out.Layers = []ProviderLayerYaml{{Provider: input.Managers.Conda, Channels: input.Channels, Packages: conda}}
	if len(pypi) > 0 || input.Managers.Pypi != "" {
		out.Layers = append(out.Layers, ProviderLayerYaml{Provider: input.Managers.Pypi, Packages: pypi})
	}
	return out, nil
}

func (p *providerPlanYaml) plan() *ProviderPlan {
	layers := make([]ProviderLayer, 0, len(p.Layers))
	for _, l := range p.Layers {
		layers = append(layers, ProviderLayer{Provider: l.Provider, Version: l.Version, Channels: append([]string(nil), l.Channels...), Packages: append([]string(nil), l.Packages...)})
	}
	return &ProviderPlan{Layers: layers}
}

func providerPlanFrom(p *ProviderPlan) *providerPlanYaml {
	out := &providerPlanYaml{SchemaVersion: 1}
	for _, l := range p.Layers {
		out.Layers = append(out.Layers, ProviderLayerYaml{Provider: l.Provider, Version: l.Version, Channels: append([]string(nil), l.Channels...), Packages: append([]string(nil), l.Packages...)})
	}
	return out
}

func (p *providerPlanYaml) validate() error {
	if p.SchemaVersion != 1 {
		return fmt.Errorf("unsupported package provider schema version %d", p.SchemaVersion)
	}
	if len(p.Layers) == 0 {
		return fmt.Errorf("provider plan must contain at least one layer")
	}
	for i, l := range p.Layers {
		provider := strings.TrimSpace(l.Provider)
		if provider != "rattler" && provider != "micromamba" && provider != "uv" && provider != "pip" {
			return fmt.Errorf("provider layer %d has unknown provider %q", i, l.Provider)
		}
		owner := provider == "rattler" || provider == "micromamba"
		if owner != (i == 0) || len(l.Packages) == 0 || owner != (len(l.Channels) > 0) {
			return fmt.Errorf("invalid package ownership, channels or order at layer %d", i)
		}
		if provider != l.Provider {
			return fmt.Errorf("invalid provider name")
		}
		pins := map[string]string{"micromamba": "2.10.0", "rattler": "0.1.0", "uv": "0.9.26", "pip": "25.0.1"}
		if l.Version == "" {
			p.Layers[i].Version = pins[provider]
		} else if l.Version != pins[provider] {
			return fmt.Errorf("unsupported %s version %q", provider, l.Version)
		}
		for _, value := range l.Packages {
			if !providerRequirement.MatchString(value) {
				return fmt.Errorf("unsupported package specification at layer %d", i)
			}
		}
		for _, value := range l.Channels {
			if !providerChannel.MatchString(value) {
				return fmt.Errorf("provider channels require a configured channel name")
			}
		}

	}
	return nil
}

var providerRequirement = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*(?:[=<>!~][A-Za-z0-9.*=<>!~,+_-]+)?$`)
var providerChannel = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)
