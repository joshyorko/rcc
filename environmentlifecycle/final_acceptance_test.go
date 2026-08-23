package environmentlifecycle

import (
	"context"
	"sync"
	"testing"

	"github.com/joshyorko/rcc/common"
)

func TestTwoIndependentAcquirersConvergeOnOneMaterialization(t *testing.T) {
	_, remote, digest := publishedFixture(t)
	previousHome:=common.Product.Home();previousShared:=common.SharedHolotree;common.Product.ForceHome(t.TempDir());common.SharedHolotree=false;t.Cleanup(func(){common.Product.ForceHome(previousHome);common.SharedHolotree=previousShared})
	results:=make([]AcquireResult,2);errs:=make([]error,2);var group sync.WaitGroup;for i:=range results{group.Add(1);go func(i int){defer group.Done();results[i],errs[i]=NewAcquirer().Acquire(context.Background(),AcquireRequest{ArtifactDigest:digest,Provider:remote})}(i)};group.Wait();for i,e:=range errs{if e!=nil{t.Fatalf("acquirer %d: %v",i,e)}};if results[0].MaterializationID!=results[1].MaterializationID||results[0].Path!=results[1].Path{t.Fatalf("convergence failed: %+v %+v",results[0],results[1])}
}

func TestRepairVsAcquireRestoresSameDigest(t *testing.T) {
	fixture,remote,digest:=publishedFixture(t);_ = fixture;previousHome:=common.Product.Home();previousShared:=common.SharedHolotree;common.Product.ForceHome(t.TempDir());common.SharedHolotree=false;t.Cleanup(func(){common.Product.ForceHome(previousHome);common.SharedHolotree=previousShared});materialization,err:=NewAcquirer().Acquire(context.Background(),AcquireRequest{ArtifactDigest:digest,Provider:remote});if err!=nil{t.Fatal(err)};path:=materialization.Path
	if err:=removeMaterialization(path);err!=nil{t.Fatal(err)}
	report,err:=RepairFromProvider(context.Background(),digest,remote);if err!=nil{t.Fatal(err)}
	if report.Verification.Digest!=digest{t.Fatalf("repair digest=%v",report.Verification.Digest)}
}

func TestGCVsFinalizationDoesNotDeleteActiveLease(t *testing.T) {
	m:=acquiredMaterialization(t);lease,err:=NewLocalMaterializer().Lease(context.Background(),m);if err!=nil{t.Fatal(err)};defer NewLocalMaterializer().Release(context.Background(),lease)
	var report GCReport;var gcErr error;done:=make(chan struct{});go func(){report,gcErr=Collect(context.Background(),GCPolicy{});close(done)}();<-done;if gcErr!=nil{t.Fatal(gcErr)};if report.SkippedActive!=1{t.Fatalf("GC finalized active materialization: %+v",report)}
}

func TestLeaseLoserCannotDeleteAnotherLeasesState(t *testing.T) {
	m:=acquiredMaterialization(t);materializer:=NewLocalMaterializer();first,err:=materializer.Lease(context.Background(),m);if err!=nil{t.Fatal(err)};second,err:=materializer.Lease(context.Background(),m);if err!=nil{t.Fatal(err)};if err:=materializer.Release(context.Background(),first);err!=nil{t.Fatal(err)};if _,err:=readLease(second.ArtifactDigest,second.ID);err!=nil{t.Fatalf("loser deleted winner lease: %v",err)};_ = materializer.Release(context.Background(),second)
}
