package artifactprovider

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"github.com/joshyorko/rcc/environmentartifact"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func (it *Filesystem) ListObjects(ctx context.Context) ([]ObjectInfo, error) {
	return listProviderFiles(ctx, filepath.Join(it.root, "objects", "sha256"))
}
func (it *Filesystem) ListManifests(ctx context.Context) ([]ManifestInfo, error) {
	objects, e := listProviderFiles(ctx, filepath.Join(it.root, "manifests", "sha256"))
	out := make([]ManifestInfo, len(objects))
	for i, v := range objects {
		out[i] = ManifestInfo{Digest: v.Digest, Size: v.Size, ModifiedAt: v.ModifiedAt}
	}
	return out, e
}
func listProviderFiles(ctx context.Context, root string) ([]ObjectInfo, error) {
	out := []ObjectInfo{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, e error) error {
		if e != nil {
			return e
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		if info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		if len(info.Name()) != 64 {
			return fmt.Errorf("invalid provider entry %q", info.Name())
		}
		d, e := environmentartifact.ParseDigest("sha256:" + info.Name())
		if e != nil {
			return e
		}
		out = append(out, ObjectInfo{Digest: d, Size: info.Size(), ModifiedAt: info.ModTime()})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Digest.Hex() < out[j].Digest.Hex() })
	return out, err
}
func (it *Filesystem) GarbageCollect(ctx context.Context, r Retention) (GCReport, error) {
	it.commitMu.Lock()
	defer it.commitMu.Unlock()
	ms, e := it.ListManifests(ctx)
	if e != nil {
		return GCReport{}, e
	}
	report := GCReport{ManifestsScanned: len(ms)}
	keep := map[string]bool{}
	now := time.Now()
	kept := 0
	for _, m := range ms {
		expired := r.MaxAge > 0 && now.Sub(m.ModifiedAt) > r.MaxAge
		if expired && (r.KeepManifests <= 0 || kept >= r.KeepManifests) {
			if e = os.Remove(it.manifestPath(m.Digest)); e == nil {
				report.ManifestsRemoved++
			}
			continue
		}
		kept++
		content, e := it.ResolveManifest(ctx, m.Digest)
		if e != nil {
			return report, e
		}
		manifest, e := environmentartifact.DecodeManifest(content)
		if e != nil {
			return report, e
		}
		keep[manifest.Specification.Descriptor.Digest.Hex()] = true
		keep[manifest.LegacyBlueprint.Descriptor.Digest.Hex()] = true
		keep[manifest.ObjectIndex.Digest.Hex()] = true
		for _, c := range manifest.Catalogs {
			keep[c.Digest.Hex()] = true
		}
		idx, e := it.GetObject(ctx, manifest.ObjectIndex)
		if e != nil {
			return report, e
		}
		idxBytes, e := io.ReadAll(io.LimitReader(idx, maxManifestBytes+1))
		idx.Close()
		if e != nil {
			return report, e
		}
		if int64(len(idxBytes)) > maxManifestBytes {
			return report, fmt.Errorf("object index exceeds bounded size")
		}
		parsed, e := environmentartifact.DecodeObjectIndex(idxBytes)
		if e != nil {
			return report, e
		}
		for _, x := range parsed.Entries {
			keep[x.StoredDigest.Hex()] = true
		}
	}
	objects, e := it.ListObjects(ctx)
	if e != nil {
		return report, e
	}
	report.ObjectsScanned = len(objects)
	for _, o := range objects {
		if keep[o.Digest.Hex()] {
			continue
		}
		if r.MaxAge > 0 && now.Sub(o.ModifiedAt) <= r.MaxAge {
			continue
		}
		if e = os.Remove(it.objectPath(o.Digest)); e == nil {
			report.ObjectsRemoved++
			report.BytesReclaimed += o.Size
		}
	}
	return report, nil
}
func (it *Filesystem) Repair(ctx context.Context) (Health, error) {
	if _, e := it.ListObjects(ctx); e != nil {
		return Health{Ready: false, Corrupt: true, Storage: "corrupt"}, e
	}
	if _, e := it.ListManifests(ctx); e != nil {
		return Health{Ready: false, Corrupt: true, Storage: "corrupt"}, e
	}
	return it.Health(ctx)
}
func (it *Filesystem) Backup(ctx context.Context, w io.Writer) error {
	if w == nil {
		return fmt.Errorf("nil backup writer")
	}
	tw := tar.NewWriter(w)
	defer tw.Close()
	return filepath.Walk(it.root, func(path string, info os.FileInfo, e error) error {
		if e != nil {
			return e
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		if info.IsDir() || strings.HasPrefix(info.Name(), ".upload-") || strings.HasPrefix(info.Name(), ".manifest-") {
			return nil
		}
		if info.Size() < 0 || info.Size() > maxProviderObjectBytes {
			return fmt.Errorf("backup member too large")
		}
		rel, e := filepath.Rel(it.root, path)
		if e != nil {
			return e
		}
		f, e := os.Open(path)
		if e != nil {
			return e
		}
		defer f.Close()
		if e = tw.WriteHeader(&tar.Header{Name: rel, Mode: 0600, Size: info.Size(), ModTime: info.ModTime()}); e != nil {
			return e
		}
		_, e = io.Copy(tw, io.LimitReader(f, maxProviderObjectBytes+1))
		return e
	})
}

func (it *Filesystem) Restore(ctx context.Context, r io.Reader) error {
	if r == nil {
		return fmt.Errorf("nil restore reader")
	}
	stage, e := os.MkdirTemp(it.root, ".restore-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(stage)
	return it.restoreArchive(ctx, r, stage, map[string]bool{})
}
func (it *Filesystem) restoreArchive(ctx context.Context, r io.Reader, stage string, seen map[string]bool) error {
	tr := tar.NewReader(r)
	for {
		if e := ctx.Err(); e != nil {
			return e
		}
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return e
		}
		rel := filepath.Clean(h.Name)
		if h.Typeflag != tar.TypeReg || rel == "." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || strings.Contains(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("unsafe backup path %q", h.Name)
		}
		if seen[rel] {
			return fmt.Errorf("duplicate backup member %q", rel)
		}
		seen[rel] = true
		if h.Size < 0 || h.Size > maxProviderObjectBytes {
			return fmt.Errorf("backup member too large")
		}
		target := filepath.Join(stage, rel)
		if e := os.MkdirAll(filepath.Dir(target), 0750); e != nil {
			return e
		}
		f, e := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return e
		}
		n, e := io.Copy(f, io.LimitReader(tr, h.Size+1))
		ce := f.Close()
		if e != nil {
			return e
		}
		if ce != nil {
			return ce
		}
		if n != h.Size {
			return fmt.Errorf("backup member size mismatch")
		}
		if e := validateRestoreMember(rel, target); e != nil {
			return e
		}
	}
	for rel := range seen {
		src := filepath.Join(stage, rel)
		dst := filepath.Join(it.root, rel)
		if e := os.MkdirAll(filepath.Dir(dst), 0750); e != nil {
			return e
		}
		if old, e := os.Open(dst); e == nil {
			newf, ne := os.Open(src)
			if ne != nil {
				old.Close()
				return ne
			}
			same, ce := sameContent(old, newf)
			old.Close()
			newf.Close()
			if ce != nil {
				return ce
			}
			if !same {
				return fmt.Errorf("backup conflicts with immutable content %q", rel)
			}
			continue
		} else if !os.IsNotExist(e) {
			return e
		}
	}
	created := []string{}
	for rel := range seen {
		src := filepath.Join(stage, rel)
		dst := filepath.Join(it.root, rel)
		if _, e := os.Stat(dst); e == nil {
			continue
		}
		if e := os.Rename(src, dst); e != nil {
			for _, p := range created {
				_ = os.Remove(p)
			}
			return e
		}
		created = append(created, dst)
	}
	return nil
}
func validateRestoreMember(rel, path string) error {
	if strings.HasPrefix(rel, "objects/sha256/") {
		name := filepath.Base(rel)
		d, e := environmentartifact.ParseDigest("sha256:" + name)
		if e != nil {
			return e
		}
		b, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		if environmentartifact.DigestBytes(b) != d {
			return fmt.Errorf("backup object digest mismatch")
		}
	}
	return nil
}
func sameContent(a, b *os.File) (bool, error) {
	as, e := a.Stat()
	if e != nil {
		return false, e
	}
	bs, e := b.Stat()
	if e != nil {
		return false, e
	}
	if as.Size() != bs.Size() {
		return false, nil
	}
	aa := make([]byte, 64*1024)
	bb := make([]byte, len(aa))
	for {
		an, ae := a.Read(aa)
		bn, be := b.Read(bb)
		if an != bn || !bytes.Equal(aa[:an], bb[:bn]) {
			return false, nil
		}
		if ae == io.EOF || be == io.EOF {
			return ae == io.EOF && be == io.EOF, nil
		}
		if ae != nil {
			return false, ae
		}
		if be != nil {
			return false, be
		}
	}
}
