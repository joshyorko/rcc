package environmentartifact

import (
	"fmt"
	"strconv"
	"strings"
)

const CompatibilitySchemaV1 = 1

type PythonRequirements struct {
	Implementation string `json:"implementation"`
	Version        string `json:"version"`
	ABI            string `json:"abi"`
}

type OSRequirements struct {
	Family             string   `json:"family"`
	MinimumVersion     string   `json:"minimumVersion"`
	KernelMinimum      string   `json:"kernelMinimum"`
	LibC               string   `json:"libc"`
	LibCMinimum        string   `json:"libcMinimum"`
	NativeArchitecture string   `json:"nativeArchitecture"`
	TranslationPolicy  string   `json:"translationPolicy"`
	Runtime            string   `json:"runtime"`
	RequiredLibraries  []string `json:"requiredLibraries"`
}

type CPURequirements struct {
	Architecture     string   `json:"architecture"`
	RequiredFeatures []string `json:"requiredFeatures"`
}

type FilesystemRequirements struct {
	CaseSensitive  bool `json:"caseSensitive"`
	Symlinks       bool `json:"symlinks"`
	Junctions      bool `json:"junctions"`
	LongPaths      bool `json:"longPaths"`
	MinimumMaxPath int  `json:"minimumMaxPath"`
}

type CompatibilityRequirements struct {
	SchemaVersion                int                    `json:"schemaVersion"`
	RelocationVersion            string                 `json:"relocationVersion"`
	Python                       PythonRequirements     `json:"python"`
	OS                           OSRequirements         `json:"os"`
	CPU                          CPURequirements        `json:"cpu"`
	Filesystem                   FilesystemRequirements `json:"filesystem"`
	SystemRequirementsOverridden bool                   `json:"systemRequirementsOverridden"`
}

type PythonCapabilities struct {
	Implementations  []string `json:"implementations"`
	Versions         []string `json:"versions"`
	ABIs             []string `json:"abis"`
	ArtifactProvided bool     `json:"artifactProvided"`
}

type OSCapabilities struct {
	Family             string   `json:"family"`
	Version            string   `json:"version"`
	KernelVersion      string   `json:"kernelVersion"`
	LibC               string   `json:"libc"`
	LibCVersion        string   `json:"libcVersion"`
	NativeArchitecture string   `json:"nativeArchitecture"`
	Translation        string   `json:"translation"`
	Runtime            string   `json:"runtime"`
	Libraries          []string `json:"libraries"`
}

type CPUCapabilities struct {
	Architecture string   `json:"architecture"`
	Features     []string `json:"features"`
}

type FilesystemCapabilities struct {
	CaseSensitive bool `json:"caseSensitive"`
	Symlinks      bool `json:"symlinks"`
	Junctions     bool `json:"junctions"`
	LongPaths     bool `json:"longPaths"`
	MaxPath       int  `json:"maxPath"`
}

type WorkerCapabilities struct {
	SchemaVersion                   int                    `json:"schemaVersion"`
	RelocationVersions              []string               `json:"relocationVersions"`
	Python                          PythonCapabilities     `json:"python"`
	OS                              OSCapabilities         `json:"os"`
	CPU                             CPUCapabilities        `json:"cpu"`
	Filesystem                      FilesystemCapabilities `json:"filesystem"`
	AllowSystemRequirementsOverride bool                   `json:"allowSystemRequirementsOverride"`
}

type CompatibilityError struct {
	Code     string `json:"code"`
	Field    string `json:"field"`
	Required string `json:"required"`
	Observed string `json:"observed"`
}

func (it *CompatibilityError) Error() string {
	return fmt.Sprintf("incompatible environment artifact [%s]: %s requires %s, worker has %s", it.Code, it.Field, it.Required, it.Observed)
}

