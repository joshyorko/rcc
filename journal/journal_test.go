package journal_test

import (
	"testing"

	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/hamlet"
	"github.com/joshyorko/rcc/journal"
)

func TestJounalCanBeCalled(t *testing.T) {
	must, wont := hamlet.Specifications(t)

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
