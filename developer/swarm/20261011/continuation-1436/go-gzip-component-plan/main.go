// Standalone Go stdlib gzip component harness. Not an RCC package benchmark.
// Compile/run only after explicit root admission recorded beside this plan.
package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	maxEvidence = int64(1 << 20)
	maxRSS      = uint64(512 << 20)
	minFree     = uint64(2688548864) // 2.5 GiB plus 4 MiB guard margin
	maxElapsed  = 120 * time.Second
)

type Entry struct {
	Encoding     string `json:"encoding"`
	LogicalID    string `json:"legacyObjectId"`
	StoredDigest string `json:"storedDigest"`
	StoredSize   int64  `json:"storedSize"`
	LogicalSize  int64  `json:"logicalSize"`
}
type Source struct {
	ProviderRoot string  `json:"provider_root"`
	Objects      []Entry `json:"objects"`
	IndexPath    string  `json:"index_path"`
	IndexSHA     string  `json:"index_sha256"`
	ManifestPath string  `json:"manifest_path"`
	ManifestSHA  string  `json:"manifest_sha256"`
}
type Plan struct {
	Status string `json:"status"`
	Source Source `json:"source"`
}
type State struct {
	Size  int64  `json:"size"`
	SHA   string `json:"sha256"`
	Mode  uint32 `json:"mode"`
	Mtime int64  `json:"mtime_unix_nano"`
}
type Sample struct {
	Phase        string `json:"phase"`
	Rep          int    `json:"repetition"`
	StoredDigest string `json:"storedDigest"`
	LogicalID    string `json:"legacyObjectId"`
	InputBytes   int    `json:"input_bytes"`
	LogicalBytes int    `json:"logical_bytes"`
	OutputBytes  int    `json:"output_bytes"`
	FrameSHA     string `json:"frame_sha256"`
	FrameBytes   int    `json:"frame_bytes"`
	WallNS       int64  `json:"wall_ns"`
	ProcessCPUNS int64  `json:"process_cpu_ns"`
	Verified     bool   `json:"verified"`
}
type Result struct {
	Status          string           `json:"status"`
	PlanSHA         string           `json:"plan_sha256"`
	RunnerSHA       string           `json:"runner_sha256"`
	GoVersion       string           `json:"go_version"`
	Selected        int              `json:"selected_objects"`
	LogicalBytes    int64            `json:"selected_logical_bytes"`
	StoredBytes     int64            `json:"selected_stored_bytes"`
	CallsByPhase    map[string]int   `json:"calls_by_phase"`
	UntimedCalls    map[string]int   `json:"untimed_calls"`
	SourceBefore    map[string]State `json:"source_before"`
	SourceAfter     map[string]State `json:"source_after"`
	IndexBefore     State            `json:"index_before"`
	IndexAfter      State            `json:"index_after"`
	ManifestBefore  State            `json:"manifest_before"`
	ManifestAfter   State            `json:"manifest_after"`
	CanonicalFrames map[string]State `json:"canonical_frames"`
	Samples         []Sample         `json:"samples"`
	ElapsedSeconds  float64          `json:"elapsed_seconds"`
	RSSPeakBytes    uint64           `json:"sampled_process_rss_peak_bytes"`
	FreeStartBytes  uint64           `json:"filesystem_free_start_bytes"`
	FreeEndBytes    uint64           `json:"filesystem_free_end_bytes"`
	OwnedBytes      int64            `json:"owned_allocated_bytes_before_result"`
	Failure         string           `json:"failure,omitempty"`
}

var sampledRSS atomic.Uint64
var phases = []string{"legacy-gzip-decode", "gzip-bestspeed-encode", "gzip-bestspeed-decode"}

