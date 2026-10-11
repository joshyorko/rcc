//go:build linux

// Private RCC #125 persistent-child lifecycle diagnostic. Source preparation
// only: this file has not been built or executed.
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/pprof"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/joshyorko/rcc/artifactprovider"
	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/environmentartifact"
	"github.com/joshyorko/rcc/environmentlifecycle"
	"golang.org/x/sys/unix"
)

const requestCount = 6

type fileRecord struct {
	Path   string `json:"path"`
	Size   int    `json:"size"`
	SHA256 string `json:"sha256"`
}
type fixture struct {
	Artifact       string       `json:"artifact"`
	GzipLevel      int          `json:"gzipLevel"`
	Workload       []fileRecord `json:"workload"`
	ExpectedOutput string       `json:"expectedOutput"`
}
type requestResult struct {
	Sequence        int    `json:"sequence"`
	PID             int    `json:"pid"`
	LeaseID         string `json:"leaseId"`
	Count           int    `json:"count"`
	AggregateSHA256 string `json:"aggregateSha256"`
}
type receipt struct {
	Type             string          `json:"type"`
	Arm              string          `json:"arm"`
	Status           string          `json:"status"`
	Started          string          `json:"started"`
	Artifact         string          `json:"artifact,omitempty"`
	Materialization  string          `json:"materialization,omitempty"`
	SourceTreeSHA256 string          `json:"sourceTreeSha256,omitempty"`
	ProviderSetupNS  int64           `json:"providerSetupNs,omitempty"`
	AcquireNS        int64           `json:"acquireNs,omitempty"`
	BatchNS          int64           `json:"batchNs,omitempty"`
	ExecuteCalls     int             `json:"executeCalls,omitempty"`
	ChildCalls       int             `json:"childCalls,omitempty"`
	Tasks            int             `json:"tasks,omitempty"`
	Requests         []requestResult `json:"requests,omitempty"`
	DistinctPIDs     int             `json:"distinctPids,omitempty"`
	OutputParity     bool            `json:"outputParity,omitempty"`
	LeasesReleased   bool            `json:"leasesReleased,omitempty"`
	SelfCPUUserNS    int64           `json:"selfCpuUserNs,omitempty"`
	SelfCPUSystemNS  int64           `json:"selfCpuSystemNs,omitempty"`
	ChildCPUUserNS   int64           `json:"childCpuUserNs,omitempty"`
	ChildCPUSystemNS int64           `json:"childCpuSystemNs,omitempty"`
	RChar            int64           `json:"rchar,omitempty"`
	ReadBytes        int64           `json:"readBytes,omitempty"`
	ReadSyscalls     int64           `json:"readSyscalls,omitempty"`
	RSSAfterBytes    int64           `json:"rssAfterBytes,omitempty"`
	ResourceScope    string          `json:"resourceScope,omitempty"`
	Error            string          `json:"error,omitempty"`
}
type usage struct{ user, system int64 }
type ioCounts struct{ rchar, readBytes, syscr int64 }

