package journal_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/journal"
)

func withProductHome(t *testing.T) string {
	t.Helper()
	previousHome := common.Product.Home()
	home := t.TempDir()
	common.Product.ForceHome(home)
	t.Cleanup(func() { common.Product.ForceHome(previousHome) })
	return home
}

func TestAppendJournalCreatesAndReloadsRecords(t *testing.T) {
	home := withProductHome(t)
	journalPath := filepath.Join(home, "journals", "events.log")
	if err := os.MkdirAll(filepath.Dir(journalPath), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := journal.AppendJournal(journalPath, []byte(`{"event":"created","detail":"one"}`)); err != nil {
		t.Fatal(err)
	}
	if err := journal.AppendJournal(journalPath, []byte(`{"event":"appended","detail":"two"}`)); err != nil {
		t.Fatal(err)
	}

	contents, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\"event\":\"created\",\"detail\":\"one\"}\n{\"event\":\"appended\",\"detail\":\"two\"}\n"
	if string(contents) != want {
		t.Fatalf("journal contents = %q, want %q", contents, want)
	}
}

func TestEventsReloadsValidRecordsAndSkipsMalformedLines(t *testing.T) {
	withProductHome(t)
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

func TestAppendJournalReportsUnavailableLocation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "events.log")
	err := journal.AppendJournal(path, []byte(`{"event":"unavailable"}`))
	if err == nil {
		t.Fatal("AppendJournal returned nil for unavailable location")
	}
	if !strings.Contains(err.Error(), "Failed to open event journal") {
		t.Fatalf("AppendJournal error = %v, want open failure", err)
	}
}