func sumCPU() (int64, error) {
	var u syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &u); err != nil {
		return 0, err
	}
	return int64(u.Utime.Sec)*1e9 + int64(u.Utime.Usec)*1e3 + int64(u.Stime.Sec)*1e9 + int64(u.Stime.Usec)*1e3, nil
}
func hashBytes(b []byte) string            { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func readFile(path string) ([]byte, error) { return os.ReadFile(path) }
func sameCritical(a, b State) bool {
	return a.Size == b.Size && a.SHA == b.SHA && a.Mode == b.Mode && a.Mtime == b.Mtime
}
func fileState(path string) (State, error) {
	b, err := readFile(path)
	if err != nil {
		return State{}, err
	}
	st, err := os.Stat(path)
	if err != nil {
		return State{}, err
	}
	return State{Size: int64(len(b)), SHA: hashBytes(b), Mode: uint32(st.Mode().Perm()), Mtime: st.ModTime().UnixNano()}, nil
}
func objectPath(root, digest string) (string, error) {
	d := strings.TrimPrefix(digest, "sha256:")
	if len(d) != 64 {
		return "", fmt.Errorf("invalid stored digest %q", digest)
	}
	if _, err := hex.DecodeString(d); err != nil {
		return "", err
	}
	return filepath.Join(root, "objects", "sha256", d[:2], d[2:4], d), nil
}
func loadLogical(plan Plan, e Entry) ([]byte, []byte, error) {
	p, err := objectPath(plan.Source.ProviderRoot, e.StoredDigest)
	if err != nil {
		return nil, nil, err
	}
	stored, err := readFile(p)
	if err != nil {
		return nil, nil, err
	}
	if int64(len(stored)) != e.StoredSize || hashBytes(stored) != strings.TrimPrefix(e.StoredDigest, "sha256:") {
		return nil, nil, fmt.Errorf("stored size/hash mismatch: %s", e.StoredDigest)
	}
	zr, err := gzip.NewReader(bytes.NewReader(stored))
	if err != nil {
		return nil, nil, err
	}
	logical, readErr := io.ReadAll(zr)
	closeErr := zr.Close()
	if readErr != nil {
		return nil, nil, readErr
	}
	if closeErr != nil {
		return nil, nil, closeErr
	}
	if int64(len(logical)) != e.LogicalSize || hashBytes(logical) != e.LogicalID {
		return nil, nil, fmt.Errorf("logical size/hash mismatch: %s", e.LogicalID)
	}
	return stored, logical, nil
}
func encode(data []byte) ([]byte, error) {
	var b bytes.Buffer
	w, err := gzip.NewWriterLevel(&b, gzip.BestSpeed)
	if err != nil {
		return nil, err
	}
	if _, err = w.Write(data); err != nil {
		return nil, err
	}
	if err = w.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
func decode(data []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	out, readErr := io.ReadAll(r)
	closeErr := r.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return out, nil
}
func diskFree(path string) (uint64, error) {
	var s syscall.Statfs_t
	if err := syscall.Statfs(path, &s); err != nil {
		return 0, err
	}
	return s.Bavail * uint64(s.Bsize), nil
}
func rssBytes() (uint64, error) {
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "VmRSS:") {
			var kb uint64
			if _, e := fmt.Sscanf(line, "VmRSS: %d kB", &kb); e != nil {
				return 0, e
			}
			return kb * 1024, nil
		}
	}
	return 0, errors.New("VmRSS missing")
}
func startResourceWatch(root string) (chan struct{}, chan error) {
	stop := make(chan struct{})
	failures := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		check := func() bool {
			rss, err := rssBytes()
			if err != nil {
				select {
				case failures <- err:
				default:
				}
				return false
			}
			for prior := sampledRSS.Load(); rss > prior; prior = sampledRSS.Load() {
				if sampledRSS.CompareAndSwap(prior, rss) {
					break
				}
			}
			if rss > maxRSS {
				select {
				case failures <- fmt.Errorf("RSS guard crossed: %d", rss):
				default:
				}
				return false
			}
			free, err := diskFree(root)
			if err != nil {
				select {
				case failures <- err:
				default:
				}
				return false
			}
			if free < minFree {
				select {
				case failures <- fmt.Errorf("free-space guard crossed: %d", free):
				default:
				}
				return false
			}
			return true
		}
		if !check() {
			return
		}
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if !check() {
					return
				}
			}
		}
	}()
	return stop, failures
}
func checkResourceFailure(ch chan error) error {
	select {
	case err := <-ch:
		return err
	default:
		return nil
	}
}