func (it CompatibilityRequirements) Validate() error {
	if it.SchemaVersion != CompatibilitySchemaV1 {
		return fmt.Errorf("unsupported compatibility schema version %d", it.SchemaVersion)
	}
	if it.RelocationVersion == "" || it.Python.Implementation == "" || it.Python.Version == "" || it.Python.ABI == "" {
		return fmt.Errorf("compatibility requires relocation and Python identity")
	}
	if it.OS.Family == "" || it.OS.MinimumVersion == "" || it.OS.KernelMinimum == "" || it.OS.NativeArchitecture == "" {
		return fmt.Errorf("compatibility requires operating-system identity")
	}
	if it.OS.TranslationPolicy != "native-only" && it.OS.TranslationPolicy != "translation-allowed" {
		return fmt.Errorf("unsupported translation policy %q", it.OS.TranslationPolicy)
	}
	if it.OS.Family == "linux" && (it.OS.LibC == "" || it.OS.LibCMinimum == "") {
		return fmt.Errorf("linux compatibility requires libc identity")
	}
	if it.CPU.Architecture == "" || it.CPU.Architecture != it.OS.NativeArchitecture {
		return fmt.Errorf("CPU and native operating-system architecture must match")
	}
	if it.Filesystem.MinimumMaxPath <= 0 {
		return fmt.Errorf("filesystem maximum-path requirement must be positive")
	}
	for field, values := range map[string][]string{
		"os.requiredLibraries": it.OS.RequiredLibraries,
		"cpu.requiredFeatures": it.CPU.RequiredFeatures,
	} {
		if err := validateCanonicalStrings(field, values); err != nil {
			return err
		}
	}
	return nil
}

func (it WorkerCapabilities) Validate() error {
	if it.SchemaVersion != CompatibilitySchemaV1 {
		return fmt.Errorf("unsupported worker compatibility schema version %d", it.SchemaVersion)
	}
	if it.OS.Family == "" || it.OS.Version == "" || it.OS.KernelVersion == "" || it.OS.NativeArchitecture == "" || it.OS.Translation == "" {
		return fmt.Errorf("worker operating-system capabilities are incomplete")
	}
	if it.CPU.Architecture == "" || it.Filesystem.MaxPath <= 0 {
		return fmt.Errorf("worker CPU or filesystem capabilities are incomplete")
	}
	for field, values := range map[string][]string{
		"relocationVersions":     it.RelocationVersions,
		"python.implementations": it.Python.Implementations,
		"python.versions":        it.Python.Versions,
		"python.abis":            it.Python.ABIs,
		"os.libraries":           it.OS.Libraries,
		"cpu.features":           it.CPU.Features,
	} {
		if err := validateCanonicalStrings(field, values); err != nil {
			return err
		}
	}
	return nil
}

