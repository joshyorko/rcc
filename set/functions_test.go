package set_test

import (
	"testing"

	"github.com/robocorp/rcc/hamlet"
	"github.com/robocorp/rcc/set"
)

func TestMakingAndAppending(t *testing.T) {
	must_be, wont_be := hamlet.Specifications(t)

	original := set.Set([]string{"B", "A", "A", "B", "B", "A"})
	must_be.Text("[A B]", original)

	updated, ok := set.Update(original, "C")
	must_be.True(ok)
	must_be.Text("[A B C]", updated)

	already, ok := set.Update([]string{"A", "B", "C"}, "C")
	wont_be.True(ok)
	must_be.Text("[A B C]", already)
}

func TestMembershipAndSorting(t *testing.T) {
	must_be, wont_be := hamlet.Specifications(t)

	original := []string{"B", "A", "D", "F", "E", "C"}
	must_be.True(set.Member(original, "F"))
	wont_be.True(set.Member(original, "G"))
	must_be.Text("[A B C D E F]", set.Sort(original))
	must_be.Text("[A B C D E F]", original)
}

func TestOperations(t *testing.T) {
	must_be, wont_be := hamlet.Specifications(t)

	smaller := set.With("D", "E", "F")
	bigger := set.With("F", "A", "C", "E", "C", "A", "P")
	must_be.True(set.Member(smaller, "D"))
	wont_be.True(set.Member(bigger, "D"))
	must_be.Text("[E F]", set.Intersect(smaller, bigger))
	must_be.Text("[A C D E F P]", set.Union(smaller, bigger))
}
