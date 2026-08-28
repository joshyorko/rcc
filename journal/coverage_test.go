package journal_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/journal"
)

func temporaryProductHome(t *testing.T) string {
	t.Helper()
	previousHome := common.Product.Home()
	home := t.TempDir()
	common.Product.ForceHome(home)
	t.Cleanup(func() { common.Product.ForceHome(previousHome) })
	return home
}

func TestAppendJournalPersistsRecordsWithoutRewritingInput(t *testing.T) {
	home := temporaryProductHome(t)
	journalPath := filepath.Join(home, "journals", "events.log")
	if err := os.MkdirAll(filepath.Dir(journalPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := journal.AppendJournal(journalPath, []byte(`{"event":"kept","detail":"one"}`)); err != nil {
		t.Fatal(err)
	}
	if err := journal.AppendJournal(journalPath, []byte("not json")); err != nil {
		t.Fatal(err)
	}

	contents, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\"event\":\"kept\",\"detail\":\"one\"}\nnot json\n"
	if string(contents) != want {
		t.Fatalf("journal contents = %q, want %q", contents, want)
	}
}

func TestEventsReloadsValidRecordsAndIgnoresMalformedLines(t *testing.T) {
	temporaryProductHome(t)
	path := common.EventJournal()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data := "{\"when\":7,\"controller\":\"test\",\"event\":\"start\",\"detail\":\"ready\"}\nmalformed\n"
	if err := os.WriteFile(path, []byte(data), 0o640); err != nil {
		t.Fatal(err)
	}

	events, err := journal.Events()
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Event != "start" || events[0].Detail != "ready" {
		t.Fatalf("events = %#v, want one valid event", events)
	}
}
