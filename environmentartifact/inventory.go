package environmentartifact

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/htfs"
)

type InventoryInput struct {
	CatalogPath      string
	LegacyBlueprint  []byte
	ExpectedPlatform string
}

type Inventory struct {
	LegacyBlueprint    Descriptor
	LegacyBlueprintKey string
	Catalog            CatalogDescriptor
	Index              ObjectIndex
	IndexBytes         []byte
	Objects            map[Digest]string
}

func InventoryV12(input InventoryInput) (Inventory, error) {
	if _, err := os.Lstat(common.HololibCompressMarker()); err == nil {
		return Inventory{}, fmt.Errorf("artifact v1 does not support a producer with compress.no active")
	} else if !os.IsNotExist(err) {
		return Inventory{}, fmt.Errorf("inspect Hololib compression mode: %w", err)
	}

	legacyKey := common.BlueprintHash(input.LegacyBlueprint)
	wantCatalogName := htfs.CatalogName(legacyKey)
	if filepath.Base(input.CatalogPath) != wantCatalogName {
		return Inventory{}, fmt.Errorf("catalog name %q does not match legacy blueprint key %s", filepath.Base(input.CatalogPath), legacyKey)
	}

	root, err := htfs.NewRoot(".")
	if err != nil {
		return Inventory{}, fmt.Errorf("create catalog view: %w", err)
	}
	if err := root.LoadFrom(input.CatalogPath); err != nil {
		return Inventory{}, fmt.Errorf("load v12 catalog: %w", err)
	}
	if root.Platform != input.ExpectedPlatform || root.Blueprint != legacyKey {
		return Inventory{}, fmt.Errorf("catalog platform or legacy blueprint key mismatch")
	}

	entries := make(map[string]ObjectEntry)
	objects := make(map[Digest]string)
	if err := inventoryDirectory(root.Tree, entries, objects); err != nil {
		return Inventory{}, err
	}
	ordered := make([]ObjectEntry, 0, len(entries))
	for _, entry := range entries {
		ordered = append(ordered, entry)
	}
	index, indexBytes, err := NewObjectIndex(ordered)
	if err != nil {
		return Inventory{}, fmt.Errorf("construct object index: %w", err)
	}
	if err := ValidateV12Catalog(root, index, root.Identity); err != nil {
		return Inventory{}, fmt.Errorf("validate v12 catalog: %w", err)
	}

	catalogDescriptor, err := descriptorFromFile(CatalogV12MediaType, input.CatalogPath)
	if err != nil {
		return Inventory{}, err
	}
	return Inventory{
		LegacyBlueprint:    Descriptor{MediaType: LegacyBlueprintMediaType, Digest: DigestBytes(input.LegacyBlueprint), Size: int64(len(input.LegacyBlueprint))},
		LegacyBlueprintKey: legacyKey,
		Catalog:            CatalogDescriptor{Descriptor: catalogDescriptor, LegacyName: wantCatalogName},
		Index:              index, IndexBytes: indexBytes, Objects: objects,
	}, nil
}

func inventoryDirectory(directory *htfs.Dir, entries map[string]ObjectEntry, objects map[Digest]string) error {
	for _, child := range directory.Dirs {
		if err := inventoryDirectory(child, entries, objects); err != nil {
			return err
		}
	}
	for _, file := range directory.Files {
		if file.IsSymlink() {
			continue
		}
		entry, path, err := inventoryObject(file.Digest, file.Size)
		if err != nil {
			return err
		}
		if prior, found := entries[file.Digest]; found && prior != entry {
			return fmt.Errorf("conflicting duplicate legacy object %s", file.Digest)
		}
		entries[file.Digest] = entry
		objects[entry.StoredDigest] = path
	}
	return nil
}

func inventoryObject(legacyID string, logicalSize int64) (ObjectEntry, string, error) {
	if !legacyObjectIDPattern.MatchString(legacyID) {
		return ObjectEntry{}, "", fmt.Errorf("invalid legacy object ID %q", legacyID)
	}
	path := htfs.ExactDefaultLocation(legacyID)
	stored, err := descriptorFromFile("application/vnd.rcc.hololib.object.v12+gzip", path)
	if err != nil {
		return ObjectEntry{}, "", fmt.Errorf("inventory legacy object %s: %w", legacyID, err)
	}
	source, err := os.Open(path)
	if err != nil {
		return ObjectEntry{}, "", err
	}
	defer source.Close()
	reader, err := gzip.NewReader(source)
	if err != nil {
		return ObjectEntry{}, "", fmt.Errorf("legacy object %s is not gzip: %w", legacyID, err)
	}
	defer reader.Close()
	hasher := sha256.New()
	written, err := io.Copy(hasher, io.LimitReader(reader, logicalSize+1))
	if err != nil {
		return ObjectEntry{}, "", fmt.Errorf("decompress legacy object %s: %w", legacyID, err)
	}
	if written != logicalSize {
		return ObjectEntry{}, "", fmt.Errorf("legacy object %s logical size mismatch: catalog %d, actual %d", legacyID, logicalSize, written)
	}
	if actual := hex.EncodeToString(hasher.Sum(nil)); actual != legacyID {
		return ObjectEntry{}, "", fmt.Errorf("legacy object %s logical digest mismatch: %s", legacyID, actual)
	}
	return ObjectEntry{
		LegacyObjectID: legacyID, StoredDigest: stored.Digest,
		StoredSize: stored.Size, LogicalSize: logicalSize,
		Encoding: "gzip", LegacyLogicalDigestAlgorithm: "sha256",
	}, path, nil
}

func descriptorFromFile(mediaType, path string) (Descriptor, error) {
	source, err := os.Open(path)
	if err != nil {
		return Descriptor{}, fmt.Errorf("open immutable content %q: %w", path, err)
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		return Descriptor{}, fmt.Errorf("stat immutable content %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return Descriptor{}, fmt.Errorf("immutable content %q is not a regular file", path)
	}
	hasher := sha256.New()
	written, err := io.Copy(hasher, source)
	if err != nil {
		return Descriptor{}, fmt.Errorf("hash immutable content %q: %w", path, err)
	}
	return Descriptor{MediaType: mediaType, Digest: Digest{hex: hex.EncodeToString(hasher.Sum(nil))}, Size: written}, nil
}
