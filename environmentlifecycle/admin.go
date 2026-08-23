package environmentlifecycle

import (
	"context"
	"errors"
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

type Inspection struct { Digest environmentartifact.Digest `json:"digest"`; State string `json:"state"`; Ready bool `json:"ready"`; Lease ReconcileReport `json:"lease"`; Path string `json:"path,omitempty"`; Corrupt bool `json:"corrupt"`; ProviderUnavailable bool `json:"providerUnavailable"` }
type Verification struct { Digest environmentartifact.Digest `json:"digest"`; Verified bool `json:"verified"`; State string `json:"state"`; Reason string `json:"reason,omitempty"` }
type RepairReport struct { Inspection Inspection `json:"inspection"`; Reconciled ReconcileReport `json:"reconciled"`; Repaired bool `json:"repaired"`; Verification Verification `json:"verification"` }

func Inspect(ctx context.Context, digest environmentartifact.Digest) (Inspection,error) { if err:=ctx.Err();err!=nil{return Inspection{},err}; report,err:=Reconcile(ctx,digest);if err!=nil{return Inspection{},err}; out:=Inspection{Digest:digest,Lease:report}; record,err:=readReadyRecord(digest);if err!=nil{if os.IsNotExist(err){out.State="absent"}else{out.State="corrupt";out.Corrupt=true};return out,nil};out.State=string(record.State);out.Ready=true;out.Path=record.Path;return out,nil }
func Verify(ctx context.Context,digest environmentartifact.Digest) (Verification,error) { in,err:=Inspect(ctx,digest);if err!=nil{return Verification{},err};out:=Verification{Digest:digest,State:in.State};if in.Corrupt||!in.Ready{return out,fmt.Errorf("%w: %s",ErrMaterializationCorrupt,in.State)};if _,err:=os.Stat(in.Path);err!=nil{return out,fmt.Errorf("%w: materialization path: %v",ErrMaterializationCorrupt,err)};out.Verified=true;return out,nil }
func Repair(ctx context.Context,digest environmentartifact.Digest) (RepairReport,error) { in,err:=Inspect(ctx,digest);if err!=nil{return RepairReport{},err};repaired:=false;if in.Corrupt{_ = os.Remove(filepath.Join(recordRoot(),digest.Hex(),string(stateReady)+".json"));repaired=true};verification,verr:=Verify(ctx,digest);if verr!=nil&&repaired{verification=Verification{Digest:digest,State:"repaired"}};return RepairReport{Inspection:in,Reconciled:in.Lease,Repaired:repaired,Verification:verification},nil }
func RepairFromProvider(ctx context.Context,digest environmentartifact.Digest,provider artifactprovider.Provider) (RepairReport,error) {if provider==nil{return RepairReport{},ErrProviderUnavailable};content,err:=acquireVerifiedContent(ctx,digest,provider);if err!=nil{return RepairReport{},fmt.Errorf("%w: %v",ErrProviderUnavailable,err)};materialization,err:=NewLocalMaterializer().Materialize(ctx,content.manifest);if err!=nil{return RepairReport{},err};return RepairReport{Repaired:true,Verification:Verification{Digest:digest,Verified:true,State:string(stateReady)},Inspection:Inspection{Digest:digest,Ready:true,State:string(stateReady),Path:materialization.Path}},nil}
func RetentionEligible(record materializationRecord, now time.Time, retention time.Duration) bool {return retention<=0||now.Sub(record.VerifiedAt)>=retention}