func EvaluateCompatibility(required CompatibilityRequirements, observed WorkerCapabilities) error {
	if err := required.Validate(); err != nil {
		return &CompatibilityError{Code: "invalid-requirements", Field: "compatibility", Required: "valid schema v1", Observed: err.Error()}
	}
	if err := observed.Validate(); err != nil {
		return &CompatibilityError{Code: "invalid-worker", Field: "worker", Required: "complete capabilities", Observed: err.Error()}
	}
	checks := []struct {
		ok                     bool
		code, field, want, got string
	}{
		{containsString(observed.RelocationVersions, required.RelocationVersion), "relocation-version", "relocationVersion", required.RelocationVersion, strings.Join(observed.RelocationVersions, ",")},
		{containsString(observed.Python.Implementations, required.Python.Implementation), "python-implementation", "python.implementation", required.Python.Implementation, strings.Join(observed.Python.Implementations, ",")},
		{observed.Python.ArtifactProvided || containsString(observed.Python.Versions, required.Python.Version), "python-version", "python.version", required.Python.Version, strings.Join(observed.Python.Versions, ",")},
		{observed.Python.ArtifactProvided || containsString(observed.Python.ABIs, required.Python.ABI), "python-abi", "python.abi", required.Python.ABI, strings.Join(observed.Python.ABIs, ",")},
		{observed.OS.Family == required.OS.Family, "os-family", "os.family", required.OS.Family, observed.OS.Family},
		{versionAtLeast(observed.OS.Version, required.OS.MinimumVersion), "os-version", "os.minimumVersion", required.OS.MinimumVersion, observed.OS.Version},
		{versionAtLeast(observed.OS.KernelVersion, required.OS.KernelMinimum), "kernel-version", "os.kernelMinimum", required.OS.KernelMinimum, observed.OS.KernelVersion},
		{observed.OS.NativeArchitecture == required.OS.NativeArchitecture, "native-architecture", "os.nativeArchitecture", required.OS.NativeArchitecture, observed.OS.NativeArchitecture},
		{required.OS.TranslationPolicy == "translation-allowed" || observed.OS.Translation == "native", "translation-policy", "os.translationPolicy", required.OS.TranslationPolicy, observed.OS.Translation},
		{required.OS.LibC == "" || observed.OS.LibC == required.OS.LibC, "libc-family", "os.libc", required.OS.LibC, observed.OS.LibC},
		{required.OS.LibCMinimum == "" || versionAtLeast(observed.OS.LibCVersion, required.OS.LibCMinimum), "libc-version", "os.libcMinimum", required.OS.LibCMinimum, observed.OS.LibCVersion},
		{required.OS.Runtime == "" || observed.OS.Runtime == required.OS.Runtime, "os-runtime", "os.runtime", required.OS.Runtime, observed.OS.Runtime},
		{observed.CPU.Architecture == required.CPU.Architecture, "cpu-architecture", "cpu.architecture", required.CPU.Architecture, observed.CPU.Architecture},
		{!required.Filesystem.CaseSensitive || observed.Filesystem.CaseSensitive, "filesystem-case", "filesystem.caseSensitive", "true", strconv.FormatBool(observed.Filesystem.CaseSensitive)},
		{!required.Filesystem.Symlinks || observed.Filesystem.Symlinks, "filesystem-symlink", "filesystem.symlinks", "true", strconv.FormatBool(observed.Filesystem.Symlinks)},
		{!required.Filesystem.Junctions || observed.Filesystem.Junctions, "filesystem-junction", "filesystem.junctions", "true", strconv.FormatBool(observed.Filesystem.Junctions)},
		{!required.Filesystem.LongPaths || observed.Filesystem.LongPaths, "filesystem-long-path", "filesystem.longPaths", "true", strconv.FormatBool(observed.Filesystem.LongPaths)},
		{observed.Filesystem.MaxPath >= required.Filesystem.MinimumMaxPath, "filesystem-max-path", "filesystem.minimumMaxPath", strconv.Itoa(required.Filesystem.MinimumMaxPath), strconv.Itoa(observed.Filesystem.MaxPath)},
		{!required.SystemRequirementsOverridden || observed.AllowSystemRequirementsOverride, "system-requirements-override", "systemRequirementsOverridden", "allowed", strconv.FormatBool(observed.AllowSystemRequirementsOverride)},
	}
	for _, check := range checks {
		if !check.ok {
			return &CompatibilityError{Code: check.code, Field: check.field, Required: check.want, Observed: check.got}
		}
	}
	for _, library := range required.OS.RequiredLibraries {
		if !containsString(observed.OS.Libraries, library) {
			return &CompatibilityError{Code: "system-library", Field: "os.requiredLibraries", Required: library, Observed: strings.Join(observed.OS.Libraries, ",")}
		}
	}
	for _, feature := range required.CPU.RequiredFeatures {
		if !containsString(observed.CPU.Features, feature) {
			return &CompatibilityError{Code: "cpu-feature", Field: "cpu.requiredFeatures", Required: feature, Observed: strings.Join(observed.CPU.Features, ",")}
		}
	}
	return nil
}

func validateCanonicalStrings(field string, values []string) error {
	if values == nil {
		return fmt.Errorf("%s must be a canonical array", field)
	}
	previous := ""
	for index, value := range values {
		if value == "" || (index > 0 && value <= previous) {
			return fmt.Errorf("%s must contain sorted unique non-empty values", field)
		}
		previous = value
	}
	return nil
}

func containsString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func versionAtLeast(observed, minimum string) bool {
	left, leftOK := numericVersion(observed)
	right, rightOK := numericVersion(minimum)
	if !leftOK || !rightOK {
		return observed == minimum
	}
	length := len(left)
	if len(right) > length {
		length = len(right)
	}
	for index := 0; index < length; index++ {
		var have, need int
		if index < len(left) {
			have = left[index]
		}
		if index < len(right) {
			need = right[index]
		}
		if have != need {
			return have > need
		}
	}
	return true
}

func numericVersion(value string) ([]int, bool) {
	value = strings.TrimSpace(value)
	if at := strings.IndexAny(value, "-+ "); at >= 0 {
		value = value[:at]
	}
	parts := strings.Split(value, ".")
	result := make([]int, len(parts))
	for index, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil {
			return nil, false
		}
		result[index] = number
	}
	return result, len(result) > 0
}
