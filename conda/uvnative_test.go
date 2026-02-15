package conda

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/joshyorko/rcc/hamlet"
)

func TestRemoveExternallyManaged(t *testing.T) {
	tests := []struct {
		name   string
		marker string
	}{
		{"linux style stdlib", filepath.Join("lib", "python3.12", EXTERNALLY_MANAGED)},
		{"windows versioned stdlib", filepath.Join("Lib", "python3.12", EXTERNALLY_MANAGED)},
		{"windows stdlib root", filepath.Join("Lib", EXTERNALLY_MANAGED)},
		{"lowercase stdlib root", filepath.Join("lib", EXTERNALLY_MANAGED)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			must_be, _ := hamlet.Specifications(t)

			targetFolder := t.TempDir()
			marker := filepath.Join(targetFolder, tt.marker)
			must_be.Nil(os.MkdirAll(filepath.Dir(marker), 0o755))
			must_be.Nil(os.WriteFile(marker, []byte("marker"), 0o644))

			removeExternallyManaged(targetFolder)

			_, err := os.Stat(marker)
			must_be.True(os.IsNotExist(err))
		})
	}
}

func TestRemoveExternallyManagedWithoutMarker(t *testing.T) {
	removeExternallyManaged(t.TempDir())
}

func TestUvPythonTarget(t *testing.T) {
	tests := []struct {
		name         string
		python       string
		targetFolder string
		expected     string
	}{
		{
			name:         "prefer resolved python path",
			python:       filepath.Join("C:", "env", "python.exe"),
			targetFolder: filepath.Join("C:", "env"),
			expected:     filepath.Join("C:", "env", "python.exe"),
		},
		{
			name:         "fallback to target folder when python missing",
			python:       "",
			targetFolder: filepath.Join("C:", "env"),
			expected:     filepath.Join("C:", "env"),
		},
		{
			name:         "fallback on whitespace-only python",
			python:       "   ",
			targetFolder: "/tmp/env",
			expected:     "/tmp/env",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			must_be, _ := hamlet.Specifications(t)
			must_be.Equal(tt.expected, uvPythonTarget(tt.python, tt.targetFolder))
		})
	}
}
