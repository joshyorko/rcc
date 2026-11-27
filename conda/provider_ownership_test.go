package conda

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProviderOwnership(t *testing.T) {
	for _, tc := range []struct {
		name      string
		mutate    func(string) error
		wantError bool
	}{
		{"addition", func(p string) error { return os.WriteFile(filepath.Join(p, "added"), []byte("new"), 0600) }, false},
		{"modified", func(p string) error { return os.WriteFile(filepath.Join(p, "existing"), []byte("changed"), 0600) }, true},
		{"replacement", func(p string) error {
			if err := os.Remove(filepath.Join(p, "existing")); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(p, "existing"), []byte("old"), 0600)
		}, true},
		{"deleted", func(p string) error { return os.Remove(filepath.Join(p, "existing")) }, true},
		{"escaping link", func(p string) error { return os.Symlink(os.TempDir(), filepath.Join(p, "link")) }, true},
		{"internal link", func(p string) error { return os.Symlink("existing", filepath.Join(p, "link")) }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := t.TempDir()
			if err := os.WriteFile(filepath.Join(p, "existing"), []byte("old"), 0600); err != nil {
				t.Fatal(err)
			}
			before, err := snapshotProviderFiles(p)
			if err != nil {
				t.Fatal(err)
			}
			if err = tc.mutate(p); err != nil {
				t.Fatal(err)
			}
			err = verifyProviderOwnership(p, before)
			if (err != nil) != tc.wantError {
				t.Fatalf("verification: %v", err)
			}
		})
	}
}

func TestProviderPackageCollision(t *testing.T) {
	p := t.TempDir()
	if err := os.Mkdir(filepath.Join(p, "conda-meta"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p, "conda-meta", "pkg.json"), []byte(`{"name":"my_package"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := checkProviderCollisions(p, []string{"my-package>=1"}); err == nil {
		t.Fatal("expected collision")
	}
	if err := checkProviderCollisions(p, []string{"other"}); err != nil {
		t.Fatal(err)
	}
}
