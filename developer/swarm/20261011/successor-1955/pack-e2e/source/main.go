//go:build linux

// Private RCC #125 isolated diagnostic harness, prepared for native admission;
// this source has not been built or executed.
package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/joshyorko/rcc/artifactprovider"
	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/environmentartifact"
	"github.com/joshyorko/rcc/environmentlifecycle"
	"github.com/joshyorko/rcc/htfs"
	"golang.org/x/sys/unix"
)

const (
	objectCount  = 512
	maxPackBytes = int64(8 << 20)
	objectMedia  = "application/vnd.rcc.hololib.object.v12+gzip"
)

type fileRecord struct {
	Path   string `json:"path"`
	Size   int    `json:"size"`
	SHA256 string `json:"sha256"`
}
type fixtureReceipt struct {
	Artifact          string       `json:"artifact"`
	ManifestSHA256    string       `json:"manifestSha256"`
	ObjectIndexSHA256 string       `json:"objectIndexSha256"`
	Files             []fileRecord `json:"files"`
	Workload          []fileRecord `json:"workload"`
	ExpectedOutput    string       `json:"expectedOutput"`
}
type packEntry struct {
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
	Offset int64  `json:"offset"`
}
type packIndex struct {
	Format            string      `json:"format"`
	Artifact          string      `json:"artifact"`
	ObjectIndexDigest string      `json:"objectIndexDigest"`
	PackSize          int64       `json:"packSize"`
	PackSHA256        string      `json:"packSha256"`
	Entries           []packEntry `json:"entries"`
}
type armRecord struct {
	Type               string `json:"type"`
	Pair               int    `json:"pair"`
	Arm                string `json:"arm"`
	Status             string `json:"status"`
	Started            string `json:"started"`
	ElapsedNS          int64  `json:"elapsedNs"`
	ColdPipelineNS     int64  `json:"coldPipelineNs,omitempty"`
	AcquireNS          int64  `json:"acquireNs,omitempty"`
	ProviderSetupNS    int64  `json:"providerSetupNs,omitempty"`
	ExecuteNS          int64  `json:"executeNs,omitempty"`
	WarmNS             int64  `json:"warmNs,omitempty"`
	SelfCPUUserNS      int64  `json:"selfCpuUserNs,omitempty"`
	SelfCPUSystemNS    int64  `json:"selfCpuSystemNs,omitempty"`
	ChildCPUUserNS     int64  `json:"childCpuUserNs,omitempty"`
	ChildCPUSystemNS   int64  `json:"childCpuSystemNs,omitempty"`
	RChar              int64  `json:"rchar,omitempty"`
	ReadBytes          int64  `json:"readBytes,omitempty"`
	ReadSyscalls       int64  `json:"readSyscalls,omitempty"`
	RSSAfterBytes      int64  `json:"rssAfterBytes,omitempty"`
	Artifact           string `json:"artifact,omitempty"`
	Materialization    string `json:"materialization,omitempty"`
	Output             string `json:"output,omitempty"`
	LeaseReleased      bool   `json:"leaseReleased,omitempty"`
	ProviderGetObjects int64  `json:"providerGetObjects,omitempty"`
	WarmProviderCalls  int64  `json:"warmProviderCalls,omitempty"`
	Error              string `json:"error,omitempty"`
}
type usage struct{ user, system int64 }
type ioCounts struct{ rchar, readBytes, syscr int64 }