func main() {
	mode := flag.String("mode", "worker", "worker or control")
	outer := flag.String("outer", "", "strict 0700 owned experiment boundary")
	providerRoot := flag.String("provider-root", "", "existing owned gzip-1 filesystem provider")
	artifact := flag.String("artifact", "", "artifact digest")
	fixturePath := flag.String("fixture", "", "existing gzip-1 fixture receipt")
	arm := flag.String("arm", "", "fresh or resident")
	tasks := flag.Int("tasks", requestCount, "number of payload verification requests (fixed at six)")
	outPath := flag.String("receipt", "", "new JSONL receipt path")
	profilePath := flag.String("profile", "", "new CPU profile path (worker mode)")
	flag.Parse()
	var err error
	switch *mode {
	case "worker":
		err = runWorker(*outer, *providerRoot, *artifact, *fixturePath, *arm, *tasks, *outPath, *profilePath)
	case "control":
		err = runFailureControl(*outer, *providerRoot, *artifact, *fixturePath, *outPath)
	default:
		err = fmt.Errorf("unknown mode %q", *mode)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runWorker(outer, providerRoot, artifact, fixturePath, arm string, tasks int, outPath, profilePath string) (retErr error) {
	started := time.Now()
	r := receipt{Type: "persistent-worker-arm", Arm: arm, Status: "RUNNING", Started: started.UTC().Format(time.RFC3339Nano), Tasks: tasks}
	written := false
	appendReceipt := func() error {
		if e := appendJSONL(outPath, r); e != nil {
			return e
		}
		written = true
		return nil
	}
	defer func() {
		if retErr != nil && !written && outPath != "" {
			r.Status = "FAILED"
			r.Error = retErr.Error()
			if e := appendReceipt(); e != nil {
				retErr = fmt.Errorf("%v; failure receipt: %w", retErr, e)
			}
		}
	}()
	if arm != "fresh" && arm != "resident" {
		return errors.New("arm must be fresh or resident")
	}
	if tasks != requestCount {
		return fmt.Errorf("tasks must be exactly %d", requestCount)
	}
	if outPath == "" || profilePath == "" {
		return errors.New("worker requires receipt and profile paths")
	}
	if e := mode700(outer); e != nil {
		return fmt.Errorf("pre-outer guard: %w", e)
	}
	if e := mode700(common.Product.Home()); e != nil {
		return fmt.Errorf("pre-home guard: %w", e)
	}
	cas := filepath.Join(common.Product.Home(), "artifacts", "v1", "content")
	if _, e := os.Lstat(cas); !errors.Is(e, os.ErrNotExist) {
		return fmt.Errorf("fresh consumer CAS required at %s (stat=%v)", cas, e)
	}
	fixtureBytes, e := os.ReadFile(fixturePath)
	if e != nil {
		return e
	}
	var f fixture
	if e = json.Unmarshal(fixtureBytes, &f); e != nil {
		return e
	}
	if f.Artifact != artifact || f.GzipLevel != 1 || len(f.Workload) != 512 || f.ExpectedOutput == "" {
		return fmt.Errorf("fixture identity/level/workload mismatch: artifact=%s level=%d files=%d", f.Artifact, f.GzipLevel, len(f.Workload))
	}
	beforeSource, e := hashTrees(providerRoot, fixturePath)
	if e != nil {
		return e
	}
	r.SourceTreeSHA256 = beforeSource
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	setup := time.Now()
	provider, e := artifactprovider.NewFilesystem(providerRoot)
	if e != nil {
		return e
	}
	r.ProviderSetupNS = time.Since(setup).Nanoseconds()
	stage := time.Now()
	acquired, e := environmentlifecycle.NewAcquirer().Acquire(ctx, environmentlifecycle.AcquireRequest{ArtifactDigest: mustDigest(artifact), Provider: provider})
	r.AcquireNS = time.Since(stage).Nanoseconds()
	if e != nil {
		return fmt.Errorf("cold acquire: %w", e)
	}
	if acquired.ArtifactDigest.String() != artifact || acquired.CacheHit != environmentlifecycle.CacheProvider {
		return errors.New("cold acquire identity/provenance mismatch")
	}
	r.Artifact = acquired.ArtifactDigest.String()
	r.Materialization = acquired.MaterializationID
	mat := environmentlifecycle.Materialization{ArtifactDigest: acquired.ArtifactDigest, ID: acquired.MaterializationID, Path: acquired.Path, CacheHit: acquired.CacheHit, Verification: acquired.Verification, TrustPolicy: acquired.TrustPolicy, TrustRequest: acquired.TrustRequest, TrustCarrier: acquired.TrustCarrier}
	leaseDir := filepath.Join(common.Product.Home(), "artifacts", "v1", "materializations", acquired.ArtifactDigest.Hex(), "leases")
	profile, e := os.OpenFile(profilePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if e != nil {
		return e
	}
	if e = pprof.StartCPUProfile(profile); e != nil {
		_ = profile.Close()
		return e
	}
	profileActive := true
	defer func() {
		if profileActive {
			pprof.StopCPUProfile()
			_ = profile.Close()
		}
	}()
	self0, e := getUsage(unix.RUSAGE_SELF)
	if e != nil {
		return e
	}
	child0, e := getUsage(unix.RUSAGE_CHILDREN)
	if e != nil {
		return e
	}
	io0, e := procIO()
	if e != nil {
		return e
	}
	var batchNS int64
	if arm == "fresh" {
		for seq := 1; seq <= requestCount; seq++ {
			var stdin bytes.Buffer
			if e = json.NewEncoder(&stdin).Encode(map[string]int{"sequence": seq}); e != nil {
				return e
			}
			var stdout, stderr bytes.Buffer
			callStart := time.Now()
			handle, child, e := environmentlifecycle.ExecuteWithStreams(ctx, environmentlifecycle.NewLocalMaterializer(), mat, []string{"python", "-c", pythonRequestProgram, fixturePath, acquired.Path, leaseDir, artifact}, &stdin, &stdout, &stderr)
			batchNS += time.Since(callStart).Nanoseconds()
			r.BatchNS = batchNS
			r.ExecuteCalls++
			if handle.LeaseID != "" {
				r.ChildCalls++
			}
			if e != nil {
				partial, _ := decodeResults(stdout.Bytes())
				r.Requests = append(r.Requests, partial...)
				return fmt.Errorf("fresh request %d execute: %w stderr=%s", seq, e, stderr.String())
			}
			rows, e := decodeResults(stdout.Bytes())
			r.Requests = append(r.Requests, rows...)
			if e != nil {
				return fmt.Errorf("fresh request %d output: %w", seq, e)
			}
			if child.ExitCode != 0 || len(rows) != 1 || rows[0].Sequence != seq || rows[0].Count != 512 || rows[0].AggregateSHA256 != f.ExpectedOutput || rows[0].LeaseID != handle.LeaseID {
				return fmt.Errorf("fresh request %d result/lease binding mismatch", seq)
			}
			released, e := leaseRemoved(common.Product.Home(), acquired.ArtifactDigest, handle.LeaseID)
			if e != nil || !released {
				return fmt.Errorf("fresh request %d lease removal: %v", seq, e)
			}
		}
	} else {
		var stdin bytes.Buffer
		enc := json.NewEncoder(&stdin)
		for seq := 1; seq <= requestCount; seq++ {
			if e = enc.Encode(map[string]int{"sequence": seq}); e != nil {
				return e
			}
		}
		var stdout, stderr bytes.Buffer
		callStart := time.Now()
		handle, child, e := environmentlifecycle.ExecuteWithStreams(ctx, environmentlifecycle.NewLocalMaterializer(), mat, []string{"python", "-c", pythonRequestProgram, fixturePath, acquired.Path, leaseDir, artifact}, &stdin, &stdout, &stderr)
		batchNS += time.Since(callStart).Nanoseconds()
		r.BatchNS = batchNS
		r.ExecuteCalls++
		if handle.LeaseID != "" {
			r.ChildCalls++
		}
		if e != nil {
			partial, _ := decodeResults(stdout.Bytes())
			r.Requests = append(r.Requests, partial...)
			return fmt.Errorf("persistent execute: %w stderr=%s", e, stderr.String())
		}
		rows, e := decodeResults(stdout.Bytes())
		r.Requests = rows
		if e != nil {
			return fmt.Errorf("persistent output: %w", e)
		}
		if child.ExitCode != 0 || len(rows) != requestCount {
			return fmt.Errorf("persistent child result count/exit mismatch: exit=%d rows=%d stderr=%s", child.ExitCode, len(rows), stderr.String())
		}
		for seq, row := range rows {
			if row.Sequence != seq+1 || row.Count != 512 || row.AggregateSHA256 != f.ExpectedOutput || row.LeaseID != handle.LeaseID {
				return fmt.Errorf("persistent request %d verification/lease mismatch", seq+1)
			}
		}
		released, e := leaseRemoved(common.Product.Home(), acquired.ArtifactDigest, handle.LeaseID)
		if e != nil || !released {
			return fmt.Errorf("persistent lease removal: %v", e)
		}
	}
	r.BatchNS = batchNS
	pprof.StopCPUProfile()
	profileActive = false
	if e = profile.Sync(); e != nil {
		return fmt.Errorf("flush CPU profile: %w", e)
	}
	if e = profile.Close(); e != nil {
		return fmt.Errorf("close CPU profile: %w", e)
	}
	self1, e := getUsage(unix.RUSAGE_SELF)
	if e != nil {
		return e
	}
	child1, e := getUsage(unix.RUSAGE_CHILDREN)
	if e != nil {
		return e
	}
	io1, e := procIO()
	if e != nil {
		return e
	}
	r.SelfCPUUserNS = self1.user - self0.user
	r.SelfCPUSystemNS = self1.system - self0.system
	r.ChildCPUUserNS = child1.user - child0.user
	r.ChildCPUSystemNS = child1.system - child0.system
	r.RChar = io1.rchar - io0.rchar
	r.ReadBytes = io1.readBytes - io0.readBytes
	r.ReadSyscalls = io1.syscr - io0.syscr
	r.RSSAfterBytes, e = procRSS()
	if e != nil {
		return e
	}
	r.ResourceScope = "post-acquire through ExecuteWithStreams return and output decoding/lease cleanup; excludes cold acquire and later source/guard validation"
	pids := map[int]bool{}
	r.OutputParity = true
	for _, row := range r.Requests {
		pids[row.PID] = true
		if row.Count != 512 || row.AggregateSHA256 != f.ExpectedOutput {
			r.OutputParity = false
		}
	}
	r.DistinctPIDs = len(pids)
	if !r.OutputParity || len(r.Requests) != requestCount {
		return errors.New("batch payload output parity failed")
	}
	if arm == "fresh" && r.DistinctPIDs != requestCount {
		return fmt.Errorf("fresh expected six distinct child PIDs, got %d", r.DistinctPIDs)
	}
	if arm == "resident" && r.DistinctPIDs != 1 {
		return fmt.Errorf("resident child PID changed: distinct=%d", r.DistinctPIDs)
	}
	r.LeasesReleased = true
	afterSource, e := hashTrees(providerRoot, fixturePath)
	if e != nil {
		return e
	}
	if beforeSource != afterSource {
		return errors.New("fixture/provider source bytes changed")
	}
	if e = mode700(common.Product.Home()); e != nil {
		return fmt.Errorf("post-home guard: %w", e)
	}
	if e = mode700(outer); e != nil {
		return fmt.Errorf("post-outer guard: %w", e)
	}
	r.Status = "PASS"
	return appendReceipt()
}

func runFailureControl(outer, providerRoot, artifact, fixturePath, outPath string) (retErr error) {
	started := time.Now()
	r := receipt{Type: "failure-control", Arm: "nonzero-child", Status: "RUNNING", Started: started.UTC().Format(time.RFC3339Nano)}
	written := false
	appendReceipt := func() error {
		if e := appendJSONL(outPath, r); e != nil {
			return e
		}
		written = true
		return nil
	}
	defer func() {
		if retErr != nil && !written && outPath != "" {
			r.Status = "FAILED"
			r.Error = retErr.Error()
			if e := appendReceipt(); e != nil {
				retErr = fmt.Errorf("%v; failure receipt: %w", retErr, e)
			}
		}
	}()
	if outPath == "" {
		return errors.New("control requires receipt path")
	}
	if e := mode700(outer); e != nil {
		return e
	}
	home := common.Product.Home()
	if e := mode700(home); e != nil {
		return e
	}
	cas := filepath.Join(home, "artifacts", "v1", "content")
	if _, e := os.Lstat(cas); !errors.Is(e, os.ErrNotExist) {
		return fmt.Errorf("fresh control CAS required (stat=%v)", e)
	}
	fb, e := os.ReadFile(fixturePath)
	if e != nil {
		return e
	}
	var f fixture
	if e = json.Unmarshal(fb, &f); e != nil {
		return e
	}
	if f.Artifact != artifact || f.GzipLevel != 1 || len(f.Workload) != 512 {
		return errors.New("control fixture identity mismatch")
	}
	provider, e := artifactprovider.NewFilesystem(providerRoot)
	if e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	acquired, e := environmentlifecycle.NewAcquirer().Acquire(ctx, environmentlifecycle.AcquireRequest{ArtifactDigest: mustDigest(artifact), Provider: provider})
	if e != nil {
		return e
	}
	mat := environmentlifecycle.Materialization{ArtifactDigest: acquired.ArtifactDigest, ID: acquired.MaterializationID, Path: acquired.Path, CacheHit: acquired.CacheHit, Verification: acquired.Verification, TrustPolicy: acquired.TrustPolicy, TrustRequest: acquired.TrustRequest, TrustCarrier: acquired.TrustCarrier}
	var stdout, stderr bytes.Buffer
	handle, child, e := environmentlifecycle.ExecuteWithStreams(ctx, environmentlifecycle.NewLocalMaterializer(), mat, []string{"python", "-c", "raise SystemExit(73)"}, nil, &stdout, &stderr)
	if e != nil {
		return fmt.Errorf("expected child exit 73 without supervisor error: %w", e)
	}
	if child.ExitCode != 73 {
		return fmt.Errorf("nonzero child returned %d, want73", child.ExitCode)
	}
	released, e := leaseRemoved(home, acquired.ArtifactDigest, handle.LeaseID)
	if e != nil || !released {
		return fmt.Errorf("failure-control lease was not removed: %v", e)
	}
	if e = mode700(home); e != nil {
		return e
	}
	if e = mode700(outer); e != nil {
		return e
	}
	r.Artifact = artifact
	r.Materialization = acquired.MaterializationID
	r.LeasesReleased = true
	r.Status = "PASS"
	return appendReceipt()
}

const pythonRequestProgram = `import hashlib,json,os,pathlib,sys
fixture=json.load(open(sys.argv[1],encoding='utf-8')); root=pathlib.Path(sys.argv[2]); lease_dir=pathlib.Path(sys.argv[3]); artifact=sys.argv[4]
for line in sys.stdin:
 request=json.loads(line); rows=[]
 for item in sorted(fixture['workload'],key=lambda x:x['path']):
  p=root/item['path']; b=p.read_bytes(); digest=hashlib.sha256(b).hexdigest()
  if len(b)!=item['size'] or digest!=item['sha256']: raise SystemExit('payload mismatch: '+item['path'])
  rows.append((item['path'],len(b),digest))
 h=hashlib.sha256()
 for path,size,digest in rows: h.update(f'{path}\0{size}\0{digest}\n'.encode())
 aggregate=h.hexdigest()
 if aggregate!=fixture['expectedOutput']: raise SystemExit('aggregate payload digest mismatch')
 active=sorted(lease_dir.glob('*.json'))
 if len(active)!=1: raise SystemExit(f'expected one live lease, found {len(active)}')
 lease=json.load(open(active[0],encoding='utf-8'))
 if lease.get('id')!=active[0].stem or lease.get('artifactDigest')!=artifact: raise SystemExit('lease identity mismatch')
 print(json.dumps({'sequence':request['sequence'],'pid':os.getpid(),'leaseId':lease['id'],'count':len(rows),'aggregateSha256':aggregate},sort_keys=True,separators=(',',':')),flush=True)`

func decodeResults(b []byte) ([]requestResult, error) {
	var out []requestResult
	s := bufio.NewScanner(bytes.NewReader(b))
	s.Buffer(make([]byte, 4096), 1<<20)
	for s.Scan() {
		var row requestResult
		if e := json.Unmarshal(s.Bytes(), &row); e != nil {
			return out, e
		}
		out = append(out, row)
	}
	if e := s.Err(); e != nil {
		return out, e
	}
	return out, nil
}
func mustDigest(s string) environmentartifact.Digest {
	d, e := environmentartifact.ParseDigest(s)
	if e != nil {
		panic(e)
	}
	return d
}
func mode700(path string) error {
	st, e := os.Lstat(path)
	if e != nil {
		return e
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 || st.Mode().Perm() != 0o700 {
		return fmt.Errorf("%s is not strict 0700 dir (mode %v)", path, st.Mode())
	}
	owner, ok := st.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != uint32(os.Geteuid()) || owner.Gid != uint32(os.Getegid()) {
		return fmt.Errorf("%s owner uid/gid mismatch", path)
	}
	return nil
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
func appendJSONL(path string, v any) error {
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
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
func getUsage(who int) (usage, error) {
	var r unix.Rusage
	if e := unix.Getrusage(who, &r); e != nil {
		return usage{}, e
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
		return ioCounts{}, errors.New("no usable /proc/self/io counters")
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
				n, e := strconv.ParseInt(p[1], 10, 64)
				return n * 1024, e
			}
		}
	}
	return 0, errors.New("VmRSS missing")
}
func hashTrees(paths ...string) (string, error) {
	h := sha256.New()
	for _, root := range paths {
		count := 0
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			st, e := os.Lstat(path)
			if e != nil {
				return e
			}
			rel, e := filepath.Rel(root, path)
			if e != nil {
				return e
			}
			fmt.Fprintf(h, "%s\x00%o\x00", filepath.ToSlash(filepath.Join(filepath.Base(root), rel)), st.Mode().Perm())
			switch {
			case st.Mode().IsRegular():
				count++
				if count > 10000 {
					return errors.New("source tree exceeds file bound")
				}
				f, e := os.Open(path)
				if e != nil {
					return e
				}
				_, e = io.Copy(h, f)
				ce := f.Close()
				if e != nil {
					return e
				}
				if ce != nil {
					return ce
				}
			case st.Mode()&os.ModeSymlink != 0:
				return fmt.Errorf("symlink in source tree: %s", path)
			case st.IsDir():
			default:
				return fmt.Errorf("nonregular source entry: %s", path)
			}
			return nil
		})
		if err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
