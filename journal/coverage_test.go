package journal_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/journal"
)

func withProductHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv(common.ROBOCORP_HOME_VARIABLE, home)
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

func TestRunJournalLifecyclePersistsThenStops(t *testing.T) {
	home := withProductHome(t)
	path := filepath.Join(home, "run", "run.log")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(journal.StopRunJournal)

	journal.ForRun(path)
	if err := common.RunJournal("  run\n started  ", "detail\tvalue", " process %s\t ready ", "one"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got journal.Event
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &got); err != nil {
		t.Fatalf("decode run event: %v", err)
	}
	if got.Event != "run started" || got.Detail != "detail\tvalue" || got.Comment != "process one ready" {
		t.Fatalf("run event = %#v", got)
	}

	journal.StopRunJournal()
	if err := common.RunJournal("after stop", "ignored", "ignored"); err != nil {
		t.Fatal(err)
	}
	afterStop, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(afterStop) != string(data) {
		t.Fatalf("StopRunJournal allowed another write: before %q, after %q", data, afterStop)
	}
}

func TestAppendJournalWarrantyVoidedIsNoOp(t *testing.T) {
	previous := common.WarrantyVoidedFlag
	common.WarrantyVoidedFlag = true
	t.Cleanup(func() { common.WarrantyVoidedFlag = previous })

	path := filepath.Join(t.TempDir(), "events.log")
	if err := journal.AppendJournal(path, []byte(`{"event":"suppressed"}`)); err != nil {
		t.Fatalf("AppendJournal with warranty voided: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("warranty-voided append created file; stat error = %v", err)
	}
}

func TestEventsReportsMissingJournal(t *testing.T) {
	withProductHome(t)
	events, err := journal.Events()
	if err == nil {
		t.Fatalf("Events() = %#v, nil error for missing journal", events)
	}
	if !strings.Contains(err.Error(), "Failed to open event journal") {
		t.Fatalf("Events() error = %v, want wrapped open failure", err)
	}
}

func TestBuildEventStatsPersistsAndReloadsLifecycle(t *testing.T) {
	home := withProductHome(t)
	if err := os.MkdirAll(common.JournalLocation(), 0o755); err != nil {
		t.Fatal(err)
	}
	controller, space := common.ControllerType, common.HolotreeSpace
	common.ControllerType, common.HolotreeSpace = "journal-test", "test-space"
	t.Cleanup(func() {
		common.ControllerType, common.HolotreeSpace = controller, space
	})

	current := journal.CurrentBuildEvent()
	previous := *current
	t.Cleanup(func() { *current = previous })
	*current = *journal.NewBuildEvent()
	current.StartNow(true)
	current.Blueprint("sha256:test-blueprint")
	current.Rebuild()
	current.PrepareComplete()
	current.Successful()
	journal.BuildEventStats("robot")

	filename := journal.CurrentEventFilename()
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("BuildEventStats did not persist %s: %v", filename, err)
	}
	var persisted journal.BuildEvent
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &persisted); err != nil {
		t.Fatalf("decode build event: %v", err)
	}
	if persisted.What != "robot" || !persisted.Build || !persisted.Success || !persisted.Retry || !persisted.Force || persisted.Controller != "journal-test" || persisted.Space != "test-space" || persisted.BlueprintHash != "sha256:test-blueprint" {
		t.Fatalf("persisted build event = %#v", persisted)
	}

	loaded, err := journal.Stats(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].What != "robot" || !loaded[0].Success {
		t.Fatalf("Stats reload = %#v, want the persisted robot event", loaded)
	}
	count, report := journal.MakeStatistics(1, false, true, false, false)
	if count != 1 || !strings.Contains(string(report), "Selected (robot) statistics: 1 samples") {
		t.Fatalf("robot-filtered statistics count=%d report=%q", count, report)
	}
	if _, err := os.Stat(filepath.Join(home, "journals")); err != nil {
		t.Fatalf("expected isolated journal directory: %v", err)
	}
}
