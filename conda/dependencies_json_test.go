package conda

import "testing"

// TestParseDependenciesJSON preserves legacy arrays and validates wrapped micromamba listings.
func TestParseDependenciesJSON(t *testing.T) {
	for _, tc := range []struct {
		name, origin, output string
		wantError            bool
	}{
		{"legacy mamba", "mamba", `[{"name":"python","version":"3.12","channel":"conda-forge"}]`, false},
		{"mamba 2.10", "mamba", `{"log_history":[],"packages":[{"name":"python","version":"3.12","channel":"conda-forge"}]}`, false},
		{"mamba logs", "mamba", `{"log_history":[{"message":"diagnostic"}],"packages":[{"name":"python","version":"3.12"}]}`, false},
		{"pip", "pypi", `[{"name":"python","version":"3.12"}]`, false},
		{"missing packages", "mamba", `{"log_history":[]}`, true},
		{"null packages", "mamba", `{"packages":null}`, true},
		{"invalid packages", "mamba", `{"packages":{}}`, true},
		{"malformed", "mamba", `{"packages":[`, true},
		{"pip object", "pypi", `{"packages":[]}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entries, err := parseDependencies(tc.origin, []byte(tc.output))
			if tc.wantError {
				if err == nil {
					t.Fatal("invalid listing was accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 || entries[0].Name != "python" || entries[0].Version != "3.12" {
				t.Fatalf("package inventory changed: %#v", entries)
			}
			if entries[0].Origin == "" || (tc.origin == "pypi" && entries[0].Origin != "pypi") {
				t.Fatalf("package origin was lost: %#v", entries[0])
			}
		})
	}
	for _, output := range []string{`[]`, `{"log_history":[],"packages":[]}`} {
		entries, err := parseDependencies("mamba", []byte(output))
		if err != nil || len(entries) != 0 {
			t.Fatalf("empty package listing failed: %v, %v", entries, err)
		}
	}
}
