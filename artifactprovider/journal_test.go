package artifactprovider

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/joshyorko/rcc/environmentartifact"
)

func TestJournalDurableRestartContract(t *testing.T) {
	path := t.TempDir()+"/provider.log"; content:=[]byte("journal durable object"); blob:=testBlob(content)
	j,err:=NewJournal(path); if err!=nil{t.Fatal(err)}; if err=j.PutObject(context.Background(),blob);err!=nil{t.Fatal(err)}
	reader,err:=j.GetObject(context.Background(),blob.Descriptor); if err!=nil{t.Fatal(err)}; raw,err:=io.ReadAll(reader); _=reader.Close(); if err!=nil||!bytes.Equal(raw,content){t.Fatalf("read=%q err=%v",raw,err)}
	j,err=NewJournal(path);if err!=nil{t.Fatal(err)}; missing,err:=j.MissingObjects(context.Background(),[]environmentartifact.Descriptor{blob.Descriptor});if err!=nil||len(missing)!=0{t.Fatalf("missing=%v err=%v",missing,err)}
}

func TestPolicyTypedQuotaAndRateErrors(t *testing.T) {
	j,_:=NewJournal(t.TempDir()+"/q.log"); p:=NewPolicy(j,Limits{MaxBytes:2,RequestsPerSecond:1}); blob:=testBlob([]byte("three")); if err:=p.PutObject(context.Background(),blob);err==nil||!errors.Is(err,ErrQuotaExceeded){t.Fatalf("quota err=%v",err)}
}
