package htfs

import (
	"bytes"
	"compress/gzip"
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
	return newPortableCatalog(root)
}

func LoadPortableCatalogBytes(content []byte) (*PortableCatalog, error) {
	root, err := NewRoot(".")
	if err != nil {
		return nil, fmt.Errorf("create portable catalog view: %w", err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(content))
	if err != nil {
		return nil, fmt.Errorf("open portable catalog bytes: %w", err)
	}
	if err := root.ReadFrom(reader); err != nil {
		_ = reader.Close()
		return nil, fmt.Errorf("decode portable catalog bytes: %w", err)
	}
	if err := reader.Close(); err != nil {
		return nil, fmt.Errorf("close portable catalog bytes: %w", err)
	}
	return newPortableCatalog(root)
}

func newPortableCatalog(root *Root) (*PortableCatalog, error) {
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

func (it *PortableCatalog) Snapshot() (catalog, info []byte, err error) {
	content, err := it.root.AsJson()
	if err != nil {
		return nil, nil, fmt.Errorf("encode portable catalog: %w", err)
	}
	var buffer bytes.Buffer
	writer, err := gzip.NewWriterLevel(&buffer, gzip.BestSpeed)
	if err != nil {
		return nil, nil, fmt.Errorf("create portable catalog compressor: %w", err)
	}
	if _, err := writer.Write(content); err != nil {
		_ = writer.Close()
		return nil, nil, fmt.Errorf("compress portable catalog: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, nil, fmt.Errorf("close portable catalog compressor: %w", err)
	}
	info, err = it.root.Info.AsJson()
	if err != nil {
		return nil, nil, fmt.Errorf("encode portable catalog info: %w", err)
	}
	return buffer.Bytes(), info, nil
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
