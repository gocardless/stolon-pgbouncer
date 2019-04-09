package matchers

import (
	"reflect"

	"github.com/onsi/gomega/types"
)

// All converts a matcher for type A to a matcher for type []A.
// Given a matcher m, it returns a new matcher which runs m against each
// element of the input, which is expected to be a slice.
// It succeeds if m succeeds for every element of the input.
func All(matcher types.GomegaMatcher) types.GomegaMatcher {
	return &allMatcher{matcher: matcher}
}

// Any converts a matcher for type A to a matcher for type []A.
// Given a matcher m, it returns a new matcher which runs m against each
// element of the input, which is expected to be a slice.
// It succeeds if m succeeds for any element of the input.
func Any(matcher types.GomegaMatcher) types.GomegaMatcher {
	return &anyMatcher{matcher: matcher}
}

type allMatcher struct {
	matcher      types.GomegaMatcher
	failingValue interface{}
}

func (m *allMatcher) Match(actual interface{}) (success bool, err error) {
	for _, e := range castToSlice(actual) {
		success, err = m.matcher.Match(e)
		if err != nil {
			return success, err
		}
		if !success {
			m.failingValue = e
			return false, nil
		}
	}
	return true, nil
}

func (m *allMatcher) FailureMessage(actual interface{}) (message string) {
	return m.matcher.FailureMessage(m.failingValue)
}
func (m *allMatcher) NegatedFailureMessage(actual interface{}) (message string) {
	return m.matcher.NegatedFailureMessage(m.failingValue)
}

type anyMatcher struct {
	matcher types.GomegaMatcher
	input   []interface{}
}

func (m *anyMatcher) Match(actual interface{}) (success bool, err error) {
	success = false
	m.input = castToSlice(actual)

	for _, e := range m.input {
		res, err := m.matcher.Match(e)
		if err != nil {
			return success, err
		}
		success = success || res
	}
	return success, nil
}

func (m *anyMatcher) FailureMessage(actual interface{}) (message string) {
	return m.matcher.FailureMessage(m.input[0])
}
func (m *anyMatcher) NegatedFailureMessage(actual interface{}) (message string) {
	return m.matcher.NegatedFailureMessage(m.input[0])
}

func castToSlice(slice interface{}) []interface{} {
	s := reflect.ValueOf(slice)
	if s.Kind() != reflect.Slice {
		panic("castToSlice() given a non-slice type")
	}

	ret := make([]interface{}, s.Len())

	for i := 0; i < s.Len(); i++ {
		ret[i] = s.Index(i).Interface()
	}

	return ret
}
