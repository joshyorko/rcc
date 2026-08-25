package journal_test

import (
	"os"
	"testing"

	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/hamlet"
	"github.com/joshyorko/rcc/journal"
)

func TestJounalCanBeCalled(t *testing.T) {
	must, wont := hamlet.Specifications(t)
	previousController := common.ControllerType
	common.Product.ForceHome(t.TempDir())
	t.Cleanup(func() {
		common.Product.ForceHome("")
		common.ControllerType = previousController
	})
	if err := os.MkdirAll(common.JournalLocation(), 0o755); err != nil {
		t.Fatal(err)
	}

	must.Equal("foo bar", journal.Unify("  foo  \t  \r\n   bar  "))

	common.ControllerType = "unittest"

	must.Nil(journal.Post("unittest", "journal-1", "from journal/journal_test.go"))
	events, err := journal.Events()
	must.Nil(err)
	wont.Nil(events)
	must.True(len(events) > 0)
	must.Nil(journal.Post("unittest", "journal-2", "from journal/journal_test.go"))
	second, err := journal.Events()
	must.True(len(second) > len(events))
}

func TestJournalHomeCleanupPreservesUnforcedHome(t *testing.T) {
	environmentHome := t.TempDir()
	t.Setenv(common.ROBOCORP_HOME_VARIABLE, environmentHome)
	common.Product.ForceHome(t.TempDir())
	func() {
		defer common.Product.ForceHome("")
		if got := common.Product.Home(); got == environmentHome {
			t.Fatal("forced home was not applied")
		}
	}()
	if got := common.Product.Home(); got != environmentHome {
		t.Fatalf("unforced home = %q, want %q", got, environmentHome)
	}
}