func main() {
	mode := flag.String("mode", "prepare", "prepare or worker")
	root := flag.String("root", "/workspace/rcc-successor-pack", "owned scratch root")
	python := flag.String("python", "", "absolute installed host Python executable")
	arm := flag.String("arm", "", "filesystem or packed")
	pair := flag.Int("pair", 0, "pair ordinal")
	artifactRoot := flag.String("artifact-root", "", "published filesystem provider root")
	packRoot := flag.String("pack-root", "", "private packed sidecar root")
	artifact := flag.String("artifact", "", "sha256 artifact identity")
	indexSHA := flag.String("index-sha", "", "pinned pack index SHA256")
	packSHA := flag.String("pack-sha", "", "pinned pack SHA256")
	receiptPath := flag.String("receipt", "", "append-only worker JSONL receipt")
	profilePath := flag.String("profile", "", "CPU profile output")
	flag.Parse()
	var err error
	switch *mode {
	case "worker":
		err = worker(*root, *python, *arm, *pair, *artifactRoot, *packRoot, *artifact, *indexSHA, *packSHA, *receiptPath, *profilePath)
	case "prepare":
		err = prepare(*root, *python)
	case "run":
		if err = prepare(*root, *python); err == nil {
			err = runSchedule(*root, *python)
		}
	default:
		err = fmt.Errorf("unknown mode %q", *mode)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func prepare(root, python string) error {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return errors.New("scratch root must be a clean absolute path")
	}
	if !filepath.IsAbs(python) {
		return errors.New("-python must be an absolute host interpreter path")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	if err := mode700(root); err != nil {
		return fmt.Errorf("scratch root mode guard: %w", err)
	}
	producerHome := filepath.Join(root, "producer-home")
	if err := os.Mkdir(producerHome, 0o700); err != nil && !os.IsExist(err) {
		return err
	}
	common.Product.ForceHome(producerHome)
	if err := mode700(producerHome); err != nil {
		return fmt.Errorf("producer home mode guard: %w", err)
	}
	if err := os.MkdirAll(common.HololibCatalogLocation(), 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(common.HololibLibraryLocation(), 0o700); err != nil {
		return err
	}

	probe, err := probeHostPython(context.Background(), python)
	if err != nil {
		return err
	}
	if probe.Implementation != "cpython" {
		return fmt.Errorf("host Python implementation %q is unsupported", probe.Implementation)
	}
	platform := environmentartifact.CurrentPlatform()
	requirements := environmentartifact.CompatibilityRequirements{
		SchemaVersion: environmentartifact.CompatibilitySchemaV1, RelocationVersion: "holotree-v12-path-rewrite-v1",
		Python:     environmentartifact.PythonRequirements{Implementation: probe.Implementation, Version: probe.Version, ABI: probe.ABI},
		OS:         environmentartifact.OSRequirements{Family: platform.OS, MinimumVersion: "1", NativeArchitecture: platform.Arch, TranslationPolicy: "native-only", RequiredLibraries: []string{}},
		CPU:        environmentartifact.CPURequirements{Architecture: platform.Arch, RequiredFeatures: []string{}},
		Filesystem: environmentartifact.FilesystemRequirements{MinimumMaxPath: 120 + len("payload-0511.bin")},
	}
	if platform.OS == "linux" {
		uname := unix.Utsname{}
		if err := unix.Uname(&uname); err != nil {
			return err
		}
		requirements.OS.KernelMinimum = strings.TrimRight(string(uname.Release[:]), "\x00")
		libc, err := exec.Command("getconf", "GNU_LIBC_VERSION").Output()
		if err != nil {
			return fmt.Errorf("read host glibc identity: %w", err)
		}
		fields := strings.Fields(string(libc))
		if len(fields) != 2 || fields[0] != "glibc" {
			return fmt.Errorf("unexpected host glibc identity %q", libc)
		}
		requirements.OS.LibC = fields[0]
		requirements.OS.LibCMinimum = fields[1]
	}
	if err := requirements.Validate(); err != nil {
		return fmt.Errorf("fixture compatibility: %w", err)
	}

	legacy := []byte("channels:\n  - conda-forge\ndependencies:\n  - python\n")
	blueprint := common.BlueprintHash(legacy)
	catalogRootPath := filepath.Join(tMustTemp(root, "catalog-root"), "h123456_123456789abcdeft")
	htree, err := htfs.NewRoot(catalogRootPath)
	if err != nil {
		return err
	}
	htree.Blueprint = blueprint
	htree.Tree.Mode = 0o750 | os.ModeDir
	pythonWrapper := []byte("#!/bin/sh\nexec " + shellQuote(python) + " \"$@\"\n")
	files := make([]fileRecord, 0, objectCount+1)
	workload := make([]fileRecord, 0, objectCount)
	objects := make([]environmentartifact.Descriptor, 0, objectCount+1)
	add := func(path string, logical []byte, mode os.FileMode) error {
		sum := sha256.Sum256(logical)
		legacyID := hex.EncodeToString(sum[:])
		stored, err := gzipBytes(logical)
		if err != nil {
			return err
		}
		casPath := htfs.ExactDefaultLocation(legacyID)
		if err := os.MkdirAll(filepath.Dir(casPath), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(casPath, stored, 0o600); err != nil {
			return err
		}
		file := &htfs.File{Name: filepath.Base(path), Size: int64(len(logical)), Mode: mode, Digest: legacyID, Rewrite: []int64{}}
		if path == "bin/python" {
			dir := &htfs.Dir{Name: "bin", Mode: 0o750 | os.ModeDir, Dirs: map[string]*htfs.Dir{}, Files: map[string]*htfs.File{}}
			dir.Files[file.Name] = file
			htree.Tree.Dirs["bin"] = dir
		} else {
			htree.Tree.Files[file.Name] = file
		}
		rec := fileRecord{Path: path, Size: len(logical), SHA256: legacyID}
		files = append(files, rec)
		if path != "bin/python" {
			workload = append(workload, rec)
		}
		objects = append(objects, environmentartifact.Descriptor{MediaType: objectMedia, Digest: environmentartifact.DigestBytes(stored), Size: int64(len(stored))})
		return nil
	}
	if err := add("bin/python", pythonWrapper, 0o750); err != nil {
		return err
	}
	for i := 0; i < objectCount; i++ {
		logical := []byte(fmt.Sprintf("rcc-pack-successor:%04d:unique-small-payload:%064x\n", i, i+1))
		if err := add(fmt.Sprintf("payload-%04d.bin", i), logical, 0o640); err != nil {
			return err
		}
	}
	catalogPath := filepath.Join(common.HololibCatalogLocation(), htfs.CatalogName(blueprint))
	if err := htree.SaveAs(catalogPath); err != nil {
		return err
	}
	spec := []byte(`{"fixture":"private-pack-e2e","objectCount":513,"source":"synthetic"}`)
	build := environmentlifecycle.BuildResult{LegacyBlueprint: legacy, CatalogPath: catalogPath, SpecificationBytes: spec, SourceKind: "synthetic-fixture", Platform: platform, Builder: environmentartifact.Builder{Kind: "rcc-test-fixture", RCCVersion: "v18.19.6-probe", CompatibilityKey: "v12-gzip-sha256"}, Compatibility: requirements}
	providerRoot := filepath.Join(root, "filesystem-provider")
	provider, err := artifactprovider.NewFilesystem(providerRoot)
	if err != nil {
		return err
	}
	published, err := environmentlifecycle.Publish(context.Background(), environmentlifecycle.PublishRequest{RobotFile: "synthetic", Provider: provider, Builder: fixtureBuilder{build}})
	if err != nil {
		return err
	}
	manifestBytes, err := provider.ResolveManifest(context.Background(), published.ArtifactDigest)
	if err != nil {
		return err
	}
	manifest, err := environmentartifact.DecodeManifest(manifestBytes)
	if err != nil {
		return err
	}
	if manifest.ArtifactDigest != published.ArtifactDigest || manifest.ObjectIndex.Size > maxPackBytes {
		return errors.New("published manifest/index bounds failed")
	}
	if len(files) != objectCount+1 {
		return errors.New("fixture object count mismatch")
	}
	indexReader, err := provider.GetObject(context.Background(), manifest.ObjectIndex)
	if err != nil {
		return err
	}
	indexBytes, err := io.ReadAll(indexReader)
	_ = indexReader.Close()
	if err != nil {
		return err
	}
	idx, err := environmentartifact.DecodeObjectIndex(indexBytes)
	if err != nil {
		return err
	}
	if idx.Count != len(files) {
		return fmt.Errorf("published index count=%d want=%d", idx.Count, len(files))
	}
	packDir := filepath.Join(root, "packed-sidecar")
	if err := os.Mkdir(packDir, 0o700); err != nil {
		return err
	}
	packFile, err := os.OpenFile(filepath.Join(packDir, "objects.pack"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	sort.Slice(objects, func(i, j int) bool { return objects[i].Digest.Hex() < objects[j].Digest.Hex() })
	entries := make([]packEntry, 0, len(objects))
	var offset int64
	for _, d := range objects {
		r, e := provider.GetObject(context.Background(), d)
		if e != nil {
			_ = packFile.Close()
			return e
		}
		data, e := io.ReadAll(io.LimitReader(r, d.Size+1))
		ce := r.Close()
		if e != nil || ce != nil {
			_ = packFile.Close()
			return fmt.Errorf("read fixture object %s: %v/%v", d.Digest, e, ce)
		}
		if int64(len(data)) != d.Size || environmentartifact.DigestBytes(data) != d.Digest {
			_ = packFile.Close()
			return errors.New("fixture source object descriptor mismatch")
		}
		if _, e = packFile.Write(data); e != nil {
			_ = packFile.Close()
			return e
		}
		entries = append(entries, packEntry{Digest: d.Digest.Hex(), Size: d.Size, Offset: offset})
		offset += d.Size
	}
	if offset > maxPackBytes {
		_ = packFile.Close()
		return errors.New("fixture pack exceeds 8 MiB")
	}
	if err := packFile.Sync(); err != nil {
		_ = packFile.Close()
		return err
	}
	if err := packFile.Close(); err != nil {
		return err
	}
	packHash, err := hashFile(filepath.Join(packDir, "objects.pack"))
	if err != nil {
		return err
	}
	pi := packIndex{Format: "rcc-private-test-pack-v1", Artifact: published.ArtifactDigest.String(), ObjectIndexDigest: manifest.ObjectIndex.Digest.String(), PackSize: offset, PackSHA256: packHash, Entries: entries}
	ib, err := json.Marshal(pi)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(packDir, "index.json"), ib, 0o600); err != nil {
		return err
	}
	indexHash := sumBytes(ib)
	fixture := fixtureReceipt{Artifact: published.ArtifactDigest.String(), ManifestSHA256: sumBytes(manifestBytes), ObjectIndexSHA256: sumBytes(indexBytes), Files: files, Workload: workload}
	fixture.ExpectedOutput = aggregateFiles(workload)
	fb, err := json.MarshalIndent(fixture, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, "fixture.json"), fb, 0o600); err != nil {
		return err
	}
	return writeJSON(filepath.Join(root, "prepared.json"), map[string]any{"status": "PREPARED_ONLY", "artifact": published.ArtifactDigest.String(), "manifestSha256": sumBytes(manifestBytes), "objectIndexSha256": sumBytes(indexBytes), "packSha256": packHash, "packIndexSha256": indexHash, "packBytes": offset, "files": len(files), "providerRoot": providerRoot, "packRoot": packDir, "fixture": filepath.Join(root, "fixture.json"), "note": "preparation source only; no worker launch or execution from this lane"})
}

type fixtureBuilder struct {
	result environmentlifecycle.BuildResult
}

func (b fixtureBuilder) Build(context.Context, string) (environmentlifecycle.BuildResult, error) {
	return b.result, nil
}
func tMustTemp(root, prefix string) string {
	p := filepath.Join(root, prefix)
	_ = os.MkdirAll(p, 0o700)
	return p
}
func gzipBytes(b []byte) ([]byte, error) {
	var out bytes.Buffer
	w, e := gzip.NewWriterLevel(&out, gzip.BestSpeed)
	if e != nil {
		return nil, e
	}
	if _, e = w.Write(b); e != nil {
		return nil, e
	}
	if e = w.Close(); e != nil {
		return nil, e
	}
	return out.Bytes(), nil
}

type pythonRuntime struct {
	Implementation string `json:"implementation"`
	Version        string `json:"version"`
	ABI            string `json:"abi"`
}

func probeHostPython(ctx context.Context, path string) (pythonRuntime, error) {
	var out pythonRuntime
	cmd := exec.CommandContext(ctx, path, "-c", "import json,platform,sys,sysconfig; print(json.dumps({'implementation':sys.implementation.name,'version':platform.python_version(),'abi':sysconfig.get_config_var('SOABI') or sys.implementation.cache_tag},sort_keys=True))")
	b, e := cmd.Output()
	if e != nil {
		return out, e
	}
	e = json.Unmarshal(b, &out)
	return out, e
}
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
func sumBytes(b []byte) string   { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func hashFile(p string) (string, error) {
	f, e := os.Open(p)
	if e != nil {
		return "", e
	}
	defer f.Close()
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return "", e
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func aggregateFiles(files []fileRecord) string {
	ordered := append([]fileRecord(nil), files...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Path < ordered[j].Path })
	h := sha256.New()
	for _, f := range ordered {
		fmt.Fprintf(h, "%s\x00%d\x00%s\n", f.Path, f.Size, f.SHA256)
	}
	return hex.EncodeToString(h.Sum(nil))
}
func mode700(path string) error {
	st, e := os.Lstat(path)
	if e != nil {
		return e
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 || st.Mode().Perm() != 0o700 {
		return fmt.Errorf("%s is not a strict 0700 directory (mode=%v)", path, st.Mode())
	}
	info, ok := st.Sys().(*syscall.Stat_t)
	if !ok || info.Uid != uint32(os.Geteuid()) || info.Gid != uint32(os.Getegid()) {
		return fmt.Errorf("%s owner uid/gid mismatch (want %d/%d)", path, os.Geteuid(), os.Getegid())
	}
	return nil
}
func writeJSON(path string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	return os.WriteFile(path, b, 0o600)
}

func worker(root, python, arm string, pair int, artifactRoot, packRoot, artifact, indexSHA, packSHA, receiptPath, profilePath string) (retErr error) {
	started := time.Now()
	record := armRecord{Type: "arm", Pair: pair, Arm: arm, Status: "RUNNING", Started: started.UTC().Format(time.RFC3339Nano)}
	if arm != "filesystem" && arm != "packed" {
		return errors.New("unknown arm")
	}
	if receiptPath == "" || profilePath == "" {
		return errors.New("worker receipt/profile paths required")
	}
	receiptWritten := false
	appendReceipt := func() error {
		record.ElapsedNS = time.Since(started).Nanoseconds()
		if err := appendJSONL(receiptPath, record); err != nil {
			return err
		}
		receiptWritten = true
		return nil
	}
	defer func() {
		if retErr != nil && !receiptWritten && receiptPath != "" {
			record.Status = "FAILED"
			record.Error = retErr.Error()
			if receiptErr := appendReceipt(); receiptErr != nil {
				retErr = fmt.Errorf("%v; write failure receipt: %w", retErr, receiptErr)
			}
		}
	}()
	commonHome := common.Product.Home()
	if e := mode700(commonHome); e != nil {
		return fmt.Errorf("pre-home guard: %w", e)
	}
	cas := filepath.Join(commonHome, "artifacts", "v1", "content")
	if _, err := os.Lstat(cas); !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("consumer CAS is not fresh/empty: %s stat error=%v", cas, err)
	}
	profile, e := os.OpenFile(profilePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if e != nil {
		return e
	}
	if e = pprof.StartCPUProfile(profile); e != nil {
		_ = profile.Close()
		return e
	}
	defer func() { pprof.StopCPUProfile(); _ = profile.Close() }()
	beforeUsage, e := getUsage(unix.RUSAGE_SELF)
	if e != nil {
		return e
	}
	beforeChildren, e := getUsage(unix.RUSAGE_CHILDREN)
	if e != nil {
		return e
	}
	beforeIO, e := procIO()
	if e != nil {
		return e
	}
	fixtureBytes, e := os.ReadFile(filepath.Join(root, "fixture.json"))
	if e != nil {
		return e
	}
	var fixture fixtureReceipt
	if e = json.Unmarshal(fixtureBytes, &fixture); e != nil {
		return e
	}
	if fixture.Artifact != artifact {
		return errors.New("worker artifact differs from fixture")
	}
	setup := time.Now()
	fsprovider, e := artifactprovider.NewFilesystem(artifactRoot)
	if e != nil {
		return e
	}
	provider := &countingProvider{Provider: fsprovider}
	var input artifactprovider.Provider = provider
	var packed *packedProvider
	if arm == "packed" {
		packed, e = newPackedProvider(provider, packRoot, artifact, indexSHA, packSHA)
		if e != nil {
			return e
		}
		defer packed.Close()
		input = packed
	}
	record.ProviderSetupNS = time.Since(setup).Nanoseconds()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stage := time.Now()
	result, e := environmentlifecycle.NewAcquirer().Acquire(ctx, environmentlifecycle.AcquireRequest{ArtifactDigest: mustDigest(artifact), Provider: input})
	record.AcquireNS = time.Since(stage).Nanoseconds()
	record.ProviderGetObjects = providerGets(input)
	if e != nil {
		record.Status = "FAILED"
		record.Error = "acquire: " + e.Error()
		appendReceipt()
		return e
	}
	record.Artifact = result.ArtifactDigest.String()
	record.Materialization = result.MaterializationID
	if result.ArtifactDigest.String() != artifact || result.CacheHit != environmentlifecycle.CacheProvider {
		e = errors.New("cold acquisition identity/provenance mismatch")
		record.Status = "FAILED"
		record.Error = e.Error()
		appendReceipt()
		return e
	}
	mat := environmentlifecycle.Materialization{ArtifactDigest: result.ArtifactDigest, ID: result.MaterializationID, Path: result.Path, CacheHit: result.CacheHit, Verification: result.Verification, TrustPolicy: result.TrustPolicy, TrustRequest: result.TrustRequest, TrustCarrier: result.TrustCarrier}
	program := pythonVerifierProgram
	stage = time.Now()
	var stdout, stderr bytes.Buffer
	handle, child, e := environmentlifecycle.ExecuteWithStreams(ctx, environmentlifecycle.NewLocalMaterializer(), mat, []string{"python", "-c", program, filepath.Join(root, "fixture.json"), result.Path}, nil, &stdout, &stderr)
	record.ExecuteNS = time.Since(stage).Nanoseconds()
	if e != nil {
		e = fmt.Errorf("execute: %w; stderr=%s", e, stderr.String())
		record.Status = "FAILED"
		record.Error = e.Error()
		appendReceipt()
		return e
	}
	var childOutput map[string]any
	if e = json.Unmarshal(stdout.Bytes(), &childOutput); e != nil {
		e = fmt.Errorf("decode child output %q: %w", stdout.String(), e)
		record.Status = "FAILED"
		record.Error = e.Error()
		appendReceipt()
		return e
	}
	if child.ExitCode != 0 || childOutput["count"] != float64(len(fixture.Workload)) || childOutput["aggregateSha256"] != fixture.ExpectedOutput {
		e = fmt.Errorf("child verification mismatch exit=%d output=%v stderr=%q", child.ExitCode, childOutput, stderr.String())
		record.Status = "FAILED"
		record.Error = e.Error()
		appendReceipt()
		return e
	}
	record.ColdPipelineNS = record.ProviderSetupNS + record.AcquireNS + record.ExecuteNS
	record.Output = stdout.String()
	record.LeaseReleased, e = leaseRemoved(commonHome, result.ArtifactDigest, handle.LeaseID)
	if !record.LeaseReleased || e != nil {
		e = fmt.Errorf("execute did not prove lease removal: %w", e)
		record.Status = "FAILED"
		record.Error = e.Error()
		appendReceipt()
		return e
	}
	stage = time.Now()
	warmRemote := &untouchedProvider{}
	warm, e := environmentlifecycle.NewAcquirer().Acquire(ctx, environmentlifecycle.AcquireRequest{ArtifactDigest: result.ArtifactDigest, Provider: warmRemote})
	record.WarmNS = time.Since(stage).Nanoseconds()
	record.WarmProviderCalls = warmRemote.calls.Load()
	if e != nil || record.WarmProviderCalls != 0 || warm.CacheHit != environmentlifecycle.CacheLocalMaterialization || warm.Path != result.Path || warm.MaterializationID != result.MaterializationID {
		e = fmt.Errorf("warm local acquisition mismatch: result=%+v err=%v providerCalls=%d", warm, e, record.WarmProviderCalls)
		record.Status = "FAILED"
		record.Error = e.Error()
		appendReceipt()
		return e
	}
	if e = mode700(commonHome); e != nil {
		record.Status = "FAILED"
		record.Error = "post-home guard: " + e.Error()
		appendReceipt()
		return e
	}
	afterUsage, e := getUsage(unix.RUSAGE_SELF)
	if e != nil {
		return e
	}
	afterChildren, e := getUsage(unix.RUSAGE_CHILDREN)
	if e != nil {
		return e
	}
	afterIO, e := procIO()
	if e != nil {
		return e
	}
	record.SelfCPUUserNS = afterUsage.user - beforeUsage.user
	record.SelfCPUSystemNS = afterUsage.system - beforeUsage.system
	record.ChildCPUUserNS = afterChildren.user - beforeChildren.user
	record.ChildCPUSystemNS = afterChildren.system - beforeChildren.system
	record.RChar = afterIO.rchar - beforeIO.rchar
	record.ReadBytes = afterIO.readBytes - beforeIO.readBytes
	record.ReadSyscalls = afterIO.syscr - beforeIO.syscr
	record.RSSAfterBytes, e = procRSS()
	if e != nil {
		return e
	}
	record.Status = "PASS"
	return appendReceipt()
}

const pythonVerifierProgram = `import hashlib,json,pathlib,sys
receipt=json.load(open(sys.argv[1],encoding='utf-8'))
rows=[]
for item in sorted(receipt['workload'],key=lambda x:x['path']):
 p=pathlib.Path(sys.argv[2])/item['path']; b=p.read_bytes(); d=hashlib.sha256(b).hexdigest()
 if len(b)!=item['size'] or d!=item['sha256']: raise SystemExit('payload mismatch: '+item['path'])
 rows.append((item['path'],len(b),d))
h=hashlib.sha256()
for path,size,digest in rows: h.update(f'{path}\0{size}\0{digest}\n'.encode())
print(json.dumps({'count':len(rows),'aggregateSha256':h.hexdigest()},sort_keys=True,separators=(',',':')))`

func mustDigest(s string) environmentartifact.Digest {
	d, e := environmentartifact.ParseDigest(s)
	if e != nil {
		panic(e)
	}
	return d
}
func leaseRemoved(home string, d environmentartifact.Digest, id string) (bool, error) {
	if id == "" {
		return false, errors.New("empty lease ID")
	}
	_, e := os.Lstat(filepath.Join(home, "artifacts", "v1", "materializations", d.Hex(), "leases", id+".json"))
	if errors.Is(e, os.ErrNotExist) {
		return true, nil
	}
	if e != nil {
		return false, e
	}
	return false, nil
}
func getUsage(who int) (usage, error) {
	var r unix.Rusage
	if err := unix.Getrusage(who, &r); err != nil {
		return usage{}, err
	}
	return usage{(r.Utime.Sec*1e6 + r.Utime.Usec) * 1000, (r.Stime.Sec*1e6 + r.Stime.Usec) * 1000}, nil
}
func procIO() (ioCounts, error) {
	b, e := os.ReadFile("/proc/self/io")
	if e != nil {
		return ioCounts{}, e
	}
	var x ioCounts
	for _, line := range strings.Split(string(b), "\n") {
		p := strings.SplitN(line, ":", 2)
		if len(p) != 2 {
			continue
		}
		v, _ := strconv.ParseInt(strings.TrimSpace(p[1]), 10, 64)
		switch p[0] {
		case "rchar":
			x.rchar = v
		case "read_bytes":
			x.readBytes = v
		case "syscr":
			x.syscr = v
		}
	}
	if x.rchar == 0 && x.readBytes == 0 && x.syscr == 0 {
		return ioCounts{}, errors.New("/proc/self/io contained no usable read counters")
	}
	return x, nil
}
func procRSS() (int64, error) {
	b, e := os.ReadFile("/proc/self/status")
	if e != nil {
		return 0, e
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "VmRSS:") {
			p := strings.Fields(line)
			if len(p) > 1 {
				n, _ := strconv.ParseInt(p[1], 10, 64)
				return n * 1024, nil
			}
		}
	}
	return 0, errors.New("/proc/self/status has no VmRSS")
}
func appendJSONL(path string, v any) error {
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if e != nil {
		return e
	}
	defer f.Close()
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	if _, e = f.Write(append(b, '\n')); e != nil {
		return e
	}
	return f.Sync()
}
func providerGets(p artifactprovider.Provider) int64 {
	if x, ok := p.(interface{ GetCount() int64 }); ok {
		return x.GetCount()
	}
	return 0
}

// packedProvider is private to this experiment. Metadata and non-packed calls
// retain the real Filesystem provider semantics. Every returned frame owns a
// separate buffer; no shared mutable slices or caller-provided offsets exist.
type countingProvider struct {
	artifactprovider.Provider
	gets atomic.Int64
}

func (p *countingProvider) GetObject(c context.Context, d environmentartifact.Descriptor) (io.ReadCloser, error) {
	p.gets.Add(1)
	return p.Provider.GetObject(c, d)
}
func (p *countingProvider) GetCount() int64 { return p.gets.Load() }

type packedProvider struct {
	base           artifactprovider.Provider
	rootFD, fileFD int
	fileSize       int64
	index          packIndex
	entries        map[string]packEntry
	gets           atomic.Int64
}

func newPackedProvider(base artifactprovider.Provider, root, artifact, indexSHA, packSHA string) (result *packedProvider, retErr error) {
	if !filepath.IsAbs(root) {
		return nil, errors.New("pack root must be absolute")
	}
	rootFD, e := openNoFollowRoot(root)
	if e != nil {
		return nil, e
	}
	packFD := -1
	defer func() {
		if retErr != nil {
			_ = unix.Close(rootFD)
			if packFD >= 0 {
				_ = unix.Close(packFD)
			}
		}
	}()
	indexBytes, e := readRegularAt(rootFD, "index.json", 1<<20)
	if e != nil {
		return nil, e
	}
	if sumBytes(indexBytes) != indexSHA {
		return nil, errors.New("pack index SHA256 mismatch")
	}
	var idx packIndex
	if e = json.Unmarshal(indexBytes, &idx); e != nil {
		return nil, e
	}
	if idx.Format != "rcc-private-test-pack-v1" || idx.Artifact != artifact || idx.PackSHA256 != packSHA || idx.PackSize < 0 || idx.PackSize > maxPackBytes {
		return nil, errors.New("pack index identity or size invalid")
	}
	manifestBytes, e := base.ResolveManifest(context.Background(), mustDigest(artifact))
	if e != nil {
		return nil, e
	}
	manifest, e := environmentartifact.DecodeManifest(manifestBytes)
	if e != nil || manifest.ArtifactDigest.String() != artifact || manifest.ObjectIndex.Digest.String() != idx.ObjectIndexDigest {
		return nil, errors.New("pack index manifest/object-index binding mismatch")
	}
	ir, e := base.GetObject(context.Background(), manifest.ObjectIndex)
	if e != nil {
		return nil, e
	}
	ib, e := io.ReadAll(io.LimitReader(ir, manifest.ObjectIndex.Size+1))
	ce := ir.Close()
	if e != nil || ce != nil || environmentartifact.DigestBytes(ib) != manifest.ObjectIndex.Digest {
		return nil, errors.New("standard object index read or digest mismatch")
	}
	oi, e := environmentartifact.DecodeObjectIndex(ib)
	if e != nil {
		return nil, e
	}
	if idx.ObjectIndexDigest != manifest.ObjectIndex.Digest.String() || oi.Count != len(idx.Entries) || manifest.ObjectIndex.MediaType == "" {
		return nil, errors.New("pack cross-binding count or descriptor identity mismatch")
	}
	standard := make(map[string]int64, len(oi.Entries))
	for _, x := range oi.Entries {
		standard[x.StoredDigest.Hex()] = x.StoredSize
	}
	if len(standard) != len(idx.Entries) || oi.Count != len(idx.Entries) || oi.Encoding != "gzip" || manifest.ObjectIndex.MediaType != environmentartifact.ObjectIndexMediaType {
		return nil, errors.New("pack count/media/encoding differs from standard object index")
	}
	for _, x := range idx.Entries {
		size, present := standard[x.Digest]
		if !present || size != x.Size || x.Digest == "" {
			return nil, errors.New("pack entry differs from standard object index")
		}
	}
	fd, e := unix.Openat(rootFD, "objects.pack", unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if e != nil {
		return nil, e
	}
	packFD = fd
	var st unix.Stat_t
	if e = unix.Fstat(fd, &st); e != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Size != idx.PackSize {
		return nil, errors.New("pack is not a regular file of exact indexed size")
	}
	full := make([]byte, int(idx.PackSize))
	e = preadExact(fd, full, 0)
	if e != nil || sumBytes(full) != idx.PackSHA256 || idx.PackSHA256 != packSHA {
		return nil, errors.New("whole-pack hash/length mismatch")
	}
	entries := make(map[string]packEntry, len(idx.Entries))
	var end int64
	previous := ""
	for _, x := range idx.Entries {
		if len(x.Digest) != 64 || x.Digest <= previous || x.Size < 0 || x.Size > maxPackBytes || x.Offset != end || x.Offset > idx.PackSize || x.Size > idx.PackSize-x.Offset {
			return nil, errors.New("pack entry order, overlap, or bounds invalid")
		}
		entries[x.Digest] = x
		end = x.Offset + x.Size
		previous = x.Digest
	}
	if end != idx.PackSize || len(entries) != len(idx.Entries) {
		return nil, errors.New("pack index does not cover exact pack bytes")
	}
	packFD = -1
	return &packedProvider{base: base, rootFD: rootFD, fileFD: fd, fileSize: idx.PackSize, index: idx, entries: entries}, nil
}
func (p *packedProvider) Close()          { _ = unix.Close(p.fileFD); _ = unix.Close(p.rootFD) }
func (p *packedProvider) GetCount() int64 { return p.gets.Load() + providerGets(p.base) }
func (p *packedProvider) Capabilities(c context.Context) (artifactprovider.Capabilities, error) {
	return p.base.Capabilities(c)
}
func (p *packedProvider) ResolveManifest(c context.Context, d environmentartifact.Digest) ([]byte, error) {
	return p.base.ResolveManifest(c, d)
}
func (p *packedProvider) MissingObjects(c context.Context, d []environmentartifact.Descriptor) ([]environmentartifact.Digest, error) {
	return p.base.MissingObjects(c, d)
}
func (p *packedProvider) PutObject(c context.Context, b artifactprovider.Blob) error {
	return p.base.PutObject(c, b)
}
func (p *packedProvider) CommitManifest(c context.Context, b []byte) error {
	return p.base.CommitManifest(c, b)
}
func (p *packedProvider) GetObject(ctx context.Context, d environmentartifact.Descriptor) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, isPackedDigest := p.entries[d.Digest.Hex()]; isPackedDigest && d.MediaType != objectMedia {
		return nil, errors.New("packed object descriptor media type mismatch")
	}
	if d.MediaType != objectMedia {
		return p.base.GetObject(ctx, d)
	}
	buf, e := p.readFrame(ctx, d)
	if e != nil {
		return nil, e
	}
	return io.NopCloser(bytes.NewReader(buf)), nil
}
func (p *packedProvider) readFrame(ctx context.Context, d environmentartifact.Descriptor) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	x, ok := p.entries[d.Digest.Hex()]
	if !ok || x.Size != d.Size {
		return nil, errors.New("packed provider descriptor does not match index")
	}
	if d.Size < 0 || d.Size > maxPackBytes || x.Offset > p.fileSize || x.Size > p.fileSize-x.Offset {
		return nil, errors.New("packed provider frame outside bounds")
	}
	var st unix.Stat_t
	if e := unix.Fstat(p.fileFD, &st); e != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Size != p.fileSize {
		return nil, errors.New("pack file stat changed")
	}
	buf := make([]byte, int(d.Size))
	e := preadExact(p.fileFD, buf, x.Offset)
	if e != nil {
		return nil, errors.New("short packed frame read")
	}
	if sumBytes(buf) != d.Digest.Hex() {
		return nil, errors.New("packed frame SHA256 mismatch")
	}
	p.gets.Add(1)
	return buf, nil
}
func openNoFollowRoot(path string) (int, error) {
	abs, e := filepath.Abs(path)
	if e != nil {
		return -1, e
	}
	if abs == "/" {
		return -1, errors.New("pack root cannot be filesystem root")
	}
	fd, e := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if e != nil {
		return -1, e
	}
	for _, c := range strings.Split(strings.TrimPrefix(abs, "/"), "/") {
		if c == "" || c == "." || c == ".." {
			_ = unix.Close(fd)
			return -1, errors.New("invalid pack root component")
		}
		next, er := unix.Openat(fd, c, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		_ = unix.Close(fd)
		if er != nil {
			return -1, er
		}
		fd = next
	}
	return fd, nil
}
func preadExact(fd int, b []byte, offset int64) error {
	for len(b) > 0 {
		n, e := unix.Pread(fd, b, offset)
		if e != nil {
			return e
		}
		if n == 0 {
			return io.ErrUnexpectedEOF
		}
		offset += int64(n)
		b = b[n:]
	}
	return nil
}
func readRegularAt(root int, name string, max int64) ([]byte, error) {
	fd, e := unix.Openat(root, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if e != nil {
		return nil, e
	}
	defer unix.Close(fd)
	var st unix.Stat_t
	if e = unix.Fstat(fd, &st); e != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Size < 0 || st.Size > max {
		return nil, errors.New("pack index is not a bounded regular file")
	}
	b := make([]byte, int(st.Size))
	if e = preadExact(fd, b, 0); e != nil {
		return nil, errors.New("short pack index read")
	}
	return b, nil
}

// Parent process is intentionally implemented after coordinator review. The
// source-only handoff must not start workers until the external source gate.
var _ = runtime.GOOS
var _ = sort.Strings
var _ = syscall.Getrusage
var _ = bufio.NewScanner

type untouchedProvider struct{ calls atomic.Int64 }

func (p *untouchedProvider) touched() error {
	p.calls.Add(1)
	return errors.New("warm path unexpectedly touched provider")
}
func (p *untouchedProvider) Capabilities(context.Context) (artifactprovider.Capabilities, error) {
	return artifactprovider.Capabilities{}, p.touched()
}
func (p *untouchedProvider) ResolveManifest(context.Context, environmentartifact.Digest) ([]byte, error) {
	return nil, p.touched()
}
func (p *untouchedProvider) MissingObjects(context.Context, []environmentartifact.Descriptor) ([]environmentartifact.Digest, error) {
	return nil, p.touched()
}
func (p *untouchedProvider) PutObject(context.Context, artifactprovider.Blob) error {
	return p.touched()
}
func (p *untouchedProvider) GetObject(context.Context, environmentartifact.Descriptor) (io.ReadCloser, error) {
	return nil, p.touched()
}
func (p *untouchedProvider) CommitManifest(context.Context, []byte) error { return p.touched() }

func runSchedule(root, python string) error {
	if e := mode700(root); e != nil {
		return fmt.Errorf("pre-root guard: %w", e)
	}
	preparedBytes, e := os.ReadFile(filepath.Join(root, "prepared.json"))
	if e != nil {
		return e
	}
	var prepared struct {
		Artifact, ProviderRoot, PackRoot string
		PackSHA256, PackIndexSHA256      string
	}
	if e = json.Unmarshal(preparedBytes, &prepared); e != nil {
		return e
	}
	workersRoot := filepath.Join(root, "workers")
	if e = os.MkdirAll(workersRoot, 0o700); e != nil {
		return e
	}
	if e = mode700(workersRoot); e != nil {
		return fmt.Errorf("workers root guard: %w", e)
	}
	producerHome := filepath.Join(root, "producer-home")
	if e = mode700(producerHome); e != nil {
		return fmt.Errorf("pre-producer-home guard: %w", e)
	}
	bin, e := os.Executable()
	if e != nil {
		return e
	}
	resultPath := filepath.Join(root, "measurements.jsonl")
	if _, e = os.Lstat(resultPath); !os.IsNotExist(e) {
		return errors.New("measurements JSONL must be a new path")
	}
	f, e := os.OpenFile(resultPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if e != nil {
		return e
	}
	_ = f.Close()
	schedule := []struct {
		pair int
		arm  string
	}{{1, "filesystem"}, {1, "packed"}, {2, "packed"}, {2, "filesystem"}, {3, "filesystem"}, {3, "packed"}}
	before, e := hashTrees(prepared.ProviderRoot, prepared.PackRoot, producerHome, filepath.Join(root, "fixture.json"))
	if e != nil {
		return e
	}
	if e = runNegativeControls(root, prepared.ProviderRoot, prepared.PackRoot, prepared.Artifact, prepared.PackIndexSHA256, prepared.PackSHA256); e != nil {
		return e
	}
	for _, s := range schedule {
		workerRoot := filepath.Join(root, "workers", fmt.Sprintf("pair-%d-%s", s.pair, s.arm))
		if e = os.MkdirAll(workerRoot, 0o700); e != nil {
			return e
		}
		if e = mode700(workerRoot); e != nil {
			return fmt.Errorf("worker root guard: %w", e)
		}
		home := filepath.Join(workerRoot, "home")
		if e = os.Mkdir(home, 0o700); e != nil {
			return e
		}
		if e = mode700(home); e != nil {
			return e
		}
		receipt := filepath.Join(workerRoot, "worker.jsonl")
		profile := filepath.Join(workerRoot, "cpu.pprof")
		logPath := filepath.Join(workerRoot, "worker.log")
		cmd := exec.Command(bin, "-mode=worker", "-root", root, "-python", python, "-arm", s.arm, "-pair", strconv.Itoa(s.pair), "-artifact-root", prepared.ProviderRoot, "-pack-root", prepared.PackRoot, "-artifact", prepared.Artifact, "-index-sha", prepared.PackIndexSHA256, "-pack-sha", prepared.PackSHA256, "-receipt", receipt, "-profile", profile)
		env := make([]string, 0, len(os.Environ())+3)
		for _, v := range os.Environ() {
			if !strings.HasPrefix(v, "ROBOCORP_HOME=") && !strings.HasPrefix(v, "RCC_HOLOTREE_MODE=") {
				env = append(env, v)
			}
		}
		env = append(env, "ROBOCORP_HOME="+home, "RCC_HOLOTREE_MODE=private")
		cmd.Env = env
		started := time.Now()
		output, runErr := cmd.CombinedOutput()
		logErr := os.WriteFile(logPath, output, 0o600)
		homeGuardErr := mode700(home)
		outerGuardErr := mode700(root)
		producerGuardErr := mode700(producerHome)
		workersGuardErr := mode700(workersRoot)
		workerRootGuardErr := mode700(workerRoot)
		rows, receiptReadErr := os.ReadFile(receipt)
		if len(rows) > 0 {
			for _, line := range bytes.Split(bytes.TrimSpace(rows), []byte("\n")) {
				if len(line) > 0 {
					if e = appendRawJSONL(resultPath, line); e != nil {
						return e
					}
				}
			}
		}
		failure := map[string]any{"type": "scheduler-arm", "pair": s.pair, "arm": s.arm, "schedulerElapsedNs": time.Since(started).Nanoseconds(), "workerError": errorText(runErr), "homeGuard": errorText(homeGuardErr), "outerGuard": errorText(outerGuardErr), "producerGuard": errorText(producerGuardErr), "workersGuard": errorText(workersGuardErr), "workerRootGuard": errorText(workerRootGuardErr), "logWriteError": errorText(logErr), "receiptReadError": errorText(receiptReadErr)}
		if e = appendJSONL(resultPath, failure); e != nil {
			return e
		}
		if logErr != nil || receiptReadErr != nil || homeGuardErr != nil || outerGuardErr != nil || producerGuardErr != nil || workersGuardErr != nil || workerRootGuardErr != nil {
			return fmt.Errorf("first-failure stop after pair %d %s: log=%v receipt=%v home=%v outer=%v producer=%v workers=%v workerRoot=%v", s.pair, s.arm, logErr, receiptReadErr, homeGuardErr, outerGuardErr, producerGuardErr, workersGuardErr, workerRootGuardErr)
		}
		if runErr != nil {
			if e = appendJSONL(resultPath, map[string]any{"type": "worker-exit", "pair": s.pair, "arm": s.arm, "exit": "failed", "elapsedNs": time.Since(started).Nanoseconds(), "error": runErr.Error(), "log": logPath}); e != nil {
				return e
			}
			return fmt.Errorf("first-failure stop at pair %d %s: %w", s.pair, s.arm, runErr)
		}
		if homeGuardErr != nil || outerGuardErr != nil || producerGuardErr != nil {
			return fmt.Errorf("post-worker strict directory guard failed: home=%v outer=%v producer=%v", homeGuardErr, outerGuardErr, producerGuardErr)
		}
		if len(rows) == 0 {
			return fmt.Errorf("worker returned without incremental receipt: pair %d %s", s.pair, s.arm)
		}
		var r armRecord
		if e = json.Unmarshal(bytes.TrimSpace(rows), &r); e != nil {
			return e
		}
		if r.Status != "PASS" {
			return fmt.Errorf("first-failure stop at pair %d %s: status=%s error=%s", s.pair, s.arm, r.Status, r.Error)
		}
		after, e := hashTrees(prepared.ProviderRoot, prepared.PackRoot, producerHome, filepath.Join(root, "fixture.json"))
		if e != nil {
			return e
		}
		if before != after {
			return fmt.Errorf("source/provider/pack bytes changed after pair %d %s", s.pair, s.arm)
		}
	}
	if e = mode700(root); e != nil {
		return fmt.Errorf("final outer guard: %w", e)
	}
	if e = mode700(producerHome); e != nil {
		return fmt.Errorf("final producer-home guard: %w", e)
	}
	return appendJSONL(resultPath, map[string]any{"type": "complete", "status": "PASS", "schedule": schedule, "sourceTreeSHA256": before, "interpretation": "paired correctness/performance diagnostics only; no winner/default claim"})
}
func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
func appendRawJSONL(path string, b []byte) error {
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if e != nil {
		return e
	}
	defer f.Close()
	if _, e = f.Write(append(append([]byte(nil), b...), '\n')); e != nil {
		return e
	}
	return f.Sync()
}
func hashTrees(paths ...string) (string, error) {
	h := sha256.New()
	for _, root := range paths {
		e := filepath.WalkDir(root, func(path string, d os.DirEntry, e error) error {
			if e != nil {
				return e
			}
			info, e := os.Lstat(path)
			if e != nil {
				return e
			}
			rel, e := filepath.Rel(root, path)
			if e != nil {
				return e
			}
			fmt.Fprintf(h, "%s\x00%o\x00", filepath.ToSlash(filepath.Join(filepath.Base(root), rel)), info.Mode().Perm())
			if info.Mode().IsRegular() {
				b, e := os.ReadFile(path)
				if e != nil {
					return e
				}
				h.Write(b)
			} else if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("unexpected symlink in immutable source tree: %s", path)
			}
			return nil
		})
		if e != nil {
			return "", e
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func runNegativeControls(root, providerRoot, packRoot, artifact, indexSHA, packSHA string) error {
	base, e := artifactprovider.NewFilesystem(providerRoot)
	if e != nil {
		return e
	}
	dest := filepath.Join(root, "negative")
	if e = os.Mkdir(dest, 0o700); e != nil && !os.IsExist(e) {
		return e
	}
	receipt := filepath.Join(root, "negative-controls.jsonl")
	control := func(name string, fn func() error) error {
		start := time.Now()
		err := fn()
		status := "PASS"
		msg := "expected rejection observed"
		if err != nil {
			status = "FAILED"
			msg = err.Error()
		}
		if w := appendJSONL(receipt, map[string]any{"type": "negative-control", "name": name, "status": status, "elapsedNs": time.Since(start).Nanoseconds(), "result": msg}); w != nil {
			return w
		}
		if err != nil {
			return fmt.Errorf("negative control %s: %w", name, err)
		}
		return nil
	}
	errExpectedReject := errors.New("expected provider rejection")
	rejected := func(name string, fn func() error) error {
		return control(name, func() error {
			err := fn()
			if !errors.Is(err, errExpectedReject) {
				if err == nil {
					return errors.New("malformed candidate was accepted")
				}
				return fmt.Errorf("control setup or unexpected error: %w", err)
			}
			return nil
		})
	}
	markRejected := func(err error) error {
		if err == nil {
			return nil
		}
		return fmt.Errorf("%w: %v", errExpectedReject, err)
	}
	clone := func(name string) (string, error) {
		d := filepath.Join(dest, name)
		if e := os.Mkdir(d, 0o700); e != nil {
			return "", e
		}
		for _, f := range []string{"index.json", "objects.pack"} {
			b, e := os.ReadFile(filepath.Join(packRoot, f))
			if e != nil {
				return "", e
			}
			if e = os.WriteFile(filepath.Join(d, f), b, 0o600); e != nil {
				return "", e
			}
		}
		return d, nil
	}
	if e = control("independent-buffers", func() error {
		p, e := newPackedProvider(base, packRoot, artifact, indexSHA, packSHA)
		if e != nil {
			return e
		}
		defer p.Close()
		var d environmentartifact.Descriptor
		for h, x := range p.entries {
			raw, _ := hex.DecodeString(h)
			dig, _ := environmentartifact.ParseDigest("sha256:" + hex.EncodeToString(raw))
			d = environmentartifact.Descriptor{MediaType: objectMedia, Digest: dig, Size: x.Size}
			break
		}
		a, e := p.readFrame(context.Background(), d)
		if e != nil {
			return e
		}
		b, e := p.readFrame(context.Background(), d)
		if e != nil {
			return e
		}
		if len(a) == 0 || len(b) == 0 || &a[0] == &b[0] || !bytes.Equal(a, b) {
			return errors.New("reads share backing storage or differ")
		}
		a[0] ^= 0xff
		if bytes.Equal(a, b) {
			return errors.New("mutating one result changed second buffer")
		}
		return nil
	}); e != nil {
		return e
	}
	if e = rejected("corrupt-index", func() error {
		d, e := clone("corrupt-index")
		if e != nil {
			return e
		}
		p := filepath.Join(d, "index.json")
		b, e := os.ReadFile(p)
		if e != nil {
			return e
		}
		b[0] ^= 1
		if e = os.WriteFile(p, b, 0o600); e != nil {
			return e
		}
		_, e = newPackedProvider(base, d, artifact, indexSHA, packSHA)
		return markRejected(e)
	}); e != nil {
		return e
	}
	if e = rejected("corrupt-pack-whole-hash", func() error {
		d, e := clone("corrupt-pack")
		if e != nil {
			return e
		}
		p := filepath.Join(d, "objects.pack")
		f, e := os.OpenFile(p, os.O_RDWR, 0)
		if e != nil {
			return e
		}
		_, e = f.WriteAt([]byte{0xff}, 0)
		_ = f.Close()
		if e != nil {
			return e
		}
		_, e = newPackedProvider(base, d, artifact, indexSHA, packSHA)
		return markRejected(e)
	}); e != nil {
		return e
	}
	if e = control("per-frame-hash-after-open", func() error {
		d, e := clone("frame-tamper")
		if e != nil {
			return e
		}
		p, e := newPackedProvider(base, d, artifact, indexSHA, packSHA)
		if e != nil {
			return e
		}
		defer p.Close()
		var digest string
		var entry packEntry
		for digest, entry = range p.entries {
			break
		}
		parsed, e := environmentartifact.ParseDigest("sha256:" + digest)
		if e != nil {
			return e
		}
		f, e := os.OpenFile(filepath.Join(d, "objects.pack"), os.O_WRONLY, 0)
		if e != nil {
			return e
		}
		_, e = f.WriteAt([]byte{0xff}, entry.Offset)
		closeErr := f.Close()
		if e != nil {
			return e
		}
		if closeErr != nil {
			return closeErr
		}
		if _, e = p.GetObject(context.Background(), environmentartifact.Descriptor{MediaType: objectMedia, Digest: parsed, Size: entry.Size}); e == nil {
			return errors.New("mutated frame passed per-frame hash")
		}
		return nil
	}); e != nil {
		return e
	}
	if e = rejected("truncated-pack", func() error {
		d, e := clone("truncated")
		if e != nil {
			return e
		}
		p := filepath.Join(d, "objects.pack")
		if e = os.Truncate(p, 1); e != nil {
			return e
		}
		_, e = newPackedProvider(base, d, artifact, indexSHA, packSHA)
		return markRejected(e)
	}); e != nil {
		return e
	}
	if e = rejected("missing-pack", func() error {
		d, e := clone("missing")
		if e != nil {
			return e
		}
		if e = os.Remove(filepath.Join(d, "objects.pack")); e != nil {
			return e
		}
		_, e = newPackedProvider(base, d, artifact, indexSHA, packSHA)
		return markRejected(e)
	}); e != nil {
		return e
	}
	if e = rejected("symlink-root", func() error {
		d := filepath.Join(dest, "symlink-root")
		if e := os.Symlink(packRoot, d); e != nil {
			return e
		}
		_, e := newPackedProvider(base, d, artifact, indexSHA, packSHA)
		return markRejected(e)
	}); e != nil {
		return e
	}
	if e = rejected("symlink-pack-file", func() error {
		d, e := clone("symlink-leaf")
		if e != nil {
			return e
		}
		if e = os.Remove(filepath.Join(d, "objects.pack")); e != nil {
			return e
		}
		if e = os.Symlink(filepath.Join(packRoot, "objects.pack"), filepath.Join(d, "objects.pack")); e != nil {
			return e
		}
		_, e = newPackedProvider(base, d, artifact, indexSHA, packSHA)
		return markRejected(e)
	}); e != nil {
		return e
	}
	if e = control("wrong-size-and-digest", func() error {
		p, e := newPackedProvider(base, packRoot, artifact, indexSHA, packSHA)
		if e != nil {
			return e
		}
		defer p.Close()
		for h, x := range p.entries {
			d, _ := environmentartifact.ParseDigest("sha256:" + h)
			if _, e = p.GetObject(context.Background(), environmentartifact.Descriptor{MediaType: objectMedia, Digest: d, Size: x.Size + 1}); e == nil {
				return errors.New("wrong size accepted")
			}
			wrong, _ := environmentartifact.ParseDigest("sha256:" + strings.Repeat("f", 64))
			if _, e = p.GetObject(context.Background(), environmentartifact.Descriptor{MediaType: objectMedia, Digest: wrong, Size: x.Size}); e == nil {
				return errors.New("wrong digest accepted")
			}
			if _, e = p.GetObject(context.Background(), environmentartifact.Descriptor{MediaType: "application/octet-stream", Digest: d, Size: x.Size}); e == nil {
				return errors.New("wrong media type accepted")
			}
			return nil
		}
		return errors.New("pack contains no entries")
	}); e != nil {
		return e
	}
	if e = rejected("out-of-range-index", func() error {
		d, e := clone("range")
		if e != nil {
			return e
		}
		b, e := os.ReadFile(filepath.Join(d, "index.json"))
		if e != nil {
			return e
		}
		var idx packIndex
		if e = json.Unmarshal(b, &idx); e != nil {
			return e
		}
		idx.Entries[0].Offset = int64(^uint64(0) >> 1)
		bad, e := json.Marshal(idx)
		if e != nil {
			return e
		}
		if e = os.WriteFile(filepath.Join(d, "index.json"), bad, 0o600); e != nil {
			return e
		}
		_, e = newPackedProvider(base, d, artifact, sumBytes(bad), packSHA)
		return markRejected(e)
	}); e != nil {
		return e
	}
	if e = rejected("duplicate-index-entry", func() error {
		d, e := clone("duplicate-entry")
		if e != nil {
			return e
		}
		b, e := os.ReadFile(filepath.Join(d, "index.json"))
		if e != nil {
			return e
		}
		var idx packIndex
		if e = json.Unmarshal(b, &idx); e != nil {
			return e
		}
		if len(idx.Entries) < 2 {
			return errors.New("fixture lacks two index entries")
		}
		idx.Entries[1].Digest = idx.Entries[0].Digest
		bad, e := json.Marshal(idx)
		if e != nil {
			return e
		}
		if e = os.WriteFile(filepath.Join(d, "index.json"), bad, 0o600); e != nil {
			return e
		}
		_, e = newPackedProvider(base, d, artifact, sumBytes(bad), packSHA)
		return markRejected(e)
	}); e != nil {
		return e
	}
	if e = rejected("overlapping-index-entry", func() error {
		d, e := clone("overlap-entry")
		if e != nil {
			return e
		}
		b, e := os.ReadFile(filepath.Join(d, "index.json"))
		if e != nil {
			return e
		}
		var idx packIndex
		if e = json.Unmarshal(b, &idx); e != nil {
			return e
		}
		if len(idx.Entries) < 2 {
			return errors.New("fixture lacks two index entries")
		}
		idx.Entries[1].Offset = idx.Entries[0].Offset
		bad, e := json.Marshal(idx)
		if e != nil {
			return e
		}
		if e = os.WriteFile(filepath.Join(d, "index.json"), bad, 0o600); e != nil {
			return e
		}
		_, e = newPackedProvider(base, d, artifact, sumBytes(bad), packSHA)
		return markRejected(e)
	}); e != nil {
		return e
	}
	return appendJSONL(receipt, map[string]any{"type": "negative-controls-complete", "status": "PASS", "count": 12})
}
