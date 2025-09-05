/*
Package rcc/hamlet provides DSL for writing terse declarative
runnable specifications.

When you want to use Hamlet for runnable specifications, first thing in your
specifications should be wrapping *testing.T with "hamlet.Specifications(t)"
and you get back two parts, one for positive declarations and one for negative
("to be" and "not to be"; "must be" and "wont be"; "be" and "not").

	func TestWrapsTestingT(t *testing.T) {
		to_be, not_to_be := hamlet.Specifications(t)
		...
	}

And then you have set of predicates to declare, how system should behave
when used in code.

	func TestToGetUnderstandingHowSystemWorks(t *testing.T) {
		must_be, wont_be := hamlet.Specifications(t)

		dirname, err := os.Getwd()
		must_be.Nil(err)
		wont_be.Nil(dirname)

		prefix, _ := path.Split(dirname)
		wont_be.Equal(prefix, dirname)
		must_be.True(strings.HasPrefix(dirname, prefix))
	}

I like my specifications short and declarative, not longwindy and procedural
code form. One line, one expectation!
*/
package hamlet

import (
	"fmt"
	"reflect"
	"regexp"
	"testing/quick"
)

// Function type used for testing panics.
type Panicable func()

// Interface to capture relevant *testing.T methods.
type Reporter interface {
	Helper()
	Errorf(format string, args ...interface{})
}

// Structure to hold context sensitive material.
type Hamlet struct {
	failer   Reporter
	expected bool
	phrase   string
}

func Specifications(test Reporter) (*Hamlet, *Hamlet) {
	return &Hamlet{test, true, "to be"}, &Hamlet{test, false, "not to be"}
}

func (it *Hamlet) Equal(expected, actual interface{}) {
	it.failer.Helper()
	it.verify(reflect.DeepEqual(expected, actual), expected, actual)
}

func (it *Hamlet) Same(expected, actual interface{}) {
	it.failer.Helper()
	it.verify(printed(expected) == printed(actual) && reflect.DeepEqual(actual, expected), expected, actual)
}

func (it *Hamlet) Text(expected string, actual interface{}) {
	it.failer.Helper()
	it.verify(expected == fmt.Sprintf("%v", actual), expected, actual)
}

func (it *Hamlet) Match(expected string, actual interface{}) {
	it.failer.Helper()
	pattern := regexp.MustCompile(expected)
	it.verify(pattern.MatchString(fmt.Sprintf("%v", actual)), expected, actual)
}

func (it *Hamlet) Type(expected string, actual interface{}) {
	it.failer.Helper()
	it.verify(expected == reflect.TypeOf(actual).String(), expected, reflect.TypeOf(actual))
}

func (it *Hamlet) True(actual bool) {
	it.failer.Helper()
	it.verify(actual, it.expected, actual)
}

func (it *Hamlet) Nil(actual interface{}) {
	it.failer.Helper()
	defer func() {
		it.failer.Helper()
		if recover() != nil {
			it.verify(false, nil, actual)
		}
	}()
	it.verify(reflect.TypeOf(actual) == nil || reflect.ValueOf(actual).IsNil(), nil, actual)
}

func (it *Hamlet) Panic(function Panicable) {
	it.failer.Helper()
	defer func() {
		it.failer.Helper()
		if recover() != nil {
			it.verify(true, "<<<panic>>>", "call")
		}
	}()
	function()
	it.verify(false, "<<<panic>>>", "call")
}

func (it *Hamlet) All(property interface{}) {
	it.failer.Helper()
	err := quick.Check(property, nil)
	it.verify(err == nil, "All", fmt.Sprintf("%v", err))
}

func (it *Hamlet) verify(comparison bool, expected, actual interface{}) {
	it.failer.Helper()
	if comparison != it.expected {
		it.failed(expected, actual)
	}
}

func (it *Hamlet) failed(expected, actual interface{}) {
	it.failer.Helper()
	it.failer.Errorf("Expected %#v %s %#v!!!", actual, it.phrase, expected)
}

func printed(value interface{}) string {
	return fmt.Sprintf("%p %#v", value, value)
}
