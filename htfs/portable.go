package htfs

import (
	"fmt"
	"os"
	"path/filepath"
)

type PortableCatalog struct {
	root             *Root
	producerIdentity string
}

func LoadPortableCatalog(filename string) (*PortableCatalog, error) {
	root, err := NewRoot(".")
	if err != nil {
		return nil, fmt.Errorf("create portable catalog view: %w", err)
	}
	if err := root.LoadFrom(filename); err != nil {
		return nil, fmt.Errorf("load portable catalog: %w", err)
	}
	if root.Info == nil || root.Identity == "" || filepath.Base(root.Path) != root.Identity {
		return nil, fmt.Errorf("portable catalog has inconsistent producer path and identity")
	}
	return &PortableCatalog{root: root, producerIdentity: root.Identity}, nil
}

func (it *PortableCatalog) Root() *Root {
	return it.root
}

func (it *PortableCatalog) Rebase(base, retainedIdentity string) error {
	if retainedIdentity != it.producerIdentity {
		return fmt.Errorf("producer identity mismatch: catalog %q, requested %q", it.producerIdentity, retainedIdentity)
	}
	absolute, err := filepath.Abs(base)
	if err != nil {
		return fmt.Errorf("resolve portable catalog base: %w", err)
	}
	rebased := filepath.Join(absolute, retainedIdentity)
	if len(rebased) == 0 || filepath.Base(rebased) != retainedIdentity {
		return fmt.Errorf("invalid portable catalog base %q", base)
	}
	it.root.Path = rebased
	it.root.Identity = retainedIdentity
	return nil
}

func (it *PortableCatalog) Restore(library Library, target string) error {
	if library == nil {
		return fmt.Errorf("portable catalog library is nil")
	}
	if filepath.Dir(target) != it.root.HolotreeBase() {
		return fmt.Errorf("portable target base mismatch: %q vs %q", filepath.Dir(target), it.root.HolotreeBase())
	}
	if len(filepath.Base(target)) != len(it.producerIdentity) {
		return fmt.Errorf("portable target identity width mismatch")
	}
	if err := it.root.Relocate(target); err != nil {
		return err
	}
	if err := it.root.Treetop(MakeBranches); err != nil {
		return fmt.Errorf("make portable catalog branches: %w", err)
	}
	score := &stats{}
	if err := it.root.AllDirs(RestoreDirectory(library, it.root, map[string]string{}, score)); err != nil {
		return fmt.Errorf("restore portable catalog: %w", err)
	}
	if err := it.root.SaveAs(target + ".meta"); err != nil {
		return fmt.Errorf("save portable materialization metadata: %w", err)
	}
	if err := os.Chmod(target+".meta", 0o640); err != nil {
		return fmt.Errorf("protect portable materialization metadata: %w", err)
	}
	return nil
}
