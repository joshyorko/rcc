// Standalone Go 1.26.9 standard-library gzip component harness.
// This is not an RCC package or filesystem LiftFile benchmark.
package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
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
	maximumEvidence = int64(1 << 20)
	maximumRSS      = uint64(512 << 20)
	minimumFree     = uint64(2688548864) // 2.5 GiB plus 4 MiB
	maximumRuntime  = 120 * time.Second
	maximumPhase    = 30 * time.Second
)

type Entry struct {
	LogicalID             string `json:"legacyObjectId"`
	StoredDigest          string `json:"storedDigest"`
	StoredSize            int64  `json:"storedSize"`
	LogicalSize           int64  `json:"logicalSize"`
	Encoding              string `json:"encoding"`
	LegacyDigestAlgorithm string `json:"legacyLogicalDigestAlgorithm"`
}
type Source struct {
	ProviderRoot               string  `json:"provider_root"`
	IndexPath                  string  `json:"index_path"`
	IndexSHA                   string  `json:"index_sha256"`
	IndexBytes                 int64   `json:"index_bytes"`
	ManifestPath               string  `json:"manifest_path"`
	ManifestSHA                string  `json:"manifest_sha256"`
	ManifestBytes              int64   `json:"manifest_bytes"`
	SelectedCount              int     `json:"selected_count"`
	SelectedLogicalBytes       int64   `json:"selected_logical_bytes"`
	SelectedStoredBytes        int64   `json:"selected_stored_bytes"`
	CorpusCount                int     `json:"corpus_count"`
	CorpusLogicalBytes         int64   `json:"corpus_logical_bytes"`
	CorpusStoredBytes          int64   `json:"corpus_stored_bytes"`
	IndexMediaType             string  `json:"index_media_type"`
	IndexEncoding              string  `json:"index_encoding"`
	IndexLegacyDigestAlgorithm string  `json:"index_legacy_digest_algorithm"`
	Objects                    []Entry `json:"objects"`
}
type Plan struct {
	Status string `json:"status"`
	Source Source `json:"source"`
}
type Index struct {
	MediaType             string  `json:"mediaType"`
	SchemaVersion         int     `json:"schemaVersion"`
	Count                 int     `json:"count"`
	TotalStoredBytes      int64   `json:"totalStoredBytes"`
	TotalLogicalBytes     int64   `json:"totalLogicalBytes"`
	Encoding              string  `json:"encoding"`
	LegacyDigestAlgorithm string  `json:"legacyLogicalDigestAlgorithm"`
	Entries               []Entry `json:"entries"`
}
type State struct {
	Size  int64  `json:"size"`
	SHA   string `json:"sha256"`
	Mode  uint32 `json:"mode"`
	Mtime int64  `json:"mtime_unix_nano"`
}
type Sample struct {
	Phase        string `json:"phase"`
	Repetition   int    `json:"repetition"`
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
type Marker struct {
	State      string `json:"state"`
	Phase      string `json:"phase"`
	Repetition int    `json:"repetition"`
	UnixNano   int64  `json:"unix_nano"`
}
type Result struct {
	Status              string           `json:"status"`
	Failure             string           `json:"failure,omitempty"`
	PlanSHA             string           `json:"plan_sha256"`
	RunnerSHA           string           `json:"runner_sha256"`
	GoVersion           string           `json:"go_version"`
	Selected            int              `json:"selected_objects"`
	LogicalBytes        int64            `json:"selected_logical_bytes"`
	StoredBytes         int64            `json:"selected_stored_bytes"`
	CallsByPhase        map[string]int   `json:"calls_by_phase"`
	UntimedCalls        map[string]int   `json:"untimed_calls"`
	IndexBefore         State            `json:"index_before"`
	IndexAfter          State            `json:"index_after"`
	ManifestBefore      State            `json:"manifest_before"`
	ManifestAfter       State            `json:"manifest_after"`
	SourceBefore        map[string]State `json:"source_before"`
	SourceAfter         map[string]State `json:"source_after"`
	CanonicalFrames     map[string]State `json:"canonical_frames"`
	Samples             []Sample         `json:"samples"`
	ElapsedSeconds      float64          `json:"elapsed_seconds"`
	SampledRSSPeakBytes uint64           `json:"sampled_process_rss_peak_bytes"`
	FreeStartBytes      uint64           `json:"filesystem_free_start_bytes"`
	FreeEndBytes        uint64           `json:"filesystem_free_end_bytes"`
	EvidenceBytes       int64            `json:"owned_evidence_bytes_before_result"`
}

var phases = []string{"legacy-gzip-decode", "gzip-bestspeed-encode", "gzip-bestspeed-decode"}
var peakRSS atomic.Uint64

func sha(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
func sumCPU() (int64, error) {
	var u syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &u); err != nil {
		return 0, err
	}
	return int64(u.Utime.Sec)*1e9 + int64(u.Utime.Usec)*1e3 + int64(u.Stime.Sec)*1e9 + int64(u.Stime.Usec)*1e3, nil
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
			if _, err := fmt.Sscanf(line, "VmRSS: %d kB", &kb); err != nil {
				return 0, err
			}
			return kb * 1024, nil
		}
	}
	return 0, errors.New("VmRSS missing")
}
func startRSSWatch(stop <-chan struct{}, failures chan<- error) {
	go func() {
		t := time.NewTicker(100 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				n, err := rssBytes()
				if err != nil {
					select {
					case failures <- err:
					default:
					}
					return
				}
				for old := peakRSS.Load(); n > old; old = peakRSS.Load() {
					if peakRSS.CompareAndSwap(old, n) {
						break
					}
				}
				if n > maximumRSS {
					select {
					case failures <- fmt.Errorf("runtime RSS guard exceeded: %d", n):
					default:
					}
					return
				}
			}
		}
	}()
}
func checkFailure(ch <-chan error) error {
	select {
	case err := <-ch:
		return err
	default:
		return nil
	}
}
func fileState(path string) (State, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return State{}, err
	}
	st, err := os.Lstat(path)
	if err != nil {
		return State{}, err
	}
	if !st.Mode().IsRegular() {
		return State{}, fmt.Errorf("source is not a regular file: %s", path)
	}
	return State{Size: int64(len(b)), SHA: sha(b), Mode: uint32(st.Mode().Perm()), Mtime: st.ModTime().UnixNano()}, nil
}
func sameState(a, b State) bool {
	return a.Size == b.Size && a.SHA == b.SHA && a.Mode == b.Mode && a.Mtime == b.Mtime
}
func allocatedEvidence(root string) (int64, error) {
	var total int64
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if filepath.Base(path) == "rcc-gzip-go" {
			return nil
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return errors.New("allocated-byte stat unavailable")
		}
		total += st.Blocks * 512
		return nil
	})
	return total, err
}
func marker(path, state, phase string, rep int) error {
	m := Marker{State: state, Phase: phase, Repetition: rep, UnixNano: time.Now().UnixNano()}
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp := path + ".tmp"
	if err = os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	if err = os.Rename(tmp, path); err != nil {
		return err
	}
	eventBytes, err := json.Marshal(m)
	if err != nil {
		return err
	}
	eventBytes = append(eventBytes, '\n')
	events, err := os.OpenFile(filepath.Join(filepath.Dir(path), "phase-events.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err = events.Write(eventBytes); err != nil {
		_ = events.Close()
		return err
	}
	return events.Close()
}
func objectPath(root, digest string) (string, error) {
	d := strings.TrimPrefix(digest, "sha256:")
	if len(d) != 64 {
		return "", fmt.Errorf("invalid digest %q", digest)
	}
	if _, err := hex.DecodeString(d); err != nil {
		return "", err
	}
	return filepath.Join(root, "objects", "sha256", d[:2], d[2:4], d), nil
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
func validateLogical(data []byte, e Entry) error {
	if int64(len(data)) != e.LogicalSize || sha(data) != e.LogicalID {
		return fmt.Errorf("logical size/hash mismatch for %s", e.LogicalID)
	}
	return nil
}
func loadObject(p Plan, e Entry) ([]byte, []byte, error) {
	path, err := objectPath(p.Source.ProviderRoot, e.StoredDigest)
	if err != nil {
		return nil, nil, err
	}
	stored, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	if int64(len(stored)) != e.StoredSize || sha(stored) != strings.TrimPrefix(e.StoredDigest, "sha256:") {
		return nil, nil, fmt.Errorf("stored size/hash mismatch for %s", e.StoredDigest)
	}
	logical, err := decode(stored)
	if err != nil {
		return nil, nil, err
	}
	if err = validateLogical(logical, e); err != nil {
		return nil, nil, err
	}
	return stored, logical, nil
}
func equalEntry(a, b Entry) bool {
	return a.LogicalID == b.LogicalID && a.StoredDigest == b.StoredDigest && a.StoredSize == b.StoredSize && a.LogicalSize == b.LogicalSize && a.Encoding == b.Encoding && a.LegacyDigestAlgorithm == b.LegacyDigestAlgorithm
}
func readAndValidateIndex(p Plan) (State, State, error) {
	ib, err := os.ReadFile(p.Source.IndexPath)
	if err != nil {
		return State{}, State{}, err
	}
	if int64(len(ib)) != p.Source.IndexBytes || sha(ib) != p.Source.IndexSHA {
		return State{}, State{}, errors.New("index byte length/SHA mismatch")
	}
	var idx Index
	if err = json.Unmarshal(ib, &idx); err != nil {
		return State{}, State{}, err
	}
	var corpusLogical, corpusStored int64
	for _, e := range idx.Entries {
		corpusLogical += e.LogicalSize
		corpusStored += e.StoredSize
	}
	if idx.Count != len(idx.Entries) || idx.Count != p.Source.CorpusCount || corpusLogical != p.Source.CorpusLogicalBytes || corpusStored != p.Source.CorpusStoredBytes || idx.TotalLogicalBytes != p.Source.CorpusLogicalBytes || idx.TotalStoredBytes != p.Source.CorpusStoredBytes || idx.MediaType != p.Source.IndexMediaType || idx.Encoding != p.Source.IndexEncoding || idx.LegacyDigestAlgorithm != p.Source.IndexLegacyDigestAlgorithm || idx.SchemaVersion != 1 {
		return State{}, State{}, errors.New("index schema/count mismatch")
	}
	selected := make([]Entry, 0, 20)
	for _, e := range idx.Entries {
		if e.LogicalSize > 1048576 {
			selected = append(selected, e)
		}
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].StoredDigest < selected[j].StoredDigest })
	if len(selected) != p.Source.SelectedCount || len(selected) != len(p.Source.Objects) {
		return State{}, State{}, errors.New("selected object count mismatch")
	}
	var logical, stored int64
	for i, e := range selected {
		if e.Encoding != "gzip" || e.LegacyDigestAlgorithm != "sha256" {
			return State{}, State{}, fmt.Errorf("unexpected object encoding/digest algorithm: %s", e.StoredDigest)
		}
		if !equalEntry(e, p.Source.Objects[i]) {
			return State{}, State{}, fmt.Errorf("plan/index descriptor mismatch at %d", i)
		}
		logical += e.LogicalSize
		stored += e.StoredSize
	}
	if logical != p.Source.SelectedLogicalBytes || stored != p.Source.SelectedStoredBytes {
		return State{}, State{}, errors.New("selected byte totals mismatch")
	}
	st, err := os.Lstat(p.Source.IndexPath)
	if err != nil {
		return State{}, State{}, err
	}
	if !st.Mode().IsRegular() {
		return State{}, State{}, errors.New("index is not a regular file")
	}
	indexState := State{Size: int64(len(ib)), SHA: sha(ib), Mode: uint32(st.Mode().Perm()), Mtime: st.ModTime().UnixNano()}
	mb, err := os.ReadFile(p.Source.ManifestPath)
	if err != nil {
		return State{}, State{}, err
	}
	if int64(len(mb)) != p.Source.ManifestBytes || sha(mb) != p.Source.ManifestSHA {
		return State{}, State{}, errors.New("manifest byte length/SHA mismatch")
	}
	mst, err := os.Lstat(p.Source.ManifestPath)
	if err != nil {
		return State{}, State{}, err
	}
	if !mst.Mode().IsRegular() {
		return State{}, State{}, errors.New("manifest is not a regular file")
	}
	manifestState := State{Size: int64(len(mb)), SHA: sha(mb), Mode: uint32(mst.Mode().Perm()), Mtime: mst.ModTime().UnixNano()}
	return indexState, manifestState, nil
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
func run(planPath, markerPath, outputPath string) (Result, error) {
	begin := time.Now()
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
	if p.Status != "FROZEN_REVISION_02_ROOT_ADMISSION_REQUIRED" {
		return Result{}, errors.New("wrong plan revision/status")
	}
	if p.Source.SelectedCount != 20 || len(p.Source.Objects) != 20 || p.Source.SelectedLogicalBytes != 102457064 || p.Source.SelectedStoredBytes != 45517701 {
		return Result{}, errors.New("frozen selection totals mismatch")
	}
	res := Result{Status: "PASS", PlanSHA: sha(planBytes), GoVersion: runtime.Version(), Selected: 20, LogicalBytes: p.Source.SelectedLogicalBytes, StoredBytes: p.Source.SelectedStoredBytes, CallsByPhase: map[string]int{}, UntimedCalls: map[string]int{}, SourceBefore: map[string]State{}, SourceAfter: map[string]State{}, CanonicalFrames: map[string]State{}}
	runnerBytes, err := os.ReadFile("main.go")
	if err != nil {
		return res, err
	}
	res.RunnerSHA = sha(runnerBytes)
	free, err := diskFree(root)
	if err != nil {
		return res, err
	}
	res.FreeStartBytes = free
	if free < minimumFree {
		return res, fmt.Errorf("free space below floor+margin: %d", free)
	}
	base, err := allocatedEvidence(root)
	if err != nil {
		return res, err
	}
	res.EvidenceBytes = base
	if base > maximumEvidence {
		return res, fmt.Errorf("evidence cap already exceeded: %d", base)
	}
	stop := make(chan struct{})
	failure := make(chan error, 1)
	startRSSWatch(stop, failure)
	defer close(stop)
	idx, man, err := readAndValidateIndex(p)
	if err != nil {
		return res, err
	}
	res.IndexBefore = idx
	res.ManifestBefore = man
	for _, e := range p.Source.Objects {
		path, _ := objectPath(p.Source.ProviderRoot, e.StoredDigest)
		state, err := fileState(path)
		if err != nil {
			return res, err
		}
		res.SourceBefore[e.StoredDigest] = state
		_, logical, err := loadObject(p, e)
		if err != nil {
			return res, err
		}
		frame, err := encode(logical)
		if err != nil {
			return res, err
		}
		res.UntimedCalls["canonical_bestspeed_encode"]++
		decoded, err := decode(frame)
		if err != nil {
			return res, err
		}
		res.UntimedCalls["canonical_bestspeed_roundtrip_decode"]++
		if err = validateLogical(decoded, e); err != nil {
			return res, err
		}
		res.CanonicalFrames[e.StoredDigest] = State{Size: int64(len(frame)), SHA: sha(frame)}
	}
	entries := append([]Entry(nil), p.Source.Objects...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].StoredDigest < entries[j].StoredDigest })
	orders := [][]string{{phases[0], phases[1], phases[2]}, {phases[1], phases[2], phases[0]}, {phases[2], phases[0], phases[1]}, {phases[0], phases[2], phases[1]}, {phases[2], phases[1], phases[0]}}
	for rep, order := range orders {
		for pi, phase := range order {
			if err = marker(markerPath, "phase_start", phase, rep); err != nil {
				return res, err
			}
			phaseStart := time.Now()
			rot := (rep + pi) % len(entries)
			scheduled := append(append([]Entry(nil), entries[rot:]...), entries[:rot]...)
			for _, e := range scheduled {
				stored, logical, err := loadObject(p, e)
				if err != nil {
					return res, err
				}
				s := Sample{Phase: phase, Repetition: rep, StoredDigest: e.StoredDigest, LogicalID: e.LogicalID, InputBytes: len(stored), LogicalBytes: len(logical), Verified: true}
				switch phase {
				case "legacy-gzip-decode":
					var out []byte
					s.WallNS, s.ProcessCPUNS, err = timed(func() error { var x error; out, x = decode(stored); return x })
					if err == nil {
						err = validateLogical(out, e)
					}
					s.OutputBytes = len(out)
				case "gzip-bestspeed-encode":
					s.InputBytes = len(logical)
					var frame []byte
					s.WallNS, s.ProcessCPUNS, err = timed(func() error { var x error; frame, x = encode(logical); return x })
					if err == nil {
						s.FrameSHA = sha(frame)
						s.FrameBytes = len(frame)
						s.OutputBytes = len(frame)
						canonical := res.CanonicalFrames[e.StoredDigest]
						if canonical.SHA != s.FrameSHA || canonical.Size != int64(s.FrameBytes) {
							err = errors.New("BestSpeed frame differs from preflight canonical")
						}
						if err == nil {
							out, x := decode(frame)
							res.UntimedCalls["encode_roundtrip_decode"]++
							if x != nil {
								err = x
							} else {
								err = validateLogical(out, e)
							}
						}
					}
				case "gzip-bestspeed-decode":
					frame, x := encode(logical)
					res.UntimedCalls["frame_preparation_encode"]++
					if x != nil {
						return res, x
					}
					canonical := res.CanonicalFrames[e.StoredDigest]
					s.FrameSHA = sha(frame)
					s.FrameBytes = len(frame)
					s.InputBytes = len(frame)
					if canonical.SHA != s.FrameSHA || canonical.Size != int64(s.FrameBytes) {
						return res, errors.New("prepared decode frame differs from canonical")
					}
					var out []byte
					s.WallNS, s.ProcessCPUNS, err = timed(func() error { var y error; out, y = decode(frame); return y })
					if err == nil {
						err = validateLogical(out, e)
					}
					s.OutputBytes = len(out)
				}
				if err != nil {
					return res, fmt.Errorf("rep=%d phase=%s digest=%s: %w", rep, phase, e.StoredDigest, err)
				}
				res.Samples = append(res.Samples, s)
				res.CallsByPhase[phase]++
				if err = checkFailure(failure); err != nil {
					return res, err
				}
				rss, e := rssBytes()
				if e != nil {
					return res, e
				}
				if rss > peakRSS.Load() {
					peakRSS.Store(rss)
				}
				if rss > maximumRSS {
					return res, fmt.Errorf("RSS guard exceeded: %d", rss)
				}
				free, e := diskFree(root)
				if e != nil {
					return res, e
				}
				if free < minimumFree {
					return res, fmt.Errorf("free-space guard exceeded: %d", free)
				}
				alloc, e := allocatedEvidence(root)
				if e != nil {
					return res, e
				}
				if alloc > maximumEvidence {
					return res, fmt.Errorf("evidence cap exceeded: %d", alloc)
				}
				if time.Since(begin) > maximumRuntime {
					return res, errors.New("internal total runtime exceeded")
				}
			}
			if time.Since(phaseStart) > maximumPhase {
				return res, fmt.Errorf("phase watchdog exceeded: %s rep %d", phase, rep)
			}
			if err = marker(markerPath, "phase_done", phase, rep); err != nil {
				return res, err
			}
		}
	}
	for _, e := range entries {
		path, _ := objectPath(p.Source.ProviderRoot, e.StoredDigest)
		s, err := fileState(path)
		if err != nil {
			return res, err
		}
		res.SourceAfter[e.StoredDigest] = s
		if !sameState(res.SourceBefore[e.StoredDigest], s) {
			return res, fmt.Errorf("source object changed: %s", e.StoredDigest)
		}
	}
	idxAfter, manAfter, err := readAndValidateIndex(p)
	if err != nil {
		return res, err
	}
	res.IndexAfter = idxAfter
	res.ManifestAfter = manAfter
	if !sameState(idx, idxAfter) || !sameState(man, manAfter) {
		return res, errors.New("index or manifest changed")
	}
	if len(res.Samples) != 300 || len(res.CallsByPhase) != 3 {
		return res, errors.New("sample count mismatch")
	}
	for _, phase := range phases {
		if res.CallsByPhase[phase] != 100 {
			return res, fmt.Errorf("wrong count for %s", phase)
		}
	}
	res.FreeEndBytes, err = diskFree(root)
	if err != nil {
		return res, err
	}
	if res.FreeEndBytes < minimumFree {
		return res, errors.New("final free-space guard exceeded")
	}
	res.SampledRSSPeakBytes = peakRSS.Load()
	res.ElapsedSeconds = time.Since(begin).Seconds()
	return res, nil
}
func main() {
	plan := flag.String("plan", "plan.json", "frozen input plan")
	markerPath := flag.String("phase-marker", "phase.json", "external watchdog phase marker")
	output := flag.String("output", "results.json", "exclusive result path")
	flag.Parse()
	res, err := run(*plan, *markerPath, *output)
	if err != nil {
		res.Status = "FAIL"
		res.Failure = err.Error()
	}
	b, e := json.MarshalIndent(res, "", "  ")
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	b = append(b, '\n')
	if _, e = os.Stat(*output); e == nil {
		fmt.Fprintln(os.Stderr, "refusing to overwrite result output")
		os.Exit(1)
	}
	used, e := allocatedEvidence(".")
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	pred := used + int64((len(b)+4095)/4096*4096)
	if pred > maximumEvidence {
		fmt.Fprintf(os.Stderr, "result evidence cap would be exceeded: %d > %d\n", pred, maximumEvidence)
		os.Exit(1)
	}
	f, e := os.OpenFile(*output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
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
