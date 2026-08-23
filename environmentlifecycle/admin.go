package environmentlifecycle

import (
	"context"
	"errors"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/joshyorko/rcc/artifactprovider"
	"github.com/joshyorko/rcc/environmentartifact"
)

var (
	ErrMaterializationCorrupt = errors.New("materialization corrupt")
	ErrProviderUnavailable = errors.New("artifact provider unavailable")
)

type Inspection struct { Digest environmentartifact.Digest `json:"digest"`; State string `json:"state"`; Ready bool `json:"ready"`; Lease ReconcileReport `json:"lease"`; Path string `json:"path,omitempty"`; Corrupt bool `json:"corrupt"`; ProviderUnavailable bool `json:"providerUnavailable"`; LastRepair time.Time `json:"lastRepair,omitempty"`; ProviderRequired bool `json:"providerRequired"` }
type Verification struct { Digest environmentartifact.Digest `json:"digest"`; Verified bool `json:"verified"`; State string `json:"state"`; Reason string `json:"reason,omitempty"` }
type RepairReport struct { Inspection Inspection `json:"inspection"`; Reconciled ReconcileReport `json:"reconciled"`; Repaired bool `json:"repaired"`; Verification Verification `json:"verification"` }
type ReferenceGraph struct { Manifest environmentartifact.Digest `json:"manifest"`; Protected []environmentartifact.Digest `json:"protected"`; SharedBytes int64 `json:"sharedBytes"` }
type repairRecord struct { Digest environmentartifact.Digest `json:"digest"`; At time.Time `json:"at"`; ProviderRequired bool `json:"providerRequired"`; State string `json:"state"` }

func BuildReferenceGraph(manifest environmentartifact.Manifest, index environmentartifact.ObjectIndex) ReferenceGraph { seen:=map[environmentartifact.Digest]bool{}; add:=func(d environmentartifact.Digest){seen[d]=true};add(manifest.Specification.Digest);add(manifest.LegacyBlueprint.Digest);for _,c:=range manifest.Catalogs{add(c.Digest)};add(manifest.ObjectIndex.Digest);for _,e:=range index.Entries{add(e.StoredDigest)};out:=ReferenceGraph{Manifest:manifest.ArtifactDigest};for d:=range seen{out.Protected=append(out.Protected,d)};return out }

func Inspect(ctx context.Context, digest environmentartifact.Digest) (Inspection,error) { if err:=ctx.Err();err!=nil{return Inspection{},err}; report,err:=Reconcile(ctx,digest);if err!=nil{return Inspection{},err}; out:=Inspection{Digest:digest,Lease:report}; record,err:=readReadyRecord(digest);if err!=nil{if os.IsNotExist(err){out.State="absent"}else{out.State="corrupt";out.Corrupt=true};return out,nil};out.State=string(record.State);out.Ready=true;out.Path=record.Path;if b,e:=os.ReadFile(filepath.Join(recordRoot(),digest.Hex(),"repair.json"));e==nil{var rr repairRecord;if json.Unmarshal(b,&rr)==nil{out.LastRepair=rr.At;out.ProviderRequired=rr.ProviderRequired}};return out,nil }
func Verify(ctx context.Context,digest environmentartifact.Digest) (Verification,error) { in,err:=Inspect(ctx,digest);if err!=nil{return Verification{},err};out:=Verification{Digest:digest,State:in.State};if in.Corrupt||!in.Ready{return out,fmt.Errorf("%w: %s",ErrMaterializationCorrupt,in.State)};if _,err:=os.Stat(in.Path);err!=nil{return out,fmt.Errorf("%w: materialization path: %v",ErrMaterializationCorrupt,err)};out.Verified=true;return out,nil }
func Repair(ctx context.Context,digest environmentartifact.Digest) (RepairReport,error) { in,err:=Inspect(ctx,digest);if err!=nil{return RepairReport{},err};repaired:=false;if in.Corrupt{_ = os.Remove(filepath.Join(recordRoot(),digest.Hex(),string(stateReady)+".json"));repaired=true};verification,verr:=Verify(ctx,digest);if verr!=nil&&repaired{verification=Verification{Digest:digest,State:"repaired"}};_ = writeRepairRecord(digest,false,"local");return RepairReport{Inspection:in,Reconciled:in.Lease,Repaired:repaired,Verification:verification},nil }
func RepairFromProvider(ctx context.Context,digest environmentartifact.Digest,provider artifactprovider.Provider) (RepairReport,error) {if provider==nil{return RepairReport{},ErrProviderUnavailable};content,err:=acquireVerifiedContent(ctx,digest,provider);if err!=nil{return RepairReport{},fmt.Errorf("%w: %v",ErrProviderUnavailable,err)};materialization,err:=NewLocalMaterializer().Materialize(ctx,content.manifest);if err!=nil{return RepairReport{},err};_ = writeRepairRecord(digest,true,"provider");return RepairReport{Repaired:true,Verification:Verification{Digest:digest,Verified:true,State:string(stateReady)},Inspection:Inspection{Digest:digest,Ready:true,State:string(stateReady),Path:materialization.Path}},nil}
func writeRepairRecord(digest environmentartifact.Digest, provider bool, state string) error {b,_:=json.Marshal(repairRecord{Digest:digest,At:time.Now().UTC(),ProviderRequired:provider,State:state});return writeAtomicMutable(recordRoot(),[]string{digest.Hex(),"repair.json"},b)}
func RetentionEligible(record materializationRecord, now time.Time, retention time.Duration) bool {return retention<=0||now.Sub(record.VerifiedAt)>=retention}
