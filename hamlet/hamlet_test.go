package hamlet_test

import (
	"fmt"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/robocorp/rcc/hamlet"
)

type Nothing struct{}
type Example struct{ Value int }

func TestCanUseTheTestingFramework(t *testing.T) {
	to_be, not_to_be := hamlet.Specifications(t)
	c := t
	c = nil
	a, b := 1, 2
	d, e := 3.14159, "hupsis"
	to_be.True(a < b)
	not_to_be.True(b < a)
	not_to_be.Nil(a)
	not_to_be.Nil(d)
	not_to_be.Nil(e)
	not_to_be.Nil(t)
	to_be.Nil(c)
	to_be.Nil(nil)
	not_to_be.Equal(a, b)
	to_be.Equal(2, b)
	not_to_be.Panic(func() {})
	to_be.Panic(func() { panic("now") })
	to_be.Same(b, b)
	not_to_be.Same(&Example{1}, &Example{2})
	not_to_be.Same(&Example{1}, &Example{1})
	not_to_be.Same(Example{1}, &Example{1})
	to_be.Same(Example{1}, Example{1})
	to_be.Same(&Nothing{}, &Nothing{})
	not_to_be.Same(&Nothing{}, Nothing{})
	to_be.Same(Nothing{}, Nothing{})
	to_be.Text("2", b)
	not_to_be.Text("2", a)
	to_be.Text("<nil>", nil)
	to_be.Type("*testing.T", t)
	to_be.All(positive_uints)
	not_to_be.All(positive_ints)
}

func TestCanGetWorkingFolder(t *testing.T) {
	be, not := hamlet.Specifications(t)

	dir, err := os.Getwd()
	be.Nil(err)
	not.Nil(dir)
	prefix, _ := path.Split(dir)
	not.Equal(prefix, dir)
	be.True(strings.HasPrefix(dir, prefix))
}

func TestMustFailsOnCorrectCases(t *testing.T) {
	be, _ := hamlet.Specifications(t)

	var mock FailerMock
	sut_must, _ := hamlet.Specifications(&mock)

	sut_must.Equal(1, 2)
	sut_must.Equal(3, 3)
	be.Text("[2 to be 1]", mock.Message)

	sut_must.Same(1, 2)
	sut_must.Same(3, 3)
	be.Text("[2 to be 1]", mock.Message)

	sut_must.Text("1", 2)
	sut_must.Text("4", 4)
	be.Text("[2 to be 1]", mock.Message)

	sut_must.Type("uint", 2)
	sut_must.Type("int", 2)
	be.Text("[int to be uint]", mock.Message)

	sut_must.True(1 == 2)
	sut_must.True(3 == 3)
	be.Text("[false to be true]", mock.Message)

	sut_must.Nil(t)
	sut_must.Nil(nil)
	be.Match("^\\[\\S+ to be <nil>]", mock.Message)

	sut_must.Match("^hello$", "hello, world")
	sut_must.Match("\\bcruel\\b", "hello, cruel world")
	be.Text("[hello, world to be ^hello$]", mock.Message)

	sut_must.Panic(func() {})
	sut_must.Panic(func() { panic("now") })
	be.Text("[call to be <<<panic>>>]", mock.Message)

	sut_must.All(positive_ints)
	sut_must.All(positive_uints)
	be.Match("^\\[#[0-9]+: failed on input -[0-9]+ to be All]$", mock.Message)

	be.Equal(9, mock.Callcount)
}

func TestWontFailsOnCorrectCases(t *testing.T) {
	be, _ := hamlet.Specifications(t)

	var mock FailerMock
	_, sut_wont := hamlet.Specifications(&mock)

	sut_wont.Equal(3, 3)
	sut_wont.Equal(1, 2)
	be.Text("[3 not to be 3]", mock.Message)

	sut_wont.Same(3, 3)
	sut_wont.Same(1, 2)
	be.Text("[3 not to be 3]", mock.Message)

	sut_wont.Text("4", 4)
	sut_wont.Text("1", 2)
	be.Text("[4 not to be 4]", mock.Message)

	sut_wont.Type("int", 2)
	sut_wont.Type("uint", 2)
	be.Text("[int not to be int]", mock.Message)

	sut_wont.True(3 == 3)
	sut_wont.True(1 == 2)
	be.Text("[true not to be false]", mock.Message)

	sut_wont.Nil(nil)
	sut_wont.Nil(t)
	be.Text("[<nil> not to be <nil>]", mock.Message)

	sut_wont.Match("\\bcruel\\b", "hello, cruel world")
	sut_wont.Match("^hello$", "hello, world")
	be.Text("[hello, cruel world not to be \\bcruel\\b]", mock.Message)

	sut_wont.Panic(func() { panic("now") })
	sut_wont.Panic(func() {})
	be.Text("[call not to be <<<panic>>>]", mock.Message)

	sut_wont.All(positive_uints)
	sut_wont.All(positive_ints)
	be.Text("[<nil> not to be All]", mock.Message)

	be.Equal(9, mock.Callcount)
}

type FailerMock struct {
	Callcount int
	Message   string
}

func (it *FailerMock) Helper() {
}

func (it *FailerMock) Errorf(_ string, args ...interface{}) {
	it.Callcount += 1
	it.Message = fmt.Sprintf("%v", args)
}

// some dummy propertis for yoda.All

func positive_uints(value uint64) bool {
	return value >= 0
}

func positive_ints(value int64) bool {
	return value >= 0
}
