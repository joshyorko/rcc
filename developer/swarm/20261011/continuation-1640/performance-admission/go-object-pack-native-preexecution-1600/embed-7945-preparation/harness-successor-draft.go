// This harness is a proposal artifact. It has not been compiled or executed.
// Run it only from the exact RCC source projection named by plan.md after root
// admission. All provider writes are confined to -provider-root.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"hash"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/joshyorko/rcc/artifactprovider"
	"github.com/joshyorko/rcc/environmentartifact"
)

type packIndex struct {
	Format    string      `json:"format"`
	PackBytes int64       `json:"pack_bytes"`
	Entries   []packEntry `json:"entries"`
}
type packEntry struct {
	Digest string `json:"digest"`
	Offset int64  `json:"offset"`
	Size   int64  `json:"size"`
}
type selected struct {
	legacy string
	digest environmentartifact.Digest
	size   int64
	offset int64
}
type passResult struct {
	Arm           string `json:"arm"`
	Replicate     int    `json:"replicate"`
	WallNS        int64  `json:"wall_ns"`
	CPUUserNS     int64  `json:"cpu_user_ns"`
	CPUSystemNS   int64  `json:"cpu_system_ns"`
	Objects       int    `json:"objects"`
	Bytes         int64  `json:"bytes"`
	ProviderCalls int    `json:"provider_getobject_calls"`
	PackFileOpens int    `json:"pack_file_opens_during_pass"`
	ReadAtCalls   int    `json:"read_at_calls"`
	ReadIOBytes   int64  `json:"proc_io_rchar_delta"`
	ReadDiskBytes int64  `json:"proc_io_read_bytes_delta"`
	ReadSyscalls  int64  `json:"proc_io_syscr_delta"`
	RSSBytes      int64  `json:"rss_bytes_sampled_after_pass"`
	OrderedSHA256 string `json:"ordered_result_sha256"`
}
type result struct {
	Status                    string       `json:"status"`
	SourceCommit              string       `json:"source_commit"`
	IndexSHA256               string       `json:"index_sha256"`
	IndexAfterSHA256          string       `json:"index_after_sha256"`
	OffsetIndexSHA256         string       `json:"offset_index_sha256"`
	OffsetIndexAfterSHA256    string       `json:"offset_index_after_sha256"`
	PackSHA256                string       `json:"pack_sha256"`
	SelectedCount             int          `json:"selected_count"`
	SelectedBytes             int64        `json:"selected_bytes"`
	PackFileOpensBeforePasses int          `json:"pack_file_opens_before_timing"`
	SourceClosureBeforeSHA256 string       `json:"source_closure_before_sha256"`
	SourceClosureAfterSHA256  string       `json:"source_closure_after_sha256"`
	PackAfterSHA256           string       `json:"pack_after_sha256"`
	Schedule                  []string     `json:"schedule"`
	Controls                  []string     `json:"untimed_controls_passed"`
	Passes                    []passResult `json:"passes"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	sourceRoot := flag.String("source-root", "", "read-only frozen provider-content root")
	indexPath := flag.String("index", "", "frozen environment object index JSON")
	packIndexPath := flag.String("offset-index", "", "frozen experiment offset-index JSON")
	packPath := flag.String("pack", "", "frozen experiment concatenation")
	providerRoot := flag.String("provider-root", "", "new owned RAM-only provider root")
	outPath := flag.String("out", "", "new owned evidence JSON path")
	flag.Parse()
	if *sourceRoot == "" || *indexPath == "" || *packIndexPath == "" || *packPath == "" || *providerRoot == "" || *outPath == "" {
		return errors.New("all six path flags are required")
	}
	for _, p := range []string{*sourceRoot, *indexPath, *packIndexPath, *packPath} {
		if _, err := os.Stat(p); err != nil {
			return fmt.Errorf("read-only input unavailable: %s: %w", p, err)
		}
	}
	if _, err := os.Lstat(*providerRoot); !os.IsNotExist(err) {
		return fmt.Errorf("provider root must be a new owned path: %s", *providerRoot)
	}
	if _, err := os.Lstat(*outPath); !os.IsNotExist(err) {
		return fmt.Errorf("output path must not exist: %s", *outPath)
	}

	idxBytes, err := os.ReadFile(*indexPath)
	if err != nil {
		return err
	}
	if hashBytes(idxBytes) != "e587078b0b8d8bce38d4e665999b9a3a0d4d9fb1de1b87a8c3a4b223e71e50f6" {
		return errors.New("frozen source index SHA256 mismatch")
	}
	idx, err := environmentartifact.DecodeObjectIndex(idxBytes)
	if err != nil {
		return fmt.Errorf("decode frozen object index: %w", err)
	}
	var pi packIndex
	piBytes, err := os.ReadFile(*packIndexPath)
	if err != nil {
		return err
	}
	if hashBytes(piBytes) != "e8dca8fa7d069883441bd8616e49234d338ab5707b3cc05d6b3a5693b2ba99e2" {
		return errors.New("frozen offset index SHA256 mismatch")
	}
	if err = json.Unmarshal(piBytes, &pi); err != nil {
		return fmt.Errorf("decode pack offset index: %w", err)
	}
	if pi.Format != "rcc-experiment-raw-concat-v1" || pi.PackBytes != 4484936 || len(pi.Entries) != 4649 {
		return errors.New("offset index does not match admitted 4,649-object fixture")
	}
	selectedObjects, err := joinSelection(idx.Entries, pi)
	if err != nil {
		return err
	}
	if len(selectedObjects) != 4649 {
		return fmt.Errorf("selected object count %d", len(selectedObjects))
	}
	var selectedBytes int64
	for _, o := range selectedObjects {
		selectedBytes += o.size
	}
	if selectedBytes != 4484936 {
		return fmt.Errorf("selected stored byte count %d", selectedBytes)
	}
	sourceBefore, err := sourceClosureHash(*sourceRoot, selectedObjects)
	if err != nil {
		return fmt.Errorf("source closure preflight: %w", err)
	}

	pack, err := os.Open(*packPath)
	if err != nil {
		return err
	}
	defer pack.Close()
	packInfo, err := pack.Stat()
	if err != nil || packInfo.Size() != pi.PackBytes {
		return errors.New("pack size differs from frozen offset index")
	}
	if err := requireSameTmpfs(*packPath, filepath.Dir(*providerRoot)); err != nil {
		return fmt.Errorf("pack and provider destination must share owned tmpfs: %w", err)
	}
	packHash, err := hashFile(*packPath)
	if err != nil {
		return err
	}
	if packHash != "44d2341ac7cecf8a407e881bd4514e1f4fb6dd59530a4d172049d5c7e40f39a9" {
		return errors.New("frozen concatenation SHA256 mismatch")
	}
	untimedPackSlices, untimedPackBytes, untimedPackIdentity, err := verifyAllPackSlices(pack, selectedObjects, pi.PackBytes)
	if err != nil { return fmt.Errorf("untimed pack slice closure: %w", err) }

	// Build an isolated provider tree from verified source files. NewFilesystem
	// is invoked only on this newly created RAM-owned root, never on the source.
	if err := copyProviderObjects(*sourceRoot, *providerRoot, selectedObjects); err != nil {
		return err
	}
	if err := requireSameTmpfs(*packPath, *providerRoot); err != nil {
		return fmt.Errorf("pack and copied provider are not on one owned tmpfs: %w", err)
	}
	provider, err := artifactprovider.NewFilesystem(*providerRoot)
	if err != nil {
		return fmt.Errorf("initialize owned filesystem provider: %w", err)
	}
	controls, err := negativeControls(provider, pack, *sourceRoot, *providerRoot, selectedObjects, pi.PackBytes)
	if err != nil {
		return fmt.Errorf("untimed control: %w", err)
	}
	sourceControls, err := sourceClosureHash(*sourceRoot, selectedObjects)
	if err != nil {
		return fmt.Errorf("source closure after controls: %w", err)
	}
	if sourceControls != sourceBefore {
		return errors.New("source closure changed during controls")
	}

	schedule := []string{"filesystem", "pack", "pack", "filesystem", "filesystem", "pack", "pack", "filesystem", "filesystem", "pack"}
	results := result{Status: "RUNNING", SourceCommit: "7945f956b428359928908a9f42e36417b113dafd", IndexSHA256: hashBytes(idxBytes), OffsetIndexSHA256: hashBytes(piBytes), PackSHA256: packHash, UntimedPackSliceCount: untimedPackSlices, UntimedPackSliceBytes: untimedPackBytes, UntimedPackOrderedSHA256: untimedPackIdentity, SelectedCount: len(selectedObjects), SelectedBytes: selectedBytes, PackFileOpensBeforePasses: 1, SourceClosureBeforeSHA256: sourceBefore, Schedule: schedule, Controls: controls}
	receipt, err := os.OpenFile(*outPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil { return err }
	defer receipt.Close()
	if err := appendJSONLine(receipt, map[string]any{"event":"run_start", "result":results}); err != nil { return err }
	wantOrdered := orderedIdentity(selectedObjects)
	armReplicates := map[string]int{}
	for i, arm := range schedule {
		armReplicates[arm]++
		fmt.Fprintf(os.Stderr, "PASS_START %d/%d arm=%s replicate=%d\n", i+1, len(schedule), arm, armReplicates[arm])
		p, err := measurePass(arm, armReplicates[arm], provider, pack, selectedObjects, pi.PackBytes)
		if err != nil {
			_ = appendJSONLine(receipt, map[string]any{"event":"pass_failed", "position":i+1, "arm":arm, "error":err.Error()})
			return fmt.Errorf("pass %d (%s): %w", i+1, arm, err)
		}
		if p.Objects != len(selectedObjects) || p.Bytes != selectedBytes || p.OrderedSHA256 != wantOrdered {
			_ = appendJSONLine(receipt, map[string]any{"event":"pass_failed", "position":i+1, "arm":arm, "error":"count/byte/identity oracle failed"})
			return fmt.Errorf("pass %d failed count/byte/identity oracle", i+1)
		}
		results.Passes = append(results.Passes, p)
		if err := appendJSONLine(receipt, map[string]any{"event":"pass_complete", "position":i+1, "pass":p}); err != nil { return err }
		fmt.Fprintf(os.Stderr, "PASS_DONE %d/%d arm=%s replicate=%d wall_ns=%d\n", i+1, len(schedule), arm, armReplicates[arm], p.WallNS)
	}
	sourceAfter, err := sourceClosureHash(*sourceRoot, selectedObjects)
	if err != nil {
		return fmt.Errorf("source closure postflight: %w", err)
	}
	if sourceAfter != sourceBefore {
		return errors.New("source closure changed during run")
	}
	results.SourceClosureAfterSHA256 = sourceAfter
	indexAfter, err := hashFile(*indexPath)
	if err != nil {
		return err
	}
	if indexAfter != results.IndexSHA256 {
		return errors.New("source index changed during run")
	}
	results.IndexAfterSHA256 = indexAfter
	offsetIndexAfter, err := hashFile(*packIndexPath)
	if err != nil {
		return err
	}
	if offsetIndexAfter != results.OffsetIndexSHA256 {
		return errors.New("offset index changed during run")
	}
	results.OffsetIndexAfterSHA256 = offsetIndexAfter
	packAfter, err := hashFile(*packPath)
	if err != nil {
		return err
	}
	if packAfter != packHash {
		return errors.New("pack changed during run")
	}
	results.PackAfterSHA256 = packAfter
	results.Status = "PASS"
	return appendJSONLine(receipt, map[string]any{"event":"run_complete", "result":results})
}

func verifyAllPackSlices(pack *os.File, objects []selected, packBytes int64) (int, int64, string, error) {
	var total int64
	for _, o := range objects {
		if _, err := packRead(pack, o.offset, o.size, o.digest.Hex(), packBytes); err != nil { return 0,0,"",fmt.Errorf("%s: %w",o.digest,err) }
		total += o.size
	}
	if len(objects)!=4649 || total!=4484936 { return 0,0,"",fmt.Errorf("untimed slice closure mismatch: objects=%d bytes=%d",len(objects),total) }
	return len(objects),total,orderedIdentity(objects),nil
}

func requireSameTmpfs(a,b string) error {
	var aFS,bFS syscall.Statfs_t
	if err:=syscall.Statfs(a,&aFS);err!=nil{return err}
	if err:=syscall.Statfs(b,&bFS);err!=nil{return err}
	const tmpfsMagic = 0x01021994
	if uint64(aFS.Type)!=tmpfsMagic || uint64(bFS.Type)!=tmpfsMagic { return fmt.Errorf("paths are not both tmpfs (types %#x, %#x)",aFS.Type,bFS.Type) }
	aInfo,err:=os.Stat(a);if err!=nil{return err}
	bInfo,err:=os.Stat(b);if err!=nil{return err}
	aStat,ok:=aInfo.Sys().(*syscall.Stat_t);if !ok{return errors.New("cannot read pack device identity")}
	bStat,ok:=bInfo.Sys().(*syscall.Stat_t);if !ok{return errors.New("cannot read provider device identity")}
	if aStat.Dev!=bStat.Dev{return errors.New("pack and provider are on different devices")}
	return nil
}

func appendJSONLine(w *os.File, value any) error {
	b,err:=json.Marshal(value);if err!=nil{return err}
	b=append(b,'\n')
	if _,err=w.Write(b);err!=nil{return err}
	return w.Sync()
}

func joinSelection(entries []environmentartifact.ObjectEntry, pi packIndex) ([]selected, error) {
	byDigest := make(map[string]environmentartifact.ObjectEntry, len(entries))
	for _, e := range entries {
		byDigest[e.StoredDigest.Hex()] = e
	}
	expectedSmall := 0
	for _, e := range entries {
		if e.StoredSize < 4096 {
			expectedSmall++
		}
	}
	if expectedSmall != len(pi.Entries) {
		return nil, errors.New("pack selection does not cover the exact small-object subset count")
	}
	got := make([]selected, 0, len(pi.Entries))
	lastEnd := int64(0)
	for _, p := range pi.Entries {
		h := strings.TrimPrefix(p.Digest, "sha256:")
		e, ok := byDigest[h]
		if !ok || e.StoredSize >= 4096 || e.StoredSize <= 0 || p.Size != e.StoredSize || p.Offset < 0 || p.Size < 0 || p.Offset > math.MaxInt64-p.Size || p.Offset != lastEnd || p.Offset+p.Size > pi.PackBytes {
			return nil, fmt.Errorf("invalid or noncanonical pack entry %s", p.Digest)
		}
		d, err := environmentartifact.ParseDigest("sha256:" + h)
		if err != nil {
			return nil, err
		}
		got = append(got, selected{legacy: e.LegacyObjectID, digest: d, size: e.StoredSize, offset: p.Offset})
		lastEnd = p.Offset + p.Size
	}
	if lastEnd != pi.PackBytes {
		return nil, errors.New("offset index does not cover exact pack bytes")
	}
	sort.Slice(got, func(i, j int) bool { return got[i].digest.Hex() < got[j].digest.Hex() })
	for i := 1; i < len(got); i++ {
		if got[i-1].digest == got[i].digest {
			return nil, errors.New("duplicate stored digest")
		}
	}
	// The pack index is sorted by digest; sorting here also makes the canonical
	// result identity independent of the legacy-ID order in the object index.
	sort.Slice(got, func(i, j int) bool { return got[i].offset < got[j].offset })
	for i := 1; i < len(got); i++ {
		if got[i-1].digest.Hex() >= got[i].digest.Hex() {
			return nil, errors.New("pack entries are not digest sorted")
		}
	}
	return got, nil
}

func objectPath(root string, d environmentartifact.Digest) string {
	h := d.Hex()
	return filepath.Join(root, "objects", "sha256", h[:2], h[2:4], h)
}

func copyProviderObjects(source, target string, objects []selected) error {
	for _, o := range objects {
		src := objectPath(source, o.digest)
		info, err := os.Lstat(src)
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("source object is not a regular file: %s", o.digest)
		}
		b, err := os.ReadFile(src)
		if err != nil {
			return fmt.Errorf("read source object %s: %w", o.digest, err)
		}
		if int64(len(b)) != o.size || hashBytes(b) != o.digest.Hex() {
			return fmt.Errorf("source object failed frozen size/SHA: %s", o.digest)
		}
		dst := objectPath(target, o.digest)
		if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
			return err
		}
		f, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		_, writeErr := f.Write(b)
		closeErr := f.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func negativeControls(provider *artifactprovider.Filesystem, pack *os.File, source, root string, objects []selected, packBytes int64) ([]string, error) {
	first := objects[0]
	path := objectPath(root, first.digest)
	original, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	controls := []string{}
	ctx := context.Background()
	corrupt := append([]byte(nil), original...)
	corrupt[0] ^= 1
	if err := os.WriteFile(path, corrupt, 0600); err != nil {
		return nil, err
	}
	if r, err := provider.GetObject(ctx, environmentartifact.Descriptor{Digest: first.digest, Size: first.size}); err == nil {
		_ = r.Close()
		_ = os.WriteFile(path, original, 0600)
		return nil, errors.New("corrupted provider bytes accepted")
	}
	if err := os.WriteFile(path, original, 0600); err != nil {
		return nil, err
	}
	controls = append(controls, "corrupted provider object rejected by RCC digest verifier; owned copy restored")
	if err := os.Remove(path); err != nil {
		return nil, err
	}
	if r, err := provider.GetObject(ctx, environmentartifact.Descriptor{Digest: first.digest, Size: first.size}); err == nil {
		_ = r.Close()
		_ = os.WriteFile(path, original, 0600)
		return nil, errors.New("missing provider object accepted")
	}
	if err := os.WriteFile(path, original, 0600); err != nil {
		return nil, err
	}
	controls = append(controls, "missing provider object rejected; owned copy restored")
	if r, err := provider.GetObject(ctx, environmentartifact.Descriptor{Digest: first.digest, Size: first.size + 1}); err == nil {
		_ = r.Close()
		return nil, errors.New("wrong provider size accepted")
	}
	controls = append(controls, "wrong declared object size rejected")
	if _, err := packRead(pack, packBytes-1, 2, first.digest.Hex(), packBytes); err == nil {
		return nil, errors.New("out-of-range pack slice accepted")
	}
	if _, err := packRead(pack, math.MaxInt64, 2, first.digest.Hex(), packBytes); err == nil {
		return nil, errors.New("overflowing pack range accepted")
	}
	controls = append(controls, "out-of-range and overflowing pack slices rejected")
	wrongDigest := "0" + first.digest.Hex()[1:]
	if wrongDigest == first.digest.Hex() {
		wrongDigest = "1" + first.digest.Hex()[1:]
	}
	if _, err := packRead(pack, first.offset, first.size, wrongDigest, packBytes); err == nil {
		return nil, errors.New("wrong stored digest accepted")
	}
	controls = append(controls, "wrong stored digest rejected")
	second, err := packRead(pack, first.offset, first.size, first.digest.Hex(), packBytes)
	if err != nil {
		return nil, err
	}
	third, err := packRead(pack, first.offset, first.size, first.digest.Hex(), packBytes)
	if err != nil {
		return nil, err
	}
	second[0] ^= 1
	if hashBytes(third) != first.digest.Hex() {
		return nil, errors.New("pack read buffers alias")
	}
	r1, err := provider.GetObject(ctx, environmentartifact.Descriptor{Digest: first.digest, Size: first.size})
	if err != nil {
		return nil, err
	}
	r2, err := provider.GetObject(ctx, environmentartifact.Descriptor{Digest: first.digest, Size: first.size})
	if err != nil {
		_ = r1.Close()
		return nil, err
	}
	_, _ = r1.Read(make([]byte, 1))
	b2, err := io.ReadAll(r2)
	_ = r1.Close()
	_ = r2.Close()
	if err != nil || hashBytes(b2) != first.digest.Hex() {
		return nil, errors.New("provider readers share state or returned corrupt bytes")
	}
	controls = append(controls, "pack output buffers and provider readers are independent")
	if _, err := os.Stat(objectPath(source, first.digest)); err != nil {
		return nil, fmt.Errorf("source fixture changed or disappeared: %w", err)
	}
	return controls, nil
}

func packRead(pack *os.File, offset, size int64, want string, packBytes int64) ([]byte, error) {
	if offset < 0 || size < 0 || offset > math.MaxInt64-size || offset+size > packBytes {
		return nil, errors.New("range outside pack")
	}
	b := make([]byte, int(size))
	n, err := pack.ReadAt(b, offset)
	if err != nil && !(err == io.EOF && n == len(b)) {
		return nil, err
	}
	if int64(n) != size || hashBytes(b) != want {
		return nil, errors.New("pack slice size or digest mismatch")
	}
	return b, nil
}

func measurePass(arm string, replicate int, provider *artifactprovider.Filesystem, pack *os.File, objects []selected, packBytes int64) (passResult, error) {
	beforeCPU, err := cpuUsage()
	if err != nil {
		return passResult{}, err
	}
	beforeIO, err := procIO()
	if err != nil { return passResult{}, fmt.Errorf("pre-pass /proc/self/io: %w", err) }
	start := time.Now()
	h := sha256.New()
	buf := make([]byte, 4096)
	var total int64
	for _, o := range objects {
		var n int
		if arm == "filesystem" {
			r, err := provider.GetObject(context.Background(), environmentartifact.Descriptor{Digest: o.digest, Size: o.size})
			if err != nil {
				return passResult{}, err
			}
			read, copyErr := io.CopyBuffer(io.Discard, r, buf)
			closeErr := r.Close()
			if copyErr != nil {
				return passResult{}, copyErr
			}
			if closeErr != nil {
				return passResult{}, closeErr
			}
			if read != o.size {
				return passResult{}, fmt.Errorf("provider returned %d bytes, expected %d", read, o.size)
			}
			n = int(read)
		} else {
			if o.size > int64(len(buf)) || o.offset > math.MaxInt64-o.size || o.offset+o.size > packBytes {
				return passResult{}, errors.New("pack range invalid")
			}
			n, err = pack.ReadAt(buf[:int(o.size)], o.offset)
			if err != nil && !(err == io.EOF && n == int(o.size)) {
				return passResult{}, err
			}
			if n != int(o.size) {
				return passResult{}, io.ErrUnexpectedEOF
			}
			sum := sha256.Sum256(buf[:n])
			if hex.EncodeToString(sum[:]) != o.digest.Hex() {
				return passResult{}, fmt.Errorf("pack digest mismatch for %s", o.digest)
			}
		}
		total += int64(n)
		writeIdentity(h, o.digest.Hex(), int64(n))
	}
	wall := time.Since(start)
	afterCPU, err := cpuUsage()
	if err != nil {
		return passResult{}, err
	}
	afterIO, err := procIO()
	if err != nil { return passResult{}, fmt.Errorf("post-pass /proc/self/io: %w", err) }
	rss, err := rssBytes()
	if err != nil { return passResult{}, fmt.Errorf("post-pass RSS: %w", err) }
	return passResult{Arm: arm, Replicate: replicate, WallNS: wall.Nanoseconds(), CPUUserNS: afterCPU.UserNS - beforeCPU.UserNS, CPUSystemNS: afterCPU.SystemNS - beforeCPU.SystemNS, Objects: len(objects), Bytes: total, ProviderCalls: countIf(arm == "filesystem", len(objects)), PackFileOpens: 0, ReadAtCalls: countIf(arm == "pack", len(objects)), ReadIOBytes: afterIO.RChar - beforeIO.RChar, ReadDiskBytes: afterIO.ReadBytes - beforeIO.ReadBytes, ReadSyscalls: afterIO.Syscr - beforeIO.Syscr, RSSBytes: rss, OrderedSHA256: hex.EncodeToString(h.Sum(nil))}, nil
}

func orderedIdentity(objects []selected) string {
	h := sha256.New()
	for _, o := range objects {
		writeIdentity(h, o.digest.Hex(), o.size)
	}
	return hex.EncodeToString(h.Sum(nil))
}
func writeIdentity(h hash.Hash, digest string, size int64) {
	_, _ = io.WriteString(h, digest)
	_, _ = fmt.Fprintf(h, ":%d\n", size)
}
func countIf(ok bool, n int) int {
	if ok {
		return n
	}
	return 0
}
func hashBytes(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
func sourceClosureHash(source string, objects []selected) (string, error) {
	h := sha256.New()
	for _, o := range objects {
		p := objectPath(source, o.digest)
		info, err := os.Lstat(p)
		if err != nil || !info.Mode().IsRegular() {
			return "", fmt.Errorf("source object is unavailable or not regular: %s", o.digest)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return "", err
		}
		digest := hashBytes(b)
		if int64(len(b)) != o.size || digest != o.digest.Hex() {
			return "", fmt.Errorf("source object changed: %s", o.digest)
		}
		writeIdentity(h, o.digest.Hex(), o.size)
		_, _ = fmt.Fprintf(h, "%s:%d:%d:%s\n", digest, info.Mode().Perm(), info.ModTime().UnixNano(), p)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

type cpuTimes struct{ UserNS, SystemNS int64 }

func cpuUsage() (cpuTimes, error) {
	var r syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &r); err != nil {
		return cpuTimes{}, err
	}
	return cpuTimes{int64(r.Utime.Sec)*1e9 + int64(r.Utime.Usec)*1e3, int64(r.Stime.Sec)*1e9 + int64(r.Stime.Usec)*1e3}, nil
}

type ioTimes struct{ RChar, ReadBytes, Syscr int64 }

func procIO() (ioTimes, error) {
	b, err := os.ReadFile("/proc/self/io")
	if err != nil { return ioTimes{}, err }
	var out ioTimes
	found := map[string]bool{}
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 { continue }
		var dst *int64
		switch fields[0] {
		case "rchar:": dst=&out.RChar
		case "read_bytes:": dst=&out.ReadBytes
		case "syscr:": dst=&out.Syscr
		default: continue
		}
		if found[fields[0]] || len(fields)!=2 { return ioTimes{}, fmt.Errorf("malformed or duplicate /proc/self/io field %q",fields[0]) }
		if _,err:=fmt.Sscanf(fields[1], "%d", dst); err!=nil { return ioTimes{}, fmt.Errorf("invalid /proc/self/io field %q: %w",fields[0],err) }
		found[fields[0]]=true
	}
	for _,key:=range []string{"rchar:","read_bytes:","syscr:"} { if !found[key] { return ioTimes{}, fmt.Errorf("required /proc/self/io field %q missing",key) } }
	return out,nil
}

func rssBytes() (int64, error) {
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "VmRSS:") {
			var kb int64
			if _, err := fmt.Sscanf(line, "VmRSS: %d kB", &kb); err != nil {
				return 0, err
			}
			return kb * 1024, nil
		}
	}
	return 0, errors.New("VmRSS missing")
}