func allocated(root string) (int64, error) {
	var n int64
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return errors.New("stat allocation unavailable")
		}
		n += st.Blocks * 512
		return nil
	})
	return n, err
}
func timed(fn func() error) (wall, cpu int64, err error) {
	c0, e := sumCPU()
	if e != nil {
		return 0, 0, e
	}
	t0 := time.Now()
	err = fn()
	wall = time.Since(t0).Nanoseconds()
	c1, e := sumCPU()
	if e != nil {
		return wall, 0, e
	}
	cpu = c1 - c0
	return
}
func validate(data []byte, e Entry) error {
	if int64(len(data)) != e.LogicalSize || hashBytes(data) != e.LogicalID {
		return fmt.Errorf("timed output mismatch %s", e.LogicalID)
	}
	return nil
}
func loadPlan(path string) (Plan, []byte, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return Plan{}, nil, e
	}
	var p Plan
	e = json.Unmarshal(b, &p)
	return p, b, e
}
func run(planPath string) (Result, error) {
	started := time.Now()
	root, err := filepath.Abs(".")
	if err != nil {
		return Result{}, err
	}
	planBytes, err := os.ReadFile(planPath)
	if err != nil {
		return Result{}, err
	}
	var p Plan
	if err = json.Unmarshal(planBytes, &p); err != nil {
		return Result{}, err
	}
	if p.Status != "FROZEN_PLAN_ONLY_ROOT_EXECUTION_ADMISSION_REQUIRED" {
		return Result{}, errors.New("plan is not frozen expected plan")
	}
	if len(p.Source.Objects) != 20 {
		return Result{}, fmt.Errorf("expected 20 selected objects, got %d", len(p.Source.Objects))
	}
	res := Result{Status: "PASS", PlanSHA: hashBytes(planBytes), Selected: len(p.Source.Objects), CallsByPhase: map[string]int{}, UntimedCalls: map[string]int{}, SourceBefore: map[string]State{}, SourceAfter: map[string]State{}, CanonicalFrames: map[string]State{}}
	runnerBytes, err := os.ReadFile("main.go")
	if err == nil {
		res.RunnerSHA = hashBytes(runnerBytes)
	}
	res.GoVersion = runtime.Version()
	free, err := diskFree(root)
	if err != nil {
		return res, err
	}
	res.FreeStartBytes = free
	if free < minFree {
		return res, fmt.Errorf("free space below guard: %d", free)
	}
	alloc, err := allocated(root)
	if err != nil {
		return res, err
	}
	res.OwnedBytes = alloc
	watchStop, watchFailure := startResourceWatch(root)
	defer close(watchStop)
	idx, err := fileState(p.Source.IndexPath)
	if err != nil {
		return res, err
	}
	if idx.SHA != p.Source.IndexSHA {
		return res, errors.New("index hash mismatch")
	}
	res.IndexBefore = idx
	man, err := fileState(p.Source.ManifestPath)
	if err != nil {
		return res, err
	}
	if man.SHA != p.Source.ManifestSHA {
		return res, errors.New("manifest hash mismatch")
	}
	res.ManifestBefore = man
	for _, e := range p.Source.Objects {
		path, _ := objectPath(p.Source.ProviderRoot, e.StoredDigest)
		st, err := fileState(path)
		if err != nil {
			return res, err
		}
		res.SourceBefore[e.StoredDigest] = st
		stored, logical, err := loadLogical(p, e)
		if err != nil {
			return res, err
		}
		_ = stored
		frame, err := encode(logical)
		if err != nil {
			return res, err
		}
		res.UntimedCalls["canonical_bestspeed_encode"]++
		round, err := decode(frame)
		if err != nil {
			return res, err
		}
		res.UntimedCalls["canonical_bestspeed_roundtrip_decode"]++
		if err = validate(round, e); err != nil {
			return res, err
		}
		res.CanonicalFrames[e.StoredDigest] = State{Size: int64(len(frame)), SHA: hashBytes(frame)}
		res.LogicalBytes += e.LogicalSize
		res.StoredBytes += e.StoredSize
	}
	entries := append([]Entry(nil), p.Source.Objects...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].StoredDigest < entries[j].StoredDigest })
	orders := [][]string{{phases[0], phases[1], phases[2]}, {phases[1], phases[2], phases[0]}, {phases[2], phases[0], phases[1]}, {phases[0], phases[2], phases[1]}, {phases[2], phases[1], phases[0]}}
	for rep, order := range orders {
		for pi, phase := range order {
			rot := (rep + pi) % len(entries)
			runEntries := append(append([]Entry(nil), entries[rot:]...), entries[:rot]...)
			for _, e := range runEntries {
				stored, logical, err := loadLogical(p, e)
				if err != nil {
					return res, err
				}
				sample := Sample{Phase: phase, Rep: rep, StoredDigest: e.StoredDigest, LogicalID: e.LogicalID, LogicalBytes: len(logical), Verified: true}
				switch phase {
				case "legacy-gzip-decode":
					sample.InputBytes = len(stored)
					var output []byte
					sample.WallNS, sample.ProcessCPUNS, err = timed(func() error { var e error; output, e = decode(stored); return e })
					if err == nil {
						err = validate(output, e)
					}
					sample.OutputBytes = len(output)
				case "gzip-bestspeed-encode":
					sample.InputBytes = len(logical)
					var frame []byte
					sample.WallNS, sample.ProcessCPUNS, err = timed(func() error { var e error; frame, e = encode(logical); return e })
					if err == nil {
						sample.FrameSHA = hashBytes(frame)
						sample.FrameBytes = len(frame)
						sample.OutputBytes = len(frame)
						canonical := res.CanonicalFrames[e.StoredDigest]
						if canonical.SHA != sample.FrameSHA || canonical.Size != int64(sample.FrameBytes) {
							err = errors.New("BestSpeed encoded frame differs from canonical preflight")
						}
						if err != nil {
							break
						}
						decoded, e2 := decode(frame)
						res.UntimedCalls["encode_roundtrip_decode"]++
						if e2 != nil {
							err = e2
						} else {
							err = validate(decoded, e)
						}
					}
				case "gzip-bestspeed-decode":
					frame, e2 := encode(logical)
					res.UntimedCalls["frame_preparation_encode"]++
					if e2 != nil {
						return res, e2
					}
					sample.InputBytes = len(frame)
					sample.FrameSHA = hashBytes(frame)
					sample.FrameBytes = len(frame)
					canonical := res.CanonicalFrames[e.StoredDigest]
					if canonical.SHA != sample.FrameSHA || canonical.Size != int64(sample.FrameBytes) {
						return res, errors.New("prepared BestSpeed decode frame differs from canonical preflight")
					}
					var output []byte
					sample.WallNS, sample.ProcessCPUNS, err = timed(func() error { var e error; output, e = decode(frame); return e })
					if err == nil {
						err = validate(output, e)
					}
					sample.OutputBytes = len(output)
				}
				if err != nil {
					return res, fmt.Errorf("rep %d phase %s object %s: %w", rep, phase, e.StoredDigest, err)
				}
				res.Samples = append(res.Samples, sample)
				res.CallsByPhase[phase]++
				if err := checkResourceFailure(watchFailure); err != nil {
					return res, err
				}
				free, err = diskFree(root)
				if err != nil {
					return res, err
				}
				if free < minFree {
					return res, fmt.Errorf("free space guard crossed: %d", free)
				}
				rss, err := rssBytes()
				if err != nil {
					return res, err
				}
				if rss > res.RSSPeakBytes {
					res.RSSPeakBytes = rss
				}
				if rss > maxRSS {
					return res, fmt.Errorf("RSS guard crossed: %d", rss)
				}
				if time.Since(started) > maxElapsed {
					return res, errors.New("total runtime watchdog exceeded")
				}
			}
		}
	}
	for _, e := range entries {
		path, _ := objectPath(p.Source.ProviderRoot, e.StoredDigest)
		st, err := fileState(path)
		if err != nil {
			return res, err
		}
		res.SourceAfter[e.StoredDigest] = st
	}
	idxAfter, err := fileState(p.Source.IndexPath)
	if err != nil {
		return res, err
	}
	manAfter, err := fileState(p.Source.ManifestPath)
	if err != nil {
		return res, err
	}
	res.IndexAfter = idxAfter
	res.ManifestAfter = manAfter
	if !sameCritical(idxAfter, idx) || !sameCritical(manAfter, man) {
		return res, errors.New("source index or manifest changed")
	}
	for k, before := range res.SourceBefore {
		after, ok := res.SourceAfter[k]
		if !ok || !sameCritical(before, after) {
			return res, fmt.Errorf("original stored object changed: %s", k)
		}
	}
	if len(res.Samples) != 300 || len(res.CallsByPhase) != 3 {
		return res, errors.New("sample cardinality mismatch")
	}
	for _, phase := range phases {
		if res.CallsByPhase[phase] != 100 {
			return res, fmt.Errorf("call count %s = %d", phase, res.CallsByPhase[phase])
		}
	}
	res.FreeEndBytes, err = diskFree(root)
	if err != nil {
		return res, err
	}
	if res.FreeEndBytes < minFree {
		return res, errors.New("final free-space guard crossed")
	}
	if err := checkResourceFailure(watchFailure); err != nil {
		return res, err
	}
	res.RSSPeakBytes = sampledRSS.Load()
	res.ElapsedSeconds = time.Since(started).Seconds()
	return res, nil
}
func main() {
	if len(os.Args) != 3 || os.Args[1] != "--plan" {
		fmt.Fprintln(os.Stderr, "usage: go-gzip-component --plan plan.json")
		os.Exit(2)
	}
	res, err := run(os.Args[2])
	if err != nil {
		res.Status = "FAIL"
		res.Failure = err.Error()
	}
	b, marshalErr := json.MarshalIndent(res, "", "  ")
	if marshalErr != nil {
		fmt.Fprintln(os.Stderr, marshalErr)
		os.Exit(1)
	}
	b = append(b, '\n')
	out := "results.json"
	if _, e := os.Stat(out); e == nil {
		fmt.Fprintln(os.Stderr, "refusing to overwrite results.json")
		os.Exit(1)
	}
	used, e := allocated(".")
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	pred := used + int64((len(b)+4095)/4096*4096)
	if pred > maxEvidence {
		fmt.Fprintf(os.Stderr, "refusing result write: predicted owned bytes %d > %d\n", pred, maxEvidence)
		os.Exit(1)
	}
	f, e := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	_, e = f.Write(b)
	closeErr := f.Close()
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	if closeErr != nil {
		fmt.Fprintln(os.Stderr, closeErr)
		os.Exit(1)
	}
	fmt.Printf("status=%s samples=%d elapsed=%.3fs\n", res.Status, len(res.Samples), res.ElapsedSeconds)
	if res.Status != "PASS" || err != nil {
		os.Exit(1)
	}
}
